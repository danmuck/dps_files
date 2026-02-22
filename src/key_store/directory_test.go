package key_store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
		isErr bool
	}{
		{"src/main.go", "src/main.go", false},
		{"./src/main.go", "src/main.go", false},
		{"src/api/", "src/api/", false},
		{"src\\api\\main.go", "src/api/main.go", false},
		{"../escape.txt", "", true},
		{"src/../escape.txt", "escape.txt", false},
		{"", "", true},
		{".", "", true},
		{"./", "", true},
	}
	for _, tt := range tests {
		got, err := NormalizePath(tt.input)
		if tt.isErr {
			if err == nil {
				t.Errorf("NormalizePath(%q) expected error, got %q", tt.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizePath(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStoreDirectoryRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	storageDir := filepath.Join(tmpDir, "storage")
	testRoot := filepath.Join(tmpDir, "upload")
	os.MkdirAll(filepath.Join(testRoot, "sub"), 0o755)
	os.WriteFile(filepath.Join(testRoot, "root.txt"), []byte("root file content"), 0o644)
	os.WriteFile(filepath.Join(testRoot, "sub", "nested.txt"), []byte("nested file content"), 0o644)

	ks, err := InitKeyStoreWithConfig(KeyStoreConfig{
		StorageDir: storageDir, DefaultTTLSeconds: 3600,
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	dirHash, err := ks.StoreDirectory(testRoot)
	if err != nil {
		t.Fatalf("StoreDirectory: %v", err)
	}

	// Verify manifest is a directory
	dirFile, err := ks.GetFileByHash(dirHash)
	if err != nil {
		t.Fatalf("GetFileByHash: %v", err)
	}
	if !dirFile.MetaData.IsDirectory() {
		t.Error("expected directory entry type")
	}

	// Verify files stored with relative paths
	_, err = ks.GetFileByName("root.txt")
	if err != nil {
		t.Errorf("GetFileByName(root.txt): %v", err)
	}
	_, err = ks.GetFileByName("sub/nested.txt")
	if err != nil {
		t.Errorf("GetFileByName(sub/nested.txt): %v", err)
	}
}

func TestListDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	storageDir := filepath.Join(tmpDir, "storage")
	testRoot := filepath.Join(tmpDir, "upload")
	os.MkdirAll(filepath.Join(testRoot, "sub"), 0o755)
	os.WriteFile(filepath.Join(testRoot, "a.txt"), []byte("aaa"), 0o644)
	os.WriteFile(filepath.Join(testRoot, "sub", "b.txt"), []byte("bbb"), 0o644)

	ks, _ := InitKeyStoreWithConfig(KeyStoreConfig{
		StorageDir: storageDir, DefaultTTLSeconds: 3600,
	})
	dirHash, _ := ks.StoreDirectory(testRoot)

	entries, err := ks.ListDirectory(dirHash)
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestReassembleDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	storageDir := filepath.Join(tmpDir, "storage")
	testRoot := filepath.Join(tmpDir, "upload")
	outputRoot := filepath.Join(tmpDir, "output")

	os.MkdirAll(filepath.Join(testRoot, "sub"), 0o755)
	os.WriteFile(filepath.Join(testRoot, "root.txt"), []byte("root file"), 0o644)
	os.WriteFile(filepath.Join(testRoot, "sub", "nested.txt"), []byte("nested file"), 0o644)

	ks, _ := InitKeyStoreWithConfig(KeyStoreConfig{
		StorageDir: storageDir, DefaultTTLSeconds: 3600,
	})
	dirHash, _ := ks.StoreDirectory(testRoot)

	err := ks.ReassembleDirectory(dirHash, outputRoot)
	if err != nil {
		t.Fatalf("ReassembleDirectory: %v", err)
	}

	gotRoot, _ := os.ReadFile(filepath.Join(outputRoot, "root.txt"))
	if string(gotRoot) != "root file" {
		t.Errorf("root.txt = %q, want %q", gotRoot, "root file")
	}
	gotNested, _ := os.ReadFile(filepath.Join(outputRoot, "sub", "nested.txt"))
	if string(gotNested) != "nested file" {
		t.Errorf("sub/nested.txt = %q, want %q", gotNested, "nested file")
	}
}

func TestDuplicateBasenamesInDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	storageDir := filepath.Join(tmpDir, "storage")
	testRoot := filepath.Join(tmpDir, "upload")

	os.MkdirAll(filepath.Join(testRoot, "a"), 0o755)
	os.MkdirAll(filepath.Join(testRoot, "b"), 0o755)
	os.WriteFile(filepath.Join(testRoot, "a", "main.go"), []byte("package a"), 0o644)
	os.WriteFile(filepath.Join(testRoot, "b", "main.go"), []byte("package b"), 0o644)

	ks, _ := InitKeyStoreWithConfig(KeyStoreConfig{
		StorageDir: storageDir, DefaultTTLSeconds: 3600,
	})
	_, err := ks.StoreDirectory(testRoot)
	if err != nil {
		t.Fatalf("StoreDirectory: %v", err)
	}

	fileA, err := ks.GetFileByName("a/main.go")
	if err != nil {
		t.Fatalf("GetFileByName(a/main.go): %v", err)
	}
	fileB, err := ks.GetFileByName("b/main.go")
	if err != nil {
		t.Fatalf("GetFileByName(b/main.go): %v", err)
	}
	if fileA.MetaData.FileHash == fileB.MetaData.FileHash {
		t.Error("files with different content should have different hashes")
	}
}

func TestDeepNesting(t *testing.T) {
	tmpDir := t.TempDir()
	storageDir := filepath.Join(tmpDir, "storage")
	testRoot := filepath.Join(tmpDir, "upload")
	outputRoot := filepath.Join(tmpDir, "output")

	deepPath := filepath.Join(testRoot, "a", "b", "c", "d")
	os.MkdirAll(deepPath, 0o755)
	os.WriteFile(filepath.Join(deepPath, "deep.txt"), []byte("deep"), 0o644)

	ks, _ := InitKeyStoreWithConfig(KeyStoreConfig{
		StorageDir: storageDir, DefaultTTLSeconds: 3600,
	})
	dirHash, err := ks.StoreDirectory(testRoot)
	if err != nil {
		t.Fatalf("StoreDirectory: %v", err)
	}

	err = ks.ReassembleDirectory(dirHash, outputRoot)
	if err != nil {
		t.Fatalf("ReassembleDirectory: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outputRoot, "a", "b", "c", "d", "deep.txt"))
	if err != nil {
		t.Fatalf("read deep.txt: %v", err)
	}
	if string(got) != "deep" {
		t.Errorf("deep.txt = %q, want %q", got, "deep")
	}
}

func TestDirectoryTotalSizeFlat(t *testing.T) {
	tmpDir := t.TempDir()
	storageDir := filepath.Join(tmpDir, "storage")
	testRoot := filepath.Join(tmpDir, "upload")
	os.MkdirAll(testRoot, 0o755)
	os.WriteFile(filepath.Join(testRoot, "a.txt"), []byte("hello"), 0o644)     // 5 bytes
	os.WriteFile(filepath.Join(testRoot, "b.txt"), []byte("world!!"), 0o644)   // 7 bytes

	ks, _ := InitKeyStoreWithConfig(KeyStoreConfig{StorageDir: storageDir, DefaultTTLSeconds: 3600})
	dirHash, err := ks.StoreDirectory(testRoot)
	if err != nil {
		t.Fatalf("StoreDirectory: %v", err)
	}

	dirFile, err := ks.GetFileByHash(dirHash)
	if err != nil {
		t.Fatalf("GetFileByHash: %v", err)
	}
	const want = uint64(5 + 7)
	if dirFile.MetaData.ContentSize != want {
		t.Errorf("MetaData.ContentSize = %d, want %d (combined file content size)", dirFile.MetaData.ContentSize, want)
	}
}

func TestDirectoryTotalSizeNested(t *testing.T) {
	tmpDir := t.TempDir()
	storageDir := filepath.Join(tmpDir, "storage")
	testRoot := filepath.Join(tmpDir, "upload")
	os.MkdirAll(filepath.Join(testRoot, "sub"), 0o755)
	os.WriteFile(filepath.Join(testRoot, "root.txt"), []byte("rootfile"), 0o644) // 8 bytes
	os.WriteFile(filepath.Join(testRoot, "sub", "nested.txt"), []byte("nestedfile"), 0o644) // 10 bytes

	ks, _ := InitKeyStoreWithConfig(KeyStoreConfig{StorageDir: storageDir, DefaultTTLSeconds: 3600})
	dirHash, err := ks.StoreDirectory(testRoot)
	if err != nil {
		t.Fatalf("StoreDirectory: %v", err)
	}

	dirFile, err := ks.GetFileByHash(dirHash)
	if err != nil {
		t.Fatalf("GetFileByHash: %v", err)
	}
	const want = uint64(8 + 10)
	if dirFile.MetaData.ContentSize != want {
		t.Errorf("root MetaData.ContentSize = %d, want %d (recursive combined size)", dirFile.MetaData.ContentSize, want)
	}
}

func TestDirectoryEntrySubdirSize(t *testing.T) {
	tmpDir := t.TempDir()
	storageDir := filepath.Join(tmpDir, "storage")
	testRoot := filepath.Join(tmpDir, "upload")
	os.MkdirAll(filepath.Join(testRoot, "sub"), 0o755)
	os.WriteFile(filepath.Join(testRoot, "top.txt"), []byte("topfile"), 0o644)       // 7 bytes
	os.WriteFile(filepath.Join(testRoot, "sub", "deep.txt"), []byte("deepfile"), 0o644) // 8 bytes

	ks, _ := InitKeyStoreWithConfig(KeyStoreConfig{StorageDir: storageDir, DefaultTTLSeconds: 3600})
	dirHash, err := ks.StoreDirectory(testRoot)
	if err != nil {
		t.Fatalf("StoreDirectory: %v", err)
	}

	entries, err := ks.ListDirectory(dirHash)
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}

	var subdirEntry *DirectoryEntry
	for i := range entries {
		if entries[i].Type == "directory" {
			subdirEntry = &entries[i]
		}
	}
	if subdirEntry == nil {
		t.Fatal("no directory entry found in listing")
	}
	const want = uint64(8)
	if subdirEntry.Size != want {
		t.Errorf("subdir DirectoryEntry.Size = %d, want %d (combined size of subdir contents)", subdirEntry.Size, want)
	}
}

func TestEmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	storageDir := filepath.Join(tmpDir, "storage")
	testRoot := filepath.Join(tmpDir, "upload")
	os.MkdirAll(testRoot, 0o755)

	ks, _ := InitKeyStoreWithConfig(KeyStoreConfig{
		StorageDir: storageDir, DefaultTTLSeconds: 3600,
	})
	dirHash, err := ks.StoreDirectory(testRoot)
	if err != nil {
		t.Fatalf("StoreDirectory: %v", err)
	}

	entries, err := ks.ListDirectory(dirHash)
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for empty dir, got %d", len(entries))
	}
}
