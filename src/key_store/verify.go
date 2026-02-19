package key_store

import (
	"crypto/sha256"
	"fmt"
	"os"
)

// ChunkError describes a single integrity problem found during verification.
type ChunkError struct {
	FileHash   [HashSize]byte
	FileName   string
	ChunkIndex uint32
	ChunkKey   [KeySize]byte
	Err        error
}

func (e ChunkError) Error() string {
	return fmt.Sprintf("chunk %d of %s (%x): %v", e.ChunkIndex, e.FileName, e.FileHash[:8], e.Err)
}

// VerifyAll performs a deep integrity scan of every stored file and chunk.
// It reads each chunk from disk, checks existence, size, and SHA-256 hash.
// Returns a list of problems found (empty means healthy).
// Does not modify any state.
func (ks *KeyStore) VerifyAll() []ChunkError {
	ks.lock.RLock()
	type fileSnap struct {
		hash [HashSize]byte
		file File
	}
	snaps := make([]fileSnap, 0, len(ks.files))
	for hash, f := range ks.files {
		snaps = append(snaps, fileSnap{hash: hash, file: cloneFileForVerify(f)})
	}
	ks.lock.RUnlock()

	var errs []ChunkError
	for _, snap := range snaps {
		errs = append(errs, ks.verifyFileChunks(snap.hash, &snap.file)...)
	}
	return errs
}

// VerifyFile performs integrity verification on a single file identified by its hash.
func (ks *KeyStore) VerifyFile(key [HashSize]byte) []ChunkError {
	ks.lock.RLock()
	f, exists := ks.files[key]
	if !exists {
		ks.lock.RUnlock()
		return []ChunkError{{
			FileHash: key,
			Err:      fmt.Errorf("file not found"),
		}}
	}
	fileCopy := cloneFileForVerify(f)
	ks.lock.RUnlock()

	return ks.verifyFileChunks(key, &fileCopy)
}

// verifyFileChunks checks each chunk reference of a file against disk.
func (ks *KeyStore) verifyFileChunks(fileHash [HashSize]byte, file *File) []ChunkError {
	var errs []ChunkError

	for i, ref := range file.References {
		if ref == nil {
			errs = append(errs, ChunkError{
				FileHash:   fileHash,
				FileName:   file.MetaData.FileName,
				ChunkIndex: uint32(i),
				Err:        fmt.Errorf("nil reference"),
			})
			continue
		}

		ce := ChunkError{
			FileHash:   fileHash,
			FileName:   file.MetaData.FileName,
			ChunkIndex: ref.FileIndex,
			ChunkKey:   ref.Key,
		}

		info, err := os.Stat(ref.Location)
		if err != nil {
			ce.Err = fmt.Errorf("missing file: %w", err)
			errs = append(errs, ce)
			continue
		}

		if uint32(info.Size()) != ref.Size {
			ce.Err = fmt.Errorf("size mismatch: got %d, expected %d", info.Size(), ref.Size)
			errs = append(errs, ce)
			continue
		}

		data, err := os.ReadFile(ref.Location)
		if err != nil {
			ce.Err = fmt.Errorf("read error: %w", err)
			errs = append(errs, ce)
			continue
		}

		hash := sha256.Sum256(data)
		if hash != ref.DataHash {
			ce.Err = fmt.Errorf("hash mismatch: got %x, expected %x", hash[:8], ref.DataHash[:8])
			errs = append(errs, ce)
		}
	}

	return errs
}

func cloneFileForVerify(file *File) File {
	cloned := File{
		MetaData:   file.MetaData,
		References: make([]*FileReference, len(file.References)),
	}

	for i, ref := range file.References {
		if ref == nil {
			continue
		}
		copyRef := *ref
		cloned.References[i] = &copyRef
	}

	return cloned
}
