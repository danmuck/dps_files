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
