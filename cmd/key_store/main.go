package main

import (
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
)

const CLEAN = true
const DATA_DIRECTORY = "./tmp/"
const RUN_ALL = false

//	var TEST_FILES = map[int]string{
//		0: "image.jpg",
//		1: "142_MB.dmg",
//		2: "ubuntu.iso",
//	}

var TEST_FILES, err = getFilesInDirectory(DATA_DIRECTORY)
var FILE = TEST_FILES[1]

func getFilesInDirectory(dirPath string) ([]string, error) {
	var files []string

	// Read directory entries
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dirPath, err)
	}

	// Add each file to slice, skipping directories and copy.* files
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasPrefix(entry.Name(), "copy.") {
			files = append(files, entry.Name())
		}
	}

	return files, nil
}

func getFilesRecursive(dirPath string) ([]string, error) {
	var files []string

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && !strings.HasPrefix(info.Name(), "copy.") {
			// Use relative path from dirPath
			relPath, err := filepath.Rel(dirPath, path)
			if err != nil {
				return err
			}
			files = append(files, relPath)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory %s: %w", dirPath, err)
	}

	return files, nil
}

func CreateDirPath(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	return nil
}

func verifyChunks(ks *key_store.KeyStore, file *key_store.File) error {
	fmt.Printf("\nVerifying stored chunks: %d \n", len(file.References))

	for i, ref := range file.References {
		if ref == nil {
			return fmt.Errorf("chunk reference %d is nil", i)
		}

		chunkData, err := ks.LoadFileReferenceData(ref.Key)
		if err != nil {
			return fmt.Errorf("failed to read chunk %d: %w", i, err)
		}

		// verify chunk size
		if uint32(len(chunkData)) != ref.Size {
			return fmt.Errorf("chunk %d size mismatch: got %d, expected %d",
				i, len(chunkData), ref.Size)
		}

		// verify chunk hash
		dataHash := sha256.Sum256(chunkData)
		if dataHash != ref.DataHash {
			return fmt.Errorf("chunk %d hash mismatch:\nstored:  %x\ncomputed: %x \n%v",
				i, ref.DataHash, dataHash, ref.String())
		}

		// progress reporting
		if i%key_store.PRINT_BLOCKS == 0 || i == int(file.MetaData.TotalBlocks-1) {
			fmt.Printf("Verified chunk %d/%d: size=%d, index=%d, hash=%x\n",
				i, file.MetaData.TotalBlocks-1, len(chunkData), ref.FileIndex, dataHash)
		}
	}
	return nil
}

func cleanupCopyFiles(dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	for _, file := range files {
		if strings.HasPrefix(strings.ToLower(file.Name()), "copy") {
			fullPath := filepath.Join(dir, file.Name())
			if err := os.Remove(fullPath); err != nil {
				return fmt.Errorf("failed to remove file %s: %w", fullPath, err)
			}
			fmt.Printf("Removed file: %s\n", fullPath)
		}
	}
	return nil
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "run" {
		fmt.Println("Usage: go run main.go run")
		fmt.Println("\nBefore running, please configure TEST_FILES in the source code:")
		fmt.Println("\nvar TEST_FILES = map[int]string{")
		fmt.Println("    0: \"your_file.ext\",")
		fmt.Println("    1: \"another_file.ext\",")
		fmt.Println("    // add more files as needed")
		fmt.Println("}")
		fmt.Println("\nCurrent TEST_FILES configuration:")
		for idx, file := range TEST_FILES {
			fmt.Printf("    %d: \"%s\"\n", idx, file)
		}
		fmt.Println("\nMake sure your files are in:", DATA_DIRECTORY)
		os.Exit(1)
	}

	// create storage directory
	storageDir := filepath.Join(".", "storage")
	if err := CreateDirPath(storageDir); err != nil {
		log.Fatal(err)
	}

	// init new keystore
	keystore, err := key_store.InitKeyStore(storageDir)
	if err != nil {
		log.Fatalf("Failed to create keystore: %v", err)
	}

	// cleanup files
	// note: should be configured up top
	if CLEAN {
		cleanupCopyFiles(DATA_DIRECTORY)
		// defer keystore.cleanupextensions(".toml")
		defer keystore.CleanupKDHT()
		// defer keystore.cleanupmetadata()
	}

	// enumurate test suite
	files := make([]string, 0)
	if RUN_ALL {
		for _, v := range TEST_FILES {
			files = append(files, v)
		}
	} else {
		files = append(files, FILE)
	}

	// execute file loading
	for _, FILE_test := range files {
		filename := DATA_DIRECTORY + FILE_test

		// read test file
		originalData, err := os.ReadFile(filename)
		if err != nil {
			log.Fatalf("Failed to read original file: %v", err)
		}

		// calculate original hash
		originalHash := sha256.Sum256(originalData)
		fmt.Printf("Original file size: %d bytes\n", len(originalData))
		fmt.Printf("Original file hash: %x\n\n", originalHash)

		// store the file
		file, err := keystore.LoadAndStoreFile(filename)
		if err != nil {
			log.Fatalf("Failed to store file: %v", err)
		}

		fmt.Printf("\nFile details:\n")
		fmt.Printf("Total size: %d bytes\n", file.MetaData.TotalSize)
		fmt.Printf("Chunk size: %d bytes\n", file.MetaData.BlockSize)
		fmt.Printf("Total chunks: %d\n", file.MetaData.TotalBlocks)
		fmt.Printf("Last chunk size: %d bytes\n",
			file.MetaData.TotalSize-uint64(file.MetaData.BlockSize*(file.MetaData.TotalBlocks-1)))
		// fmt.Printf("\nFile:: %v \n", file.String())

		// verify chunks
		if err := verifyChunks(keystore, file); err != nil {
			log.Fatalf("Chunk verification failed: %v", err)
		}

		// calculate last chunk size
		lastChunkSize := file.MetaData.TotalSize % uint64(file.MetaData.BlockSize)
		if lastChunkSize == 0 {
			lastChunkSize = uint64(file.MetaData.BlockSize)
		}
		fmt.Printf("Expected last chunk size: %d\n", lastChunkSize)

		// reassemble the file
		realPath := DATA_DIRECTORY + "copy." + FILE_test

		fmt.Printf("\nReassembling file to: %s\n", realPath)
		if err := keystore.ReassembleFileToPath(file.MetaData.FileHash, realPath); err != nil {
			log.Fatalf("Failed to reassemble file: %v", err)
		}

		reassembledHash, length, err := key_store.HashFile(realPath)
		if err != nil {
			log.Fatalf("Failed to verify reassembled file: %v", err)
		}

		fmt.Printf("\nReassembly complete:\n")
		fmt.Printf("Original size: %d bytes\n", file.MetaData.TotalSize)
		fmt.Printf("Original hash: %x\n", file.MetaData.FileHash)
		fmt.Printf("Reassembled size: %d bytes\n", length)
		fmt.Printf("Reassembled hash: %x\n", reassembledHash)

		if file.MetaData.FileHash != reassembledHash {
			log.Fatalf("Hash mismatch after reassembly!\nOriginal: %x\nReassembled: %x",
				file.MetaData.FileHash, reassembledHash)
		}

		fmt.Printf("Successfully reassembled file to: %s\n", realPath)
	}
}
