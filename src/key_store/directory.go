package key_store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	logs "github.com/danmuck/smplog"
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

// renameFile updates the filename in both in-memory indexes and persists the
// updated metadata to disk. The caller must not hold ks.lock.
func (ks *KeyStore) renameFile(fileHash [HashSize]byte, newName string) error {
	ks.lock.Lock()
	file, exists := ks.files[fileHash]
	if !exists {
		ks.lock.Unlock()
		return fmt.Errorf("file not found for hash %x", fileHash)
	}
	oldName := file.MetaData.FileName
	if oldName == newName {
		ks.lock.Unlock()
		return nil
	}
	delete(ks.filesByName, oldName)
	file.MetaData.FileName = newName
	ks.filesByName[newName] = fileHash
	ks.lock.Unlock()

	// re-persist with updated name
	return ks.fileToMemory(file)
}

// StoreDirectory recursively stores a directory tree and returns the manifest hash.
func (ks *KeyStore) StoreDirectory(rootPath string) ([HashSize]byte, error) {
	rootPath = filepath.Clean(rootPath)
	info, err := os.Stat(rootPath)
	if err != nil {
		return [HashSize]byte{}, fmt.Errorf("stat root: %w", err)
	}
	if !info.IsDir() {
		return [HashSize]byte{}, fmt.Errorf("%s is not a directory", rootPath)
	}
	return ks.storeDirectoryRecursive(rootPath, rootPath)
}

// storeDirectoryRecursive processes a single directory level.
func (ks *KeyStore) storeDirectoryRecursive(dirPath, rootPath string) ([HashSize]byte, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return [HashSize]byte{}, fmt.Errorf("read dir %s: %w", dirPath, err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var children []DirectoryEntry

	for _, entry := range entries {
		childPath := filepath.Join(dirPath, entry.Name())
		relPath, err := filepath.Rel(rootPath, childPath)
		if err != nil {
			return [HashSize]byte{}, fmt.Errorf("rel path: %w", err)
		}
		relPath = filepath.ToSlash(relPath)

		if entry.IsDir() {
			subHash, err := ks.storeDirectoryRecursive(childPath, rootPath)
			if err != nil {
				return [HashSize]byte{}, err
			}
			children = append(children, DirectoryEntry{
				Name: entry.Name(),
				Path: relPath,
				Hash: subHash,
				Type: "directory",
				Size: 0,
			})
		} else {
			file, err := ks.LoadAndStoreFileLocal(childPath)
			if err != nil {
				return [HashSize]byte{}, fmt.Errorf("store file %s: %w", relPath, err)
			}
			// Rename to relative path
			if err := ks.renameFile(file.MetaData.FileHash, relPath); err != nil {
				return [HashSize]byte{}, fmt.Errorf("rename %s: %w", relPath, err)
			}
			children = append(children, DirectoryEntry{
				Name: entry.Name(),
				Path: relPath,
				Hash: file.MetaData.FileHash,
				Type: "file",
				Size: file.MetaData.TotalSize,
			})
		}
	}

	// Build and store manifest
	dirRelPath, err := filepath.Rel(rootPath, dirPath)
	if err != nil {
		return [HashSize]byte{}, fmt.Errorf("rel path for dir: %w", err)
	}
	dirRelPath = filepath.ToSlash(dirRelPath)
	if dirRelPath == "." {
		dirRelPath = filepath.Base(rootPath)
	}

	manifest := DirectoryManifest{
		Path:     dirRelPath,
		Children: children,
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return [HashSize]byte{}, fmt.Errorf("marshal manifest: %w", err)
	}

	manifestFile, err := ks.StoreFileLocal(dirRelPath, manifestData)
	if err != nil {
		return [HashSize]byte{}, fmt.Errorf("store manifest: %w", err)
	}
	manifestHash := manifestFile.MetaData.FileHash

	// Mark as directory
	ks.lock.Lock()
	if stored, ok := ks.files[manifestHash]; ok {
		stored.MetaData.EntryType = "directory"
	}
	ks.lock.Unlock()
	if err := ks.fileToMemory(manifestFile); err != nil {
		return [HashSize]byte{}, fmt.Errorf("persist directory metadata: %w", err)
	}
	// Update the returned file copy too
	manifestFile.MetaData.EntryType = "directory"

	// Set ParentHash on children
	for _, child := range children {
		ks.lock.Lock()
		if stored, ok := ks.files[child.Hash]; ok {
			stored.MetaData.ParentHash = manifestHash
		}
		ks.lock.Unlock()
		// Persist child with updated parent
		childFile, err := ks.GetFileByHash(child.Hash)
		if err != nil {
			logs.Warnf("failed to get child %s for parent update: %v", child.Path, err)
			continue
		}
		childFile.MetaData.ParentHash = manifestHash
		if err := ks.fileToMemory(childFile); err != nil {
			logs.Warnf("failed to persist parent hash for %s: %v", child.Path, err)
		}
	}

	return manifestHash, nil
}

// ListDirectory reads a directory manifest and returns its children.
func (ks *KeyStore) ListDirectory(dirHash [HashSize]byte) ([]DirectoryEntry, error) {
	data, err := ks.ReassembleFileToBytes(dirHash)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var manifest DirectoryManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return manifest.Children, nil
}

// ReassembleDirectory reconstructs a directory tree on disk from its manifest hash.
func (ks *KeyStore) ReassembleDirectory(dirHash [HashSize]byte, outputRoot string) error {
	children, err := ks.ListDirectory(dirHash)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	for _, child := range children {
		childOutput := filepath.Join(outputRoot, child.Name)
		if child.Type == "directory" {
			if err := ks.ReassembleDirectory(child.Hash, childOutput); err != nil {
				return fmt.Errorf("reassemble dir %s: %w", child.Path, err)
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(childOutput), 0o755); err != nil {
				return fmt.Errorf("create parent dir: %w", err)
			}
			if err := ks.ReassembleFileToPath(child.Hash, childOutput); err != nil {
				return fmt.Errorf("reassemble file %s: %w", child.Path, err)
			}
		}
	}
	return nil
}
