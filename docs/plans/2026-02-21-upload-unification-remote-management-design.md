# Upload Unification & Remote Management Design

**Date:** 2026-02-21
**Status:** Approved

## Overview

Two related changes to the TUI client and gRPC layer:

1. **Upload unification** — collapse `store`, `upload`, and `upload-dir` into a single `upload` command with inline path entry and an optional browse fallback.
2. **Remote management RPCs** — add `Verify`, `Expire`, `Clean`, `Stats` RPCs to `dps.proto` so that all management operations work against remote servers, not just local storage.

---

## Section 1: Upload Unification

### Goal

Replace three separate menu entries (`store`, `upload`, `upload-dir`) with a single `upload` command that handles any path — file or directory — and falls back to a browse view of `local/upload/` when no path is typed.

### Menu changes

- **Removed:** `store`, `upload-dir`, `updir`
- **Kept:** `upload` (unified)

### `upload` flow

1. Prompt: `Enter path [local/upload/]: `
2. **Non-empty input:**
   - `filepath.Clean` + `os.Stat` the input
   - File → run `executeStoreTargets`
   - Dir → confirm `Store <path> recursively? [y/N]:` → on yes, call `ks.StoreDirectory`
   - Not found → friendly error, re-prompt
3. **Empty input (browse mode):**
   - List all entries in `local/upload/` as a numbered menu
   - Files shown as `N) filename`
   - Directories shown as `N) [dir] dirname/`
   - User picks by index or `all` (`all` applies to files only)
   - Selecting a file → `executeStoreTargets` with full path
   - Selecting a `[dir]` entry → confirm recursion prompt → `ks.StoreDirectory`

### Code changes

- **Removed:** `ActionStore`, `ActionUploadDir` constants; `resolveStorePath`, `promptUploadSelection` functions
- **Added:** `promptUploadPath(input, cfg)` in `menu.go` — handles both path entry and browse fallback
- **CLI parser:** `store`, `upload-dir`, `updir` args removed; `upload` remains
- **`executeActionOnce`:** single `ActionUpload` branch replaces `ActionUpload`, `ActionStore`, `ActionUploadDir`

---

## Section 2: Remote Management RPCs

### Goal

Add four management RPCs to `dps.proto` so `verify`, `expire`, `clean`, `deep-clean`, and `stats` work against remote servers in addition to local storage.

### Proto additions (`src/api/pb/dps.proto`)

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

message VerifyRequest {}
message VerifyError { uint64 chunk_index = 1; string file_name = 2; string error = 3; }
message VerifyResponse { repeated VerifyError errors = 1; }

message ExpireRequest {}
message ExpireResponse { int64 removed = 1; }

// deep=true → .kdht + metadata + cache; false → .kdht only
message CleanRequest { bool deep = 1; }
message CleanResponse { int64 removed_kdht = 1; int64 removed_metadata = 2; int64 removed_cache = 3; }

message StatsRequest {}
message StatsResponse {
  uint64 data_bytes     = 1;
  uint64 metadata_bytes = 2;
  uint64 cache_bytes    = 3;
  uint64 total_bytes    = 4;
  int64  file_count     = 5;
}
```

### Server-side (`src/api/grpc/server.go`)

Four new methods on `grpcserver.Server`:
- `Verify` → type-asserts `FileLedger` to `*key_store.KeyStoreLedger`, calls `ks.VerifyAll()`, maps results to `VerifyError` messages
- `Expire` → calls `ks.CleanupExpired()`, returns count
- `Clean` → calls `ks.CleanupKDHT()` (deep=false) or full deep clean (deep=true), returns counts
- `Stats` → walks storage dirs, returns byte counts and file count

The `FileLedger` interface is not extended. The gRPC server type-asserts to reach the concrete `*key_store.KeyStore` via `KeyStoreLedger.RawStore()` (a new unexported accessor or the existing `RawKeyStore` pattern from `server_node.go`).

### Client-side (`cmd/client/remote.go`)

Four new methods on `GRPCClient`:
- `Verify() ([]VerifyError, error)`
- `Expire() (int64, error)`
- `Clean(deep bool) (CleanResult, error)`
- `Stats() (StatsResult, error)`

### `executeActionOnce` changes

Remove the blanket local-only guard for `ActionClean`, `ActionDeepClean`, `ActionVerify`, `ActionExpire`. Each action branches on `cfg.Mode`:
- `ModeRun` → existing KeyStore calls (unchanged)
- `ModeRemote` → new GRPCClient methods

`ActionStats` unified: remote path uses the new `Stats` RPC instead of `List` + reachability check.

### Regeneration

`make build-protobuf` re-runs protoc to update `src/api/pb/` (go, go-grpc, grpc-gateway outputs).

---

## Implementation Order

1. Edit `dps.proto` — add messages and RPCs
2. Run `make build-protobuf` — regenerate `pb/`
3. Implement server-side methods in `grpcserver.Server`
4. Add client methods to `GRPCClient` in `remote.go`
5. Refactor `menu.go` — add `promptUploadPath`, remove old prompt functions
6. Refactor `runtime_config.go` — remove `ActionStore`, `ActionUploadDir`
7. Refactor `executeActionOnce` in `main.go` — unified upload branch, remote management branches
8. Remove `directory.go` execute function body (logic moves into `executeActionOnce`)
9. Update `printUsage` and CLI parser
10. Run `make test` — confirm all tests pass
