package key_store

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
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

func (ks *KeyStore) blockPath(id [KeySize]byte) string {
	return filepath.Join(ks.storageDir, fmt.Sprintf("%x%s", id, FileExtension))
}

// store by value, return error
func (ks *KeyStore) StoreFileReference(ref *FileReference, data []byte) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	if ref.FileIndex%PRINT_BLOCKS == 0 {
		fmt.Printf("Storing block %d: expected size=%d, actual size=%d\n",
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
	// ref.DataHash = sha256.Sum256(data)

	// create block file
	blockPath := ks.blockPath(ref.Key)
	if err := os.WriteFile(blockPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write block file: %w", err)
	}

	if VERIFY {
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

	// store the reference
	ks.references[ref.Key] = *ref
	// update the actual file
	// fmt.Println(ks.files[ref.Parent])
	// ks.files[ref.Parent].References[ref.FileIndex].Location = blockPath

	if ref.FileIndex%100 == 0 {
		fmt.Printf("Block %d stored with hash: %x\n", ref.FileIndex, ref.DataHash)
	}
	return nil
}

// return by value
func (ks *KeyStore) LoadFileReferenceData(key [KeySize]byte) ([]byte, error) {
	ks.lock.RLock()
	defer ks.lock.RUnlock()

	block, exists := ks.references[key]
	if !exists {
		return nil, fmt.Errorf("block not found for key %x", key)
	}

	data, err := os.ReadFile(block.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to read block file: %w", err)
	}

	// verify data integrity
	dataHash := sha256.Sum256(data)
	if dataHash != block.DataHash {
		return nil, fmt.Errorf("block data corruption detected:\nstored hash:  %x\ncomputed hash: %x",
			block.DataHash, dataHash)
	}

	return data, nil
}

// return by value
func (ks *KeyStore) GetFileReference(key [KeySize]byte) (FileReference, error) {
	ks.lock.RLock()
	defer ks.lock.RUnlock()

	block, exists := ks.references[key]
	if !exists {
		return FileReference{}, fmt.Errorf("block not found for key %x", key)
	}
	return block, nil
}

// delete reference by key
func (ks *KeyStore) DeleteFileReference(key [KeySize]byte) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	block, exists := ks.references[key]
	if !exists {
		return fmt.Errorf("block not found for key %x", key)
	}

	if err := os.Remove(block.Location); err != nil {
		return fmt.Errorf("failed to delete block file: %w", err)
	}

	delete(ks.references, key)
	return nil
}
