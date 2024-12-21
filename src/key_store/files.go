package key_store

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type File struct {
	MetaData   MetaData         `toml:"metadata"`
	References []*FileReference `toml:"references,omitempty"`
}

const (
	R_USER = 0400 // read permission for owner
	W_USER = 0200 // write permission for owner
	X_USER = 0100 // execute permission for owner

	R_GROUP = 0040 // read permission for group
	W_GROUP = 0020 // write permission for group
	X_GROUP = 0010 // execute permission for group

	R_OTHER = 0004 // read permission for others
	W_OTHER = 0002 // write permission for others
	X_OTHER = 0001 // execute permission for others
)

// this stores an entire file locally
func (ks *KeyStore) StoreFileLocal(name string, fileData []byte, permissions int) (*File, error) {
	// prepare metadata
	metadata, err := PrepareMetaData(name, fileData, permissions)
	if err != nil {
		return nil, err
	}

	// calculate and store file hash
	metadata.FileHash = sha256.Sum256(fileData)

	// create file object
	file := &File{
		MetaData:   metadata,
		References: make([]*FileReference, metadata.TotalBlocks),
	}

	// process file data into chunks
	var totalBytesProcessed uint64 = 0
	for i := uint32(0); i < metadata.TotalBlocks; i++ {
		// calculate chunk boundaries
		startIdx := uint64(i) * uint64(metadata.BlockSize)
		endIdx := startIdx + uint64(metadata.BlockSize)
		if endIdx > metadata.TotalSize {
			endIdx = metadata.TotalSize
		}

		blockData := fileData[startIdx:endIdx]
		blockSize := uint32(len(blockData))

		// create filereference for this block
		block := FileReference{
			FileName:  metadata.FileName,
			Parent:    metadata.FileHash,
			Size:      blockSize,
			FileIndex: i,
			Protocol:  "file",
			DataHash:  sha256.Sum256(blockData),
		}

		// calculate block dht routing id
		blockIDtmp := append(metadata.FileHash[:], byte(i))
		blockID := sha1.Sum(blockIDtmp)
		block.Key = blockID

		// store the block
		if err := ks.StoreFileReference(&block, blockData); err != nil {
			// cleanup any chunks we've already stored
			for j := uint32(0); j < i; j++ {
				if file.References[j] != nil {
					ks.DeleteFileReference(file.References[j].Key)
				}
			}
			return nil, fmt.Errorf("failed to store block %d: %w", i, err)
		}

		// store reference in file
		blockRef := block // make a copy
		file.References[i] = &blockRef

		// debug print after storing each chunk reference
		fmt.Printf("Added block reference %d: Key=%x, Size=%d\n", i, blockRef.Key, blockRef.Size)

		totalBytesProcessed += uint64(blockSize)

		// debug output for progress
		if i%PRINT_BLOCKS == 0 || i == metadata.TotalBlocks-1 {
			fmt.Printf("Stored block %d/%d (%.1f%%)\n",
				i+1, metadata.TotalBlocks, float64(i+1)/float64(metadata.TotalBlocks)*100)
		}
	}

	// verify total bytes processed
	if totalBytesProcessed != metadata.TotalSize {
		// cleanup all chunks on size mismatch
		for _, ref := range file.References {
			if ref != nil {
				ks.DeleteFileReference(ref.Key)
			}
		}
		return nil, fmt.Errorf("processed bytes (%d) doesn't match file size (%d)",
			totalBytesProcessed, metadata.TotalSize)
	}

	fmt.Printf("\nValidating file before storage:\n")
	fmt.Printf("File name: %s\n", file.MetaData.FileName)
	fmt.Printf("Total blocks: %d\n", file.MetaData.TotalBlocks)
	fmt.Printf("References count: %d\n", len(file.References))

	for i, ref := range file.References {
		if ref == nil {
			fmt.Printf("Warning: Reference %d is nil\n", i)
		} else if i%PRINT_BLOCKS == 0 || i == len(file.References)-1 {
			fmt.Printf("Reference %d: Key=%x, Size=%d\n", i, ref.Key, ref.Size)
		}
	}
	// store the complete file with metadata and references
	if err := ks.fileToMemory(file); err != nil {
		// cleanup all chunks on failure
		for _, ref := range file.References {
			if ref != nil {
				ks.DeleteFileReference(ref.Key)
			}
		}
		return nil, fmt.Errorf("failed to store file metadata: %w", err)
	}

	return file, nil
}

func (ks *KeyStore) StoreFileRemote(name string, fileData []byte, permissions int) (*File, error) {
	// prepare metadata
	metadata, err := PrepareMetaData(name, fileData, permissions)
	if err != nil {
		return nil, err
	}

	// calculate and store file hash
	metadata.FileHash = sha256.Sum256(fileData)

	// create file object
	file := &File{
		MetaData:   metadata,
		References: make([]*FileReference, metadata.TotalBlocks),
	}

	// process file data into chunks
	var totalBytesProcessed uint64 = 0
	for i := uint32(0); i < metadata.TotalBlocks; i++ {
		// calculate chunk boundaries
		startIdx := uint64(i) * uint64(metadata.BlockSize)
		endIdx := startIdx + uint64(metadata.BlockSize)
		if endIdx > metadata.TotalSize {
			endIdx = metadata.TotalSize
		}

		blockData := fileData[startIdx:endIdx]
		blockSize := uint32(len(blockData))

		// create filereference for this block
		block := FileReference{
			FileName:  metadata.FileName,
			Parent:    metadata.FileHash,
			Size:      blockSize,
			FileIndex: i,
			Protocol:  "file",
			DataHash:  sha256.Sum256(blockData),
		}

		// calculate block dht routing id
		blockIDtmp := append(metadata.FileHash[:], byte(i))
		blockID := sha1.Sum(blockIDtmp)
		block.Key = blockID

		// store the block
		if err := ks.StoreFileReference(&block, blockData); err != nil {
			// cleanup any chunks we've already stored
			for j := uint32(0); j < i; j++ {
				if file.References[j] != nil {
					ks.DeleteFileReference(file.References[j].Key)
				}
			}
			return nil, fmt.Errorf("failed to store block %d: %w", i, err)
		}

		// store reference in file
		blockRef := block // make a copy
		file.References[i] = &blockRef

		// debug print after storing each chunk reference
		fmt.Printf("Added block reference %d: Key=%x, Size=%d\n", i, blockRef.Key, blockRef.Size)

		totalBytesProcessed += uint64(blockSize)

		// debug output for progress
		if i%PRINT_BLOCKS == 0 || i == metadata.TotalBlocks-1 {
			fmt.Printf("Stored block %d/%d (%.1f%%)\n",
				i+1, metadata.TotalBlocks, float64(i+1)/float64(metadata.TotalBlocks)*100)
		}
	}

	// verify total bytes processed
	if totalBytesProcessed != metadata.TotalSize {
		// cleanup all chunks on size mismatch
		for _, ref := range file.References {
			if ref != nil {
				ks.DeleteFileReference(ref.Key)
			}
		}
		return nil, fmt.Errorf("processed bytes (%d) doesn't match file size (%d)",
			totalBytesProcessed, metadata.TotalSize)
	}

	fmt.Printf("\nValidating file before storage:\n")
	fmt.Printf("File name: %s\n", file.MetaData.FileName)
	fmt.Printf("Total blocks: %d\n", file.MetaData.TotalBlocks)
	fmt.Printf("References count: %d\n", len(file.References))

	for i, ref := range file.References {
		if ref == nil {
			fmt.Printf("Warning: Reference %d is nil\n", i)
		} else if i%PRINT_BLOCKS == 0 || i == len(file.References)-1 {
			fmt.Printf("Reference %d: Key=%x, Size=%d\n", i, ref.Key, ref.Size)
		}
	}
	// store the complete file with metadata and references
	if err := ks.fileToMemory(file); err != nil {
		// cleanup all chunks on failure
		for _, ref := range file.References {
			if ref != nil {
				ks.DeleteFileReference(ref.Key)
			}
		}
		return nil, fmt.Errorf("failed to store file metadata: %w", err)
	}

	return file, nil
}

func (ks *KeyStore) ReassembleFileToBytes(key [HashSize]byte) ([]byte, error) {
	// get the complete file record
	file, err := ks.fileFromMemory(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}

	// pre-allocate the complete file buffer
	fileData := make([]byte, file.MetaData.TotalSize)
	var bytesWritten uint64 = 0

	// read and verify each chunk using stored references
	for i, ref := range file.References {
		if ref == nil {
			return nil, fmt.Errorf("missing block reference at index %d", i)
		}

		// get block data
		blockData, err := ks.LoadFileReferenceData(ref.Key)
		if err != nil {
			return nil, fmt.Errorf("failed to read block %d: %w", i, err)
		}

		// verify chunk size
		if uint32(len(blockData)) != ref.Size {
			return nil, fmt.Errorf("block %d size mismatch: got %d, expected %d",
				i, len(blockData), ref.Size)
		}

		// verify chunk integrity
		dataHash := sha256.Sum256(blockData)
		if dataHash != ref.DataHash {
			return nil, fmt.Errorf("block %d data corruption detected", i)
		}

		// copy chunk data to correct position
		startIdx := uint64(i) * uint64(file.MetaData.BlockSize)
		copy(fileData[startIdx:], blockData)
		bytesWritten += uint64(len(blockData))

		// progress reporting
		if i%100 == 0 || i == int(file.MetaData.TotalBlocks-1) {
			fmt.Printf("Reassembled block %d/%d (%.1f%%)\n",
				i+1, file.MetaData.TotalBlocks,
				float64(i+1)/float64(file.MetaData.TotalBlocks)*100)
		}
	}

	// verify total bytes reassembled
	if bytesWritten != file.MetaData.TotalSize {
		return nil, fmt.Errorf("size mismatch: wrote %d bytes, expected %d",
			bytesWritten, file.MetaData.TotalSize)
	}

	// verify final file integrity
	fileHash := sha256.Sum256(fileData)
	if fileHash != file.MetaData.FileHash {
		return nil, fmt.Errorf("reassembled file hash mismatch")
	}

	return fileData, nil
}

func (ks *KeyStore) ReassembleFileToPath(key [HashSize]byte, outputPath string) error {
	// get the complete file record
	file, err := ks.fileFromMemory(key)
	if err != nil {
		return fmt.Errorf("failed to get file: %w", err)
	}

	// create output file
	f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	var bytesWritten uint64 = 0
	hasher := sha256.New()

	// process blocks using stored references
	for i, ref := range file.References {
		if ref == nil {
			return fmt.Errorf("missing block reference at index %d", i)
		}

		// get block data
		blockData, err := ks.LoadFileReferenceData(ref.Key)
		if err != nil {
			return fmt.Errorf("failed to read block %d: %w", i, err)
		}

		// verify chunk size
		expectedSize := ref.Size
		if uint32(len(blockData)) != expectedSize {
			return fmt.Errorf("block %d size mismatch: got %d, expected %d",
				i, len(blockData), expectedSize)
		}

		// verify chunk integrity
		dataHash := sha256.Sum256(blockData)
		if dataHash != ref.DataHash {
			return fmt.Errorf("block %d data corruption detected: stored hash %x, computed hash %x",
				i, ref.DataHash, dataHash)
		}

		// write chunk to file
		n, err := f.Write(blockData)
		if err != nil {
			return fmt.Errorf("failed to write block %d: %w", i, err)
		}

		// update hash
		hasher.Write(blockData)

		bytesWritten += uint64(n)

		// progress reporting
		if i%100 == 0 || i == int(file.MetaData.TotalBlocks-1) {
			fmt.Printf("Wrote block %d/%d (%.1f%%) - size=%d bytes\n",
				i+1, file.MetaData.TotalBlocks,
				float64(i+1)/float64(file.MetaData.TotalBlocks)*100,
				n)
		}
	}

	// verify total size
	if bytesWritten != file.MetaData.TotalSize {
		return fmt.Errorf("size mismatch: wrote %d bytes, expected %d",
			bytesWritten, file.MetaData.TotalSize)
	}

	// flush the file to ensure all data is written
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to flush file: %w", err)
	}

	// close the file before verifying
	f.Close()

	// verify final file integrity
	reassembledHash, length, err := HashFile(outputPath)
	if err != nil {
		return fmt.Errorf("failed to hash reassembled file: %w", err)
	}

	if length != int64(file.MetaData.TotalSize) {
		return fmt.Errorf("final size mismatch: got %d, expected %d",
			length, file.MetaData.TotalSize)
	}

	if reassembledHash != file.MetaData.FileHash {
		return fmt.Errorf("final hash mismatch:\n  got:      %x\n  expected: %x",
			reassembledHash, file.MetaData.FileHash)
	}

	return nil
}

func (ks *KeyStore) LoadAndStoreFile(localFilePath string) (*File, error) {
	// open the file
	f, err := os.Open(localFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open local file: %w", err)
	}
	defer f.Close()

	// get file info for size
	fileInfo, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	// calculate file hash using streaming
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return nil, fmt.Errorf("failed to calculate file hash: %w", err)
	}

	// reset file pointer
	if _, err := f.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("failed to reset file position: %w", err)
	}

	// prepare metadata
	fileName := filepath.Base(localFilePath)
	metadata := MetaData{
		FileName:    fileName,
		TotalSize:   uint64(fileInfo.Size()),
		Modified:    time.Now().UnixNano(),
		Permissions: uint32(fileInfo.Mode().Perm()),
		BlockSize:   CalculateBlockSize(uint64(fileInfo.Size())),
	}
	var fileHash [HashSize]byte
	copy(fileHash[:], hash.Sum(nil))
	metadata.FileHash = fileHash
	// copy(metadata.filehash[:], hash.sum(nil))
	metadata.TotalBlocks = uint32((metadata.TotalSize + uint64(metadata.BlockSize) - 1) / uint64(metadata.BlockSize))

	// create file object
	file := &File{
		MetaData:   metadata,
		References: make([]*FileReference, metadata.TotalBlocks),
	}

	fmt.Printf("Starting chunking process:\n")
	fmt.Printf("Total size: %d bytes\n", metadata.TotalSize)
	fmt.Printf("Block size: %d bytes\n", metadata.BlockSize)
	fmt.Printf("Expected blocks: %d\n", metadata.TotalBlocks)

	// process file in chunks
	buffer := make([]byte, metadata.BlockSize)
	var totalBytesRead uint64 = 0

	for i := uint32(0); i < metadata.TotalBlocks; i++ {
		// calculate expected block size
		var bytesToRead uint32 = metadata.BlockSize
		if i == metadata.TotalBlocks-1 {
			// for the last block, calculate remaining bytes
			remainingBytes := metadata.TotalSize - totalBytesRead
			bytesToRead = uint32(remainingBytes)
			fmt.Printf("Last block %d: Reading remaining %d bytes\n", i, bytesToRead)
		}

		// read block
		n, err := io.ReadFull(f, buffer[:bytesToRead])
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("failed to read block %d: %w", i, err)
		}

		if n == 0 {
			return nil, fmt.Errorf("unexpected end of file at block %d", i)
		}

		if i%100 == 0 || i == metadata.TotalBlocks-1 {
			fmt.Printf("Block %d: Read %d bytes (total: %d/%d)\n",
				i, n, totalBytesRead+uint64(n), metadata.TotalSize)
		}
		blockData := buffer[:n]

		// create filereference for this block
		block := FileReference{
			FileName:  metadata.FileName,
			Parent:    metadata.FileHash,
			Size:      uint32(n),
			FileIndex: i,
			Protocol:  "file",
			DataHash:  sha256.Sum256(blockData),
		}

		// calculate chunk's dht routing id
		blockIDtmp := append(metadata.FileHash[:], make([]byte, 8)...)
		binary.LittleEndian.PutUint64(blockIDtmp[len(metadata.FileHash):], uint64(i))
		blockID := sha1.Sum(blockIDtmp)
		block.Key = blockID

		// store the block
		if err := ks.StoreFileReference(&block, blockData); err != nil {
			// cleanup on failure
			for j := uint32(0); j < i; j++ {
				if file.References[j] != nil {
					ks.DeleteFileReference(file.References[j].Key)
				}
			}
			return nil, fmt.Errorf("failed to store block %d: %w", i, err)
		}

		// block doesnt have a local address block.Location at this point
		// it is assigned one inside StoreFileReference but that value is not
		// reflected to this point
		// store reference in file
		blockRef := block // make a copy
		file.References[i] = &blockRef

		totalBytesRead += uint64(n)

		// progress reporting
		if i%100 == 0 || i == metadata.TotalBlocks-1 {
			PrintMemUsage()
			fmt.Printf("Stored block %d/%d (%.1f%%) - size: %d bytes\n",
				i+1, metadata.TotalBlocks,
				float64(i+1)/float64(metadata.TotalBlocks)*100,
				n)
		}
	}
	if VERIFY {
		// verify total bytes read
		if totalBytesRead != metadata.TotalSize {
			// cleanup on failure
			for _, ref := range file.References {
				if ref != nil {
					ks.DeleteFileReference(ref.Key)
				}
			}
			return nil, fmt.Errorf("total bytes read (%d) doesn't match file size (%d)",
				totalBytesRead, metadata.TotalSize)
		}

		// final verification
		fmt.Printf("\n=== Final Verification ===\n")
		fmt.Printf("Total blocks stored: %d\n", len(file.References))
		for i, ref := range file.References {
			if ref == nil {
				return nil, fmt.Errorf("missing reference for block %d", i)
			}
			if i%PRINT_BLOCKS == 0 || i == len(file.References)-1 {
				fmt.Printf("Block %d: Size=%d, Index=%d\n", i, ref.Size, ref.FileIndex)
			}
		}
	}

	// fmt.Println(file.String())
	// store the complete file metadata
	if err := ks.fileToMemory(file); err != nil {
		// cleanup on failure
		for _, ref := range file.References {
			if ref != nil {
				ks.DeleteFileReference(ref.Key)
			}
		}
		return nil, fmt.Errorf("failed to store file: %w", err)
	}

	return file, nil
}
