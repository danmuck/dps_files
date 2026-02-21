package key_store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danmuck/dps_files/src/api/ledgers"
)

func TestKeyStoreImplementsFileLedger(t *testing.T) {
	dir, _ := os.MkdirTemp("", "fl-test-*")
	defer os.RemoveAll(dir)
	ks, err := InitKeyStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	var _ ledgers.FileLedger = (*KeyStoreLedger)(nil)

	fl := NewFileLedger(ks)

	data := []byte("hello file ledger")
	fid, err := fl.StoreFileLocal("test.txt", data)
	if err != nil {
		t.Fatalf("StoreFileLocal: %v", err)
	}
	if fid == (ledgers.FileID{}) {
		t.Fatal("expected non-zero FileID")
	}

	files, err := fl.ListKnownFiles()
	if err != nil {
		t.Fatalf("ListKnownFiles: %v", err)
	}
	if len(files) != 1 || files[0] != fid {
		t.Fatalf("expected [%x], got %v", fid, files)
	}

	got, err := fl.ReassembleFileToBytes(fid)
	if err != nil {
		t.Fatalf("ReassembleFileToBytes: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("expected %q, got %q", data, got)
	}

	err = fl.DeleteFile(fid)
	if err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	files, _ = fl.ListKnownFiles()
	if len(files) != 0 {
		t.Fatalf("expected empty after delete, got %d", len(files))
	}
}

func TestFileLedgerDirectoryRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	storageDir := filepath.Join(tmpDir, "storage")
	testRoot := filepath.Join(tmpDir, "upload")
	outputRoot := filepath.Join(tmpDir, "output")

	os.MkdirAll(filepath.Join(testRoot, "sub"), 0o755)
	os.WriteFile(filepath.Join(testRoot, "file.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(testRoot, "sub", "nested.txt"), []byte("world"), 0o644)

	ks, _ := InitKeyStoreWithConfig(KeyStoreConfig{
		StorageDir: storageDir, DefaultTTLSeconds: 3600,
	})
	fl := NewFileLedger(ks)

	dirID, err := fl.StoreDirectory(testRoot)
	if err != nil {
		t.Fatalf("StoreDirectory: %v", err)
	}

	entries, err := fl.ListDirectory(dirID)
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	err = fl.ReassembleDirectory(dirID, outputRoot)
	if err != nil {
		t.Fatalf("ReassembleDirectory: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(outputRoot, "sub", "nested.txt"))
	if string(got) != "world" {
		t.Errorf("nested.txt = %q, want %q", got, "world")
	}
}
