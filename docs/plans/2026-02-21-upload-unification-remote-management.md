# Upload Unification & Remote Management Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Collapse `store`/`upload`/`upload-dir` into a single `upload` command with inline path entry and browse fallback, and add `Verify`/`Expire`/`Clean`/`Stats` gRPC RPCs so management operations work against remote servers.

**Architecture:** The unified `upload` command prompts for a path; empty input drops into a browse view of `local/upload/` showing files and `[dir]` entries. Management RPCs are added to the proto, implemented server-side by type-asserting `FileLedger` → `*key_store.KeyStoreLedger`, and consumed client-side via new `GRPCClient` methods. The local management code paths are unchanged.

**Tech Stack:** Go, gRPC + grpc-gateway, protobuf (`src/api/pb/dps.proto`), `key_store.KeyStore`, `bufconn` for tests.

**Design doc:** `docs/plans/2026-02-21-upload-unification-remote-management-design.md`

---

## Task 1: Add `StorageDir()` accessor and `DeepClean()` to `KeyStore`

The gRPC management RPCs need to walk storage directories and run deep-clean. These belong on `KeyStore` itself, not scattered across the server.

**Files:**
- Modify: `src/key_store/key_store.go`

**Step 1: Add the two methods at the bottom of `key_store.go`**

Find the end of the file (after `CleanupExpired`) and append:

```go
// StorageDir returns the root storage directory path.
func (ks *KeyStore) StorageDir() string {
	return ks.storageDir
}

// DeepCleanResult holds counts of files removed by DeepClean.
type DeepCleanResult struct {
	RemovedKDHT     int
	RemovedMetadata int
	RemovedCache    int
}

// DeepClean removes all .kdht chunks, all metadata .toml files, and all cache
// entries. It returns counts of what was removed. Recreates the directories
// so the keystore remains usable.
func (ks *KeyStore) DeepClean() (DeepCleanResult, error) {
	var result DeepCleanResult

	// Count + remove .kdht files.
	kdhtPattern := filepath.Join(ks.storageDir, "data", "*.kdht")
	kdhtFiles, err := filepath.Glob(kdhtPattern)
	if err != nil {
		return result, fmt.Errorf("glob kdht: %w", err)
	}
	result.RemovedKDHT = len(kdhtFiles)
	if err := ks.CleanupKDHT(); err != nil {
		return result, fmt.Errorf("cleanup kdht: %w", err)
	}

	// Count + remove metadata files.
	metaDir := filepath.Join(ks.storageDir, "metadata")
	metaEntries, err := os.ReadDir(metaDir)
	if err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("read metadata dir: %w", err)
	}
	for _, e := range metaEntries {
		if !e.IsDir() {
			result.RemovedMetadata++
		}
	}
	if err := os.RemoveAll(metaDir); err != nil {
		return result, fmt.Errorf("remove metadata dir: %w", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return result, fmt.Errorf("recreate metadata dir: %w", err)
	}

	// Count + remove cache files.
	cacheDir := filepath.Join(ks.storageDir, ".cache")
	cacheEntries, err := os.ReadDir(cacheDir)
	if err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("read cache dir: %w", err)
	}
	for _, e := range cacheEntries {
		if !e.IsDir() {
			result.RemovedCache++
		}
	}
	if err := os.RemoveAll(cacheDir); err != nil {
		return result, fmt.Errorf("remove cache dir: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return result, fmt.Errorf("recreate cache dir: %w", err)
	}

	return result, nil
}
```

**Step 2: Verify it compiles**

```bash
cd /path/to/dps_files
go build ./src/key_store/...
```
Expected: no output, exit 0.

**Step 3: Commit**

```bash
git add src/key_store/key_store.go
git commit -m "feat(key_store): add StorageDir() accessor and DeepClean() method"
```

---

## Task 2: Add management messages and RPCs to `dps.proto`

**Files:**
- Modify: `src/api/pb/dps.proto`

**Step 1: Add message types before the `service DPSFiles` block**

Insert after the `ListDirResponse` message (line 73), before `service DPSFiles {`:

```protobuf
// --- Management RPCs ---

message VerifyRequest {}
message VerifyError {
  uint64 chunk_index = 1;
  string file_name   = 2;
  string error       = 3;
}
message VerifyResponse {
  repeated VerifyError errors = 1;
}

message ExpireRequest {}
message ExpireResponse {
  int64 removed = 1;
}

// deep=true removes .kdht + metadata + cache; false removes .kdht only.
message CleanRequest {
  bool deep = 1;
}
message CleanResponse {
  int64 removed_kdht     = 1;
  int64 removed_metadata = 2;
  int64 removed_cache    = 3;
}

message StatsRequest {}
message StatsResponse {
  uint64 data_bytes     = 1;
  uint64 metadata_bytes = 2;
  uint64 cache_bytes    = 3;
  uint64 total_bytes    = 4;
  int64  file_count     = 5;
}
```

**Step 2: Add RPC declarations inside `service DPSFiles`**

Append before the closing `}` of `service DPSFiles`:

```protobuf
  rpc Verify(VerifyRequest) returns (VerifyResponse) {
    option (google.api.http) = { get: "/v1/admin/verify" };
  }

  rpc Expire(ExpireRequest) returns (ExpireResponse) {
    option (google.api.http) = { post: "/v1/admin/expire" body: "*" };
  }

  rpc Clean(CleanRequest) returns (CleanResponse) {
    option (google.api.http) = { post: "/v1/admin/clean" body: "*" };
  }

  rpc Stats(StatsRequest) returns (StatsResponse) {
    option (google.api.http) = { get: "/v1/admin/stats" };
  }
```

**Step 3: Commit**

```bash
git add src/api/pb/dps.proto
git commit -m "feat(proto): add Verify, Expire, Clean, Stats management RPCs"
```

---

## Task 3: Regenerate `pb/` from proto

**Files:**
- Auto-generated: `src/api/pb/dps.pb.go`, `src/api/pb/dps_grpc.pb.go`, `src/api/pb/dps.pb.gw.go`

**Step 1: Run protoc**

```bash
make build-protobuf
```
Expected: exits 0, three files in `src/api/pb/` have updated timestamps.

**Step 2: Verify generated files compile**

```bash
go build ./src/api/pb/...
```
Expected: no output, exit 0.

**Step 3: Commit generated files**

```bash
git add src/api/pb/
git commit -m "chore(pb): regenerate from proto with management RPCs"
```

---

## Task 4: Implement management RPCs in the gRPC server

**Files:**
- Modify: `src/api/grpc/server.go`
- Modify: `src/api/grpc/server_test.go`

The gRPC server holds a `ledgers.FileLedger`. Management operations need the concrete `*key_store.KeyStoreLedger`. We type-assert once per call through a small helper.

**Step 1: Add import for `key_store` to `server.go`**

In the `import` block of `server.go`, add:

```go
"path/filepath"

"github.com/danmuck/dps_files/src/key_store"
```

**Step 2: Add the `managedStore` helper**

Add after the `New` constructor, before `Upload`:

```go
// managedStore type-asserts storage to *key_store.KeyStoreLedger.
// Returns an Unimplemented gRPC error if the backend is not KeyStoreLedger.
func (s *Server) managedStore() (*key_store.KeyStore, error) {
	ledger, ok := s.storage.(*key_store.KeyStoreLedger)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "management RPCs require KeyStoreLedger backend")
	}
	return ledger.KeyStore(), nil
}
```

**Step 3: Add the four RPC methods**

Append at the end of `server.go`:

```go
// Verify runs a full integrity scan and returns any chunk errors found.
func (s *Server) Verify(_ context.Context, _ *pb.VerifyRequest) (*pb.VerifyResponse, error) {
	ks, err := s.managedStore()
	if err != nil {
		return nil, err
	}
	chunkErrs := ks.VerifyAll()
	protoErrs := make([]*pb.VerifyError, len(chunkErrs))
	for i, ce := range chunkErrs {
		protoErrs[i] = &pb.VerifyError{
			ChunkIndex: uint64(ce.ChunkIndex),
			FileName:   ce.FileName,
			Error:      ce.Err.Error(),
		}
	}
	return &pb.VerifyResponse{Errors: protoErrs}, nil
}

// Expire sweeps TTL-expired files and returns the number removed.
func (s *Server) Expire(_ context.Context, _ *pb.ExpireRequest) (*pb.ExpireResponse, error) {
	ks, err := s.managedStore()
	if err != nil {
		return nil, err
	}
	removed := ks.CleanupExpired()
	return &pb.ExpireResponse{Removed: int64(removed)}, nil
}

// Clean removes stored data files. If req.Deep is true, also removes metadata and cache.
func (s *Server) Clean(_ context.Context, req *pb.CleanRequest) (*pb.CleanResponse, error) {
	ks, err := s.managedStore()
	if err != nil {
		return nil, err
	}
	if req.Deep {
		result, cleanErr := ks.DeepClean()
		if cleanErr != nil {
			return nil, status.Errorf(codes.Internal, "deep clean: %v", cleanErr)
		}
		return &pb.CleanResponse{
			RemovedKdht:     int64(result.RemovedKDHT),
			RemovedMetadata: int64(result.RemovedMetadata),
			RemovedCache:    int64(result.RemovedCache),
		}, nil
	}
	// Shallow clean: .kdht only.
	kdhtPattern := filepath.Join(ks.StorageDir(), "data", "*.kdht")
	kdhtFiles, globErr := filepath.Glob(kdhtPattern)
	if globErr != nil {
		return nil, status.Errorf(codes.Internal, "glob kdht: %v", globErr)
	}
	if cleanErr := ks.CleanupKDHT(); cleanErr != nil {
		return nil, status.Errorf(codes.Internal, "cleanup kdht: %v", cleanErr)
	}
	return &pb.CleanResponse{RemovedKdht: int64(len(kdhtFiles))}, nil
}

// Stats returns byte-level storage usage for the server's storage root.
func (s *Server) Stats(_ context.Context, _ *pb.StatsRequest) (*pb.StatsResponse, error) {
	ks, err := s.managedStore()
	if err != nil {
		return nil, err
	}
	storageDir := ks.StorageDir()
	entries, readErr := os.ReadDir(storageDir)
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, status.Errorf(codes.Internal, "read storage dir: %v", readErr)
	}
	var dataBytes, metaBytes, cacheBytes, otherBytes uint64
	for _, entry := range entries {
		entryPath := filepath.Join(storageDir, entry.Name())
		size, sizeErr := dirSize(entryPath)
		if sizeErr != nil {
			return nil, status.Errorf(codes.Internal, "stat %s: %v", entry.Name(), sizeErr)
		}
		switch entry.Name() {
		case "data":
			dataBytes += size
		case "metadata":
			metaBytes += size
		case ".cache":
			cacheBytes += size
		default:
			otherBytes += size
		}
	}
	summaries := s.storage.ListKnownFilesMetadata()
	return &pb.StatsResponse{
		DataBytes:     dataBytes,
		MetadataBytes: metaBytes,
		CacheBytes:    cacheBytes,
		TotalBytes:    dataBytes + metaBytes + cacheBytes + otherBytes,
		FileCount:     int64(len(summaries)),
	}, nil
}

// dirSize returns the total byte size of all files under path.
func dirSize(path string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Size() > 0 {
			total += uint64(info.Size())
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	return total, nil
}
```

**Step 4: Add `"io/fs"` and `"os"` to the import block of `server.go`** (they are needed by `dirSize` and `Stats`).

**Step 5: Write failing tests in `server_test.go`**

Add after `TestDeleteFile`:

```go
func TestVerifyEmpty(t *testing.T) {
	client := newTestServer(t)
	resp, err := client.Verify(context.Background(), &pb.VerifyRequest{})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(resp.Errors) != 0 {
		t.Errorf("expected no errors on empty store, got %d", len(resp.Errors))
	}
}

func TestExpireEmpty(t *testing.T) {
	client := newTestServer(t)
	resp, err := client.Expire(context.Background(), &pb.ExpireRequest{})
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if resp.Removed < 0 {
		t.Errorf("expected non-negative removed count, got %d", resp.Removed)
	}
}

func TestCleanShallow(t *testing.T) {
	client := newTestServer(t)
	// Upload a file first so there is something to clean.
	stream, _ := client.Upload(context.Background())
	stream.Send(&pb.UploadChunk{Name: "toclean.txt", Size: 5})
	stream.Send(&pb.UploadChunk{Data: []byte("hello")})
	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatalf("upload: %v", err)
	}
	resp, err := client.Clean(context.Background(), &pb.CleanRequest{Deep: false})
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	if resp.RemovedKdht < 0 {
		t.Errorf("unexpected negative kdht count: %d", resp.RemovedKdht)
	}
}

func TestCleanDeep(t *testing.T) {
	client := newTestServer(t)
	stream, _ := client.Upload(context.Background())
	stream.Send(&pb.UploadChunk{Name: "todeep.txt", Size: 5})
	stream.Send(&pb.UploadChunk{Data: []byte("world")})
	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatalf("upload: %v", err)
	}
	resp, err := client.Clean(context.Background(), &pb.CleanRequest{Deep: true})
	if err != nil {
		t.Fatalf("deep clean: %v", err)
	}
	_ = resp // counts vary; just ensure no error
}

func TestStats(t *testing.T) {
	client := newTestServer(t)
	resp, err := client.Stats(context.Background(), &pb.StatsRequest{})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if resp.TotalBytes < 0 {
		t.Errorf("unexpected negative total bytes")
	}
}
```

**Step 6: Run tests**

```bash
go test ./src/api/grpc/... -v -run "TestVerify|TestExpire|TestClean|TestStats"
```
Expected: all five tests PASS.

**Step 7: Run full test suite to catch regressions**

```bash
make test
```
Expected: PASS.

**Step 8: Commit**

```bash
git add src/api/grpc/server.go src/api/grpc/server_test.go
git commit -m "feat(grpc): implement Verify, Expire, Clean, Stats management RPCs"
```

---

## Task 5: Add management methods to `GRPCClient`

**Files:**
- Modify: `cmd/client/remote.go`

**Step 1: Add result types near the top of `remote.go`** (after `RemoteFileEntry`):

```go
// VerifyIssue is a single integrity error returned by the remote Verify RPC.
type VerifyIssue struct {
	ChunkIndex uint64
	FileName   string
	Err        string
}

// RemoteCleanResult holds counts from the remote Clean RPC.
type RemoteCleanResult struct {
	RemovedKDHT     int64
	RemovedMetadata int64
	RemovedCache    int64
}

// RemoteStats holds storage statistics from the remote Stats RPC.
type RemoteStats struct {
	DataBytes     uint64
	MetadataBytes uint64
	CacheBytes    uint64
	TotalBytes    uint64
	FileCount     int64
}
```

**Step 2: Add four methods to `GRPCClient`** (append at end of `remote.go`):

```go
// Verify runs a remote integrity scan and returns any chunk errors.
func (c *GRPCClient) Verify() ([]VerifyIssue, error) {
	ctx, cancel := c.ctx()
	defer cancel()
	resp, err := c.stub.Verify(ctx, &pb.VerifyRequest{})
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	issues := make([]VerifyIssue, len(resp.Errors))
	for i, e := range resp.Errors {
		issues[i] = VerifyIssue{
			ChunkIndex: e.ChunkIndex,
			FileName:   e.FileName,
			Err:        e.Error,
		}
	}
	return issues, nil
}

// Expire triggers TTL expiry on the remote server and returns the count removed.
func (c *GRPCClient) Expire() (int64, error) {
	ctx, cancel := c.ctx()
	defer cancel()
	resp, err := c.stub.Expire(ctx, &pb.ExpireRequest{})
	if err != nil {
		return 0, fmt.Errorf("expire: %w", err)
	}
	return resp.Removed, nil
}

// Clean removes chunk data (and optionally metadata + cache) from the remote server.
func (c *GRPCClient) Clean(deep bool) (RemoteCleanResult, error) {
	ctx, cancel := c.ctx()
	defer cancel()
	resp, err := c.stub.Clean(ctx, &pb.CleanRequest{Deep: deep})
	if err != nil {
		return RemoteCleanResult{}, fmt.Errorf("clean: %w", err)
	}
	return RemoteCleanResult{
		RemovedKDHT:     resp.RemovedKdht,
		RemovedMetadata: resp.RemovedMetadata,
		RemovedCache:    resp.RemovedCache,
	}, nil
}

// RemoteStorageStats fetches storage usage from the remote server.
func (c *GRPCClient) RemoteStorageStats() (RemoteStats, error) {
	ctx, cancel := c.ctx()
	defer cancel()
	resp, err := c.stub.Stats(ctx, &pb.StatsRequest{})
	if err != nil {
		return RemoteStats{}, fmt.Errorf("stats: %w", err)
	}
	return RemoteStats{
		DataBytes:     resp.DataBytes,
		MetadataBytes: resp.MetadataBytes,
		CacheBytes:    resp.CacheBytes,
		TotalBytes:    resp.TotalBytes,
		FileCount:     resp.FileCount,
	}, nil
}
```

**Step 3: Build to verify**

```bash
go build ./cmd/client/...
```
Expected: no output, exit 0.

**Step 4: Commit**

```bash
git add cmd/client/remote.go
git commit -m "feat(client): add Verify, Expire, Clean, Stats methods to GRPCClient"
```

---

## Task 6: Add `getUploadDirEntries` to `filesystem.go`

The browse fallback for `upload` needs both files and directory names from `local/upload/`.

**Files:**
- Modify: `cmd/client/filesystem.go`

**Step 1: Add the `UploadEntry` type and `getUploadDirEntries` function**

Append to `filesystem.go`:

```go
// UploadEntry describes one item in the upload directory listing.
type UploadEntry struct {
	Name  string
	IsDir bool
}

// getUploadDirEntries lists all non-copy entries in dirPath,
// returning both files and subdirectory names tagged with IsDir.
// copy.* files are excluded as before.
func getUploadDirEntries(dirPath string) ([]UploadEntry, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", dirPath, err)
	}
	var result []UploadEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), "copy.") {
			continue
		}
		result = append(result, UploadEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	return result, nil
}
```

**Step 2: Build**

```bash
go build ./cmd/client/...
```

**Step 3: Commit**

```bash
git add cmd/client/filesystem.go
git commit -m "feat(client): add getUploadDirEntries for unified upload browse mode"
```

---

## Task 7: Add `promptUploadPath` to `menu.go` and remove old prompt functions

**Files:**
- Modify: `cmd/client/menu.go`

**Step 1: Remove `promptUploadSelection` and `resolveStorePath`**

Delete the entire `promptUploadSelection` function (lines ~254–317) and the entire `resolveStorePath` function (lines ~319–352). They are replaced by `promptUploadPath`.

**Step 2: Add `promptUploadPath`**

Add the new function in their place:

```go
// promptUploadPath handles the unified upload command.
//
// Prompt flow:
//   - Non-empty input: stat the path → file or dir routing.
//   - Empty input: browse local/upload/ with files and [dir] entries.
//
// Returns the resolved path, whether it is a directory, and any error.
// Returns errMenuBack if the user cancels.
func promptUploadPath(input io.Reader, cfg RuntimeConfig) (path string, isDir bool, err error) {
	reader := getBufferedReader(input)

	for {
		logs.Promptf("\nEnter path [%s]: ", cfg.UploadDirectory)
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			if readErr == io.EOF {
				return "", false, fmt.Errorf("no path provided")
			}
			return "", false, fmt.Errorf("read path: %w", readErr)
		}

		candidate := strings.TrimSpace(line)
		if strings.EqualFold(candidate, "e") {
			return "", false, errMenuBack
		}

		if candidate != "" {
			// Explicit path given: stat it.
			resolved := filepath.Clean(candidate)
			info, statErr := os.Stat(resolved)
			if statErr != nil {
				logs.Printf("Path not found: %v. Try again or press Enter to browse.\n", statErr)
				continue
			}
			return resolved, info.IsDir(), nil
		}

		// Empty input: browse local/upload/.
		entries, listErr := getUploadDirEntries(cfg.UploadDirectory)
		if listErr != nil || len(entries) == 0 {
			logs.Printf("No entries found in %s. Enter a path manually.\n", cfg.UploadDirectory)
			continue
		}

		logs.Titlef("\n%s:\n", cfg.UploadDirectory)
		for i, e := range entries {
			if e.IsDir {
				logs.Dataf("  %d) [dir] %s/\n", i, e.Name)
			} else {
				logs.Dataf("  %d) %s\n", i, e.Name)
			}
		}
		logs.Promptf("\nSelect [0-%d] or 'all' (files only): ", len(entries)-1)

		selLine, selErr := reader.ReadString('\n')
		if selErr != nil {
			if selErr == io.EOF {
				return "", false, fmt.Errorf("no selection")
			}
			return "", false, fmt.Errorf("read selection: %w", selErr)
		}
		sel := strings.TrimSpace(strings.ToLower(selLine))
		if sel == "e" {
			return "", false, errMenuBack
		}
		if sel == "all" || sel == "a" || sel == "*" {
			// Sentinel value: caller iterates all files in upload dir.
			return cfg.UploadDirectory, false, nil
		}

		idx, convErr := strconv.Atoi(sel)
		if convErr != nil || idx < 0 || idx >= len(entries) {
			logs.Printf("Invalid selection %q.\n", sel)
			continue
		}
		chosen := entries[idx]
		return filepath.Join(cfg.UploadDirectory, chosen.Name), chosen.IsDir, nil
	}
}

// confirmDirectoryUpload asks the user to confirm a recursive directory store.
// Returns true if confirmed, false (or errMenuBack) otherwise.
func confirmDirectoryUpload(input io.Reader, dirPath string) (bool, error) {
	reader := getBufferedReader(input)
	logs.Promptf("Store directory %q recursively? [y/N]: ", dirPath)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	choice := strings.ToLower(strings.TrimSpace(line))
	return choice == "y" || choice == "yes", nil
}
```

**Step 3: Update the menu display and action routing**

In `promptAction`, replace the `upload`, `store`, and `upload-dir` / `updir` cases with a single `upload` case:

Old lines to remove:
```go
case string(ActionUpload), "u", "up":
    if len(indexedFiles) == 0 {
        logs.StatusWarn("No indexed files are available under " + cfg.UploadDirectory + ".")
        logs.Printf("\n")
        continue
    }
    return ActionUpload, "upload (from upload dir)", nil

case string(ActionUploadDir), "ud", "updir":
    return ActionUploadDir, "upload directory", nil

case string(ActionStore), "s":
    return ActionStore, "store (explicit filepath)", nil
```

Replace with:
```go
case string(ActionUpload), "u", "up":
    return ActionUpload, "upload", nil
```

**Step 4: Update the menu display strings**

In `promptAction`, the printed menu lines currently read:
```
logs.Menuf("  store 	(chunk/store explicit filepath)\n")
logs.Menuf("  upload 	(chunk/store files from upload dir)\n")
logs.Menuf("  upload-dir 	(chunk/store entire directory)\n")
```

Replace all three with:
```go
logs.Menuf("  upload 	(store file or directory by path)\n")
```

**Step 5: Update the help text in the `default` case**

Remove the `store`, `upload`, and `updir` hint lines from the `default:` block:
```go
logs.KeyHint("u, up", "upload — store files from upload dir")
logs.KeyHint("s", "store — store explicit filepath")
```

Replace with:
```go
logs.KeyHint("u, up", "upload — store file or directory by path")
```

**Step 6: Build**

```bash
go build ./cmd/client/...
```

**Step 7: Commit**

```bash
git add cmd/client/menu.go
git commit -m "feat(client): unified upload command with path prompt and browse fallback"
```

---

## Task 8: Remove `ActionStore` and `ActionUploadDir` from `runtime_config.go`

**Files:**
- Modify: `cmd/client/runtime_config.go`

**Step 1: Remove the constant declarations**

Delete these two lines from the `const` block:
```go
ActionStore     MenuAction = "store"
ActionUploadDir MenuAction = "upload-dir"
```

**Step 2: Remove their CLI parser cases**

In `parseCLI`, delete the cases for `ActionStore` and `ActionUploadDir`/`"ud"`/`"updir"`:

```go
case string(ActionStore):
    ...
case string(ActionUploadDir), "ud", "updir":
    ...
```

**Step 3: Update `printUsage`**

Remove `store` and `upload-dir` from the actions description string. The new line should read:

```go
fmt.Println("Actions: upload (path prompt; empty = browse upload dir), clean (.kdht only), deep-clean (.kdht + metadata + cache), view (inspect metadata + optional reassemble), stats (storage/system stats), verify (deep integrity scan), delete (remove a single file), expire (sweep TTL-expired files), download (write stored file to disk).")
```

Also update the `fmt.Printf` usage line to remove `STORE_PATH_FLAG`:

```go
fmt.Printf("Usage: go run main.go [run|remote] [upload|clean|deep-clean|view|stats|verify|delete|expire|download] [%s] [%s] [%s N]\n",
    REASSEMBLE_FLAG,
    VERBOSE_FLAG,
    TTL_SECONDS_FLAG,
)
```

And remove the `STORE_PATH_FLAG` const and the line about it in usage output.

**Step 4: Build**

```bash
go build ./cmd/client/...
```

**Step 5: Commit**

```bash
git add cmd/client/runtime_config.go
git commit -m "refactor(client): remove ActionStore and ActionUploadDir constants and CLI args"
```

---

## Task 9: Refactor `executeActionOnce` in `main.go`

This is the core wiring task. Three changes:
1. Remove the blanket local-only guard for `ActionClean`, `ActionDeepClean`, `ActionVerify`, `ActionExpire`.
2. Replace `ActionUpload`, `ActionStore`, `ActionUploadDir` branches with unified `ActionUpload`.
3. Add remote branches for management actions.

**Files:**
- Modify: `cmd/client/main.go`
- Modify: `cmd/client/directory.go` (remove `executeUploadDirAction`)

**Step 1: Remove the local-only guard block**

Delete these lines from `executeActionOnce`:
```go
switch cfg.Action {
case ActionClean, ActionDeepClean, ActionVerify, ActionExpire:
    if cfg.Mode == ModeRemote {
        logs.Printf("Action %q is local-only. Switch to local mode to use it.\n", cfg.Action)
        return nil
    }
}
```

**Step 2: Replace the upload/store/upload-dir dispatch**

Remove from `executeActionOnce`:
```go
case ActionUpload:
    selectedUploads, selection, err := promptUploadSelection(indexedFiles, input, cfg)
    ...
    selectedTargets = ...

case ActionStore:
    storePath, selection, err := resolveStorePath(input, cfg)
    ...
    selectedTargets = []string{storePath}
```

And the second switch cases:
```go
case ActionUpload:
    if cfg.CleanCopyFiles { ... }
    if err := executeStoreTargets(cfg, keystore, selectedTargets); err != nil { ... }
case ActionStore:
    if cfg.CleanCopyFiles { ... }
    if err := executeStoreTargets(cfg, keystore, selectedTargets); err != nil { ... }
```

Replace with a single `ActionUpload` case that covers all upload logic:

```go
case ActionUpload:
    resolvedPath, isDir, resolveErr := promptUploadPath(input, cfg)
    if resolveErr != nil {
        return resolveErr
    }

    if isDir {
        if cfg.Mode == ModeRemote {
            logs.Println("Directory upload is not supported in remote mode. Upload files individually.")
            return nil
        }
        confirmed, confirmErr := confirmDirectoryUpload(input, resolvedPath)
        if confirmErr != nil {
            return confirmErr
        }
        if !confirmed {
            return errMenuBack
        }
        logs.Printf("\nUploading directory %q...\n", resolvedPath)
        dirHash, storeErr := keystore.StoreDirectory(resolvedPath)
        if storeErr != nil {
            return fmt.Errorf("store directory: %w", storeErr)
        }
        logs.Printf("Directory stored. Root hash: %x\n", dirHash)
        return nil
    }

    // File path — may be the upload dir itself (sentinel for "all").
    var filePaths []string
    if resolvedPath == filepath.Clean(cfg.UploadDirectory) {
        // "all" was selected in browse mode: collect all files (not dirs).
        entries, entErr := getUploadDirEntries(cfg.UploadDirectory)
        if entErr != nil {
            return fmt.Errorf("index upload dir: %w", entErr)
        }
        for _, e := range entries {
            if !e.IsDir {
                filePaths = append(filePaths, filepath.Join(cfg.UploadDirectory, e.Name))
            }
        }
        if len(filePaths) == 0 {
            logs.Println("No files found in upload directory.")
            return nil
        }
    } else {
        filePaths = []string{resolvedPath}
    }

    if cfg.CleanCopyFiles {
        if err := cleanupCopyFiles(cfg.KeyStore.StorageDir); err != nil {
            logs.Warnf("cleanup copy files: %v", err)
        }
    }
    if err := executeStoreTargets(cfg, keystore, filePaths); err != nil {
        return fmt.Errorf("upload failed: %w", err)
    }
```

**Step 3: Update management action cases to branch on mode**

Replace:
```go
case ActionVerify:
    return executeVerifyAction(cfg, keystore)
```
With:
```go
case ActionVerify:
    if cfg.Mode == ModeRemote {
        return executeRemoteVerify(cfg)
    }
    return executeVerifyAction(cfg, keystore)
```

Replace:
```go
case ActionExpire:
    return executeExpireAction(cfg, keystore)
```
With:
```go
case ActionExpire:
    if cfg.Mode == ModeRemote {
        return executeRemoteExpire(cfg)
    }
    return executeExpireAction(cfg, keystore)
```

Replace:
```go
case ActionClean:
    removed, err := cleanupAllKDHTFiles(cfg.KeyStore.StorageDir)
    ...
case ActionDeepClean:
    result, err := deepCleanStorage(cfg.KeyStore.StorageDir)
    ...
```
With:
```go
case ActionClean:
    if cfg.Mode == ModeRemote {
        return executeRemoteClean(cfg, false)
    }
    removed, err := cleanupAllKDHTFiles(cfg.KeyStore.StorageDir)
    if err != nil {
        return fmt.Errorf("failed to clean .kdht files: %w", err)
    }
    logs.Printf("Clean complete: removed %d .kdht file(s) from %s\n", removed, filepath.Join(cfg.KeyStore.StorageDir, "data"))
    return nil
case ActionDeepClean:
    if cfg.Mode == ModeRemote {
        return executeRemoteClean(cfg, true)
    }
    result, err := deepCleanStorage(cfg.KeyStore.StorageDir)
    if err != nil {
        return fmt.Errorf("failed to deep clean storage: %w", err)
    }
    logs.Printf("Deep clean complete: removed %d .kdht, %d metadata file(s), %d cache file(s).\n",
        result.RemovedKDHT, result.RemovedMetadata, result.RemovedCache)
    return nil
```

Replace the `ActionStats` case:
```go
case ActionStats:
    if err := executeStatsAction(cfg); err != nil {
        return fmt.Errorf("failed to collect stats: %w", err)
    }
    return nil
```
With:
```go
case ActionStats:
    return executeStatsAction(cfg)
```
(The remote path is already handled inside `executeStatsAction`; update that function in Step 5.)

**Step 4: Add the three remote management helpers at the bottom of `main.go`**

```go
func executeRemoteVerify(cfg RuntimeConfig) error {
    if cfg.RemoteAddr == "" {
        return fmt.Errorf("remote mode requires an address")
    }
    client, err := NewGRPCClient(cfg.RemoteAddr)
    if err != nil {
        return fmt.Errorf("connect to remote: %w", err)
    }
    defer client.Close()
    issues, err := client.Verify()
    if err != nil {
        return fmt.Errorf("remote verify: %w", err)
    }
    if len(issues) == 0 {
        logs.StatusInfo("Remote: all chunks verified — healthy."); logs.Printf("\n")
        return nil
    }
    logs.Printf("Remote found %d integrity error(s):\n", len(issues))
    for _, iss := range issues {
        logs.MenuItem(int(iss.ChunkIndex), iss.FileName+" — "+iss.Err, false)
        logs.Printf("\n")
    }
    return nil
}

func executeRemoteExpire(cfg RuntimeConfig) error {
    if cfg.RemoteAddr == "" {
        return fmt.Errorf("remote mode requires an address")
    }
    client, err := NewGRPCClient(cfg.RemoteAddr)
    if err != nil {
        return fmt.Errorf("connect to remote: %w", err)
    }
    defer client.Close()
    removed, err := client.Expire()
    if err != nil {
        return fmt.Errorf("remote expire: %w", err)
    }
    logs.Printf("Remote expire complete: %d file(s) removed.\n", removed)
    return nil
}

func executeRemoteClean(cfg RuntimeConfig, deep bool) error {
    if cfg.RemoteAddr == "" {
        return fmt.Errorf("remote mode requires an address")
    }
    client, err := NewGRPCClient(cfg.RemoteAddr)
    if err != nil {
        return fmt.Errorf("connect to remote: %w", err)
    }
    defer client.Close()
    result, err := client.Clean(deep)
    if err != nil {
        return fmt.Errorf("remote clean: %w", err)
    }
    if deep {
        logs.Printf("Remote deep clean: removed %d .kdht, %d metadata, %d cache file(s).\n",
            result.RemovedKDHT, result.RemovedMetadata, result.RemovedCache)
    } else {
        logs.Printf("Remote clean: removed %d .kdht file(s).\n", result.RemovedKDHT)
    }
    return nil
}
```

**Step 5: Update `executeStatsAction` in `stats.go` to use the new `Stats` RPC**

In `stats.go`, replace the remote section:
```go
if cfg.Mode == ModeRemote && cfg.RemoteAddr != "" {
    logs.Titlef("\nRemote Server: %s\n", cfg.RemoteAddr)
    client, dialErr := NewGRPCClient(cfg.RemoteAddr)
    if dialErr != nil {
        logs.Dataf("  Status: unreachable (%v)\n", dialErr)
    } else {
        defer client.Close()
        entries, listErr := client.List()
        if listErr != nil {
            logs.Dataf("  Status: unreachable (%v)\n", listErr)
        } else {
            var totalSize uint64
            for _, e := range entries {
                totalSize += e.Size
            }
            logs.Dataf("  Status: reachable\n")
            logs.Dataf("  Files: %d  Total size: %s\n", len(entries), formatBytes(totalSize))
        }
    }
}
```

With:
```go
if cfg.Mode == ModeRemote && cfg.RemoteAddr != "" {
    logs.Titlef("\nRemote Server: %s\n", cfg.RemoteAddr)
    client, dialErr := NewGRPCClient(cfg.RemoteAddr)
    if dialErr != nil {
        logs.Dataf("  Status: unreachable (%v)\n", dialErr)
    } else {
        defer client.Close()
        rs, statsErr := client.RemoteStorageStats()
        if statsErr != nil {
            logs.Dataf("  Status: unreachable (%v)\n", statsErr)
        } else {
            logs.Dataf("  Status: reachable\n")
            logs.Dataf("  Files: %d\n", rs.FileCount)
            logs.Field("  data/", formatBytes(rs.DataBytes)); logs.Printf("\n")
            logs.Field("  metadata/", formatBytes(rs.MetadataBytes)); logs.Printf("\n")
            logs.Field("  .cache/", formatBytes(rs.CacheBytes)); logs.Printf("\n")
            logs.Field("  total", formatBytes(rs.TotalBytes)); logs.Printf("\n")
        }
    }
}
```

**Step 6: Remove `executeUploadDirAction` from `directory.go`**

Delete the entire `executeUploadDirAction` function — it is now inlined in the `ActionUpload` case. If `directory.go` has no other functions after removal, delete the file; otherwise keep it for any remaining helpers.

**Step 7: Remove the `indexedFiles` parameter from `executeActionOnce`**

The `ActionUpload` case no longer uses `indexedFiles` (removed `promptUploadSelection`). Remove the parameter from the signature and all call sites.

Update `runInteractiveSession`:
```go
err = executeActionOnce(cfg, keystore, reader)
```
Update `main()` one-shot path:
```go
if err := executeActionOnce(cfg, keystore, os.Stdin); err != nil {
```
Update `refreshMenuContext` — it still returns `indexedFiles` for the menu display of "no files to upload" warning? Actually, with the new upload command that warning is gone. Simplify `refreshMenuContext` to only return `metadataCount`:
```go
func refreshMenuContext(cfg RuntimeConfig, keystore *key_store.KeyStore) (int, error) {
    if err := keystore.ReloadLocalState(); err != nil {
        return 0, fmt.Errorf("failed to reload keystore state: %w", err)
    }
    return len(keystore.ListKnownFiles()), nil
}
```

Update all callers.

**Step 8: Build**

```bash
go build ./cmd/client/...
```
Expected: no output, exit 0.

**Step 9: Run the full test suite**

```bash
make test
```
Expected: PASS.

**Step 10: Commit**

```bash
git add cmd/client/main.go cmd/client/stats.go cmd/client/directory.go
git commit -m "feat(client): unified upload command and remote management action routing"
```

---

## Task 10: Final verification

**Step 1: Build all packages**

```bash
make build
```
Expected: all binaries built, no errors.

**Step 2: Run full test suite with coverage**

```bash
make test-coverage
```
Expected: PASS with coverage output.

**Step 3: Smoke test the client (interactive)**

```bash
make client ARGS="--mode local --storage local/storage"
```

Verify:
- Menu shows `upload` but not `store` or `upload-dir`
- Typing `upload` then Enter prompts for a path
- Pressing Enter again at the path prompt shows the browse listing with `[dir]` entries if any subdirs exist in `local/upload/`
- Typing a file path stores it
- Typing a directory path shows confirmation prompt

**Step 4: Final commit if any last fixes**

```bash
git add -p
git commit -m "fix(client): post-integration cleanup"
```
