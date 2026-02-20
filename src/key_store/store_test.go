package key_store

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testDataDir is a persistent directory for reusable test files.
// Files here survive across test runs to avoid regenerating large files.
var testDataDir = filepath.Join("..", "..", "local", "upload")

// ensureTestFile returns the path to a test file of the given size.
// If the file already exists with the correct size, it is reused.
// Otherwise it is (re)generated with random data.
func ensureTestFile(t *testing.T, name string, size int64) string {
	t.Helper()

	if err := os.MkdirAll(testDataDir, 0755); err != nil {
		t.Fatalf("Failed to create test data dir: %v", err)
	}

	path := filepath.Join(testDataDir, name)

	if info, err := os.Stat(path); err == nil && info.Size() == size {
		t.Logf("Reusing existing test file: %s (%d bytes)", path, size)
		return path
	}

	t.Logf("Generating test file: %s (%d bytes)", path, size)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer f.Close()

	const chunkSize = 4 * 1024 * 1024 // 4MB write chunks
	buf := make([]byte, chunkSize)
	remaining := size

	for remaining > 0 {
		n := int64(chunkSize)
		if remaining < n {
			n = remaining
		}
		if _, err := rand.Read(buf[:n]); err != nil {
			t.Fatalf("Failed to generate random data: %v", err)
		}
		if _, err := f.Write(buf[:n]); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		remaining -= n
	}

	return path
}

// newTestKeyStore creates a KeyStore in a temp directory for testing (quiet mode).
func newTestKeyStore(t *testing.T) *KeyStore {
	t.Helper()
	storageDir := filepath.Join(t.TempDir(), "storage")
	ks, err := InitKeyStoreWithConfig(KeyStoreConfig{
		StorageDir: storageDir,
		Verbose:    false,
	})
	if err != nil {
		t.Fatalf("Failed to create keystore: %v", err)
	}
	t.Cleanup(func() { ks.Cleanup() })
	return ks
}

// storeAndVerify stores a file, verifies metadata and chunk integrity,
// then reassembles and verifies the output matches the original.
func storeAndVerify(t *testing.T, ks *KeyStore, filePath string) {
	t.Helper()

	originalData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read original file: %v", err)
	}
	originalHash := sha256.Sum256(originalData)
	t.Logf("Original: %d bytes, hash: %x", len(originalData), originalHash[:8])

	file, err := ks.LoadAndStoreFileLocal(filePath)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	// Verify metadata
	if file.MetaData.TotalSize != uint64(len(originalData)) {
		t.Errorf("Size mismatch: got %d, want %d", file.MetaData.TotalSize, len(originalData))
	}
	if file.MetaData.FileHash != originalHash {
		t.Errorf("Hash mismatch: got %x, want %x", file.MetaData.FileHash, originalHash)
	}

	// Verify every chunk
	for i, ref := range file.References {
		if ref == nil {
			t.Fatalf("Chunk reference %d is nil", i)
		}
		chunkData, err := ks.LoadFileReferenceData(ref.Key)
		if err != nil {
			t.Fatalf("Failed to read chunk %d: %v", i, err)
		}
		if uint32(len(chunkData)) != ref.Size {
			t.Errorf("Chunk %d size mismatch: got %d, want %d", i, len(chunkData), ref.Size)
		}
	}

	// Reassemble and verify
	reassembled, err := ks.ReassembleFileToBytes(file.MetaData.FileHash)
	if err != nil {
		t.Fatalf("Failed to reassemble file: %v", err)
	}
	if len(reassembled) != len(originalData) {
		t.Errorf("Reassembled size mismatch: got %d, want %d", len(reassembled), len(originalData))
	}
	reassembledHash := sha256.Sum256(reassembled)
	if reassembledHash != originalHash {
		t.Errorf("Reassembled hash mismatch: got %x, want %x", reassembledHash, originalHash)
	}
}

// --- Tests ---

func TestLargeFileChunking(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	// 256MB max for automated tests
	filePath := ensureTestFile(t, "test_256mb.dat", 256*1024*1024)
	ks := newTestKeyStore(t)
	storeAndVerify(t, ks, filePath)
}

func TestSmallFileChunking(t *testing.T) {
	sizes := []struct {
		name string
		size int64
	}{
		{"1KB", 1024},
		{"1MB", 1024 * 1024},
		{"5MB", 5 * 1024 * 1024},
	}

	for _, tc := range sizes {
		t.Run(tc.name, func(t *testing.T) {
			filePath := ensureTestFile(t, fmt.Sprintf("test_%s.dat", tc.name), tc.size)
			ks := newTestKeyStore(t)
			storeAndVerify(t, ks, filePath)
		})
	}
}

func TestEmptyFile(t *testing.T) {
	ks := newTestKeyStore(t)

	// Create a zero-byte file
	emptyFile := filepath.Join(t.TempDir(), "empty.dat")
	if err := os.WriteFile(emptyFile, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create empty file: %v", err)
	}

	file, err := ks.LoadAndStoreFileLocal(emptyFile)
	if err != nil {
		t.Fatalf("Failed to store empty file: %v", err)
	}

	if file.MetaData.TotalSize != 0 {
		t.Errorf("Expected TotalSize 0, got %d", file.MetaData.TotalSize)
	}
	if file.MetaData.TotalBlocks != 0 {
		t.Errorf("Expected TotalBlocks 0, got %d", file.MetaData.TotalBlocks)
	}
}

func TestSingleChunkFile(t *testing.T) {
	ks := newTestKeyStore(t)

	// Create a file smaller than MinBlockSize
	data := make([]byte, 1000)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate data: %v", err)
	}

	tmpFile := filepath.Join(t.TempDir(), "tiny.dat")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	file, err := ks.LoadAndStoreFileLocal(tmpFile)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	if file.MetaData.TotalBlocks != 1 {
		t.Errorf("Expected 1 block, got %d", file.MetaData.TotalBlocks)
	}
	if file.MetaData.BlockSize != uint32(len(data)) {
		t.Errorf("Expected block size %d, got %d", len(data), file.MetaData.BlockSize)
	}

	// Reassemble and verify
	reassembled, err := ks.ReassembleFileToBytes(file.MetaData.FileHash)
	if err != nil {
		t.Fatalf("Failed to reassemble: %v", err)
	}
	if sha256.Sum256(reassembled) != sha256.Sum256(data) {
		t.Error("Reassembled data doesn't match original")
	}
}

func TestExactBlockSizeFile(t *testing.T) {
	ks := newTestKeyStore(t)

	// File exactly MinBlockSize bytes — should be exactly 1 chunk
	data := make([]byte, MinBlockSize)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate data: %v", err)
	}

	tmpFile := filepath.Join(t.TempDir(), "exact_block.dat")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	file, err := ks.LoadAndStoreFileLocal(tmpFile)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	if file.MetaData.TotalBlocks != 1 {
		t.Errorf("Expected 1 block for MinBlockSize file, got %d", file.MetaData.TotalBlocks)
	}

	storeAndVerify(t, ks, tmpFile)
}

func TestKeyStorePersistence(t *testing.T) {
	tempDir := t.TempDir()
	storageDir := filepath.Join(tempDir, "storage")

	// Create keystore and store a file
	ks1, err := InitKeyStore(storageDir)
	if err != nil {
		t.Fatalf("Failed to create keystore: %v", err)
	}

	data := make([]byte, 1024*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate test data: %v", err)
	}

	testFile := filepath.Join(t.TempDir(), "test.dat")
	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	file, err := ks1.LoadAndStoreFileLocal(testFile)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}
	originalHash := file.MetaData.FileHash

	if err := ks1.UpdateLocalMetaData(); err != nil {
		t.Fatalf("Failed to save metadata: %v", err)
	}

	// Create a new keystore from the same directory — should load persisted state
	ks2, err := InitKeyStore(storageDir)
	if err != nil {
		t.Fatalf("Failed to load keystore: %v", err)
	}

	reassembled, err := ks2.ReassembleFileToBytes(originalHash)
	if err != nil {
		t.Fatalf("Failed to reassemble from reloaded keystore: %v", err)
	}

	reassembledHash := sha256.Sum256(reassembled)
	if reassembledHash != originalHash {
		t.Error("Reassembled file hash does not match original after reload")
	}
	if len(reassembled) != len(data) {
		t.Errorf("Reassembled size %d != original %d", len(reassembled), len(data))
	}
}

func TestChunkCorruptionDetected(t *testing.T) {
	ks := newTestKeyStore(t)

	data := make([]byte, MinBlockSize*2)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate data: %v", err)
	}

	tmpFile := filepath.Join(t.TempDir(), "corrupt_test.dat")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	file, err := ks.LoadAndStoreFileLocal(tmpFile)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	// Corrupt the first chunk on disk
	ref := file.References[0]
	chunkPath := ks.GetLocalBlockLocation(ref.Key)
	corruptData := make([]byte, ref.Size)
	if _, err := rand.Read(corruptData); err != nil {
		t.Fatalf("Failed to generate corrupt data: %v", err)
	}
	if err := os.WriteFile(chunkPath, corruptData, 0644); err != nil {
		t.Fatalf("Failed to write corrupt chunk: %v", err)
	}

	// Reassembly should detect corruption
	_, err = ks.ReassembleFileToBytes(file.MetaData.FileHash)
	if err == nil {
		t.Error("Expected corruption error, got nil")
	}
}

func TestCleanupRemovesAllFiles(t *testing.T) {
	storageDir := filepath.Join(t.TempDir(), "storage")
	ks, err := InitKeyStore(storageDir)
	if err != nil {
		t.Fatalf("Failed to create keystore: %v", err)
	}

	data := make([]byte, MinBlockSize*3)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate data: %v", err)
	}

	tmpFile := filepath.Join(t.TempDir(), "cleanup_test.dat")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	_, err = ks.LoadAndStoreFileLocal(tmpFile)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	// Verify files exist
	entries, _ := os.ReadDir(storageDir)
	if len(entries) == 0 {
		t.Fatal("Expected storage files to exist before cleanup")
	}

	if err := ks.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify .kdht files are gone
	kdhtFiles, _ := filepath.Glob(filepath.Join(storageDir, "data", "*.kdht"))
	if len(kdhtFiles) > 0 {
		t.Errorf("Found %d .kdht files after cleanup", len(kdhtFiles))
	}

	// Verify metadata dir is gone
	metadataDir := filepath.Join(storageDir, "metadata")
	if _, err := os.Stat(metadataDir); !os.IsNotExist(err) {
		t.Error("Metadata directory still exists after cleanup")
	}
}

func TestStreamFile(t *testing.T) {
	ks := newTestKeyStore(t)

	data := make([]byte, MinBlockSize*3+500) // 3 full chunks + partial
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate data: %v", err)
	}

	file, err := ks.StoreFileLocal("stream_test.dat", data)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	var buf bytes.Buffer
	if err := ks.StreamFile(file.MetaData.FileHash, &buf); err != nil {
		t.Fatalf("StreamFile failed: %v", err)
	}

	if buf.Len() != len(data) {
		t.Errorf("Streamed size %d != original %d", buf.Len(), len(data))
	}
	if sha256.Sum256(buf.Bytes()) != sha256.Sum256(data) {
		t.Error("Streamed data hash does not match original")
	}
}

func TestStreamFileByName(t *testing.T) {
	ks := newTestKeyStore(t)

	data := make([]byte, MinBlockSize*2)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate data: %v", err)
	}

	_, err := ks.StoreFileLocal("byname_test.dat", data)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	var buf bytes.Buffer
	if err := ks.StreamFileByName("byname_test.dat", &buf); err != nil {
		t.Fatalf("StreamFileByName failed: %v", err)
	}

	if sha256.Sum256(buf.Bytes()) != sha256.Sum256(data) {
		t.Error("Streamed data does not match original")
	}

	// Non-existent name should error
	if err := ks.StreamFileByName("nope.dat", &buf); err == nil {
		t.Error("Expected error for non-existent filename")
	}
}

func TestGetFileByName(t *testing.T) {
	ks := newTestKeyStore(t)

	data := make([]byte, 1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate data: %v", err)
	}

	stored, err := ks.StoreFileLocal("lookup_test.dat", data)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	// Lookup by name
	found, err := ks.GetFileByName("lookup_test.dat")
	if err != nil {
		t.Fatalf("GetFileByName failed: %v", err)
	}
	if found.MetaData.FileHash != stored.MetaData.FileHash {
		t.Error("Found file hash does not match stored file hash")
	}

	// Non-existent name
	if _, err := ks.GetFileByName("missing.dat"); err == nil {
		t.Error("Expected error for non-existent filename")
	}
}

func TestGetFileByNamePersistence(t *testing.T) {
	tempDir := t.TempDir()
	storageDir := filepath.Join(tempDir, "storage")

	// Store a file
	ks1, err := InitKeyStore(storageDir)
	if err != nil {
		t.Fatalf("Failed to create keystore: %v", err)
	}

	data := make([]byte, 2048)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate data: %v", err)
	}

	stored, err := ks1.StoreFileLocal("persist_name.dat", data)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	// Reload from disk — name index should be rebuilt
	ks2, err := InitKeyStore(storageDir)
	if err != nil {
		t.Fatalf("Failed to reload keystore: %v", err)
	}

	found, err := ks2.GetFileByName("persist_name.dat")
	if err != nil {
		t.Fatalf("GetFileByName after reload failed: %v", err)
	}
	if found.MetaData.FileHash != stored.MetaData.FileHash {
		t.Error("File hash mismatch after reload")
	}
}

func TestStreamChunkRange(t *testing.T) {
	ks := newTestKeyStore(t)

	// 4 chunks exactly
	data := make([]byte, MinBlockSize*4)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate data: %v", err)
	}

	file, err := ks.StoreFileLocal("range_test.dat", data)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}
	hash := file.MetaData.FileHash
	blockSize := int(file.MetaData.BlockSize)

	// Stream chunks [1, 3) — should be chunks 1 and 2
	var buf bytes.Buffer
	n, err := ks.StreamChunkRange(hash, 1, 3, &buf)
	if err != nil {
		t.Fatalf("StreamChunkRange failed: %v", err)
	}

	expectedBytes := uint64(blockSize * 2)
	if n != expectedBytes {
		t.Errorf("Bytes written %d != expected %d", n, expectedBytes)
	}
	// Verify content matches the original slice
	expectedData := data[blockSize : blockSize*3]
	if !bytes.Equal(buf.Bytes(), expectedData) {
		t.Error("Chunk range data does not match original slice")
	}

	// end=0 means stream to end
	buf.Reset()
	n, err = ks.StreamChunkRange(hash, 2, 0, &buf)
	if err != nil {
		t.Fatalf("StreamChunkRange (end=0) failed: %v", err)
	}
	expectedData = data[blockSize*2:]
	if !bytes.Equal(buf.Bytes(), expectedData) {
		t.Error("Chunk range (to end) data does not match")
	}

	// Invalid range
	_, err = ks.StreamChunkRange(hash, 3, 2, &buf)
	if err == nil {
		t.Error("Expected error for invalid range start >= end")
	}

	// start == end
	_, err = ks.StreamChunkRange(hash, 2, 2, &buf)
	if err == nil {
		t.Error("Expected error for empty range")
	}
}

func TestStreamFileDetectsCorruption(t *testing.T) {
	ks := newTestKeyStore(t)

	data := make([]byte, MinBlockSize*2)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate data: %v", err)
	}

	file, err := ks.StoreFileLocal("corrupt_stream.dat", data)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	// Corrupt chunk 0
	ref := file.References[0]
	chunkPath := ks.GetLocalBlockLocation(ref.Key)
	corrupt := make([]byte, ref.Size)
	if _, err := rand.Read(corrupt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chunkPath, corrupt, 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := ks.StreamFile(file.MetaData.FileHash, &buf); err == nil {
		t.Error("Expected StreamFile to detect corruption")
	}
}

func TestFileByNameOverwrite(t *testing.T) {
	ks := newTestKeyStore(t)

	// Store two different files with the same name — latest wins
	data1 := make([]byte, 512)
	data2 := make([]byte, 1024)
	if _, err := rand.Read(data1); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(data2); err != nil {
		t.Fatal(err)
	}

	_, err := ks.StoreFileLocal("same.dat", data1)
	if err != nil {
		t.Fatal(err)
	}
	file2, err := ks.StoreFileLocal("same.dat", data2)
	if err != nil {
		t.Fatal(err)
	}

	found, err := ks.GetFileByName("same.dat")
	if err != nil {
		t.Fatal(err)
	}
	if found.MetaData.FileHash != file2.MetaData.FileHash {
		t.Error("Expected name index to point to latest stored file")
	}
}

func TestKeyStoreConfig(t *testing.T) {
	storageDir := filepath.Join(t.TempDir(), "storage")

	// Quiet mode — should not produce output (we just verify it doesn't panic)
	cfg := DefaultConfig(storageDir)
	cfg.Verbose = false
	ks, err := InitKeyStoreWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create quiet keystore: %v", err)
	}

	data := make([]byte, 1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	_, err = ks.StoreFileLocal("quiet.dat", data)
	if err != nil {
		t.Fatalf("StoreFileLocal in quiet mode failed: %v", err)
	}
	ks.Cleanup()

	// Verify-on-write mode
	cfg2 := DefaultConfig(filepath.Join(t.TempDir(), "storage2"))
	cfg2.VerifyOnWrite = true
	cfg2.Verbose = false
	ks2, err := InitKeyStoreWithConfig(cfg2)
	if err != nil {
		t.Fatalf("Failed to create verify keystore: %v", err)
	}
	_, err = ks2.StoreFileLocal("verify.dat", data)
	if err != nil {
		t.Fatalf("StoreFileLocal with verify-on-write failed: %v", err)
	}
	ks2.Cleanup()

	// DefaultConfig should be verbose
	cfg = DefaultConfig(storageDir)
	if !cfg.Verbose {
		t.Error("DefaultConfig should have Verbose=true")
	}
	if cfg.DefaultTTLSeconds != DefaultFileTTLSeconds {
		t.Errorf("DefaultConfig should have DefaultTTLSeconds=%d, got %d", DefaultFileTTLSeconds, cfg.DefaultTTLSeconds)
	}
}

func TestConfigurableDefaultTTL(t *testing.T) {
	storageDir := filepath.Join(t.TempDir(), "storage")
	cfg := DefaultConfig(storageDir)
	cfg.Verbose = false
	cfg.DefaultTTLSeconds = 2

	ks, err := InitKeyStoreWithConfig(cfg)
	if err != nil {
		t.Fatalf("failed to create keystore: %v", err)
	}
	t.Cleanup(func() { ks.Cleanup() })

	data := make([]byte, 1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}

	inMemoryFile, err := ks.StoreFileLocal("ttl_cfg_mem.dat", data)
	if err != nil {
		t.Fatalf("StoreFileLocal failed: %v", err)
	}
	if inMemoryFile.MetaData.TTL != 2 {
		t.Fatalf("expected StoreFileLocal TTL=2, got %d", inMemoryFile.MetaData.TTL)
	}

	diskPath := filepath.Join(t.TempDir(), "ttl_cfg_disk.dat")
	if err := os.WriteFile(diskPath, data, 0644); err != nil {
		t.Fatalf("failed to write local input file: %v", err)
	}
	localFile, err := ks.LoadAndStoreFileLocal(diskPath)
	if err != nil {
		t.Fatalf("LoadAndStoreFileLocal failed: %v", err)
	}
	if localFile.MetaData.TTL != 2 {
		t.Fatalf("expected LoadAndStoreFileLocal TTL=2, got %d", localFile.MetaData.TTL)
	}

	remoteHandler := &DefaultRemoteHandler{}
	remoteFile, err := ks.LoadAndStoreFileRemote(diskPath, remoteHandler)
	if err != nil {
		t.Fatalf("LoadAndStoreFileRemote failed: %v", err)
	}
	if remoteFile.MetaData.TTL != 2 {
		t.Fatalf("expected LoadAndStoreFileRemote TTL=2, got %d", remoteFile.MetaData.TTL)
	}
}

func TestChunkLocResolution(t *testing.T) {
	ks := newTestKeyStore(t)

	data := make([]byte, MinBlockSize*2)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}

	file, err := ks.StoreFileLocal("chunkloc.dat", data)
	if err != nil {
		t.Fatalf("StoreFileLocal failed: %v", err)
	}

	// Verify each chunk can be resolved through the new index
	for i, ref := range file.References {
		if ref == nil {
			t.Fatalf("nil reference at index %d", i)
		}
		got, err := ks.GetFileReference(ref.Key)
		if err != nil {
			t.Fatalf("GetFileReference failed for chunk %d: %v", i, err)
		}
		if got.Key != ref.Key {
			t.Errorf("chunk %d key mismatch", i)
		}
		if got.DataHash != ref.DataHash {
			t.Errorf("chunk %d hash mismatch", i)
		}
	}
}

func TestTTLExpiry(t *testing.T) {
	ks := newTestKeyStore(t)

	data := make([]byte, 1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}

	file, err := ks.StoreFileLocal("ttl.dat", data)
	if err != nil {
		t.Fatal(err)
	}

	// Set TTL to 1 second (modify directly in memory)
	ks.lock.Lock()
	f := ks.files[file.MetaData.FileHash]
	f.MetaData.TTL = 1
	f.MetaData.Modified = file.MetaData.Modified // keep original modified time
	ks.lock.Unlock()

	// Should still be accessible immediately
	_, err = ks.fileFromMemory(file.MetaData.FileHash)
	if err != nil {
		t.Fatalf("File should be accessible before TTL expires: %v", err)
	}

	// Wait for TTL to expire
	time.Sleep(2 * time.Second)

	_, err = ks.fileFromMemory(file.MetaData.FileHash)
	if err == nil {
		t.Error("Expected error for expired file")
	}
}

func TestTTLZeroNeverExpires(t *testing.T) {
	ks := newTestKeyStore(t)

	data := make([]byte, 1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}

	file, err := ks.StoreFileLocal("nottl.dat", data)
	if err != nil {
		t.Fatal(err)
	}

	// Set TTL to 0 (no expiry)
	ks.lock.Lock()
	ks.files[file.MetaData.FileHash].MetaData.TTL = 0
	ks.lock.Unlock()

	// Should always be accessible
	_, err = ks.fileFromMemory(file.MetaData.FileHash)
	if err != nil {
		t.Fatalf("TTL=0 file should never expire: %v", err)
	}
}

func TestCleanupExpired(t *testing.T) {
	ks := newTestKeyStore(t)

	data1 := make([]byte, 1024)
	data2 := make([]byte, 1024)
	if _, err := rand.Read(data1); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(data2); err != nil {
		t.Fatal(err)
	}

	file1, err := ks.StoreFileLocal("expire1.dat", data1)
	if err != nil {
		t.Fatal(err)
	}
	file2, err := ks.StoreFileLocal("keep.dat", data2)
	if err != nil {
		t.Fatal(err)
	}

	// Set file1 TTL=1s, file2 TTL=0 (no expiry)
	ks.lock.Lock()
	ks.files[file1.MetaData.FileHash].MetaData.TTL = 1
	ks.files[file2.MetaData.FileHash].MetaData.TTL = 0
	ks.lock.Unlock()

	time.Sleep(2 * time.Second)

	removed := ks.CleanupExpired()
	if removed != 1 {
		t.Errorf("Expected 1 expired file removed, got %d", removed)
	}

	// file2 should still be accessible
	_, err = ks.fileFromMemory(file2.MetaData.FileHash)
	if err != nil {
		t.Fatalf("Non-expired file should still be accessible: %v", err)
	}
}

func TestDeleteFile(t *testing.T) {
	ks := newTestKeyStore(t)

	data := make([]byte, MinBlockSize*2)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}

	file, err := ks.StoreFileLocal("delete_me.dat", data)
	if err != nil {
		t.Fatal(err)
	}

	hash := file.MetaData.FileHash

	// Verify chunks exist
	for _, ref := range file.References {
		if _, err := os.Stat(ref.Location); err != nil {
			t.Fatalf("Chunk file should exist: %v", err)
		}
	}

	// Delete the file
	if err := ks.DeleteFile(hash); err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	// Verify file is gone from memory
	_, err = ks.fileFromMemory(hash)
	if err == nil {
		t.Error("Expected error after deleting file")
	}

	// Verify chunks are gone from disk
	for _, ref := range file.References {
		if _, err := os.Stat(ref.Location); !os.IsNotExist(err) {
			t.Error("Chunk file should be deleted")
		}
	}

	// Verify name lookup is gone
	_, err = ks.GetFileByName("delete_me.dat")
	if err == nil {
		t.Error("Expected error for deleted file name lookup")
	}
}

func TestStoreFromReader(t *testing.T) {
	ks := newTestKeyStore(t)

	data := make([]byte, MinBlockSize*2+999)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	originalHash := sha256.Sum256(data)

	r := bytes.NewReader(data)
	file, err := ks.StoreFromReader("reader_test.dat", r, uint64(len(data)))
	if err != nil {
		t.Fatalf("StoreFromReader failed: %v", err)
	}

	if file.MetaData.FileName != "reader_test.dat" {
		t.Errorf("Expected filename reader_test.dat, got %s", file.MetaData.FileName)
	}
	if file.MetaData.FileHash != originalHash {
		t.Error("File hash mismatch")
	}

	// Retrieve via StreamFile and compare
	var buf bytes.Buffer
	if err := ks.StreamFile(file.MetaData.FileHash, &buf); err != nil {
		t.Fatalf("StreamFile failed: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Error("Streamed data does not match original")
	}

	// Verify name lookup works
	found, err := ks.GetFileByName("reader_test.dat")
	if err != nil {
		t.Fatalf("GetFileByName failed: %v", err)
	}
	if found.MetaData.FileHash != originalHash {
		t.Error("Name lookup returned wrong file")
	}
}

func TestStoreFromReaderSizeMismatch(t *testing.T) {
	ks := newTestKeyStore(t)

	data := make([]byte, 1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}

	// Claim size is larger than actual data
	r := bytes.NewReader(data)
	_, err := ks.StoreFromReader("bad.dat", r, 2048)
	if err == nil {
		t.Error("Expected error for size mismatch")
	}
}

func TestStoreFileLocalAndLoadAndStoreFileLocalProduceSameKeys(t *testing.T) {
	// Both methods should produce identical chunk keys for the same data
	data := make([]byte, MinBlockSize*2)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("Failed to generate data: %v", err)
	}

	// Method 1: StoreFileLocal (in-memory data)
	ks1 := newTestKeyStore(t)
	file1, err := ks1.StoreFileLocal("test.dat", data)
	if err != nil {
		t.Fatalf("StoreFileLocal failed: %v", err)
	}

	// Method 2: LoadAndStoreFileLocal (from disk)
	ks2 := newTestKeyStore(t)
	tmpFile := filepath.Join(t.TempDir(), "test.dat")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	file2, err := ks2.LoadAndStoreFileLocal(tmpFile)
	if err != nil {
		t.Fatalf("LoadAndStoreFileLocal failed: %v", err)
	}

	// Verify same number of chunks
	if len(file1.References) != len(file2.References) {
		t.Fatalf("Different chunk counts: %d vs %d", len(file1.References), len(file2.References))
	}

	// Verify identical keys
	for i := range file1.References {
		if file1.References[i].Key != file2.References[i].Key {
			t.Errorf("Chunk %d key mismatch: %x vs %x",
				i, file1.References[i].Key, file2.References[i].Key)
		}
	}
}

func TestReuploadRefreshesTTL(t *testing.T) {
	storageDir := filepath.Join(t.TempDir(), "storage")
	cfg := DefaultConfig(storageDir)
	cfg.Verbose = false
	cfg.DefaultTTLSeconds = 1 // expire after 1 second

	ks, err := InitKeyStoreWithConfig(cfg)
	if err != nil {
		t.Fatalf("failed to create keystore: %v", err)
	}
	t.Cleanup(func() { ks.Cleanup() })

	data := make([]byte, 4096)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}

	// Initial upload
	if _, err := ks.StoreFileLocal("refresh.dat", data); err != nil {
		t.Fatalf("initial StoreFileLocal failed: %v", err)
	}

	// Wait for TTL to elapse
	time.Sleep(2 * time.Second)

	// Re-upload same bytes — should refresh the entry
	reup, err := ks.StoreFileLocal("refresh.dat", data)
	if err != nil {
		t.Fatalf("re-upload StoreFileLocal failed: %v", err)
	}

	// Modified should be recent (within the last 5 seconds)
	modifiedSec := reup.MetaData.Modified / 1e9
	if time.Now().Unix()-modifiedSec > 5 {
		t.Errorf("Modified timestamp not refreshed after re-upload: %d", reup.MetaData.Modified)
	}

	// Reassembly must succeed (file must not be considered expired)
	got, err := ks.ReassembleFileToBytes(reup.MetaData.FileHash)
	if err != nil {
		t.Fatalf("ReassembleFileToBytes after re-upload failed: %v", err)
	}
	if string(got) != string(data) {
		t.Error("reassembled data does not match original")
	}
}
