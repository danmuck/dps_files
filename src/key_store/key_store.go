package key_store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

type KeyStore struct {
	storageDir string
	config     KeyStoreConfig
	lock       sync.RWMutex

	chunkIndex  map[[KeySize]byte]chunkLoc
	files       map[[HashSize]byte]*File
	filesByName map[string][HashSize]byte // filename → file hash
}

var ErrFileHashCached = errors.New("file hash already present in cache")

// InitKeyStore creates a KeyStore with default config (verbose, no verify-on-write).
func InitKeyStore(storageDir string) (*KeyStore, error) {
	return InitKeyStoreWithConfig(DefaultConfig(storageDir))
}

// InitKeyStoreWithConfig creates a KeyStore with the given configuration.
func InitKeyStoreWithConfig(cfg KeyStoreConfig) (*KeyStore, error) {
	if cfg.DefaultTTLSeconds == 0 {
		cfg.DefaultTTLSeconds = DefaultFileTTLSeconds
	}

	ks := &KeyStore{
		chunkIndex:  make(map[[KeySize]byte]chunkLoc),
		files:       make(map[[HashSize]byte]*File),
		filesByName: make(map[string][HashSize]byte),
		storageDir:  cfg.StorageDir,
		config:      cfg,
	}

	// create directories if they don't exist
	if err := os.MkdirAll(cfg.StorageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	// load metadata files
	chunkDataDir := ks.chunkDataDir()
	if err := os.MkdirAll(chunkDataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create chunk data directory: %w", err)
	}

	metadataDir := filepath.Join(cfg.StorageDir, "metadata")
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create metadata directory: %w", err)
	}

	entries, err := os.ReadDir(metadataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".toml") {
			// extract hash from filename
			hashStr := strings.TrimSuffix(entry.Name(), ".toml")
			var fileHash [HashSize]byte
			hashBytes, err := hex.DecodeString(hashStr)
			if err != nil {
				if ks.config.Verbose {
					fmt.Printf("Warning: invalid metadata filename %s: %v\n", entry.Name(), err)
				}
				continue
			}
			copy(fileHash[:], hashBytes)

			// load complete file struct
			var file File
			if _, err := toml.DecodeFile(filepath.Join(metadataDir, entry.Name()), &file); err != nil {
				if ks.config.Verbose {
					fmt.Printf("Warning: failed to decode file %s: %v\n", entry.Name(), err)
				}
				continue
			}

			// store file in memory
			ks.files[fileHash] = &file
			ks.filesByName[file.MetaData.FileName] = fileHash

			// build chunk index
			for i, ref := range file.References {
				if ref != nil {
					// Normalize location to current storage layout so older metadata
					// written with previous paths remains readable.
					ref.Location = ks.GetLocalBlockLocation(ref.Key)
					ks.chunkIndex[ref.Key] = chunkLoc{
						FileHash:   fileHash,
						ChunkIndex: uint32(i),
					}
				}
			}
		}
	}

	return ks, nil
}

// ReloadLocalState rebuilds in-memory indexes from metadata files on disk.
// This is useful when external cleanup or filesystem operations occur after
// initialization (for example, deep-clean actions from CLI code paths).
func (ks *KeyStore) ReloadLocalState() error {
	fresh, err := InitKeyStoreWithConfig(ks.config)
	if err != nil {
		return fmt.Errorf("failed to reload local state: %w", err)
	}

	ks.lock.Lock()
	defer ks.lock.Unlock()

	ks.chunkIndex = fresh.chunkIndex
	ks.files = fresh.files
	ks.filesByName = fresh.filesByName

	return nil
}

// store file to memory and write metadata toml to file system
// NOTE: this does not store data to disk, only metadata
func (ks *KeyStore) fileToMemory(file *File) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	// store in memory
	ks.files[file.MetaData.FileHash] = file
	ks.filesByName[file.MetaData.FileName] = file.MetaData.FileHash

	// build chunk index entries
	for i, ref := range file.References {
		if ref != nil {
			ks.chunkIndex[ref.Key] = chunkLoc{
				FileHash:   file.MetaData.FileHash,
				ChunkIndex: uint32(i),
			}
		}
	}

	// create metadata directory if it doesn't exist
	metadataDir := filepath.Join(ks.storageDir, "metadata")
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	// create metadata file path
	filename := fmt.Sprintf("%x.toml", file.MetaData.FileHash)
	metadataPath := filepath.Join(metadataDir, filename)

	// open file for writing
	f, err := os.Create(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to create metadata file: %w", err)
	}
	defer f.Close()

	// create toml encoder
	encoder := toml.NewEncoder(f)
	encoder.Indent = "    "

	// encode the complete file structure
	if err := encoder.Encode(file); err != nil {
		return fmt.Errorf("failed to encode file: %w", err)
	}

	if err := ks.upsertCacheEntry(file); err != nil {
		return fmt.Errorf("failed to update cache entry: %w", err)
	}

	return nil
}

// return a copy of file from memory
// NOTE: this does not return data
func (ks *KeyStore) fileFromMemory(key [HashSize]byte) (*File, error) {
	ks.lock.RLock()
	defer ks.lock.RUnlock()

	file, exists := ks.files[key]
	if !exists {
		return nil, fmt.Errorf("file not found for hash %x", key)
	}

	if ks.isExpired(file) {
		return nil, fmt.Errorf("file expired: %s (TTL=%ds)", file.MetaData.FileName, file.MetaData.TTL)
	}

	if ks.config.Verbose {
		fmt.Printf("Loaded file metadata from %s\n", file.ShortString())
		fmt.Printf("Number of references: %d\n", len(file.References))
		for i, ref := range file.References {
			if ref != nil && (i%PRINT_BLOCKS == 0 || i == len(file.References)-1) {
				fmt.Printf("Reference %d: Key=%x, DataHash=%x\n",
					i, ref.Key, ref.DataHash)
			}
		}
	}
	// return a copy to prevent concurrent modification issues
	fileCopy := *file
	return &fileCopy, nil
}

// isExpired returns true if the file's TTL has elapsed since its Modified time.
// TTL=0 means no expiry.
func (ks *KeyStore) isExpired(file *File) bool {
	if file.MetaData.TTL == 0 {
		return false
	}
	modifiedSec := file.MetaData.Modified / 1e9 // nanoseconds → seconds
	return time.Now().Unix() > modifiedSec+int64(file.MetaData.TTL)
}

// return copies in slice of all file references
func (ks *KeyStore) ListStoredFileReferences() []FileReference {
	ks.lock.RLock()
	defer ks.lock.RUnlock()

	blocks := make([]FileReference, 0, len(ks.chunkIndex))
	for _, loc := range ks.chunkIndex {
		file, exists := ks.files[loc.FileHash]
		if !exists {
			continue
		}
		if int(loc.ChunkIndex) < len(file.References) && file.References[loc.ChunkIndex] != nil {
			blocks = append(blocks, *file.References[loc.ChunkIndex])
		}
	}
	return blocks
}

// return copies in slice
func (ks *KeyStore) ListKnownFiles() []MetaData {
	ks.lock.RLock()
	defer ks.lock.RUnlock()

	entries := make([]MetaData, 0, len(ks.files))
	for _, file := range ks.files {
		entries = append(entries, file.MetaData) // copy the metadata from the file struct
	}
	return entries
}

func (ks *KeyStore) existingFileByHash(key [HashSize]byte) (*File, bool) {
	ks.lock.RLock()
	file, exists := ks.files[key]
	if !exists {
		ks.lock.RUnlock()
		return nil, false
	}

	isStale := ks.fileHasMissingLocalReferences(file)
	ks.lock.RUnlock()
	if isStale {
		ks.dropFileFromMemory(key)
		return nil, false
	}

	fileCopy := *file
	return &fileCopy, true
}

func (ks *KeyStore) fileHasMissingLocalReferences(file *File) bool {
	for _, ref := range file.References {
		if ref == nil {
			return true
		}
		if ks.isLocalReference(ref) && !ks.localReferenceExists(ref) {
			return true
		}
	}
	return false
}

func (ks *KeyStore) dropFileFromMemory(key [HashSize]byte) {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	file, exists := ks.files[key]
	if !exists {
		return
	}

	for _, ref := range file.References {
		if ref == nil {
			continue
		}
		delete(ks.chunkIndex, ref.Key)
	}

	delete(ks.filesByName, file.MetaData.FileName)
	delete(ks.files, key)
}

// GetFileByHash returns a file by its SHA-256 hash.
func (ks *KeyStore) GetFileByHash(key [HashSize]byte) (*File, error) {
	return ks.fileFromMemory(key)
}

// GetFileByName returns a file by its original filename.
func (ks *KeyStore) GetFileByName(name string) (*File, error) {
	ks.lock.RLock()
	hash, exists := ks.filesByName[name]
	ks.lock.RUnlock()
	if !exists {
		return nil, fmt.Errorf("file not found: %s", name)
	}
	return ks.fileFromMemory(hash)
}

// StreamFile streams a file's chunks directly to w without buffering the
// entire file in memory. Each chunk is verified before writing.
// Memory usage is O(blockSize) regardless of file size.
func (ks *KeyStore) StreamFile(key [HashSize]byte, w io.Writer) error {
	file, err := ks.fileFromMemory(key)
	if err != nil {
		return fmt.Errorf("failed to get file metadata: %w", err)
	}

	hasher := sha256.New()
	var bytesWritten uint64

	for i, ref := range file.References {
		if ref == nil {
			return fmt.Errorf("missing block reference at index %d", i)
		}

		blockData, err := ks.LoadFileReferenceData(ref.Key)
		if err != nil {
			return fmt.Errorf("failed to read block %d: %w", i, err)
		}

		if uint32(len(blockData)) != ref.Size {
			return fmt.Errorf("block %d size mismatch: got %d, expected %d",
				i, len(blockData), ref.Size)
		}

		dataHash := sha256.Sum256(blockData)
		if dataHash != ref.DataHash {
			return fmt.Errorf("block %d data corruption detected", i)
		}

		n, err := w.Write(blockData)
		if err != nil {
			return fmt.Errorf("failed to write block %d: %w", i, err)
		}
		hasher.Write(blockData)
		bytesWritten += uint64(n)
	}

	if bytesWritten != file.MetaData.TotalSize {
		return fmt.Errorf("size mismatch: wrote %d bytes, expected %d",
			bytesWritten, file.MetaData.TotalSize)
	}

	var finalHash [HashSize]byte
	copy(finalHash[:], hasher.Sum(nil))
	if finalHash != file.MetaData.FileHash {
		return fmt.Errorf("streamed file hash mismatch")
	}

	return nil
}

// StreamFileByName streams a file by its original filename.
func (ks *KeyStore) StreamFileByName(name string, w io.Writer) error {
	ks.lock.RLock()
	hash, exists := ks.filesByName[name]
	ks.lock.RUnlock()
	if !exists {
		return fmt.Errorf("file not found: %s", name)
	}
	return ks.StreamFile(hash, w)
}

// StreamChunkRange streams chunks [start, end) from a file to w.
// Useful for resumable transfers and HTTP Range requests.
// start is inclusive, end is exclusive. end=0 means stream to the last chunk.
func (ks *KeyStore) StreamChunkRange(key [HashSize]byte, start, end uint32, w io.Writer) (uint64, error) {
	file, err := ks.fileFromMemory(key)
	if err != nil {
		return 0, fmt.Errorf("failed to get file metadata: %w", err)
	}

	totalChunks := uint32(len(file.References))
	if end == 0 || end > totalChunks {
		end = totalChunks
	}
	if start >= end {
		return 0, fmt.Errorf("invalid chunk range: [%d, %d)", start, end)
	}

	var bytesWritten uint64
	for i := start; i < end; i++ {
		ref := file.References[i]
		if ref == nil {
			return bytesWritten, fmt.Errorf("missing block reference at index %d", i)
		}

		blockData, err := ks.LoadFileReferenceData(ref.Key)
		if err != nil {
			return bytesWritten, fmt.Errorf("failed to read block %d: %w", i, err)
		}

		if uint32(len(blockData)) != ref.Size {
			return bytesWritten, fmt.Errorf("block %d size mismatch: got %d, expected %d",
				i, len(blockData), ref.Size)
		}

		dataHash := sha256.Sum256(blockData)
		if dataHash != ref.DataHash {
			return bytesWritten, fmt.Errorf("block %d data corruption detected", i)
		}

		n, err := w.Write(blockData)
		if err != nil {
			return bytesWritten, fmt.Errorf("failed to write block %d: %w", i, err)
		}
		bytesWritten += uint64(n)
	}

	return bytesWritten, nil
}

func (ks *KeyStore) Cleanup() error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	// clean up tracked chunk files via chunkIndex → file → reference → location
	for key, loc := range ks.chunkIndex {
		file, exists := ks.files[loc.FileHash]
		if !exists {
			continue
		}
		if int(loc.ChunkIndex) < len(file.References) && file.References[loc.ChunkIndex] != nil {
			loc := file.References[loc.ChunkIndex].Location
			if err := os.Remove(loc); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to delete chunk %x: %w", key, err)
			}
		}
	}

	// clean up any orphaned .kdht files on disk (e.g. from crashed mid-store)
	chunkDataDir := ks.chunkDataDir()
	entries, err := os.ReadDir(chunkDataDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read chunk data directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".kdht" {
			if err := os.Remove(filepath.Join(chunkDataDir, entry.Name())); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to delete orphaned chunk %s: %w", entry.Name(), err)
			}
		}
	}

	// clean up metadata files
	metadataDir := filepath.Join(ks.storageDir, "metadata")
	if err := os.RemoveAll(metadataDir); err != nil {
		return fmt.Errorf("failed to delete metadata directory: %w", err)
	}

	// reset the maps
	ks.chunkIndex = make(map[[KeySize]byte]chunkLoc)
	ks.files = make(map[[HashSize]byte]*File)
	ks.filesByName = make(map[string][HashSize]byte)
	return nil
}

func (ks *KeyStore) CleanupKDHT() error {
	err := ks.CleanupExtensions(".kdht")
	return err
}

func (ks *KeyStore) CleanupMetaData() error {
	err := ks.CleanupExtensions(".toml")
	return err
}

func (ks *KeyStore) CleanupExtensions(extensions ...string) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	// create a map for quick extension lookup
	validExt := make(map[string]bool)
	for _, ext := range extensions {
		validExt[ext] = true
	}

	// clean up chunk files via chunkIndex
	for key, loc := range ks.chunkIndex {
		file, exists := ks.files[loc.FileHash]
		if !exists {
			continue
		}
		if int(loc.ChunkIndex) < len(file.References) && file.References[loc.ChunkIndex] != nil {
			refLoc := file.References[loc.ChunkIndex].Location
			if validExt[filepath.Ext(refLoc)] {
				if err := os.Remove(refLoc); err != nil {
					return fmt.Errorf("failed to delete chunk %x: %w", key, err)
				}
			}
		}
	}

	// clean up metadata directory
	metadataDir := filepath.Join(ks.storageDir, "metadata")
	if entries, err := os.ReadDir(metadataDir); err == nil {
		for _, entry := range entries {
			if validExt[filepath.Ext(entry.Name())] {
				fullPath := filepath.Join(metadataDir, entry.Name())
				if err := os.Remove(fullPath); err != nil {
					return fmt.Errorf("failed to delete metadata file %s: %w", entry.Name(), err)
				}
			}
		}
	}

	// reset the maps
	ks.chunkIndex = make(map[[KeySize]byte]chunkLoc)
	ks.files = make(map[[HashSize]byte]*File)
	ks.filesByName = make(map[string][HashSize]byte)
	return nil
}

func (ks *KeyStore) cacheDir() string {
	return filepath.Join(ks.storageDir, ".cache")
}

func (ks *KeyStore) cachePathForHash(fileHash [HashSize]byte) string {
	return filepath.Join(ks.cacheDir(), fmt.Sprintf("%x.toml", fileHash))
}

func (ks *KeyStore) upsertCacheEntry(file *File) error {
	cacheDir := ks.cacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	cachePath := ks.cachePathForHash(file.MetaData.FileHash)
	f, err := os.Create(cachePath)
	if err != nil {
		return fmt.Errorf("failed to create cache file: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	encoder.Indent = "    "
	if err := encoder.Encode(file); err != nil {
		return fmt.Errorf("failed to encode cache file: %w", err)
	}

	return nil
}

func isCacheVariant(stem, candidateName string) bool {
	if !strings.HasSuffix(candidateName, ".toml") {
		return false
	}
	candidateStem := strings.TrimSuffix(candidateName, ".toml")
	return candidateStem == stem || strings.HasPrefix(candidateStem, stem+" ")
}

func (ks *KeyStore) cacheEntryPathsForHash(fileHash [HashSize]byte) ([]string, error) {
	stem := fmt.Sprintf("%x", fileHash)
	cacheDir := ks.cacheDir()

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache directory: %w", err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if isCacheVariant(stem, entry.Name()) {
			paths = append(paths, filepath.Join(cacheDir, entry.Name()))
		}
	}

	return paths, nil
}

func (ks *KeyStore) pruneCachePath(cachePath string) error {
	if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to prune cache entry %s: %w", cachePath, err)
	}
	return nil
}

func (ks *KeyStore) isLocalReference(ref *FileReference) bool {
	if ref == nil {
		return false
	}

	if ref.Protocol == "" || ref.Protocol == "file" {
		return true
	}

	if ref.Location == "" {
		return false
	}

	cleanLocation := filepath.Clean(ref.Location)
	cleanStorage := filepath.Clean(ks.storageDir)
	rel, err := filepath.Rel(cleanStorage, cleanLocation)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}

	return strings.HasPrefix(filepath.ToSlash(cleanLocation), "local/storage/")
}

func (ks *KeyStore) localReferenceExists(ref *FileReference) bool {
	if ref == nil {
		return false
	}

	paths := []string{}
	if ref.Location != "" {
		paths = append(paths, ref.Location)
	}

	keyPath := ks.GetLocalBlockLocation(ref.Key)
	if ref.Location != keyPath {
		paths = append(paths, keyPath)
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	return false
}

func (ks *KeyStore) cacheEntryIsLive(cachePath string) (bool, error) {
	var file File
	if _, err := toml.DecodeFile(cachePath, &file); err != nil {
		return false, fmt.Errorf("failed to decode cache entry %s: %w", cachePath, err)
	}

	for _, ref := range file.References {
		if ref == nil {
			return false, nil
		}
		if ks.isLocalReference(ref) && !ks.localReferenceExists(ref) {
			return false, nil
		}
	}

	return true, nil
}

func (ks *KeyStore) pruneDeadCacheEntries() error {
	cacheDir := ks.cacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}

		cachePath := filepath.Join(cacheDir, entry.Name())
		live, err := ks.cacheEntryIsLive(cachePath)
		if err != nil {
			if ks.config.Verbose {
				fmt.Printf("Warning: %v\n", err)
			}
			if err := ks.pruneCachePath(cachePath); err != nil {
				return err
			}
			continue
		}

		if !live {
			if err := ks.pruneCachePath(cachePath); err != nil {
				return err
			}
		}
	}

	return nil
}

func (ks *KeyStore) hasCacheEntryForHash(fileHash [HashSize]byte) (bool, error) {
	cachePaths, err := ks.cacheEntryPathsForHash(fileHash)
	if err != nil {
		return false, err
	}

	cached := false
	for _, cachePath := range cachePaths {
		live, err := ks.cacheEntryIsLive(cachePath)
		if err != nil {
			if ks.config.Verbose {
				fmt.Printf("Warning: %v\n", err)
			}
			if err := ks.pruneCachePath(cachePath); err != nil {
				return false, err
			}
			continue
		}

		if !live {
			if err := ks.pruneCachePath(cachePath); err != nil {
				return false, err
			}
			continue
		}

		cached = true
	}

	return cached, nil
}

func (ks *KeyStore) ensureHashNotCached(fileHash [HashSize]byte, fileName string) error {
	cached, err := ks.hasCacheEntryForHash(fileHash)
	if err != nil {
		return err
	}
	if cached {
		return fmt.Errorf("%w: %q (%x)", ErrFileHashCached, fileName, fileHash)
	}
	return nil
}

func (ks *KeyStore) moveToCache(sourcePath string) error {
	// create cache directory
	cacheDir := ks.cacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// get filename and create destination path
	fileName := filepath.Base(sourcePath)
	destPath := filepath.Join(cacheDir, fileName)
	stem := strings.TrimSuffix(fileName, ".toml")

	// If the same metadata (or a legacy duplicate variant) already exists in cache,
	// discard the source metadata file instead of creating another cache entry.
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if isCacheVariant(stem, entry.Name()) {
			if err := os.Remove(sourcePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove duplicate metadata source: %w", err)
			}
			if ks.config.Verbose {
				fmt.Printf("Cache already contains metadata for %s; discarded duplicate source\n", stem)
			}
			return nil
		}
	}

	// move the file
	if err := os.Rename(sourcePath, destPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to move file to cache: %w", err)
	}

	if ks.config.Verbose {
		fmt.Printf("Moved to cache: %s\n", fileName)
	}
	return nil
}

func (ks *KeyStore) verifyFileReferences() error {
	if ks.config.Verbose {
		fmt.Printf("Verifying file references ... \n")
	}

	// Track which files have missing chunks by their hash
	orphanedFileHashes := make(map[[HashSize]byte]bool)

	// Check each file's references for missing chunk data on disk
	for fileHash, file := range ks.files {
		for _, ref := range file.References {
			if ref == nil {
				continue
			}
			blockPath := ks.GetLocalBlockLocation(ref.Key)
			if _, err := os.Stat(blockPath); os.IsNotExist(err) {
				orphanedFileHashes[fileHash] = true
				// Remove this reference from the chunk index
				delete(ks.chunkIndex, ref.Key)
			}
		}
	}

	// Move only the affected metadata files to cache
	if len(orphanedFileHashes) > 0 {
		metadataDir := filepath.Join(ks.storageDir, "metadata")
		for fileHash := range orphanedFileHashes {
			// Remove from name index
			if file, ok := ks.files[fileHash]; ok {
				delete(ks.filesByName, file.MetaData.FileName)
			}
			// Remove the file from in-memory map
			delete(ks.files, fileHash)

			// Move its metadata file to cache
			fileName := fmt.Sprintf("%x.toml", fileHash)
			sourcePath := filepath.Join(metadataDir, fileName)
			if err := ks.moveToCache(sourcePath); err != nil {
				if ks.config.Verbose {
					fmt.Printf("Warning: %v\n", err)
				}
			}
		}
	}

	return nil
}

// DeleteFile removes a file and all its chunks from storage and memory.
func (ks *KeyStore) DeleteFile(key [HashSize]byte) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	file, exists := ks.files[key]
	if !exists {
		return fmt.Errorf("file not found for hash %x", key)
	}

	// delete chunk files and index entries
	for _, ref := range file.References {
		if ref == nil {
			continue
		}
		if ref.Location != "" {
			if err := os.Remove(ref.Location); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to delete chunk %x: %w", ref.Key, err)
			}
		}
		delete(ks.chunkIndex, ref.Key)
	}

	// delete metadata file
	metadataDir := filepath.Join(ks.storageDir, "metadata")
	metadataPath := filepath.Join(metadataDir, fmt.Sprintf("%x.toml", key))
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete metadata file: %w", err)
	}

	// remove from memory
	delete(ks.filesByName, file.MetaData.FileName)
	delete(ks.files, key)

	return nil
}

// CleanupExpired removes all expired files and returns the count of files removed.
func (ks *KeyStore) CleanupExpired() int {
	ks.lock.RLock()
	var expiredKeys [][HashSize]byte
	for key, file := range ks.files {
		if ks.isExpired(file) {
			expiredKeys = append(expiredKeys, key)
		}
	}
	ks.lock.RUnlock()

	removed := 0
	for _, key := range expiredKeys {
		if err := ks.DeleteFile(key); err == nil {
			removed++
		}
	}
	return removed
}
