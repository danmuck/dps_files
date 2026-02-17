package key_store

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type MetaData struct {
	FileHash  [HashSize]byte `toml:"file_hash"`
	TotalSize uint64         `toml:"total_size"`
	FileName  string         `toml:"file_name"`
	Modified  int64          `toml:"modified"`
	// MimeType    string           `toml:"mime_type"`
	Permissions uint32           `toml:"permissions"`
	Signature   [CryptoSize]byte `toml:"signature"`
	TTL         uint64           `toml:"ttl"`
	BlockSize   uint32           `toml:"chunk_size"`
	TotalBlocks uint32           `toml:"total_chunks"`
}

func PrepareMetaDataSecure(name string, data []byte, signature [CryptoSize]byte) (metadata MetaData, e error) {
	metadata.TotalSize = uint64(len(data))
	metadata.TTL = DefaultFileTTLSeconds
	metadata.FileName = name
	metadata.Modified = time.Now().UnixNano()
	metadata.Permissions = DEFAULT_PERMISSIONS
	metadata.Signature = signature
	metadata.BlockSize = CalculateBlockSize(metadata.TotalSize)
	if metadata.BlockSize > 0 {
		metadata.TotalBlocks = uint32((metadata.TotalSize + uint64(metadata.BlockSize) - 1) / uint64(metadata.BlockSize))
	}

	return metadata, nil
}

func PrepareMetaData(name string, data []byte) (metadata MetaData, e error) {
	metadata.TotalSize = uint64(len(data))
	metadata.TTL = DefaultFileTTLSeconds
	metadata.FileName = name
	metadata.Modified = time.Now().UnixNano()
	metadata.Permissions = DEFAULT_PERMISSIONS
	metadata.BlockSize = CalculateBlockSize(metadata.TotalSize)

	// calculate total chunks with proper rounding up
	if metadata.BlockSize > 0 {
		metadata.TotalBlocks = uint32((metadata.TotalSize + uint64(metadata.BlockSize) - 1) / uint64(metadata.BlockSize))
	}

	return metadata, nil
}

func (ks *KeyStore) UpdateLocalMetaData() error {
	ks.lock.RLock()
	defer ks.lock.RUnlock()

	metadataDir := filepath.Join(ks.storageDir, "metadata")
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	// save each file's complete data
	for hash, file := range ks.files {
		filename := fmt.Sprintf("%x.toml", hash)
		path := filepath.Join(metadataDir, filename)

		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("failed to create metadata file: %w", err)
		}

		encoder := toml.NewEncoder(f)
		encoder.Indent = "  "

		if err := encoder.Encode(file); err != nil {
			f.Close()
			return fmt.Errorf("failed to encode file data: %w", err)
		}
		f.Close()
	}

	return nil
}

func (ks *KeyStore) LoadLocalFileToMemory(key [HashSize]byte) (*File, error) {
	metadataDir := filepath.Join(ks.storageDir, "metadata")
	metadataPath := filepath.Join(metadataDir, fmt.Sprintf("%x.toml", key))

	var file File
	if _, err := toml.DecodeFile(metadataPath, &file); err != nil {
		return nil, fmt.Errorf("failed to decode metadata file: %w", err)
	}

	// Filter out references with no location (not stored locally)
	for i, ref := range file.References {
		if ref != nil && ref.Location == "" {
			file.References[i] = nil
		}
	}
	return &file, nil
}

func (ks *KeyStore) LoadAllLocalFilesToMemory() error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	metadataDir := filepath.Join(ks.storageDir, "metadata")

	// create directory if it doesn't exist
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	// read directory entries
	entries, err := os.ReadDir(metadataDir)
	if err != nil {
		return fmt.Errorf("failed to read metadata directory: %w", err)
	}

	// process each .toml file
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".toml") {
			// extract hash from filename
			hashStr := strings.TrimSuffix(entry.Name(), ".toml")
			var fileHash [HashSize]byte
			hashBytes, err := hex.DecodeString(hashStr)
			if err != nil {
				return fmt.Errorf("invalid metadata filename %s: %w", entry.Name(), err)
			}
			copy(fileHash[:], hashBytes)

			// load file metadata
			file, err := ks.LoadLocalFileToMemory(fileHash)
			if err != nil {
				return fmt.Errorf("failed to load metadata for %s: %w", entry.Name(), err)
			}

			// add to in-memory maps
			ks.files[fileHash] = file // store the complete file struct
			ks.filesByName[file.MetaData.FileName] = fileHash
			for i, ref := range file.References {
				if ref != nil && ref.Location != "" {
					ks.chunkIndex[ref.Key] = chunkLoc{
						FileHash:   fileHash,
						ChunkIndex: uint32(i),
					}
				}
			}
		}
	}

	return nil
}
