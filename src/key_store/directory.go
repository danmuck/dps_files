package key_store

import (
	"fmt"
	"path/filepath"
	"strings"
)

// DirectoryEntry represents a single child in a directory manifest.
type DirectoryEntry struct {
	Name string         `json:"name"` // basename (e.g. "main.go")
	Path string         `json:"path"` // full relative path (e.g. "src/api/main.go")
	Hash [HashSize]byte `json:"hash"` // file hash or manifest hash
	Type string         `json:"type"` // "file" or "directory"
	Size uint64         `json:"size"` // file size; 0 for directories
}

// DirectoryManifest is the JSON blob stored as a directory's chunked data.
type DirectoryManifest struct {
	Path     string           `json:"path"`
	Children []DirectoryEntry `json:"children"`
}

// NormalizePath cleans and validates a relative path for storage.
// Forward slashes only, no ./ prefix, no .. traversal, non-empty.
func NormalizePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	// normalize separators (explicit replace handles all platforms)
	p = strings.ReplaceAll(p, "\\", "/")
	// clean the path (resolves . and ..)
	cleaned := filepath.ToSlash(filepath.Clean(p))
	// reject traversal (only paths that escape above root)
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("path traversal not allowed: %q", p)
	}
	// strip leading ./
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "" || cleaned == "." {
		return "", fmt.Errorf("empty path after normalization")
	}
	// preserve trailing slash for directories
	if strings.HasSuffix(p, "/") && !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}
	return cleaned, nil
}
