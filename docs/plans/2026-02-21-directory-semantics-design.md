# Directory Semantics & Phase 1C/2A Cleanup — Design

**Goal:** Add first-class directory support to the storage system (recursive upload/download, directory manifests, path-aware indexing, directory browsing) and clean up the Phase 1C/2A technical debt before building on it.

**Architecture:** Directories are modeled as special MetaData entries (`EntryType = "directory"`) whose chunked data is a JSON manifest listing children with hashes. Files use full relative paths as their `FileName`. Lookups remain hash-primary with path-based convenience. Existing flat metadata is backward-compatible (no EntryType = root-level file).

**Tech Stack:** Go, TOML (metadata persistence), JSON (directory manifests), Protobuf (RPC commands)

---

## Part A: Phase 1C/2A Cleanup (Prerequisite)

Clean up the foundation before building directory features.

### 1. smplog Consistency

Audit all `key_store` library code for stray `fmt.Printf`, `fmt.Fprintf(os.Stderr, ...)`, or `log.*` calls. Replace with `smplog` equivalents. Same for `transport` package.

### 2. PRINT_BLOCKS Runtime Config

`PRINT_BLOCKS` is currently a compile-time const controlling chunk-level progress output. Either make it a `KeyStoreConfig` field (`VerboseChunks bool`) or remove it from library code and move progress output to `cmd/` layer only.

### 3. Extract Shared Chunking Logic

`StoreFileLocal` and `LoadAndStoreFileLocal` duplicate the chunk-split-store loop. Extract into a private `chunkAndStore(metadata, reader) error` helper that both call.

### 4. Add context.Context

Add `context.Context` parameter to `StoreFileLocal`, `LoadAndStoreFileLocal`, `StoreFromReader`, `StreamFile`, `StreamChunkRange`, and their FileLedger counterparts. Enables cancellation support for long operations.

### 5. Update buildplan.md

Mark Phase 2A items completed by the transport/node restructure: uint32 framing, Dial method, connection pooling. Update Stage 2 description text.

---

## Part B: Directory Semantics

### MetaData Changes

Add two fields to the `MetaData` struct:

```go
EntryType  string           // "file" (default/empty) or "directory"
ParentHash [HashSize]byte   // hash of parent directory manifest; zero for root-level
```

`FileName` becomes the full relative path:
- Files: `src/api/main.go`
- Directories: `src/api/`

TOML tags: `entry_type`, `parent_hash`. Backward compatible — missing fields default to empty string and zero hash, which means "root-level file".

### Directory Manifest Format

A directory manifest is stored as chunked data (like any file). Its content is JSON:

```json
{
  "path": "src/api/",
  "children": [
    {"name": "main.go", "hash": "abc123...", "type": "file", "size": 1024},
    {"name": "transport/", "hash": "def456...", "type": "directory"}
  ]
}
```

The manifest's SHA-256 becomes its MetaData.FileHash, and it gets chunked, stored, and indexed like any file. Its `EntryType = "directory"`.

### Recursive Upload Flow

1. Walk directory tree bottom-up (leaves first)
2. For each file: chunk and store as today, but `FileName = relative/path/to/file`
3. For each directory: build manifest JSON from children's hashes → store as a file → get directory hash → set `ParentHash` on each child's MetaData
4. Return root directory hash

### Name Index & Lookups

`filesByName` index uses full relative path as key. This is backward compatible — flat files have no slashes and work identically to today.

- **By hash:** unchanged
- **By path:** `GetFileByName("src/api/main.go")` — same mechanism as today
- **Browse directory:** load manifest by hash → parse JSON → return `[]DirectoryEntry`

### DirectoryEntry Struct

```go
type DirectoryEntry struct {
    Name string          // basename (e.g. "main.go")
    Path string          // full relative path (e.g. "src/api/main.go")
    Hash [HashSize]byte  // file hash or manifest hash
    Type string          // "file" or "directory"
    Size uint64          // file size; 0 for directories
}
```

### Recursive Download

`ReassembleDirectory(dirHash, outputRoot)`:
1. Load manifest by hash
2. For each child file: reassemble to `outputRoot/child.Path`
3. For each child directory: recurse

### KeyStore New Methods

- `StoreDirectory(rootPath string) ([HashSize]byte, error)` — recursive ingest
- `ListDirectory(dirHash [HashSize]byte) ([]DirectoryEntry, error)` — parse manifest
- `ReassembleDirectory(dirHash [HashSize]byte, outputRoot string) error` — recursive download
- `GetByPath(relativePath string) (*File, error)` — alias for GetFileByName

### FileLedger Interface Additions

```go
StoreDirectory(rootPath string) (FileID, error)
ListDirectory(dirID FileID) ([]DirectoryEntry, error)
ReassembleDirectory(dirID FileID, outputRoot string) error
```

### RPC Additions (rpc.proto)

- `UPLOAD_DIR = 15`
- `LIST_DIR = 16`

### HTTP Additions (ServerNode)

- `PUT /dirs/{path...}` — upload directory
- `GET /dirs/hash/{hex}` — list directory contents
- `GET /dirs/hash/{hex}/tree` — full recursive listing

### TUI Additions (cmd/client)

- Upload menu: "upload directory" option that walks a local path
- View: directories shown with `[DIR]` prefix, selectable to drill in
- Download: directory selection recreates folder tree under output path

### Backward Compatibility

- Existing flat metadata (no `EntryType`, no `ParentHash` in TOML) loads as `EntryType=""` and `ParentHash=zero` — treated as root-level file
- All existing tests continue to pass unchanged
- `filesByName` index: existing entries are simple filenames, coexist with path-keyed entries

### Path Normalization Rules

- Forward slashes only (normalized on ingest)
- No leading `./` prefix (stripped)
- `../` traversal rejected with error
- Directories end with `/`
- Case-sensitive (matches filesystem behavior on Linux/macOS default)

---

## Part C: Testing

### Unit Tests (key_store)

- Round-trip: store directory tree → reassemble → verify identical structure and contents
- Nested directories: 3+ levels with files at each level
- Duplicate basenames: `a/main.go` and `b/main.go` both stored and retrieved
- Empty directory: manifest with zero children
- Backward compat: flat metadata loads without EntryType/ParentHash
- Manifest integrity: tampered manifest detected on reassemble
- Single file from directory: fetch by full path, reassemble just that file
- Path normalization: trailing slashes, `./` prefix, `../` rejection
- context.Context cancellation mid-store aborts cleanly

### Integration Tests (nodes)

- ServerNode HandleRPC for UPLOAD_DIR and LIST_DIR
- HTTP PUT/GET /dirs/ round-trip
- ClientNode directory upload to remote server

---

## Execution Order

1. Phase 1C/2A cleanup (Part A) — prerequisite foundation work
2. MetaData + manifest model (Part B core) — data model changes
3. KeyStore directory methods — StoreDirectory, ListDirectory, ReassembleDirectory
4. FileLedger + KeyStoreLedger adapter — interface additions
5. RPC + HTTP + ServerNode — network surface
6. TUI — cmd/client directory support
7. Tests throughout each step
