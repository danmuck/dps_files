package key_store

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	logs "github.com/danmuck/smplog"
)

// this is metadata for locally stored chunks of a file
type FileReference struct {
	Key       [KeySize]byte  `toml:"key"`
	FileName  string         `toml:"file_name"`
	Size      uint32         `toml:"chunk_size"`
	FileIndex uint32         `toml:"chunk_index"`
	Location  string         `toml:"location"`
	Protocol  string         `toml:"protocol"`
	DataHash  [HashSize]byte `toml:"data_hash"`
	Parent    [HashSize]byte `toml:"parent"`
	// MetaData  *MetaData      `toml:"metadata,omitempty"`
}

func (ks *KeyStore) chunkDataDir() string {
	return filepath.Join(ks.storageDir, "data")
}

func (ks *KeyStore) GetLocalBlockLocation(id [KeySize]byte) string {
	return filepath.Join(ks.chunkDataDir(), fmt.Sprintf("%x%s", id, FileExtension))
}

// store by value, return error
func (ks *KeyStore) StoreFileReference(ref *FileReference, data []byte) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	if ks.config.Verbose && ref.FileIndex%PRINT_BLOCKS == 0 {
		logs.Debugf("Storing block %d: expected size=%d, actual size=%d",
			ref.FileIndex, ref.Size, len(data))
	}
	if uint32(len(data)) != ref.Size {
		return fmt.Errorf("block %d data size (%d) doesn't match reference size (%d)",
			ref.FileIndex, len(data), ref.Size)
	}

	// calculate data hash
	tmpHash := sha256.Sum256(data)
	if tmpHash != ref.DataHash {
		return fmt.Errorf("block %d hash (%s) doesn't match data hash (%s)",
			ref.FileIndex, ref.DataHash[:], tmpHash[:])
	}

	// create block file
	blockPath := ks.GetLocalBlockLocation(ref.Key)
	if err := os.MkdirAll(filepath.Dir(blockPath), 0755); err != nil {
		return fmt.Errorf("failed to create block directory: %w", err)
	}
	if err := os.WriteFile(blockPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write block file: %w", err)
	}

	if ks.config.VerifyOnWrite {
		// verify the written data immediately
		writtenData, err := os.ReadFile(blockPath)
		if err != nil {
			return fmt.Errorf("failed to verify written block: %w", err)
		}
		// verify size
		if len(writtenData) != len(data) {
			return fmt.Errorf("written block size mismatch: got %d, expected %d",
				len(writtenData), len(data))
		}

		// verify hash of written data
		writtenHash := sha256.Sum256(writtenData)
		if writtenHash != ref.DataHash {
			return fmt.Errorf("block data verification failed after write:\nstored:  %x\nwritten: %x",
				ref.DataHash, writtenHash)
		}
	}

	ref.Location = blockPath
	ref.Protocol = "file"

	// store in chunk index
	ks.chunkIndex[ref.Key] = chunkLoc{
		FileHash:   ref.Parent,
		ChunkIndex: ref.FileIndex,
	}

	if ks.config.Verbose && ref.FileIndex%100 == 0 {
		logs.Debugf("Block %d stored with hash: %x", ref.FileIndex, ref.DataHash)
	}
	return nil
}

// resolveChunk looks up a chunk key in the index and returns the FileReference.
// Caller must hold at least ks.lock.RLock().
func (ks *KeyStore) resolveChunk(key [KeySize]byte) (*FileReference, error) {
	loc, exists := ks.chunkIndex[key]
	if !exists {
		return nil, fmt.Errorf("block not found for key %x", key)
	}
	file, exists := ks.files[loc.FileHash]
	if !exists {
		return nil, fmt.Errorf("parent file not found for key %x", key)
	}
	if int(loc.ChunkIndex) >= len(file.References) || file.References[loc.ChunkIndex] == nil {
		return nil, fmt.Errorf("chunk reference missing at index %d for key %x", loc.ChunkIndex, key)
	}
	return file.References[loc.ChunkIndex], nil
}

// return by value
func (ks *KeyStore) LoadFileReferenceData(key [KeySize]byte) ([]byte, error) {
	ks.lock.RLock()
	defer ks.lock.RUnlock()

	ref, err := ks.resolveChunk(key)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(ref.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to read block file: %w", err)
	}

	// verify data integrity
	dataHash := sha256.Sum256(data)
	if dataHash != ref.DataHash {
		return nil, fmt.Errorf("block data corruption detected:\nstored hash:  %x\ncomputed hash: %x",
			ref.DataHash, dataHash)
	}

	return data, nil
}

// return by value
func (ks *KeyStore) GetFileReference(key [KeySize]byte) (FileReference, error) {
	ks.lock.RLock()
	defer ks.lock.RUnlock()

	ref, err := ks.resolveChunk(key)
	if err != nil {
		return FileReference{}, err
	}
	return *ref, nil
}

// delete reference by key
func (ks *KeyStore) DeleteFileReference(key [KeySize]byte) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	loc, exists := ks.chunkIndex[key]
	if !exists {
		return fmt.Errorf("block not found for key %x", key)
	}

	// Default to deterministic key-based path. If parent metadata exists in memory,
	// prefer the stored location and clear the reference slot.
	blockPath := ks.GetLocalBlockLocation(key)
	if file, ok := ks.files[loc.FileHash]; ok {
		if int(loc.ChunkIndex) < len(file.References) && file.References[loc.ChunkIndex] != nil {
			if file.References[loc.ChunkIndex].Location != "" {
				blockPath = file.References[loc.ChunkIndex].Location
			}
			file.References[loc.ChunkIndex] = nil
		}
	}

	if err := os.Remove(blockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete block file: %w", err)
	}

	delete(ks.chunkIndex, key)
	return nil
}
