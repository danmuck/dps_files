package nodes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClientNode_LocalMode(t *testing.T) {
	dir, _ := os.MkdirTemp("", "cn-local-*")
	defer os.RemoveAll(dir)

	cn, err := NewClientNode([]byte("local-client-node-!!"), WithLocalStorage(dir))
	if err != nil {
		t.Fatal(err)
	}
	// Compile-time interface check.
	var _ ClientNode = cn

	if err := cn.Start(); err != nil {
		t.Fatal(err)
	}
	defer cn.Shutdown()

	if cn.LocalServer() == nil {
		t.Fatal("expected non-nil local server in local mode")
	}
	if cn.LocalServer().Storage() == nil {
		t.Fatal("expected non-nil storage on local server")
	}
}

func TestClientNode_RemoteMode(t *testing.T) {
	cn, err := NewClientNode([]byte("remote-client-node!!"), WithRemotes("localhost:9999"))
	if err != nil {
		t.Fatal(err)
	}
	var _ ClientNode = cn

	if err := cn.Start(); err != nil {
		t.Fatal(err)
	}
	defer cn.Shutdown()

	if cn.LocalServer() != nil {
		t.Fatal("expected nil local server in remote mode")
	}
}

func TestClientNode_LocalUploadAndList(t *testing.T) {
	dir, _ := os.MkdirTemp("", "cn-ul-*")
	defer os.RemoveAll(dir)

	cn, err := NewClientNode([]byte("local-upload-node-!!"), WithLocalStorage(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := cn.Start(); err != nil {
		t.Fatal(err)
	}
	defer cn.Shutdown()

	storage := cn.LocalServer().Storage()

	// Upload via storage ledger.
	fid, err := storage.StoreFileLocal("hello.txt", []byte("hello from client node"))
	if err != nil {
		t.Fatalf("StoreFileLocal: %v", err)
	}
	if len(fid) != 32 {
		t.Fatalf("expected 32-byte file hash, got %d bytes", len(fid))
	}

	// List via convenience method.
	ids, err := cn.ListLocal()
	if err != nil {
		t.Fatalf("ListLocal: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 file, got %d", len(ids))
	}

	// Download via storage ledger.
	data, err := storage.ReassembleFileToBytes(fid)
	if err != nil {
		t.Fatalf("ReassembleFileToBytes: %v", err)
	}
	if string(data) != "hello from client node" {
		t.Fatalf("expected 'hello from client node', got %q", data)
	}

	// Delete via storage ledger.
	if err := storage.DeleteFile(fid); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}

	// Verify empty after delete.
	ids, err = cn.ListLocal()
	if err != nil {
		t.Fatalf("ListLocal after delete: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 files after delete, got %d", len(ids))
	}
}

func TestClientNode_UploadMethodReadsFile(t *testing.T) {
	dir, _ := os.MkdirTemp("", "cn-upload-*")
	defer os.RemoveAll(dir)

	// Create a test file to upload.
	testFile := filepath.Join(dir, "test_input.txt")
	if err := os.WriteFile(testFile, []byte("upload test data"), 0644); err != nil {
		t.Fatal(err)
	}

	storageDir, _ := os.MkdirTemp("", "cn-storage-*")
	defer os.RemoveAll(storageDir)

	cn, err := NewClientNode([]byte("upload-method-node!!"), WithLocalStorage(storageDir))
	if err != nil {
		t.Fatal(err)
	}
	if err := cn.Start(); err != nil {
		t.Fatal(err)
	}
	defer cn.Shutdown()

	// Upload directly to the local server's storage ledger.
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	_, err = cn.LocalServer().Storage().StoreFileLocal("test_input.txt", data)
	if err != nil {
		t.Fatalf("StoreFileLocal: %v", err)
	}

	ids, err := cn.ListLocal()
	if err != nil {
		t.Fatalf("ListLocal: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("expected at least one file after upload")
	}
}

func TestClientNode_ListLocalNoServer(t *testing.T) {
	cn, err := NewClientNode([]byte("no-server-list-node!"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cn.Start(); err != nil {
		t.Fatal(err)
	}
	defer cn.Shutdown()

	_, err = cn.ListLocal()
	if err == nil {
		t.Fatal("expected error when no local server configured")
	}
}
