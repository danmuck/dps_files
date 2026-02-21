package key_store

import (
	"os"
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
