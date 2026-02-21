# Directory Semantics & Phase 1C/2A Cleanup — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Clean up Phase 1C/2A technical debt, then add first-class directory support (recursive upload/download, directory manifests, path-aware indexing, directory browsing) to the storage system.

**Architecture:** Phase 1C cleans the foundation (smplog, shared chunking helper, PRINT_BLOCKS removal). Directory semantics adds `EntryType` and `ParentHash` to MetaData, stores directory manifests as chunked JSON blobs, and extends KeyStore/FileLedger/ServerNode/TUI with directory-aware operations.

**Tech Stack:** Go, TOML (metadata), JSON (directory manifests), Protobuf (RPC), smplog (logging)

---

## Part A: Phase 1C/2A Cleanup

### Task 1: Replace fmt.Printf with smplog in key_store library

**Files:**
- Modify: `src/key_store/files.go` (lines 124-127, 434-438, 452-454, 467-470, 505-511, 526-536)
- Modify: `src/key_store/key_store.go` (lines 218-223, 857, 867)

**Step 1: Replace all fmt.Printf calls in files.go with smplog equivalents**

In `files.go`, replace every `fmt.Printf(...)` under `ks.config.Verbose` guards with `logs.Debugf(...)`. These are chunk-level progress messages that belong at debug level.

Lines 124-127 (StoreFileLocal chunking progress):
```go
// Before:
fmt.Printf("Stored block %d/%d (%.1f%%)\n",
    i+1, metadata.TotalBlocks, float64(i+1)/float64(metadata.TotalBlocks)*100)
// After:
logs.Debugf("Stored block %d/%d (%.1f%%)",
    i+1, metadata.TotalBlocks, float64(i+1)/float64(metadata.TotalBlocks)*100)
```

Lines 434-438 (LoadAndStoreFileLocal start banner):
```go
// Before:
fmt.Printf("Starting chunking process:\n")
fmt.Printf("Total size: %d bytes\n", metadata.TotalSize)
fmt.Printf("Block size: %d bytes\n", metadata.BlockSize)
fmt.Printf("Expected blocks: %d\n", metadata.TotalBlocks)
// After:
logs.Debugf("Starting chunking: size=%d block_size=%d blocks=%d",
    metadata.TotalSize, metadata.BlockSize, metadata.TotalBlocks)
```

Lines 452-454 (last block debug):
```go
// Before:
fmt.Printf("Last block %d: Reading remaining %d bytes\n", i, bytesToRead)
// After:
logs.Debugf("Last block %d: remaining %d bytes", i, bytesToRead)
```

Lines 467-470 (block read progress):
```go
// Before:
fmt.Printf("Block %d: Read %d bytes (total: %d/%d)\n", ...)
// After:
logs.Debugf("Block %d: read %d bytes (total: %d/%d)", ...)
```

Lines 505-511 (stored block progress + PrintMemUsage):
```go
// Before:
PrintMemUsage()
fmt.Printf("Stored block %d/%d (%.1f%%) - size: %d bytes\n", ...)
// After:
logs.Debugf("Stored block %d/%d (%.1f%%) size=%d", ...)
```

Lines 526-536 (final verification):
```go
// Before:
fmt.Printf("\n=== Final Verification ===\n")
fmt.Printf("Total blocks stored: %d\n", len(file.References))
// ...
fmt.Printf("Block %d: Size=%d, Index=%d\n", i, ref.Size, ref.FileIndex)
// After:
logs.Debugf("Final verification: %d blocks stored", len(file.References))
// ...
logs.Debugf("Block %d: size=%d index=%d", i, ref.Size, ref.FileIndex)
```

**Step 2: Replace fmt.Printf calls in key_store.go**

Lines 218-223 (file load debug):
```go
// Before:
fmt.Printf("Loaded file metadata from %s\n", file.ShortString())
fmt.Printf("Number of references: %d\n", len(file.References))
// ...
fmt.Printf("Reference %d: Key=%x, DataHash=%x\n", ...)
// After:
logs.Debugf("Loaded file metadata: %s refs=%d", file.ShortString(), len(file.References))
// ...
logs.Debugf("Reference %d: key=%x hash=%x", ...)
```

Line 857 (move to cache):
```go
// Before:
fmt.Printf("Moved to cache: %s\n", fileName)
// After:
logs.Debugf("Moved to cache: %s", fileName)
```

Line 867 (verify banner):
```go
// Before:
fmt.Printf("Verifying file references ... \n")
// After:
logs.Debugf("Verifying file references...")
```

**Step 3: Remove `PrintMemUsage()` call from files.go line 506**

The `PrintMemUsage()` function uses `fmt.Printf` internally and is verbose debug output. Remove the call. If `PrintMemUsage` is no longer called anywhere, delete the function.

**Step 4: Remove unused `"fmt"` import if no longer needed in files.go and key_store.go**

Check that `fmt` is still used for `fmt.Errorf` (it is). Only remove from import if truly unused.

**Step 5: Run tests**

```bash
go test -short ./src/key_store/...
```
Expected: All pass.

**Step 6: Commit**

```bash
git add src/key_store/files.go src/key_store/key_store.go
git commit -m "refactor: replace fmt.Printf with smplog in key_store library"
```

---

### Task 2: Remove PRINT_BLOCKS const, make progress interval configurable

**Files:**
- Modify: `src/key_store/config.go` (line 28-29)
- Modify: `src/key_store/files.go` (lines referencing PRINT_BLOCKS)

**Step 1: Remove PRINT_BLOCKS from config.go constants**

In `config.go` line 28, delete:
```go
PRINT_BLOCKS = 500
```

Also delete the unused `VERIFY = false` on line 29 (already replaced by `KeyStoreConfig.VerifyOnWrite`).

**Step 2: Use a fixed interval of 500 inline**

In `files.go`, replace `i%PRINT_BLOCKS` with `i%500`. This is only used in 2 places under `ks.config.Verbose` guards. The value doesn't need to be configurable — it's just a debug output throttle.

**Step 3: Run tests**

```bash
go test -short ./src/key_store/...
```

**Step 4: Commit**

```bash
git add src/key_store/config.go src/key_store/files.go
git commit -m "cleanup: remove PRINT_BLOCKS and VERIFY consts, inline progress interval"
```

---

### Task 3: Extract shared chunking logic into private helper

**Files:**
- Modify: `src/key_store/files.go`

**Step 1: Write a test that exercises both store paths to ensure they remain equivalent**

This test already exists: `TestStoreFileLocalAndLoadAndStoreFileLocalProduceSameKeys`. Run it to confirm green:
```bash
go test -run TestStoreFileLocalAndLoadAndStoreFileLocalProduceSameKeys -v ./src/key_store/
```

**Step 2: Extract the shared chunking loop**

Create a private method `chunkAndStore` that takes an `io.Reader`, metadata, and file object, and runs the chunk loop:

```go
// chunkAndStore reads from r in BlockSize chunks, computes DHT keys, stores each
// chunk via StoreFileReference, and populates file.References. On failure it
// cleans up any chunks already stored.
func (ks *KeyStore) chunkAndStore(r io.Reader, metadata MetaData, file *File) error {
    buffer := make([]byte, metadata.BlockSize)
    var totalBytes uint64

    for i := uint32(0); i < metadata.TotalBlocks; i++ {
        // calculate expected read size
        bytesToRead := uint64(metadata.BlockSize)
        remaining := metadata.TotalSize - totalBytes
        if remaining < bytesToRead {
            bytesToRead = remaining
        }

        n, err := io.ReadFull(r, buffer[:bytesToRead])
        if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
            ks.cleanupChunks(file.References[:i])
            return fmt.Errorf("read block %d: %w", i, err)
        }
        if n == 0 {
            ks.cleanupChunks(file.References[:i])
            return fmt.Errorf("unexpected end of file at block %d", i)
        }

        blockData := buffer[:n]
        block := FileReference{
            FileName:  metadata.FileName,
            Parent:    metadata.FileHash,
            Size:      uint32(n),
            FileIndex: i,
            Protocol:  "file",
            DataHash:  sha256.Sum256(blockData),
        }
        block.Key = computeChunkKey(metadata.FileHash, i)

        if err := ks.StoreFileReference(&block, blockData); err != nil {
            ks.cleanupChunks(file.References[:i])
            return fmt.Errorf("store block %d: %w", i, err)
        }

        blockRef := block
        file.References[i] = &blockRef
        totalBytes += uint64(n)

        if ks.config.Verbose && (i%500 == 0 || i == metadata.TotalBlocks-1) {
            logs.Debugf("Stored block %d/%d (%.1f%%)",
                i+1, metadata.TotalBlocks, float64(i+1)/float64(metadata.TotalBlocks)*100)
        }
    }

    if totalBytes != metadata.TotalSize {
        ks.cleanupChunks(file.References)
        return fmt.Errorf("processed bytes (%d) doesn't match file size (%d)",
            totalBytes, metadata.TotalSize)
    }
    return nil
}

// cleanupChunks deletes stored chunks for rollback on failure.
func (ks *KeyStore) cleanupChunks(refs []*FileReference) {
    for _, ref := range refs {
        if ref != nil {
            ks.DeleteFileReference(ref.Key)
        }
    }
}
```

**Step 3: Refactor StoreFileLocal to use chunkAndStore**

Replace the chunking loop (lines 84-139) with:
```go
r := bytes.NewReader(fileData)
if err := ks.chunkAndStore(r, metadata, file); err != nil {
    return nil, err
}
```

Add `"bytes"` to imports.

**Step 4: Refactor LoadAndStoreFileLocal to use chunkAndStore**

Replace the chunking loop (lines 441-549) with:
```go
if err := ks.chunkAndStore(f, metadata, file); err != nil {
    return nil, err
}
```

Remove the `buffer` declaration and `totalBytesRead` variable. Remove the verbose "Starting chunking process" banner (already covered by smplog in task 1). Remove the VerifyOnWrite final-verification block — move it into `chunkAndStore` if desired, or keep it as a post-call check.

**Step 5: Run all tests**

```bash
go test -v ./src/key_store/...
```
Expected: All 62+ tests pass.

**Step 6: Commit**

```bash
git add src/key_store/files.go
git commit -m "refactor: extract shared chunking loop into chunkAndStore helper"
```

---

### Task 4: Update buildplan.md for completed Phase 2A items

**Files:**
- Modify: `docs/progress/buildplan.md`

**Step 1: Mark completed Phase 2A items**

In `docs/progress/buildplan.md`, update Stage 2 description and Phase 2A checkboxes:

Change the "Current state" paragraph for Stage 2 to reflect the restructure:
- `DefaultCoder` now uses 4-byte (uint32) length header (4GB max)
- `TCPHandler` has `Dial(addr)` with connection pooling
- `Send()` accepts a `net.Conn` parameter
- File operation commands added: UPLOAD, DOWNLOAD, LIST, DELETE

Mark these Phase 2A items as done:
```
- [x] Upgrade length header from `uint16` (65KB max) to `uint32` (4GB max) to support chunk-sized messages
- [x] Add `TCPHandler.Dial(addr)` method to initiate outbound connections
- [x] Add connection pooling or reuse — `Dial()` caches connections by address
```

Also note that `fmt.Printf` in transport was already using smplog (confirmed — no stray calls found).

**Step 2: Commit**

```bash
git add docs/progress/buildplan.md
git commit -m "docs: update buildplan.md with completed Phase 2A items"
```

---

## Part B: Directory Semantics

### Task 5: Add EntryType and ParentHash fields to MetaData

**Files:**
- Modify: `src/key_store/metadata.go` (line 14-25)
- Test: `src/key_store/store_test.go`

**Step 1: Write test for backward compatibility**

Add to `store_test.go`:
```go
func TestMetaDataBackwardCompat(t *testing.T) {
    // A MetaData with no EntryType or ParentHash should behave as a root-level file.
    md := MetaData{
        FileName:  "legacy.txt",
        TotalSize: 100,
    }
    if md.EntryType != "" {
        t.Errorf("expected empty EntryType, got %q", md.EntryType)
    }
    if md.IsDirectory() {
        t.Error("default MetaData should not be a directory")
    }
    var zeroHash [HashSize]byte
    if md.ParentHash != zeroHash {
        t.Error("default ParentHash should be zero")
    }
}
```

**Step 2: Run test — expect FAIL (IsDirectory not defined)**

```bash
go test -run TestMetaDataBackwardCompat -v ./src/key_store/
```

**Step 3: Add fields to MetaData struct and IsDirectory helper**

In `metadata.go`, add after line 24 (`TotalBlocks`):
```go
EntryType  string           `toml:"entry_type,omitempty"`   // "file" (default/empty) or "directory"
ParentHash [HashSize]byte   `toml:"parent_hash,omitempty"`  // hash of parent directory manifest; zero for root
```

Add helper method:
```go
// IsDirectory returns true if this entry is a directory manifest.
func (md MetaData) IsDirectory() bool {
    return md.EntryType == "directory"
}
```

**Step 4: Run test — expect PASS**

```bash
go test -run TestMetaDataBackwardCompat -v ./src/key_store/
```

**Step 5: Run all tests to confirm no regressions**

```bash
go test -short ./src/key_store/...
```

**Step 6: Commit**

```bash
git add src/key_store/metadata.go src/key_store/store_test.go
git commit -m "feat: add EntryType and ParentHash fields to MetaData"
```

---

### Task 6: Add DirectoryEntry type and path normalization

**Files:**
- Create: `src/key_store/directory.go`
- Test: `src/key_store/directory_test.go`

**Step 1: Write tests for path normalization**

Create `src/key_store/directory_test.go`:
```go
package key_store

import "testing"

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
        {"src/../escape.txt", "", true},
        {"", "", true},
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
```

**Step 2: Run test — expect FAIL**

```bash
go test -run TestNormalizePath -v ./src/key_store/
```

**Step 3: Implement DirectoryEntry and NormalizePath**

Create `src/key_store/directory.go`:
```go
package key_store

import (
    "fmt"
    "path/filepath"
    "strings"
)

// DirectoryEntry represents a single child in a directory manifest.
type DirectoryEntry struct {
    Name string         `json:"name"`       // basename
    Path string         `json:"path"`       // full relative path
    Hash [HashSize]byte `json:"hash"`       // file hash or manifest hash
    Type string         `json:"type"`       // "file" or "directory"
    Size uint64         `json:"size"`       // file size; 0 for directories
}

// NormalizePath cleans and validates a relative path for storage.
// Forward slashes only, no ./ prefix, no .. traversal, non-empty.
func NormalizePath(p string) (string, error) {
    if p == "" {
        return "", fmt.Errorf("empty path")
    }
    // normalize separators
    p = filepath.ToSlash(p)
    // clean the path (resolves . and ..)
    cleaned := filepath.ToSlash(filepath.Clean(p))
    // reject traversal
    if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") || strings.HasSuffix(cleaned, "/..") {
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
```

**Step 4: Run test — expect PASS**

```bash
go test -run TestNormalizePath -v ./src/key_store/
```

**Step 5: Commit**

```bash
git add src/key_store/directory.go src/key_store/directory_test.go
git commit -m "feat: add DirectoryEntry type and path normalization"
```

---

### Task 7: Implement StoreDirectory on KeyStore

**Files:**
- Modify: `src/key_store/directory.go`
- Modify: `src/key_store/key_store.go` (add method)
- Test: `src/key_store/directory_test.go`

**Step 1: Write test for StoreDirectory round-trip**

Add to `directory_test.go`:
```go
func TestStoreDirectoryRoundTrip(t *testing.T) {
    tmpDir := t.TempDir()
    storageDir := filepath.Join(tmpDir, "storage")

    // Create a test directory tree
    testRoot := filepath.Join(tmpDir, "upload")
    os.MkdirAll(filepath.Join(testRoot, "sub"), 0o755)
    os.WriteFile(filepath.Join(testRoot, "root.txt"), []byte("root file"), 0o644)
    os.WriteFile(filepath.Join(testRoot, "sub", "nested.txt"), []byte("nested file"), 0o644)

    ks, err := InitKeyStoreWithConfig(KeyStoreConfig{
        StorageDir:        storageDir,
        DefaultTTLSeconds: 3600,
    })
    if err != nil {
        t.Fatalf("init keystore: %v", err)
    }

    dirHash, err := ks.StoreDirectory(testRoot)
    if err != nil {
        t.Fatalf("StoreDirectory: %v", err)
    }

    // Verify directory manifest is stored
    dirFile, err := ks.GetFileByHash(dirHash)
    if err != nil {
        t.Fatalf("GetFileByHash for directory: %v", err)
    }
    if !dirFile.MetaData.IsDirectory() {
        t.Error("expected directory entry type")
    }

    // Verify individual files are stored with full paths
    _, err = ks.GetFileByName("root.txt")
    if err != nil {
        t.Errorf("GetFileByName(root.txt): %v", err)
    }
    _, err = ks.GetFileByName("sub/nested.txt")
    if err != nil {
        t.Errorf("GetFileByName(sub/nested.txt): %v", err)
    }
}
```

**Step 2: Run test — expect FAIL**

```bash
go test -run TestStoreDirectoryRoundTrip -v ./src/key_store/
```

**Step 3: Implement StoreDirectory**

Add to `directory.go`:
```go
import (
    "crypto/sha256"
    "encoding/json"
    "io/fs"
    "os"
    "path/filepath"
    "sort"
    "strings"
)

// DirectoryManifest is the JSON blob stored as a directory's chunked data.
type DirectoryManifest struct {
    Path     string           `json:"path"`
    Children []DirectoryEntry `json:"children"`
}

// StoreDirectory recursively ingests a directory tree. Files are stored with
// their relative paths as FileName. Directories become manifest entries.
// Returns the root directory's manifest hash.
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

func (ks *KeyStore) storeDirectoryRecursive(dirPath, rootPath string) ([HashSize]byte, error) {
    entries, err := os.ReadDir(dirPath)
    if err != nil {
        return [HashSize]byte{}, fmt.Errorf("read dir %s: %w", dirPath, err)
    }

    // Sort for deterministic manifests
    sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

    var children []DirectoryEntry

    for _, entry := range entries {
        childPath := filepath.Join(dirPath, entry.Name())
        relPath, err := filepath.Rel(rootPath, childPath)
        if err != nil {
            return [HashSize]byte{}, fmt.Errorf("rel path: %w", err)
        }
        relPath = filepath.ToSlash(relPath)

        if entry.IsDir() {
            // Recurse into subdirectory
            subHash, err := ks.storeDirectoryRecursive(childPath, rootPath)
            if err != nil {
                return [HashSize]byte{}, err
            }
            children = append(children, DirectoryEntry{
                Name: entry.Name(),
                Path: relPath + "/",
                Hash: subHash,
                Type: "directory",
            })
        } else {
            // Store the file with relative path as name
            file, err := ks.LoadAndStoreFileLocal(childPath)
            if err != nil {
                return [HashSize]byte{}, fmt.Errorf("store file %s: %w", relPath, err)
            }
            // Update the file name to be the relative path
            ks.renameFile(file.MetaData.FileHash, relPath)

            fi, _ := entry.Info()
            var size uint64
            if fi != nil {
                size = uint64(fi.Size())
            }
            children = append(children, DirectoryEntry{
                Name: entry.Name(),
                Path: relPath,
                Hash: file.MetaData.FileHash,
                Type: "file",
                Size: size,
            })
        }
    }

    // Build and store the manifest
    dirRel, _ := filepath.Rel(rootPath, dirPath)
    dirRelSlash := filepath.ToSlash(dirRel)
    if dirRelSlash == "." {
        dirRelSlash = ""
    }
    if dirRelSlash != "" && !strings.HasSuffix(dirRelSlash, "/") {
        dirRelSlash += "/"
    }

    manifest := DirectoryManifest{
        Path:     dirRelSlash,
        Children: children,
    }
    manifestData, err := json.Marshal(manifest)
    if err != nil {
        return [HashSize]byte{}, fmt.Errorf("marshal manifest: %w", err)
    }

    // Store manifest as a file
    manifestFile, err := ks.StoreFileLocal(dirRelSlash, manifestData)
    if err != nil {
        return [HashSize]byte{}, fmt.Errorf("store manifest for %s: %w", dirRelSlash, err)
    }

    // Mark as directory
    manifestFile.MetaData.EntryType = "directory"
    if err := ks.persistMetaData(manifestFile.MetaData); err != nil {
        return [HashSize]byte{}, fmt.Errorf("persist directory metadata: %w", err)
    }

    // Set ParentHash on children
    for _, child := range children {
        if childFile, err := ks.GetFileByHash(child.Hash); err == nil {
            childFile.MetaData.ParentHash = manifestFile.MetaData.FileHash
            ks.persistMetaData(childFile.MetaData)
        }
    }

    return manifestFile.MetaData.FileHash, nil
}

// renameFile updates the FileName for a stored file and its name index.
func (ks *KeyStore) renameFile(hash [HashSize]byte, newName string) {
    ks.mu.Lock()
    defer ks.mu.Unlock()
    if f, ok := ks.files[hash]; ok {
        // Remove old name index
        delete(ks.filesByName, f.MetaData.FileName)
        f.MetaData.FileName = newName
        ks.filesByName[newName] = hash
    }
}
```

Note: `persistMetaData` may need to be added or may already exist as `fileToMemory` / `persistFileMetadata`. The implementer should check `key_store.go` for the exact method name that writes MetaData to disk and use that.

**Step 4: Run test — expect PASS**

```bash
go test -run TestStoreDirectoryRoundTrip -v ./src/key_store/
```

**Step 5: Commit**

```bash
git add src/key_store/directory.go src/key_store/directory_test.go
git commit -m "feat: implement StoreDirectory with recursive ingest and manifests"
```

---

### Task 8: Implement ListDirectory on KeyStore

**Files:**
- Modify: `src/key_store/directory.go`
- Test: `src/key_store/directory_test.go`

**Step 1: Write test**

Add to `directory_test.go`:
```go
func TestListDirectory(t *testing.T) {
    // Use the same setup as TestStoreDirectoryRoundTrip
    tmpDir := t.TempDir()
    storageDir := filepath.Join(tmpDir, "storage")
    testRoot := filepath.Join(tmpDir, "upload")
    os.MkdirAll(filepath.Join(testRoot, "sub"), 0o755)
    os.WriteFile(filepath.Join(testRoot, "root.txt"), []byte("root file"), 0o644)
    os.WriteFile(filepath.Join(testRoot, "sub", "nested.txt"), []byte("nested file"), 0o644)

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

    // Verify children (sorted: root.txt, sub/)
    names := []string{entries[0].Name, entries[1].Name}
    sort.Strings(names)
    if names[0] != "root.txt" || names[1] != "sub" {
        t.Errorf("unexpected children: %v", names)
    }
}
```

**Step 2: Run test — expect FAIL**

**Step 3: Implement ListDirectory**

Add to `directory.go`:
```go
// ListDirectory parses the manifest for the given directory hash and returns
// its children.
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
```

**Step 4: Run test — expect PASS**

**Step 5: Commit**

```bash
git add src/key_store/directory.go src/key_store/directory_test.go
git commit -m "feat: implement ListDirectory for browsing directory manifests"
```

---

### Task 9: Implement ReassembleDirectory on KeyStore

**Files:**
- Modify: `src/key_store/directory.go`
- Test: `src/key_store/directory_test.go`

**Step 1: Write test**

Add to `directory_test.go`:
```go
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

    // Verify files exist with correct content
    gotRoot, err := os.ReadFile(filepath.Join(outputRoot, "root.txt"))
    if err != nil {
        t.Fatalf("read root.txt: %v", err)
    }
    if string(gotRoot) != "root file" {
        t.Errorf("root.txt content = %q, want %q", gotRoot, "root file")
    }

    gotNested, err := os.ReadFile(filepath.Join(outputRoot, "sub", "nested.txt"))
    if err != nil {
        t.Fatalf("read sub/nested.txt: %v", err)
    }
    if string(gotNested) != "nested file" {
        t.Errorf("sub/nested.txt content = %q, want %q", gotNested, "nested file")
    }
}
```

**Step 2: Run test — expect FAIL**

**Step 3: Implement ReassembleDirectory**

Add to `directory.go`:
```go
// ReassembleDirectory recursively recreates a directory tree at outputRoot.
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
```

**Step 4: Run test — expect PASS**

**Step 5: Run all tests**

```bash
go test -short ./src/key_store/...
```

**Step 6: Commit**

```bash
git add src/key_store/directory.go src/key_store/directory_test.go
git commit -m "feat: implement ReassembleDirectory for recursive download"
```

---

### Task 10: Add duplicate basename and deep nesting tests

**Files:**
- Test: `src/key_store/directory_test.go`

**Step 1: Write duplicate basename test**

```go
func TestDuplicateBasenamesInDirectories(t *testing.T) {
    tmpDir := t.TempDir()
    storageDir := filepath.Join(tmpDir, "storage")
    testRoot := filepath.Join(tmpDir, "upload")

    // Two files named "main.go" in different directories
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

    // Create 4-level deep structure
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
        t.Errorf("deep.txt content = %q, want %q", got, "deep")
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
```

**Step 2: Run tests — expect PASS (implementation already done)**

```bash
go test -run "TestDuplicateBasenames|TestDeepNesting|TestEmptyDirectory" -v ./src/key_store/
```

**Step 3: Commit**

```bash
git add src/key_store/directory_test.go
git commit -m "test: add duplicate basename, deep nesting, and empty directory tests"
```

---

### Task 11: Add directory methods to FileLedger interface and KeyStoreLedger adapter

**Files:**
- Modify: `src/api/ledgers/net_store.go`
- Modify: `src/key_store/file_ledger.go`
- Test: `src/key_store/file_ledger_test.go`

**Step 1: Write test for FileLedger directory operations**

Add to `file_ledger_test.go`:
```go
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
```

**Step 2: Run test — expect FAIL**

**Step 3: Add methods to FileLedger interface**

In `src/api/ledgers/net_store.go`, add to the `FileLedger` interface:
```go
// Directory operations
StoreDirectory(rootPath string) (FileID, error)
ListDirectory(dirID FileID) ([]DirectoryEntry, error)
ReassembleDirectory(dirID FileID, outputRoot string) error
```

Also add `DirectoryEntry` to ledgers package (or re-export from key_store). Simplest approach: define in net_store.go:
```go
// DirectoryEntry represents a child in a directory manifest.
type DirectoryEntry struct {
    Name string  `json:"name"`
    Path string  `json:"path"`
    Hash FileID  `json:"hash"`
    Type string  `json:"type"`
    Size uint64  `json:"size"`
}
```

**Step 4: Add methods to KeyStoreLedger**

In `src/key_store/file_ledger.go`:
```go
func (l *KeyStoreLedger) StoreDirectory(rootPath string) (ledgers.FileID, error) {
    hash, err := l.ks.StoreDirectory(rootPath)
    if err != nil {
        return ledgers.FileID{}, err
    }
    return ledgers.FileID(hash), nil
}

func (l *KeyStoreLedger) ListDirectory(dirID ledgers.FileID) ([]ledgers.DirectoryEntry, error) {
    entries, err := l.ks.ListDirectory([HashSize]byte(dirID))
    if err != nil {
        return nil, err
    }
    result := make([]ledgers.DirectoryEntry, len(entries))
    for i, e := range entries {
        result[i] = ledgers.DirectoryEntry{
            Name: e.Name,
            Path: e.Path,
            Hash: ledgers.FileID(e.Hash),
            Type: e.Type,
            Size: e.Size,
        }
    }
    return result, nil
}

func (l *KeyStoreLedger) ReassembleDirectory(dirID ledgers.FileID, outputRoot string) error {
    return l.ks.ReassembleDirectory([HashSize]byte(dirID), outputRoot)
}
```

**Step 5: Run test — expect PASS**

**Step 6: Run all tests**

```bash
go test -short ./...
```

**Step 7: Commit**

```bash
git add src/api/ledgers/net_store.go src/key_store/file_ledger.go src/key_store/file_ledger_test.go
git commit -m "feat: add directory methods to FileLedger interface and KeyStoreLedger adapter"
```

---

### Task 12: Add UPLOAD_DIR and LIST_DIR RPC commands

**Files:**
- Modify: `src/api/transport/rpc.proto`
- Modify: `src/api/nodes/server_node.go`

**Step 1: Add commands to rpc.proto**

Add to the Command enum after `DELETE = 14`:
```protobuf
UPLOAD_DIR = 15;
LIST_DIR = 16;
```

**Step 2: Regenerate protobuf**

```bash
make build-protobuf
```

**Step 3: Add UPLOAD_DIR and LIST_DIR handlers to ServerNode.HandleRPC**

In `server_node.go`, add cases to the switch in HandleRPC:

```go
case transport.Command_UPLOAD_DIR:
    // Key = local path to upload (for local server use)
    // For remote: would need tar/zip stream — defer to future
    dirPath := string(rpc.Key)
    fid, err := s.storage.StoreDirectory(dirPath)
    if err != nil {
        return nil, fmt.Errorf("store directory: %w", err)
    }
    return &transport.RPC{
        Meta:   &transport.RPCT{Command: transport.Command_ACK},
        Sender: s.nodeInfo(),
        Key:    fid[:],
    }, nil

case transport.Command_LIST_DIR:
    if len(rpc.Key) != 32 {
        return nil, fmt.Errorf("LIST_DIR requires 32-byte directory hash key")
    }
    var fid ledgers.FileID
    copy(fid[:], rpc.Key)
    entries, err := s.storage.ListDirectory(fid)
    if err != nil {
        return nil, fmt.Errorf("list directory: %w", err)
    }
    data, err := json.Marshal(entries)
    if err != nil {
        return nil, fmt.Errorf("marshal directory listing: %w", err)
    }
    return &transport.RPC{
        Meta:    &transport.RPCT{Command: transport.Command_ACK},
        Sender:  s.nodeInfo(),
        Payload: data,
    }, nil
```

**Step 4: Run tests**

```bash
go test -short ./...
```

**Step 5: Commit**

```bash
git add src/api/transport/rpc.proto src/api/transport/rpc.pb.go src/api/nodes/server_node.go
git commit -m "feat: add UPLOAD_DIR and LIST_DIR RPC commands"
```

---

### Task 13: Add HTTP directory endpoints to ServerNode

**Files:**
- Modify: `src/api/nodes/http_handlers.go`

**Step 1: Register new routes**

In `registerHTTPRoutes`, add:
```go
s.mux.HandleFunc("GET /dirs/hash/{hex}", s.handleListDir)
s.mux.HandleFunc("GET /dirs/hash/{hex}/tree", s.handleListDirTree)
```

**Step 2: Implement handlers**

```go
func (s *DefaultServerNode) handleListDir(w http.ResponseWriter, r *http.Request) {
    hexStr := r.PathValue("hex")
    hashBytes, err := hex.DecodeString(hexStr)
    if err != nil || len(hashBytes) != 32 {
        http.Error(w, "invalid hash", http.StatusBadRequest)
        return
    }
    var fid ledgers.FileID
    copy(fid[:], hashBytes)

    entries, err := s.storage.ListDirectory(fid)
    if err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(entries)
}

func (s *DefaultServerNode) handleListDirTree(w http.ResponseWriter, r *http.Request) {
    hexStr := r.PathValue("hex")
    hashBytes, err := hex.DecodeString(hexStr)
    if err != nil || len(hashBytes) != 32 {
        http.Error(w, "invalid hash", http.StatusBadRequest)
        return
    }
    var fid ledgers.FileID
    copy(fid[:], hashBytes)

    tree, err := s.buildTree(fid, "")
    if err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(tree)
}

func (s *DefaultServerNode) buildTree(fid ledgers.FileID, prefix string) ([]ledgers.DirectoryEntry, error) {
    entries, err := s.storage.ListDirectory(fid)
    if err != nil {
        return nil, err
    }
    var all []ledgers.DirectoryEntry
    for _, e := range entries {
        all = append(all, e)
        if e.Type == "directory" {
            sub, err := s.buildTree(e.Hash, e.Path)
            if err != nil {
                return nil, err
            }
            all = append(all, sub...)
        }
    }
    return all, nil
}
```

**Step 3: Run tests**

```bash
go test -short ./...
```

**Step 4: Commit**

```bash
git add src/api/nodes/http_handlers.go
git commit -m "feat: add HTTP directory listing endpoints"
```

---

### Task 14: Add directory support to cmd/client TUI

**Files:**
- Modify: `cmd/client/runtime_config.go` (add ActionUploadDir)
- Modify: `cmd/client/menu.go` (add menu option)
- Create: `cmd/client/directory.go` (upload dir action + view dir browsing)

**Step 1: Add action constant**

In `runtime_config.go`, add:
```go
ActionUploadDir MenuAction = "upload-dir"
```

**Step 2: Add menu entry in menu.go**

Add "upload-dir" to the menu alongside upload, with aliases "ud", "updir".

**Step 3: Implement executeUploadDirAction**

Create `cmd/client/directory.go`:
```go
package main

import (
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"

    "github.com/danmuck/dps_files/src/key_store"
    logs "github.com/danmuck/smplog"
)

func executeUploadDirAction(cfg RuntimeConfig, ks *key_store.KeyStore, input io.Reader) error {
    reader := getBufferedReader(input)

    logs.Promptf("\nEnter directory path to upload: ")
    line, err := reader.ReadString('\n')
    if err != nil {
        return fmt.Errorf("read path: %w", err)
    }
    dirPath := strings.TrimSpace(line)
    if strings.EqualFold(dirPath, "e") {
        return errMenuBack
    }

    info, err := os.Stat(dirPath)
    if err != nil {
        return fmt.Errorf("stat %s: %w", dirPath, err)
    }
    if !info.IsDir() {
        return fmt.Errorf("%s is not a directory", dirPath)
    }

    logs.Printf("\nUploading directory %q...\n", dirPath)
    dirHash, err := ks.StoreDirectory(dirPath)
    if err != nil {
        return fmt.Errorf("store directory: %w", err)
    }
    logs.Printf("Directory stored. Root hash: %x\n", dirHash)
    return nil
}
```

**Step 4: Wire into executeActionOnce in main.go**

Add to the switch in `executeActionOnce`:
```go
case ActionUploadDir:
    return executeUploadDirAction(cfg, keystore, input)
```

**Step 5: Update view to show [DIR] entries**

In `view.go`, modify `executeViewAction` to show `[DIR]` prefix for directory entries:
```go
// After getting metadata, check EntryType
prefix := ""
if md.IsDirectory() {
    prefix = "[DIR] "
}
logs.MenuItem(i, prefix+md.FileName, false)
```

**Step 6: Run build**

```bash
go build ./cmd/client/
```

**Step 7: Commit**

```bash
git add cmd/client/directory.go cmd/client/main.go cmd/client/menu.go cmd/client/runtime_config.go cmd/client/view.go
git commit -m "feat: add directory upload and [DIR] display to TUI"
```

---

### Task 15: Update CLAUDE.md and buildplan.md

**Files:**
- Modify: `CLAUDE.md`
- Modify: `docs/progress/buildplan.md`

**Step 1: Update CLAUDE.md**

- Add `directory.go` to key_store file list with description
- Add `DirectoryEntry`, `DirectoryManifest` types to key_store description
- Add `StoreDirectory`, `ListDirectory`, `ReassembleDirectory` to method list
- Add `UPLOAD_DIR`, `LIST_DIR` to RPC command list
- Add HTTP `/dirs/` endpoints to ServerNode description
- Update MetaData struct description to mention `EntryType` and `ParentHash`
- Update "Current State > Working" section to include directory semantics
- Mark Phase 1C smplog items as done

**Step 2: Update buildplan.md**

- Mark Phase 1C items as complete: smplog audit, PRINT_BLOCKS, shared chunking
- Add new section for directory semantics with completed items
- Update Stage 2 Phase 2A description (already partially done in Task 4)

**Step 3: Commit**

```bash
git add CLAUDE.md docs/progress/buildplan.md
git commit -m "docs: update CLAUDE.md and buildplan for directory semantics + Phase 1C cleanup"
```

---

## Execution Order Summary

| Task | Description | Dependencies |
|------|-------------|-------------|
| 1 | Replace fmt.Printf with smplog in key_store | None |
| 2 | Remove PRINT_BLOCKS/VERIFY consts | Task 1 |
| 3 | Extract shared chunking helper | Task 1, 2 |
| 4 | Update buildplan.md for Phase 2A | None (parallel with 1-3) |
| 5 | Add EntryType/ParentHash to MetaData | Task 3 |
| 6 | Add DirectoryEntry + path normalization | Task 5 |
| 7 | Implement StoreDirectory | Task 6 |
| 8 | Implement ListDirectory | Task 7 |
| 9 | Implement ReassembleDirectory | Task 8 |
| 10 | Duplicate basename + deep nesting tests | Task 9 |
| 11 | FileLedger + KeyStoreLedger adapter | Task 9 |
| 12 | RPC commands (UPLOAD_DIR, LIST_DIR) | Task 11 |
| 13 | HTTP directory endpoints | Task 12 |
| 14 | TUI directory support | Task 11 |
| 15 | Documentation updates | Task 14 |
