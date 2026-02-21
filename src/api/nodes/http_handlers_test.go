package nodes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestServerNode_HTTP_UploadAndList(t *testing.T) {
	dir, _ := os.MkdirTemp("", "http-test-*")
	defer os.RemoveAll(dir)

	sn, err := NewServerNode([]byte("http-server-node-id!"), "localhost:0", dir)
	if err != nil {
		t.Fatal(err)
	}

	// Upload
	data := "http upload test data"
	body := strings.NewReader(data)
	req := httptest.NewRequest("PUT", "/files/test.txt", body)
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Length", strconv.Itoa(len(data)))
	w := httptest.NewRecorder()
	sn.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "hash") {
		t.Fatalf("upload response missing hash: %s", w.Body.String())
	}

	// List
	req = httptest.NewRequest("GET", "/files", nil)
	w = httptest.NewRecorder()
	sn.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "test.txt") {
		t.Fatalf("expected test.txt in list, got %s", w.Body.String())
	}
}

func TestServerNode_HTTP_DownloadByName(t *testing.T) {
	dir, _ := os.MkdirTemp("", "http-test-*")
	defer os.RemoveAll(dir)

	sn, _ := NewServerNode([]byte("http-server-node-id!"), "localhost:0", dir)

	// Upload first
	dlData := "download me by name"
	body := strings.NewReader(dlData)
	req := httptest.NewRequest("PUT", "/files/dl-test.txt", body)
	req.ContentLength = int64(len(dlData))
	req.Header.Set("Content-Length", strconv.Itoa(len(dlData)))
	w := httptest.NewRecorder()
	sn.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d %s", w.Code, w.Body.String())
	}

	// Download by name
	req = httptest.NewRequest("GET", "/files/dl-test.txt", nil)
	w = httptest.NewRecorder()
	sn.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("download: expected 200, got %d", w.Code)
	}
	if w.Body.String() != "download me by name" {
		t.Fatalf("expected 'download me by name', got %q", w.Body.String())
	}
}
