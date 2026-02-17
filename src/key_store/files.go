package key_store

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// computeChunkKey produces a deterministic 20-byte DHT routing key for a chunk.
// It appends the chunk index as a little-endian uint64 to the file hash,
// then takes SHA-1 of the result. This is the canonical key derivation used
// everywhere chunks are created or looked up.
func computeChunkKey(fileHash [HashSize]byte, chunkIndex uint32) [KeySize]byte {
	buf := make([]byte, HashSize+8)
	copy(buf[:HashSize], fileHash[:])
	binary.LittleEndian.PutUint64(buf[HashSize:], uint64(chunkIndex))
	return sha1.Sum(buf)
}

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

	DEFAULT_PERMISSIONS = R_USER | W_USER | R_GROUP | R_OTHER
)

// this stores arbitrary data as a file locally
func (ks *KeyStore) StoreFileLocal(name string, fileData []byte) (*File, error) {
	// prepare metadata
	metadata, err := PrepareMetaData(name, fileData)
	if err != nil {
		return nil, err
	}
	metadata.TTL = ks.config.DefaultTTLSeconds

	// calculate and store file hash
	metadata.FileHash = sha256.Sum256(fileData)
	if existing, ok := ks.existingFileByHash(metadata.FileHash); ok {
		return existing, nil
	}
	if err := ks.ensureHashNotCached(metadata.FileHash, metadata.FileName); err != nil {
		return nil, err
	}

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
		endIdx := min(startIdx+uint64(metadata.BlockSize), metadata.TotalSize)

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

		// calculate block dht routing key
		block.Key = computeChunkKey(metadata.FileHash, i)

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

		totalBytesProcessed += uint64(blockSize)

		// progress output
		if ks.config.Verbose && (i%PRINT_BLOCKS == 0 || i == metadata.TotalBlocks-1) {
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

// Reassemble a file and return its data as bytes
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
		if ks.config.Verbose && (i%100 == 0 || i == int(file.MetaData.TotalBlocks-1)) {
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

// Reassemble a file and save it locally
func (ks *KeyStore) ReassembleFileToPath(key [HashSize]byte, outputPath string) error {
	// get the complete file record
	file, err := ks.fileFromMemory(key)
	if err != nil {
		return fmt.Errorf("failed to get file: %w", err)
	}

	// create output file
	f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fs.FileMode(file.MetaData.Permissions))
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
		if ks.config.Verbose && (i%100 == 0 || i == int(file.MetaData.TotalBlocks-1)) {
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

// StoreFromReader ingests a file from an io.Reader (e.g. a network connection)
// and stores it locally. It spills to a temp file to avoid buffering the entire
// upload in memory, then delegates to LoadAndStoreFileLocal for hash+chunk.
func (ks *KeyStore) StoreFromReader(name string, r io.Reader, size uint64) (*File, error) {
	// create temp file in storage dir
	tmp, err := os.CreateTemp(ks.storageDir, "upload-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	// stream reader to disk
	written, err := io.Copy(tmp, r)
	if err != nil {
		tmp.Close()
		return nil, fmt.Errorf("failed to write upload data: %w", err)
	}
	tmp.Close()

	if uint64(written) != size {
		return nil, fmt.Errorf("upload size mismatch: received %d bytes, expected %d", written, size)
	}

	// delegate to existing two-pass pipeline
	file, err := ks.LoadAndStoreFileLocal(tmpPath)
	if err != nil {
		return nil, err
	}

	// patch filename — temp file had a random name
	if file.MetaData.FileName != name {
		ks.lock.Lock()
		delete(ks.filesByName, file.MetaData.FileName)
		file.MetaData.FileName = name
		// update in-memory copy
		if stored, ok := ks.files[file.MetaData.FileHash]; ok {
			stored.MetaData.FileName = name
		}
		ks.filesByName[name] = file.MetaData.FileHash
		ks.lock.Unlock()

		// re-persist metadata with correct filename
		if err := ks.fileToMemory(file); err != nil {
			return nil, fmt.Errorf("failed to re-persist metadata: %w", err)
		}
	}

	return file, nil
}

// Upload a file from your local file system and save the entire file to local storage
// NOTE: prepare a document for ethe kdht but store the file in blocks locally
func (ks *KeyStore) LoadAndStoreFileLocal(localFilePath string) (*File, error) {
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
		TTL:         ks.config.DefaultTTLSeconds,
		BlockSize:   CalculateBlockSize(uint64(fileInfo.Size())),
	}
	var fileHash [HashSize]byte
	copy(fileHash[:], hash.Sum(nil))
	metadata.FileHash = fileHash
	if existing, ok := ks.existingFileByHash(metadata.FileHash); ok {
		return existing, nil
	}
	if err := ks.ensureHashNotCached(metadata.FileHash, metadata.FileName); err != nil {
		return nil, err
	}
	if metadata.BlockSize > 0 {
		metadata.TotalBlocks = uint32((metadata.TotalSize + uint64(metadata.BlockSize) - 1) / uint64(metadata.BlockSize))
	}

	// create file object
	file := &File{
		MetaData:   metadata,
		References: make([]*FileReference, metadata.TotalBlocks),
	}

	if ks.config.Verbose {
		fmt.Printf("Starting chunking process:\n")
		fmt.Printf("Total size: %d bytes\n", metadata.TotalSize)
		fmt.Printf("Block size: %d bytes\n", metadata.BlockSize)
		fmt.Printf("Expected blocks: %d\n", metadata.TotalBlocks)
	}

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
			if ks.config.Verbose {
				fmt.Printf("Last block %d: Reading remaining %d bytes\n", i, bytesToRead)
			}
		}

		// read block
		n, err := io.ReadFull(f, buffer[:bytesToRead])
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("failed to read block %d: %w", i, err)
		}

		if n == 0 {
			return nil, fmt.Errorf("unexpected end of file at block %d", i)
		}

		if ks.config.Verbose && (i%100 == 0 || i == metadata.TotalBlocks-1) {
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

		// calculate chunk's dht routing key
		block.Key = computeChunkKey(metadata.FileHash, i)

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

		// StoreFileReference sets block.Location, but block is a local copy.
		// Copy after store so the reference has the location set.
		blockRef := block
		file.References[i] = &blockRef

		totalBytesRead += uint64(n)

		// progress reporting
		if ks.config.Verbose && (i%100 == 0 || i == metadata.TotalBlocks-1) {
			PrintMemUsage()
			fmt.Printf("Stored block %d/%d (%.1f%%) - size: %d bytes\n",
				i+1, metadata.TotalBlocks,
				float64(i+1)/float64(metadata.TotalBlocks)*100,
				n)
		}
	}
	if ks.config.VerifyOnWrite {
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

		if ks.config.Verbose {
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

// Upload a file from your local file system and pass it to a RemoteHandler to process the
// data elsewhere
//
// NOTE: this is how data is passed to the network
func (ks *KeyStore) LoadAndStoreFileRemote(localFilePath string, handler RemoteHandler) (*File, error) {
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
		TTL:         ks.config.DefaultTTLSeconds,
		BlockSize:   CalculateBlockSize(uint64(fileInfo.Size())),
	}
	var fileHash [HashSize]byte
	copy(fileHash[:], hash.Sum(nil))
	metadata.FileHash = fileHash
	if existing, ok := ks.existingFileByHash(metadata.FileHash); ok {
		return existing, nil
	}
	if err := ks.ensureHashNotCached(metadata.FileHash, metadata.FileName); err != nil {
		return nil, err
	}
	if metadata.BlockSize > 0 {
		metadata.TotalBlocks = uint32((metadata.TotalSize + uint64(metadata.BlockSize) - 1) / uint64(metadata.BlockSize))
	}

	// Start receiver — StartReceiver launches its own goroutine internally
	handler.StartReceiver(&metadata)

	// create file object
	file := &File{
		MetaData:   metadata,
		References: make([]*FileReference, metadata.TotalBlocks),
	}

	if ks.config.Verbose {
		fmt.Printf("Starting chunking process:\n")
		fmt.Printf("Total size: %d bytes\n", metadata.TotalSize)
		fmt.Printf("Block size: %d bytes\n", metadata.BlockSize)
		fmt.Printf("Expected blocks: %d\n", metadata.TotalBlocks)
	}

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
			if ks.config.Verbose {
				fmt.Printf("Last block %d: Reading remaining %d bytes\n", i, bytesToRead)
			}
		}

		// read block
		n, err := io.ReadFull(f, buffer[:bytesToRead])
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("failed to read block %d: %w", i, err)
		}

		if n == 0 {
			return nil, fmt.Errorf("unexpected end of file at block %d", i)
		}

		if ks.config.Verbose && (i%100 == 0 || i == metadata.TotalBlocks-1) {
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

		// calculate chunk's dht routing key
		block.Key = computeChunkKey(metadata.FileHash, i)

		handler.PassFileReference(&block, blockData)

		file.References[i] = &block

		totalBytesRead += uint64(n)

		// progress reporting
		if ks.config.Verbose && (i%100 == 0 || i == metadata.TotalBlocks-1) {
			PrintMemUsage()
			fmt.Printf("Stored block %d/%d (%.1f%%) - size: %d bytes\n",
				i+1, metadata.TotalBlocks,
				float64(i+1)/float64(metadata.TotalBlocks)*100,
				n)
		}
	}
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
