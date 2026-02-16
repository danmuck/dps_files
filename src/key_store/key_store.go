package key_store

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

type KeyStore struct {
	storageDir string
	lock       sync.RWMutex

	references map[[KeySize]byte]FileReference
	files      map[[HashSize]byte]*File
}

func InitKeyStore(storageDir string) (*KeyStore, error) {
	ks := &KeyStore{
		references: make(map[[KeySize]byte]FileReference),
		files:      make(map[[HashSize]byte]*File),
		storageDir: storageDir,
	}

	// create directories if they don't exist
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	// load metadata files
	metadataDir := filepath.Join(storageDir, "metadata")
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
				fmt.Printf("Warning: invalid metadata filename %s: %v\n", entry.Name(), err)
				continue
			}
			copy(fileHash[:], hashBytes)

			// load complete file struct
			var file File
			if _, err := toml.DecodeFile(filepath.Join(metadataDir, entry.Name()), &file); err != nil {
				fmt.Printf("Warning: failed to decode file %s: %v\n", entry.Name(), err)
				continue
			}

			// store file in memory
			ks.files[fileHash] = &file

			// also store file references
			for _, ref := range file.References {
				if ref != nil {
					ks.references[ref.Key] = *ref
				}
			}
		}
	}

	// verify chunks and handle orphaned metadata
	if err := ks.verifyFileReferences(); err != nil {
		fmt.Printf("Warning: error during chunk verification: %v\n", err)
	}

	return ks, nil
}

// store file to memory and write metadata toml to file system
// NOTE: this does not store data to disk, only metadata
func (ks *KeyStore) fileToMemory(file *File) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	// store in memory
	ks.files[file.MetaData.FileHash] = file

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
	fmt.Printf("Loaded file metadata from %s\n", file.ShortString())
	fmt.Printf("Number of references: %d\n", len(file.References))
	for i, ref := range file.References {
		if ref != nil && (i%PRINT_BLOCKS == 0 || i == len(file.References)-1) {
			fmt.Printf("Reference %d: Key=%x, DataHash=%x\n",
				i, ref.Key, ref.DataHash)
		}
	}
	// return a copy to prevent concurrent modification issues
	fileCopy := *file
	return &fileCopy, nil
}

// return copies in slice of all file references
func (ks *KeyStore) ListStoredFileReferences() []FileReference {
	ks.lock.RLock()
	defer ks.lock.RUnlock()

	blocks := make([]FileReference, 0, len(ks.references))
	for _, block := range ks.references {
		blocks = append(blocks, block) // copy by value
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

func (ks *KeyStore) Cleanup() error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	// clean up tracked chunk files
	for id, block := range ks.references {
		if err := os.Remove(block.Location); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete chunk %x: %w", id, err)
		}
	}

	// clean up any orphaned .kdht files on disk (e.g. from crashed mid-store)
	entries, err := os.ReadDir(ks.storageDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read storage directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".kdht" {
			if err := os.Remove(filepath.Join(ks.storageDir, entry.Name())); err != nil && !os.IsNotExist(err) {
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
	ks.references = make(map[[KeySize]byte]FileReference)
	ks.files = make(map[[HashSize]byte]*File) // changed to *file
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

	// clean up chunk files
	for id, block := range ks.references {
		if validExt[filepath.Ext(block.Location)] {
			if err := os.Remove(block.Location); err != nil {
				return fmt.Errorf("failed to delete chunk %x: %w", id, err)
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
	ks.references = make(map[[KeySize]byte]FileReference)
	ks.files = make(map[[HashSize]byte]*File)
	return nil
}

func (ks *KeyStore) moveToCache(sourcePath string) error {
	// create cache directory
	cacheDir := filepath.Join(ks.storageDir, ".cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// get filename and create destination path
	fileName := filepath.Base(sourcePath)
	destPath := filepath.Join(cacheDir, fileName)

	// move the file
	if err := os.Rename(sourcePath, destPath); err != nil {
		return fmt.Errorf("failed to move file to cache: %w", err)
	}

	fmt.Printf("Moved to cache: %s\n", fileName)
	return nil
}

func (ks *KeyStore) verifyFileReferences() error {
	fmt.Printf("Verifying file references ... \n")

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
				// Remove this reference from the in-memory map
				delete(ks.references, ref.Key)
			}
		}
	}

	// Move only the affected metadata files to cache
	if len(orphanedFileHashes) > 0 {
		metadataDir := filepath.Join(ks.storageDir, "metadata")
		for fileHash := range orphanedFileHashes {
			// Remove the file from in-memory map
			delete(ks.files, fileHash)

			// Move its metadata file to cache
			fileName := fmt.Sprintf("%x.toml", fileHash)
			sourcePath := filepath.Join(metadataDir, fileName)
			if err := ks.moveToCache(sourcePath); err != nil {
				fmt.Printf("Warning: %v\n", err)
			}
		}
	}

	return nil
}
