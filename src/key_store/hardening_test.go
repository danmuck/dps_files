package key_store

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("failed to generate random bytes: %v", err)
	}
	return buf
}

func newKeyStoreAt(t *testing.T, storageDir string) *KeyStore {
	t.Helper()
	ks, err := InitKeyStoreWithConfig(KeyStoreConfig{
		StorageDir: storageDir,
		Verbose:    false,
	})
	if err != nil {
		t.Fatalf("failed to create keystore: %v", err)
	}
	return ks
}

func seedCacheEntry(t *testing.T, ks *KeyStore, fileName string, data []byte, includeChunk bool) ([HashSize]byte, string) {
	t.Helper()

	hash := sha256.Sum256(data)
	key := computeChunkKey(hash, 0)
	location := ks.GetLocalBlockLocation(key)

	if includeChunk {
		if err := os.MkdirAll(filepath.Dir(location), 0755); err != nil {
			t.Fatalf("failed to create chunk directory: %v", err)
		}
		if err := os.WriteFile(location, data, 0644); err != nil {
			t.Fatalf("failed to write chunk file: %v", err)
		}
	}

	file := &File{
		MetaData: MetaData{
			FileHash:    hash,
			TotalSize:   uint64(len(data)),
			FileName:    fileName,
			Modified:    time.Now().UnixNano(),
			Permissions: DEFAULT_PERMISSIONS,
			TTL:         DefaultFileTTLSeconds,
			BlockSize:   uint32(len(data)),
			TotalBlocks: 1,
		},
		References: []*FileReference{
			{
				Key:       key,
				FileName:  fileName,
				Size:      uint32(len(data)),
				FileIndex: 0,
				Location:  location,
				Protocol:  "file",
				DataHash:  sha256.Sum256(data),
				Parent:    hash,
			},
		},
	}

	if err := ks.upsertCacheEntry(file); err != nil {
		t.Fatalf("failed to seed cache entry: %v", err)
	}

	return hash, ks.cachePathForHash(hash)
}

func TestListStoredFileReferencesAndKnownFiles(t *testing.T) {
	ks := newTestKeyStore(t)

	fileA, err := ks.StoreFileLocal("list_a.bin", randomBytes(t, int(MinBlockSize*2+11)))
	if err != nil {
		t.Fatalf("failed to store file A: %v", err)
	}
	fileB, err := ks.StoreFileLocal("list_b.bin", randomBytes(t, 1024))
	if err != nil {
		t.Fatalf("failed to store file B: %v", err)
	}

	refs := ks.ListStoredFileReferences()
	expectedRefs := int(fileA.MetaData.TotalBlocks + fileB.MetaData.TotalBlocks)
	if len(refs) != expectedRefs {
		t.Fatalf("expected %d references, got %d", expectedRefs, len(refs))
	}

	files := ks.ListKnownFiles()
	if len(files) != 2 {
		t.Fatalf("expected 2 known files, got %d", len(files))
	}

	names := map[string]bool{}
	for _, md := range files {
		names[md.FileName] = true
	}
	if !names["list_a.bin"] || !names["list_b.bin"] {
		t.Fatalf("expected list_a.bin and list_b.bin in known files, got: %+v", names)
	}
}

func TestReassembleFileToPath(t *testing.T) {
	ks := newTestKeyStore(t)

	data := randomBytes(t, int(MinBlockSize*2+73))
	stored, err := ks.StoreFileLocal("path_output.bin", data)
	if err != nil {
		t.Fatalf("failed to store file: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "reassembled.bin")
	if err := ks.ReassembleFileToPath(stored.MetaData.FileHash, outPath); err != nil {
		t.Fatalf("ReassembleFileToPath failed: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("reassembled output does not match original data")
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("failed to stat output file: %v", err)
	}
	if uint32(info.Mode().Perm()) != stored.MetaData.Permissions {
		t.Fatalf("output permissions mismatch: got %o want %o", info.Mode().Perm(), stored.MetaData.Permissions)
	}
}

func TestDeleteFileReference(t *testing.T) {
	ks := newTestKeyStore(t)

	stored, err := ks.StoreFileLocal("delete_ref.bin", randomBytes(t, 4096))
	if err != nil {
		t.Fatalf("failed to store file: %v", err)
	}
	if len(stored.References) != 1 {
		t.Fatalf("expected single reference, got %d", len(stored.References))
	}

	ref := stored.References[0]
	if err := ks.DeleteFileReference(ref.Key); err != nil {
		t.Fatalf("DeleteFileReference failed: %v", err)
	}

	if _, err := os.Stat(ref.Location); !os.IsNotExist(err) {
		t.Fatalf("expected chunk file to be deleted, stat err: %v", err)
	}

	if _, err := ks.GetFileReference(ref.Key); err == nil {
		t.Fatal("expected GetFileReference to fail after deletion")
	}
	if _, err := ks.LoadFileReferenceData(ref.Key); err == nil {
		t.Fatal("expected LoadFileReferenceData to fail after deletion")
	}

	var missing [KeySize]byte
	copy(missing[:], ShaCheckSum([]byte("missing"), KeySize))
	if err := ks.DeleteFileReference(missing); err == nil {
		t.Fatal("expected deleting missing reference to fail")
	}
}

func TestLoadLocalFileToMemoryFiltersMissingLocation(t *testing.T) {
	ks := newTestKeyStore(t)

	stored, err := ks.StoreFileLocal("filter_location.bin", randomBytes(t, int(MinBlockSize*2)))
	if err != nil {
		t.Fatalf("failed to store file: %v", err)
	}

	metadataPath := filepath.Join(ks.storageDir, "metadata", fmt.Sprintf("%x.toml", stored.MetaData.FileHash))

	var diskFile File
	if _, err := toml.DecodeFile(metadataPath, &diskFile); err != nil {
		t.Fatalf("failed to decode metadata file: %v", err)
	}
	if len(diskFile.References) < 2 {
		t.Fatalf("expected at least 2 references, got %d", len(diskFile.References))
	}
	diskFile.References[0].Location = ""

	f, err := os.Create(metadataPath)
	if err != nil {
		t.Fatalf("failed to rewrite metadata: %v", err)
	}
	enc := toml.NewEncoder(f)
	if err := enc.Encode(diskFile); err != nil {
		f.Close()
		t.Fatalf("failed to encode modified metadata: %v", err)
	}
	f.Close()

	loaded, err := ks.LoadLocalFileToMemory(stored.MetaData.FileHash)
	if err != nil {
		t.Fatalf("LoadLocalFileToMemory failed: %v", err)
	}
	if loaded.References[0] != nil {
		t.Fatal("expected reference[0] to be filtered out when location is empty")
	}
	if loaded.References[1] == nil {
		t.Fatal("expected reference[1] to remain available")
	}
}

func TestLoadAllLocalFilesToMemoryRebuildsIndexes(t *testing.T) {
	storageDir := filepath.Join(t.TempDir(), "storage")
	ks1 := newKeyStoreAt(t, storageDir)

	fileA, err := ks1.StoreFileLocal("reload_a.bin", randomBytes(t, 1024))
	if err != nil {
		t.Fatalf("failed storing file A: %v", err)
	}
	fileB, err := ks1.StoreFileLocal("reload_b.bin", randomBytes(t, int(MinBlockSize+77)))
	if err != nil {
		t.Fatalf("failed storing file B: %v", err)
	}

	ks2 := newKeyStoreAt(t, storageDir)

	ks2.lock.Lock()
	ks2.files = make(map[[HashSize]byte]*File)
	ks2.filesByName = make(map[string][HashSize]byte)
	ks2.chunkIndex = make(map[[KeySize]byte]chunkLoc)
	ks2.lock.Unlock()

	if err := ks2.LoadAllLocalFilesToMemory(); err != nil {
		t.Fatalf("LoadAllLocalFilesToMemory failed: %v", err)
	}

	expectedFiles := 2
	if len(ks2.files) != expectedFiles {
		t.Fatalf("expected %d files in memory, got %d", expectedFiles, len(ks2.files))
	}
	if len(ks2.filesByName) != expectedFiles {
		t.Fatalf("expected %d name indexes, got %d", expectedFiles, len(ks2.filesByName))
	}

	expectedRefs := int(fileA.MetaData.TotalBlocks + fileB.MetaData.TotalBlocks)
	if len(ks2.chunkIndex) != expectedRefs {
		t.Fatalf("expected %d chunk index entries, got %d", expectedRefs, len(ks2.chunkIndex))
	}

	if _, err := ks2.GetFileByName("reload_a.bin"); err != nil {
		t.Fatalf("expected reload_a.bin to be resolvable by name: %v", err)
	}
	if _, err := ks2.GetFileByName("reload_b.bin"); err != nil {
		t.Fatalf("expected reload_b.bin to be resolvable by name: %v", err)
	}

	_ = ks1.Cleanup()
	_ = ks2.Cleanup()
}

func TestCleanupKDHTAndMetaData(t *testing.T) {
	// CleanupKDHT should remove chunk files but keep metadata files.
	ksKDHT := newTestKeyStore(t)
	fileKDHT, err := ksKDHT.StoreFileLocal("cleanup_kdht.bin", randomBytes(t, 2048))
	if err != nil {
		t.Fatalf("failed to store cleanup_kdht.bin: %v", err)
	}
	metadataKDHT := filepath.Join(ksKDHT.storageDir, "metadata", fmt.Sprintf("%x.toml", fileKDHT.MetaData.FileHash))
	if err := ksKDHT.CleanupKDHT(); err != nil {
		t.Fatalf("CleanupKDHT failed: %v", err)
	}
	kdhtAfter, _ := filepath.Glob(filepath.Join(ksKDHT.storageDir, "data", "*.kdht"))
	if len(kdhtAfter) != 0 {
		t.Fatalf("expected no .kdht files after CleanupKDHT, got %d", len(kdhtAfter))
	}
	if _, err := os.Stat(metadataKDHT); err != nil {
		t.Fatalf("expected metadata file to remain after CleanupKDHT: %v", err)
	}

	// CleanupMetaData should remove metadata files but keep chunk files.
	ksMeta := newTestKeyStore(t)
	fileMeta, err := ksMeta.StoreFileLocal("cleanup_meta.bin", randomBytes(t, 2048))
	if err != nil {
		t.Fatalf("failed to store cleanup_meta.bin: %v", err)
	}
	metadataMeta := filepath.Join(ksMeta.storageDir, "metadata", fmt.Sprintf("%x.toml", fileMeta.MetaData.FileHash))
	chunkPath := fileMeta.References[0].Location
	if err := ksMeta.CleanupMetaData(); err != nil {
		t.Fatalf("CleanupMetaData failed: %v", err)
	}
	if _, err := os.Stat(metadataMeta); !os.IsNotExist(err) {
		t.Fatalf("expected metadata file to be removed, stat err: %v", err)
	}
	if _, err := os.Stat(chunkPath); err != nil {
		t.Fatalf("expected chunk file to remain after CleanupMetaData: %v", err)
	}
}

func TestVerifyFileReferencesMovesOrphanMetadataToCache(t *testing.T) {
	ks := newTestKeyStore(t)

	stored, err := ks.StoreFileLocal("orphan.bin", randomBytes(t, int(MinBlockSize*2)))
	if err != nil {
		t.Fatalf("failed to store file: %v", err)
	}
	if len(stored.References) < 1 {
		t.Fatal("expected at least one reference")
	}

	metadataPath := filepath.Join(ks.storageDir, "metadata", fmt.Sprintf("%x.toml", stored.MetaData.FileHash))
	cachePath := filepath.Join(ks.storageDir, ".cache", fmt.Sprintf("%x.toml", stored.MetaData.FileHash))

	if err := os.Remove(stored.References[0].Location); err != nil {
		t.Fatalf("failed to remove chunk file to simulate orphan: %v", err)
	}

	if err := ks.verifyFileReferences(); err != nil {
		t.Fatalf("verifyFileReferences failed: %v", err)
	}

	if _, err := os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatalf("expected metadata to be moved from original location, stat err: %v", err)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected metadata to be moved to cache, stat err: %v", err)
	}

	if _, err := ks.fileFromMemory(stored.MetaData.FileHash); err == nil {
		t.Fatal("expected orphaned file to be removed from in-memory map")
	}
	if _, err := ks.GetFileByName("orphan.bin"); err == nil {
		t.Fatal("expected orphaned filename to be removed from name index")
	}
}

func TestMoveToCacheDeduplicatesByHash(t *testing.T) {
	ks := newTestKeyStore(t)

	data := randomBytes(t, 2048)
	hash := sha256.Sum256(data)
	fileName := fmt.Sprintf("%x.toml", hash)
	cacheDir := filepath.Join(ks.storageDir, ".cache")
	metadataDir := filepath.Join(ks.storageDir, "metadata")

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		t.Fatalf("failed to create metadata dir: %v", err)
	}

	cachePath := filepath.Join(cacheDir, fileName)
	if err := os.WriteFile(cachePath, []byte("existing cache entry"), 0644); err != nil {
		t.Fatalf("failed to seed cache entry: %v", err)
	}

	sourcePath := filepath.Join(metadataDir, fileName)
	if err := os.WriteFile(sourcePath, []byte("duplicate metadata"), 0644); err != nil {
		t.Fatalf("failed to create duplicate source metadata: %v", err)
	}

	if err := ks.moveToCache(sourcePath); err != nil {
		t.Fatalf("moveToCache failed: %v", err)
	}

	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Fatalf("expected duplicate source metadata to be removed, stat err: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(cacheDir, fmt.Sprintf("%x*.toml", hash)))
	if err != nil {
		t.Fatalf("failed to glob cache entries: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 cache entry for hash %x, got %d (%v)", hash, len(matches), matches)
	}
}

func TestStoreFileLocalSkipsWhenHashAlreadyCached(t *testing.T) {
	ks := newTestKeyStore(t)

	data := randomBytes(t, 4096)
	hash, _ := seedCacheEntry(t, ks, "cached.bin", data, true)

	if _, err := ks.StoreFileLocal("cached.bin", data); err == nil {
		t.Fatal("expected StoreFileLocal to skip storing when hash exists in cache")
	} else if !errors.Is(err, ErrFileHashCached) {
		t.Fatalf("expected ErrFileHashCached, got: %v", err)
	}

	metadataPath := filepath.Join(ks.storageDir, "metadata", fmt.Sprintf("%x.toml", hash))
	if _, err := os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatalf("expected metadata file to not be created, stat err: %v", err)
	}
	if _, exists := ks.files[hash]; exists {
		t.Fatal("expected file to not be added to in-memory map when hash is cached")
	}
}

func TestStoreFileLocalWritesCacheEntry(t *testing.T) {
	ks := newTestKeyStore(t)

	data := randomBytes(t, 4096)
	stored, err := ks.StoreFileLocal("write_cache.bin", data)
	if err != nil {
		t.Fatalf("StoreFileLocal failed: %v", err)
	}

	cachePath := filepath.Join(ks.storageDir, ".cache", fmt.Sprintf("%x.toml", stored.MetaData.FileHash))
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cache entry to exist after store, stat err: %v", err)
	}
}

func TestLoadAndStoreFileLocalSkipsWhenHashAlreadyCached(t *testing.T) {
	ks := newTestKeyStore(t)

	input := randomBytes(t, int(MinBlockSize+123))
	inputPath := filepath.Join(t.TempDir(), "cached_input.bin")
	if err := os.WriteFile(inputPath, input, 0644); err != nil {
		t.Fatalf("failed to write input file: %v", err)
	}

	hash, _ := seedCacheEntry(t, ks, filepath.Base(inputPath), input, true)

	if _, err := ks.LoadAndStoreFileLocal(inputPath); err == nil {
		t.Fatal("expected LoadAndStoreFileLocal to skip storing when hash exists in cache")
	} else if !errors.Is(err, ErrFileHashCached) {
		t.Fatalf("expected ErrFileHashCached, got: %v", err)
	}

	metadataPath := filepath.Join(ks.storageDir, "metadata", fmt.Sprintf("%x.toml", hash))
	if _, err := os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatalf("expected metadata file to not be created, stat err: %v", err)
	}
	if _, exists := ks.files[hash]; exists {
		t.Fatal("expected file to not be added to in-memory map when hash is cached")
	}
}

func TestLoadAndStoreFileLocalPrunesDeadLocalCacheEntry(t *testing.T) {
	ks := newTestKeyStore(t)

	input := randomBytes(t, int(MinBlockSize+123))
	inputPath := filepath.Join(t.TempDir(), "stale_cache_input.bin")
	if err := os.WriteFile(inputPath, input, 0644); err != nil {
		t.Fatalf("failed to write input file: %v", err)
	}

	hash, cachePath := seedCacheEntry(t, ks, filepath.Base(inputPath), input, false)
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected seeded cache entry to exist, stat err: %v", err)
	}

	file, err := ks.LoadAndStoreFileLocal(inputPath)
	if err != nil {
		t.Fatalf("expected stale cache to be pruned and upload to proceed, got: %v", err)
	}
	if file == nil {
		t.Fatal("expected stored file result")
	}

	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cache entry to be recreated after successful store, stat err: %v", err)
	}
	if _, err := ks.GetFileByName(filepath.Base(inputPath)); err != nil {
		t.Fatalf("expected file to be indexed after stale cache pruning: %v", err)
	}
	if _, err := ks.fileFromMemory(hash); err != nil {
		t.Fatalf("expected file hash to be present in memory after store: %v", err)
	}
}

func TestInitKeyStoreDoesNotPruneStorageOnStartup(t *testing.T) {
	storageDir := filepath.Join(t.TempDir(), "storage")
	ks1 := newKeyStoreAt(t, storageDir)

	data := randomBytes(t, int(MinBlockSize+19))
	stored, err := ks1.StoreFileLocal("startup_no_prune.bin", data)
	if err != nil {
		t.Fatalf("failed to seed stored file: %v", err)
	}

	if len(stored.References) == 0 || stored.References[0] == nil {
		t.Fatal("expected at least one stored reference")
	}
	if err := os.Remove(stored.References[0].Location); err != nil {
		t.Fatalf("failed removing chunk to simulate stale local data: %v", err)
	}

	metadataPath := filepath.Join(storageDir, "metadata", fmt.Sprintf("%x.toml", stored.MetaData.FileHash))
	cachePath := filepath.Join(storageDir, ".cache", fmt.Sprintf("%x.toml", stored.MetaData.FileHash))
	if _, err := os.Stat(metadataPath); err != nil {
		t.Fatalf("expected metadata before restart, stat err: %v", err)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cache before restart, stat err: %v", err)
	}

	if _, err := InitKeyStoreWithConfig(KeyStoreConfig{
		StorageDir: storageDir,
		Verbose:    false,
	}); err != nil {
		t.Fatalf("failed to restart keystore: %v", err)
	}

	if _, err := os.Stat(metadataPath); err != nil {
		t.Fatalf("startup should not move/delete metadata, stat err: %v", err)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("startup should not prune cache entries, stat err: %v", err)
	}
}

func TestLoadAndStoreFileLocalReuploadsMissingDataAfterRestart(t *testing.T) {
	storageDir := filepath.Join(t.TempDir(), "storage")
	ks1 := newKeyStoreAt(t, storageDir)

	input := randomBytes(t, int(MinBlockSize*2+77))
	inputPath := filepath.Join(t.TempDir(), "restart_reupload.bin")
	if err := os.WriteFile(inputPath, input, 0644); err != nil {
		t.Fatalf("failed to write input file: %v", err)
	}

	first, err := ks1.LoadAndStoreFileLocal(inputPath)
	if err != nil {
		t.Fatalf("initial store failed: %v", err)
	}
	if len(first.References) == 0 {
		t.Fatal("expected initial store to create references")
	}

	kdhtFiles, err := filepath.Glob(filepath.Join(storageDir, "data", "*.kdht"))
	if err != nil {
		t.Fatalf("failed to list stored chunks: %v", err)
	}
	for _, path := range kdhtFiles {
		if err := os.Remove(path); err != nil {
			t.Fatalf("failed to remove chunk %s: %v", path, err)
		}
	}

	ks2 := newKeyStoreAt(t, storageDir)
	reuploaded, err := ks2.LoadAndStoreFileLocal(inputPath)
	if err != nil {
		t.Fatalf("expected reupload to proceed when data is missing, got: %v", err)
	}
	if reuploaded == nil {
		t.Fatal("expected reupload result")
	}

	for i, ref := range reuploaded.References {
		if ref == nil {
			t.Fatalf("expected reference %d after reupload", i)
		}
		if _, err := os.Stat(ref.Location); err != nil {
			t.Fatalf("expected chunk %d to exist after reupload, stat err: %v", i, err)
		}
	}
}

func TestUtilityFunctions(t *testing.T) {
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "src.bin")
	srcData := randomBytes(t, 1500)
	if err := os.WriteFile(srcPath, srcData, 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	hash, size, err := HashFile(srcPath)
	if err != nil {
		t.Fatalf("HashFile failed: %v", err)
	}
	if size != int64(len(srcData)) {
		t.Fatalf("HashFile size mismatch: got %d want %d", size, len(srcData))
	}
	if hash != sha256.Sum256(srcData) {
		t.Fatal("HashFile hash mismatch")
	}

	dstDir := filepath.Join(tmp, "dest")
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		t.Fatalf("failed to create destination dir: %v", err)
	}
	if err := CopyFile(srcPath, dstDir); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}
	copiedPath := filepath.Join(dstDir, filepath.Base(srcPath))
	copiedData, err := os.ReadFile(copiedPath)
	if err != nil {
		t.Fatalf("failed to read copied file: %v", err)
	}
	if !bytes.Equal(copiedData, srcData) {
		t.Fatal("CopyFile did not preserve content")
	}

	if !ValidateSHA256(srcData, copiedData) {
		t.Fatal("ValidateSHA256 should succeed for identical payloads")
	}
	if ValidateSHA256(srcData, append(copiedData, 0x01)) {
		t.Fatal("ValidateSHA256 should fail for different payloads")
	}

	if _, err := SliceToArray20(make([]byte, KeySize-1)); err == nil {
		t.Fatal("expected SliceToArray20 to fail for wrong length")
	}
	arr, err := SliceToArray20(make([]byte, KeySize))
	if err != nil {
		t.Fatalf("SliceToArray20 failed for valid length: %v", err)
	}
	if len(arr) != KeySize {
		t.Fatalf("expected key array length %d, got %d", KeySize, len(arr))
	}

	if len(ShaCheckSum(srcData, KeySize)) != KeySize {
		t.Fatalf("expected ShaCheckSum(KeySize) length %d", KeySize)
	}
	if len(ShaCheckSum(srcData, HashSize)) != HashSize {
		t.Fatalf("expected ShaCheckSum(HashSize) length %d", HashSize)
	}
	if len(ShaCheckSum(srcData, CryptoSize)) != CryptoSize {
		t.Fatalf("expected ShaCheckSum(CryptoSize) length %d", CryptoSize)
	}
	if len(ShaCheckSum(srcData, 13)) != KeySize {
		t.Fatalf("expected default ShaCheckSum length %d", KeySize)
	}
}

func TestPrepareMetaDataSecure(t *testing.T) {
	sig := [CryptoSize]byte{}
	copy(sig[:], randomBytes(t, CryptoSize))

	data := randomBytes(t, 987)
	md, err := PrepareMetaDataSecure("secure.bin", data, sig)
	if err != nil {
		t.Fatalf("PrepareMetaDataSecure failed: %v", err)
	}

	if md.FileName != "secure.bin" {
		t.Fatalf("unexpected filename: %s", md.FileName)
	}
	if md.TotalSize != uint64(len(data)) {
		t.Fatalf("unexpected total size: %d", md.TotalSize)
	}
	if md.Signature != sig {
		t.Fatal("signature mismatch")
	}
	if md.TTL != DefaultFileTTLSeconds {
		t.Fatalf("expected default secure TTL of 24h seconds, got %d", md.TTL)
	}
	if md.Modified == 0 {
		t.Fatal("expected modified timestamp to be set")
	}
}

func TestLoadAndStoreFileRemoteWithDefaultHandler(t *testing.T) {
	ks := newTestKeyStore(t)

	localPath := filepath.Join(t.TempDir(), "remote_input.bin")
	input := randomBytes(t, int(MinBlockSize+333))
	if err := os.WriteFile(localPath, input, 0644); err != nil {
		t.Fatalf("failed to write remote input: %v", err)
	}

	handler := &DefaultRemoteHandler{}

	type result struct {
		file *File
		err  error
	}
	done := make(chan result, 1)
	go func() {
		f, err := ks.LoadAndStoreFileRemote(localPath, handler)
		done <- result{file: f, err: err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("LoadAndStoreFileRemote failed: %v", res.err)
		}
		if res.file == nil || len(res.file.References) == 0 {
			t.Fatal("expected remote store to return file with references")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("LoadAndStoreFileRemote timed out")
	}

	if _, err := ks.GetFileByName(filepath.Base(localPath)); err != nil {
		t.Fatalf("expected remote-stored file to be indexed by name: %v", err)
	}
}

func TestStringAndFormattingHelpers(t *testing.T) {
	md := MetaData{
		FileName:    "fmt.bin",
		TotalSize:   2048,
		Modified:    time.Now().UnixNano(),
		Permissions: DEFAULT_PERMISSIONS,
		TTL:         60,
		BlockSize:   1024,
		TotalBlocks: 2,
	}
	copy(md.FileHash[:], ShaCheckSum([]byte("file-hash"), HashSize))

	ref := FileReference{
		FileName:  "fmt.bin",
		FileIndex: 0,
		Size:      1024,
		Location:  "/tmp/fmt.kdht",
		Protocol:  "file",
	}
	copy(ref.Key[:], ShaCheckSum([]byte("chunk-key"), KeySize))
	copy(ref.DataHash[:], ShaCheckSum([]byte("chunk-hash"), HashSize))
	copy(ref.Parent[:], md.FileHash[:])

	file := File{MetaData: md, References: []*FileReference{&ref}}

	if !strings.Contains(file.String(), "File {") {
		t.Fatal("File.String() missing expected prefix")
	}
	if !strings.Contains(ref.String(), "FileReference {") {
		t.Fatal("FileReference.String() missing expected prefix")
	}
	if !strings.Contains(md.String(), "MetaData {") {
		t.Fatal("MetaData.String() missing expected prefix")
	}
	if !strings.Contains(file.ShortString(), "fmt.bin") {
		t.Fatal("File.ShortString() missing filename")
	}
	if !strings.Contains(ref.ShortString(), "idx: 0") {
		t.Fatal("FileReference.ShortString() missing index")
	}
	if !strings.Contains(md.ShortString(), "fmt.bin") {
		t.Fatal("MetaData.ShortString() missing filename")
	}

	if got := formatBytes(999); got != "999 B" {
		t.Fatalf("formatBytes small value mismatch: %q", got)
	}
	if got := formatBytes(1024); !strings.Contains(got, "KiB") {
		t.Fatalf("formatBytes expected KiB unit, got %q", got)
	}
	if got := indent("a\nb"); got != "  a\n  b" {
		t.Fatalf("indent mismatch: %q", got)
	}
}

func TestStoreFileLocalErrorInjectionCleansChunks(t *testing.T) {
	ks := newTestKeyStore(t)

	metadataDir := filepath.Join(ks.storageDir, "metadata")
	if err := os.RemoveAll(metadataDir); err != nil {
		t.Fatalf("failed to remove metadata dir: %v", err)
	}
	if err := os.WriteFile(metadataDir, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("failed to inject metadata-dir error: %v", err)
	}

	_, err := ks.StoreFileLocal("inject_store_fail.bin", randomBytes(t, int(MinBlockSize*2)))
	if err == nil {
		t.Fatal("expected StoreFileLocal to fail when metadata directory is not a directory")
	}

	kdhtFiles, err := filepath.Glob(filepath.Join(ks.storageDir, "data", "*.kdht"))
	if err != nil {
		t.Fatalf("failed to list .kdht files: %v", err)
	}
	if len(kdhtFiles) != 0 {
		t.Fatalf("expected no orphaned .kdht files after failed store, found %d", len(kdhtFiles))
	}
	if len(ks.chunkIndex) != 0 {
		t.Fatalf("expected chunkIndex cleanup after failed store, found %d entries", len(ks.chunkIndex))
	}
}

func TestLoadAndStoreFileLocalErrorInjectionCleansChunks(t *testing.T) {
	ks := newTestKeyStore(t)

	inputPath := filepath.Join(t.TempDir(), "inject_load_fail.bin")
	if err := os.WriteFile(inputPath, randomBytes(t, int(MinBlockSize*2+321)), 0644); err != nil {
		t.Fatalf("failed to write input file: %v", err)
	}

	metadataDir := filepath.Join(ks.storageDir, "metadata")
	if err := os.RemoveAll(metadataDir); err != nil {
		t.Fatalf("failed to remove metadata dir: %v", err)
	}
	if err := os.WriteFile(metadataDir, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("failed to inject metadata-dir error: %v", err)
	}

	_, err := ks.LoadAndStoreFileLocal(inputPath)
	if err == nil {
		t.Fatal("expected LoadAndStoreFileLocal to fail when metadata directory is not a directory")
	}

	kdhtFiles, err := filepath.Glob(filepath.Join(ks.storageDir, "data", "*.kdht"))
	if err != nil {
		t.Fatalf("failed to list .kdht files: %v", err)
	}
	if len(kdhtFiles) != 0 {
		t.Fatalf("expected no orphaned .kdht files after failed load/store, found %d", len(kdhtFiles))
	}
	if len(ks.chunkIndex) != 0 {
		t.Fatalf("expected chunkIndex cleanup after failed load/store, found %d entries", len(ks.chunkIndex))
	}
}

func TestConcurrentStores(t *testing.T) {
	ks := newTestKeyStore(t)

	const workers = 8
	type input struct {
		name string
		data []byte
		hash [HashSize]byte
	}

	inputs := make([]input, workers)
	for i := 0; i < workers; i++ {
		data := randomBytes(t, int(MinBlockSize)+i*37+11)
		inputs[i] = input{
			name: fmt.Sprintf("concurrent_store_%d.bin", i),
			data: data,
			hash: sha256.Sum256(data),
		}
	}

	errCh := make(chan error, workers)
	var wg sync.WaitGroup

	for i := range inputs {
		in := inputs[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			stored, err := ks.StoreFileLocal(in.name, in.data)
			if err != nil {
				errCh <- fmt.Errorf("store failed for %s: %w", in.name, err)
				return
			}
			if stored.MetaData.FileHash != in.hash {
				errCh <- fmt.Errorf("hash mismatch for %s", in.name)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("%v", err)
		}
	}

	for _, in := range inputs {
		file, err := ks.GetFileByName(in.name)
		if err != nil {
			t.Fatalf("failed to lookup %s: %v", in.name, err)
		}

		var out bytes.Buffer
		if err := ks.StreamFile(file.MetaData.FileHash, &out); err != nil {
			t.Fatalf("stream failed for %s: %v", in.name, err)
		}
		if !bytes.Equal(out.Bytes(), in.data) {
			t.Fatalf("streamed payload mismatch for %s", in.name)
		}
	}
}

func TestConcurrentReads(t *testing.T) {
	ks := newTestKeyStore(t)

	data := randomBytes(t, int(MinBlockSize*3+99))
	stored, err := ks.StoreFileLocal("concurrent_reads.bin", data)
	if err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	const readers = 8
	errCh := make(chan error, readers)
	var wg sync.WaitGroup

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			var out bytes.Buffer
			if err := ks.StreamFile(stored.MetaData.FileHash, &out); err != nil {
				errCh <- fmt.Errorf("reader %d stream failed: %w", readerID, err)
				return
			}
			if !bytes.Equal(out.Bytes(), data) {
				errCh <- fmt.Errorf("reader %d got mismatched payload", readerID)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("%v", err)
		}
	}
}

func TestConcurrentStoreAndRead(t *testing.T) {
	ks := newTestKeyStore(t)

	const (
		storeCount = 12
		readers    = 4
	)

	type storedInput struct {
		name string
		data []byte
		hash [HashSize]byte
	}

	inputs := make([]storedInput, storeCount)
	for i := 0; i < storeCount; i++ {
		data := randomBytes(t, int(MinBlockSize)+i*53+7)
		inputs[i] = storedInput{
			name: fmt.Sprintf("mix_%d.bin", i),
			data: data,
			hash: sha256.Sum256(data),
		}
	}

	errCh := make(chan error, readers+storeCount)
	done := make(chan struct{})

	var readWG sync.WaitGroup
	for i := 0; i < readers; i++ {
		readWG.Add(1)
		go func(readerID int) {
			defer readWG.Done()
			for {
				select {
				case <-done:
					return
				default:
				}

				files := ks.ListKnownFiles()
				for _, md := range files {
					var out bytes.Buffer
					if err := ks.StreamFile(md.FileHash, &out); err != nil {
						errCh <- fmt.Errorf("reader %d stream failed: %w", readerID, err)
						return
					}
					if sha256.Sum256(out.Bytes()) != md.FileHash {
						errCh <- fmt.Errorf("reader %d hash mismatch for %s", readerID, md.FileName)
						return
					}
				}

				time.Sleep(2 * time.Millisecond)
			}
		}(i)
	}

	for _, in := range inputs {
		if _, err := ks.StoreFileLocal(in.name, in.data); err != nil {
			errCh <- fmt.Errorf("store failed for %s: %w", in.name, err)
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	close(done)
	readWG.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("%v", err)
		}
	}

	for _, in := range inputs {
		file, err := ks.GetFileByName(in.name)
		if err != nil {
			t.Fatalf("missing stored file %s: %v", in.name, err)
		}

		var out bytes.Buffer
		if err := ks.StreamFile(file.MetaData.FileHash, &out); err != nil {
			t.Fatalf("stream failed for %s: %v", in.name, err)
		}
		if !bytes.Equal(out.Bytes(), in.data) {
			t.Fatalf("payload mismatch for %s", in.name)
		}
	}
}

func TestConcurrentDeletes(t *testing.T) {
	ks := newTestKeyStore(t)

	const workers = 8
	type storedFile struct {
		name string
		hash [HashSize]byte
	}

	files := make([]storedFile, 0, workers)
	for i := 0; i < workers; i++ {
		name := fmt.Sprintf("delete_%d.bin", i)
		data := randomBytes(t, int(MinBlockSize)+i*19+5)
		stored, err := ks.StoreFileLocal(name, data)
		if err != nil {
			t.Fatalf("failed to seed %s: %v", name, err)
		}
		files = append(files, storedFile{name: name, hash: stored.MetaData.FileHash})
	}

	errCh := make(chan error, workers)
	var wg sync.WaitGroup

	for _, f := range files {
		file := f
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ks.DeleteFile(file.hash); err != nil {
				errCh <- fmt.Errorf("delete failed for %s: %w", file.name, err)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("%v", err)
		}
	}

	for _, f := range files {
		if _, err := ks.GetFileByHash(f.hash); err == nil {
			t.Fatalf("expected deleted hash lookup to fail: %s", f.name)
		}
		if _, err := ks.GetFileByName(f.name); err == nil {
			t.Fatalf("expected deleted name lookup to fail: %s", f.name)
		}

		var out bytes.Buffer
		if err := ks.StreamFile(f.hash, &out); err == nil {
			t.Fatalf("expected stream to fail for deleted file: %s", f.name)
		}
	}
}

func TestCrashRecoveryIntent(t *testing.T) {
	storageDir := filepath.Join(t.TempDir(), "storage")
	ks := newKeyStoreAt(t, storageDir)

	data := randomBytes(t, int(MinBlockSize*2+101))
	md, err := PrepareMetaData("crash_recovery.bin", data)
	if err != nil {
		t.Fatalf("PrepareMetaData failed: %v", err)
	}
	md.TTL = ks.config.DefaultTTLSeconds
	md.FileHash = sha256.Sum256(data)

	if err := ks.writeIntent(md); err != nil {
		t.Fatalf("writeIntent failed: %v", err)
	}

	for i := uint32(0); i < md.TotalBlocks; i++ {
		start := uint64(i) * uint64(md.BlockSize)
		end := min(start+uint64(md.BlockSize), md.TotalSize)
		key := computeChunkKey(md.FileHash, i)
		path := ks.GetLocalBlockLocation(key)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("failed to create chunk dir: %v", err)
		}
		if err := os.WriteFile(path, data[start:end], 0644); err != nil {
			t.Fatalf("failed to seed chunk: %v", err)
		}
	}

	seeded, err := filepath.Glob(filepath.Join(storageDir, "data", "*.kdht"))
	if err != nil {
		t.Fatalf("failed to list seeded chunks: %v", err)
	}
	if len(seeded) == 0 {
		t.Fatal("expected seeded orphaned chunks")
	}

	_, err = InitKeyStoreWithConfig(KeyStoreConfig{StorageDir: storageDir, Verbose: false})
	if err != nil {
		t.Fatalf("restart failed: %v", err)
	}

	recovered, err := filepath.Glob(filepath.Join(storageDir, "data", "*.kdht"))
	if err != nil {
		t.Fatalf("failed to list chunks after recovery: %v", err)
	}
	if len(recovered) != 0 {
		t.Fatalf("expected orphaned chunks to be removed, found %d", len(recovered))
	}

	intents, err := filepath.Glob(filepath.Join(storageDir, ".intents", "*.json"))
	if err != nil {
		t.Fatalf("failed to list intent files: %v", err)
	}
	if len(intents) != 0 {
		t.Fatalf("expected recovered intent files to be removed, found %d", len(intents))
	}
}

func TestIntentRecoveryDoesNotDeleteCommittedFile(t *testing.T) {
	storageDir := filepath.Join(t.TempDir(), "storage")
	ks := newKeyStoreAt(t, storageDir)

	data := randomBytes(t, int(MinBlockSize*2+57))
	stored, err := ks.StoreFileLocal("committed.bin", data)
	if err != nil {
		t.Fatalf("StoreFileLocal failed: %v", err)
	}

	if err := ks.writeIntent(stored.MetaData); err != nil {
		t.Fatalf("failed to seed stale intent: %v", err)
	}

	ksReloaded, err := InitKeyStoreWithConfig(KeyStoreConfig{StorageDir: storageDir, Verbose: false})
	if err != nil {
		t.Fatalf("restart failed: %v", err)
	}

	var out bytes.Buffer
	if err := ksReloaded.StreamFile(stored.MetaData.FileHash, &out); err != nil {
		t.Fatalf("committed file should still be readable: %v", err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Fatal("reloaded committed file content mismatch")
	}

	intents, err := filepath.Glob(filepath.Join(storageDir, ".intents", "*.json"))
	if err != nil {
		t.Fatalf("failed to list intent files: %v", err)
	}
	if len(intents) != 0 {
		t.Fatalf("expected stale intent to be removed, found %d", len(intents))
	}
}

func TestVerifyAllDetectsCorruption(t *testing.T) {
	ks := newTestKeyStore(t)

	data := randomBytes(t, int(MinBlockSize*3+23))
	stored, err := ks.StoreFileLocal("verify_corruption.bin", data)
	if err != nil {
		t.Fatalf("StoreFileLocal failed: %v", err)
	}
	if len(stored.References) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(stored.References))
	}

	ref0 := stored.References[0]
	corrupt0 := randomBytes(t, int(ref0.Size))
	if err := os.WriteFile(ref0.Location, corrupt0, 0644); err != nil {
		t.Fatalf("failed to corrupt chunk 0: %v", err)
	}

	ref1 := stored.References[1]
	truncatedLen := int(ref1.Size / 2)
	if truncatedLen == 0 {
		truncatedLen = 1
	}
	if err := os.WriteFile(ref1.Location, randomBytes(t, truncatedLen), 0644); err != nil {
		t.Fatalf("failed to truncate chunk 1: %v", err)
	}

	ref2 := stored.References[2]
	if err := os.Remove(ref2.Location); err != nil {
		t.Fatalf("failed to delete chunk 2: %v", err)
	}

	errs := ks.VerifyAll()
	if len(errs) != 3 {
		t.Fatalf("expected 3 integrity errors, got %d: %+v", len(errs), errs)
	}

	msgByChunk := make(map[uint32]string, len(errs))
	for _, ce := range errs {
		msgByChunk[ce.ChunkIndex] = ce.Err.Error()
	}

	if msg, ok := msgByChunk[0]; !ok || !strings.Contains(msg, "hash mismatch") {
		t.Fatalf("expected hash mismatch for chunk 0, got %q", msg)
	}
	if msg, ok := msgByChunk[1]; !ok || !strings.Contains(msg, "size mismatch") {
		t.Fatalf("expected size mismatch for chunk 1, got %q", msg)
	}
	if msg, ok := msgByChunk[2]; !ok || !strings.Contains(msg, "missing file") {
		t.Fatalf("expected missing file for chunk 2, got %q", msg)
	}
}

func TestVerifyAllCleanStore(t *testing.T) {
	ks := newTestKeyStore(t)

	data := randomBytes(t, int(MinBlockSize*2+17))
	if _, err := ks.StoreFileLocal("verify_clean.bin", data); err != nil {
		t.Fatalf("StoreFileLocal failed: %v", err)
	}

	errs := ks.VerifyAll()
	if len(errs) != 0 {
		t.Fatalf("expected no integrity errors, got %d: %+v", len(errs), errs)
	}
}
