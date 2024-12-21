package key_store

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const dataDir string = "./data/"

func TestLargeFileChunking(t *testing.T) {
	// skip if running short tests
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	// setup
	storageDir := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		t.Fatalf("Failed to create storage directory: %v", err)
	}

	// create keystore
	keystore, err := InitKeyStore(storageDir)
	if err != nil {
		t.Fatalf("Failed to create keystore: %v", err)
	}
	defer keystore.Cleanup()

	// test file path - you might want to make this configurable
	filename := dataDir + "ubuntu.iso"

	// load original file
	originalData, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read original file: %v", err)
	}

	// calculate original hash
	originalHash := sha256.Sum256(originalData)
	t.Logf("Original file size: %d bytes", len(originalData))
	t.Logf("Original file hash: %x", originalHash)

	// store the file
	file, err := keystore.LoadAndStoreFile(filename)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	// verify file metadata
	t.Run("Verify Metadata", func(t *testing.T) {
		if file.MetaData.TotalSize != uint64(len(originalData)) {
			t.Errorf("Size mismatch: got %d, want %d",
				file.MetaData.TotalSize, len(originalData))
		}
		if file.MetaData.FileHash != originalHash {
			t.Errorf("Hash mismatch: got %x, want %x",
				file.MetaData.FileHash, originalHash)
		}
	})

	// verify chunks
	t.Run("Verify Chunks", func(t *testing.T) {
		for i, ref := range file.References {
			if ref == nil {
				t.Fatalf("Chunk reference %d is nil", i)
			}

			chunkData, err := keystore.LoadFileReferenceData(ref.Key)
			if err != nil {
				t.Fatalf("Failed to read chunk %d: %v", i, err)
			}

			// verify chunk index
			if ref.FileIndex != uint32(i) {
				t.Errorf("Chunk %d has incorrect index: got %d", i, ref.FileIndex)
			}

			// verify chunk size
			if uint32(len(chunkData)) != ref.Size {
				t.Errorf("Chunk %d size mismatch: got %d, want %d",
					i, len(chunkData), ref.Size)
			}

			// log progress occasionally
			if i%100 == 0 || i == int(file.MetaData.TotalBlocks-1) {
				t.Logf("Verified chunk %d/%d: size=%d",
					i, file.MetaData.TotalBlocks-1, len(chunkData))
			}
		}
	})

	// test reassembly
	t.Run("Reassemble File", func(t *testing.T) {
		reassembledData, err := keystore.ReassembleFileToBytes(file.MetaData.FileHash)
		if err != nil {
			t.Fatalf("Failed to reassemble file: %v", err)
		}

		// verify size
		if len(reassembledData) != len(originalData) {
			t.Errorf("Reassembled size mismatch: got %d, want %d",
				len(reassembledData), len(originalData))
		}

		// verify hash
		reassembledHash := sha256.Sum256(reassembledData)
		if reassembledHash != originalHash {
			t.Errorf("Reassembled hash mismatch: got %x, want %x",
				reassembledHash, originalHash)
		}
	})
}

// helper function to create test files
func createTestFile(t *testing.T, size int) (string, []byte) {
	t.Helper()

	// create random data
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil { // use crypto/rand
		t.Fatalf("Failed to generate random data: %v", err)
	}

	// create temporary file
	tmpfile := filepath.Join(t.TempDir(), fmt.Sprintf("test-%d.dat", size))
	if err := os.WriteFile(tmpfile, data, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	return tmpfile, data
}

// test with smaller files
func TestSmallFileChunking(t *testing.T) {
	sizes := []int{
		1024,            // 1kb
		1024 * 1024,     // 1mb
		5 * 1024 * 1024, // 5mb
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("Size_%d", size), func(t *testing.T) {
			// create storage directory
			storageDir := filepath.Join(t.TempDir(), "storage")
			if err := os.MkdirAll(storageDir, 0755); err != nil {
				t.Fatalf("Failed to create storage directory: %v", err)
			}

			// create keystore
			keystore, err := InitKeyStore(storageDir)
			if err != nil {
				t.Fatalf("Failed to create keystore: %v", err)
			}
			defer keystore.Cleanup()

			// create test file
			filename, originalData := createTestFile(t, size)
			originalHash := sha256.Sum256(originalData)

			// store and verify
			file, err := keystore.LoadAndStoreFile(filename)
			if err != nil {
				t.Fatalf("Failed to store file: %v", err)
			}

			// verify metadata
			if file.MetaData.FileHash != originalHash {
				t.Errorf("Hash mismatch for size %d", size)
			}

			// reassemble and verify
			reassembled, err := keystore.ReassembleFileToBytes(file.MetaData.FileHash)
			if err != nil {
				t.Fatalf("Failed to reassemble file: %v", err)
			}

			reassembledHash := sha256.Sum256(reassembled)
			if reassembledHash != originalHash {
				t.Errorf("Reassembled hash mismatch for size %d", size)
			}
		})
	}
}

func TestKeyStorePersistence(t *testing.T) {
	// create temp directory that will be cleaned up after test
	tempDir := t.TempDir()
	storageDir := filepath.Join(tempDir, "storage")

	// create initial keystore and store a file
	ks1, err := InitKeyStore(storageDir)
	if err != nil {
		t.Fatalf("Failed to create initial keystore: %v", err)
	}

	// create a test file
	data := make([]byte, 1024*1024) // 1mb test file
	_, err = rand.Read(data)        // fill with random data
	if err != nil {
		t.Fatalf("Failed to generate test data: %v", err)
	}

	testFile := filepath.Join(t.TempDir(), "test.dat")
	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// store the file
	file, err := ks1.LoadAndStoreFile(testFile)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	originalHash := file.MetaData.FileHash

	// save metadata state
	if err := ks1.SaveMetadata(); err != nil {
		t.Fatalf("Failed to save metadata: %v", err)
	}

	// create new keystore instance from same directory
	ks2, err := InitKeyStore(storageDir)
	if err != nil {
		t.Fatalf("Failed to load keystore: %v", err)
	}

	// try to reassemble the file
	reassembled, err := ks2.ReassembleFileToBytes(originalHash)
	if err != nil {
		t.Fatalf("Failed to reassemble file: %v", err)
	}

	// verify the reassembled data matches original
	reassembledHash := sha256.Sum256(reassembled)
	if reassembledHash != originalHash {
		t.Errorf("Reassembled file hash does not match original")
	}

	// verify file size
	if len(reassembled) != len(data) {
		t.Errorf("Reassembled file size %d does not match original size %d",
			len(reassembled), len(data))
	}
}
