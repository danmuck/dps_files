# CLAUDE.md — dps_files Project Guide

> **Before every pass, read `AGENTS.md` first.** It defines execution policy, documentation governance, buildlog discipline, and canonical doc rules that apply to all work in this repository.

## Execution Discipline

- After any code or documentation update, review `docs/progress/` artifacts before concluding the pass.
- Update the relevant `docs/progress` buildplan/checklist entries in the same pass so tracker state matches repository state.

## Context Management

- Use `/compact` frequently during long sessions to reduce token surface and keep context focused.
- Use `/clear` when starting a new logical task or when prior context is no longer relevant.
- All agents (including subagents) should `/compact` after completing major steps.

## Project Overview

**dps_files** is a decentralized file storage system written in Go. It combines three distributed-systems primitives:

- **Kademlia DHT** — Peer discovery and distributed chunk routing using XOR-distance-based k-buckets.
- **Raft Consensus** — A root cluster of server nodes maintains authoritative metadata via leader election and log replication.

Files are split into fixed-size chunks, each assigned a 20-byte SHA-1 DHT key (via `computeChunkKey`). Chunks are stored locally as `.kdht` files and (when networking is complete) distributed across DHT participants. Metadata is persisted as `.toml` files.

## Directory Structure

```
cmd/
  server/main.go      — ServerNode entry point (gRPC + optional gRPC-Gateway HTTP)
  client/main.go      — ClientNode entry point (interactive TUI, local or remote mode)
  gen_file/main.go    — Test file generator (size-aware, reuses existing files)
  internal/logcfg/    — Shared smplog config loader

src/
  api/
    nodes/             — Node, ServerNode, ClientNode interfaces + DefaultServerNode, DefaultClientNode, DefaultRouter, gateway.go
    pb/                — Generated gRPC + grpc-gateway code from dps.proto (DPSFilesClient, DPSFilesServer, all message types)
    grpc/              — grpcserver.Server: implements pb.DPSFilesServer backed by ledgers.FileLedger
    ledgers/           — Interfaces for LogManager, MetadataStore, FileLedger, SnapshotManager, BackupLedger
  key_store/           — KeyStore, KeyStoreLedger (FileLedger adapter), File, FileReference, MetaData, RemoteHandler, chunking pipeline

tools/gen_text/        — Python test file generator (legacy, outputs should target local/upload)
docs/progress/         — Build plan and progress tracking
docs/plans/            — Design documents and implementation plans
local/upload/          — Upload/test input files
local/storage/         — Runtime data (gitignored): data/*.kdht, .cache/, metadata/
local/logs/            — Operation logs from transfer workflows
```

## Build & Run Commands

All commands are in the `Makefile`:

```sh
make test                                  # build, go test -v ./..., clean .build/
make test-coverage                         # build, go test -v ./... -cover, clean .build/
make build                                 # go build each cmd/* into .build/<name>/
make server ARGS="--addr :9000 --http :8080 --storage local/storage"  # run ServerNode
make client ARGS="--mode local --storage local/storage"               # run ClientNode TUI
make gen-file SIZE=256MB FILE=local/upload/test.dat # generate test file
make tidy                                  # go mod tidy
make build-protobuf                        # protoc → src/api/pb/ (go, go-grpc, grpc-gateway plugins)
make clean                                 # rm -rf .build/
```

## Key Packages & Files

### `key_store` — Local File Storage Pipeline (FUNCTIONAL)

- **`key_store.go`** — `KeyStore` struct: manages chunk storage directory, metadata persistence, file operations, verification. Includes streaming (`StreamFile`, `StreamFileByName`, `StreamChunkRange`), TTL expiry (`CleanupExpired`), cache management, and `StoreFromReader`.
- **`file_ledger.go`** — `KeyStoreLedger` adapter: wraps `*KeyStore` to implement the `ledgers.FileLedger` interface. Converts between KeyStore's concrete types and FileLedger's `FileID`/`ChunkID` typed aliases.
- **`files.go`** — `File` struct, `StoreFileLocal`, `LoadAndStoreFileLocal`, `LoadAndStoreFileRemote`, `StoreFromReader`, `ReassembleFileToBytes`, `ReassembleFileToPath`. Contains `computeChunkKey` — the canonical DHT key derivation.
- **`file_reference.go`** — `FileReference` struct: per-chunk metadata (key, hash, index, location, protocol).
- **`metadata.go`** — `MetaData` struct: per-file metadata (`EntryType`, `ParentHash` for directory support, `IsDirectory()` helper). TOML serialization to `local/storage/metadata/`.
- **`directory.go`** — `DirectoryEntry`, `DirectoryManifest` types, `NormalizePath()`, `StoreDirectory()`, `ListDirectory()`, `ReassembleDirectory()` for recursive directory ingest/browse/download.
- **`config.go`** — `KeyStoreConfig`, `DefaultConfig()`, `CalculateBlockSize()` with promotion logic, `HashFile()`, `CopyFile()`, `ValidateSHA256()`. Constants (`KeySize=20`, `HashSize=32`, `CryptoSize=64`, block size limits), `RemoteHandler` interface, `DefaultRemoteHandler`.
- **`verify.go`** — `VerifyAll()`, `VerifyFile()`: deep integrity scanning of all stored chunks.
- **`intent.go`** — Crash recovery via intent files (write-ahead before chunking).
- **`string.go`** — String/formatting helpers.

### `nodes` — Node Types & Routing (FUNCTIONAL)

- **`nodes.go`** — Interfaces: `Node`, `ServerNode` (Storage), `ClientNode` (Stub/LocalServer). `NodeInfo` struct (ID, Address — moved here from the former transport package). `NodeState` enum (Follower/Candidate/Leader).
- **`default.go`** — `DefaultNode`: identity-only base (address, pubKey, Router). No TCPHandler, no exit channel, no Start()/Shutdown(). Constructor: `NewDefaultNode(id, addr)`.
- **`server_node.go`** — `DefaultServerNode`: embeds DefaultNode, holds `*grpc.Server` + `net.Listener`. `Start()` binds TCP and calls `grpcServer.Serve`. `WithHTTP(addr)` option starts gRPC-Gateway on a second port. `Addr()` returns the live listener address. `RawKeyStore()` exposes the underlying KeyStore for TUI access. Constructor: `NewServerNode(id, addr, storageDir, opts...)`.
- **`client_node.go`** — `DefaultClientNode`: embeds DefaultNode, holds one `*grpc.ClientConn` (`activeConn`). In local mode starts an embedded ServerNode and connects to it over gRPC on `localhost:0`. `Stub() (pb.DPSFilesClient, error)` returns the gRPC stub. `LocalServer() *DefaultServerNode` returns the embedded server (nil in remote mode). Constructor: `NewClientNode(id, opts...)` with `WithLocalStorage(dir)`, `WithRemotes(addrs...)`.
- **`gateway.go`** — `serveGateway(httpAddr, grpcAddr)`: starts an HTTP/JSON reverse proxy using grpc-gateway that forwards to the gRPC server. Called by `DefaultServerNode.Start()` when `WithHTTP` is configured.
- **`routing.go`** — `RoutingTable` and `KademliaRouting` interfaces. `DefaultRouter` (map-based, functional).

### `pb` — Generated gRPC & Gateway Code (GENERATED)

- **`dps.proto`** — Service definition for `DPSFiles`: `Upload` (client-streaming), `Download` (server-streaming), `Delete`, `List`, `UploadDir`, `ListDir`. HTTP annotations map each RPC to a REST route under `/v1/`. Message types: `UploadChunk`, `UploadResponse`, `DownloadRequest`, `DataChunk`, `DeleteRequest`, `DeleteResponse`, `ListRequest`, `ListResponse`, `FileEntry`, `UploadDirRequest`, `UploadDirResponse`, `ListDirRequest`, `ListDirResponse`, `DirEntry`.
- **`dps.pb.go`** — Generated message types (protoc-gen-go).
- **`dps_grpc.pb.go`** — Generated `DPSFilesClient`, `DPSFilesServer`, `RegisterDPSFilesServer`, `NewDPSFilesClient` (protoc-gen-go-grpc).
- **`dps.pb.gw.go`** — Generated `RegisterDPSFilesHandlerFromEndpoint` HTTP/JSON gateway (protoc-gen-grpc-gateway).

### `grpc` — gRPC Server Implementation (FUNCTIONAL)

Package name: `grpcserver`.

- **`server.go`** — `Server` struct: implements `pb.DPSFilesServer` backed by `ledgers.FileLedger`. Constructor: `grpcserver.New(storage ledgers.FileLedger) *Server`. Methods: `Upload` (client-streaming via `io.Pipe`, calls `StoreFromReader`), `Download` (server-streaming via `io.Pipe`, calls `StreamFile`/`StreamFileByName`), `Delete`, `List`, `UploadDir`, `ListDir`. Upload/Download use `io.Pipe` so no large in-memory buffers are needed for large files.
- **`server_test.go`** — Tests using `bufconn` in-memory transport: `TestUploadAndList`, `TestDownloadByHash`, `TestDeleteFile`.

### `ledgers` — Consensus & Backup Interfaces (INTERFACES ONLY)

- **`net_store.go`** — `LogManager`, `MetadataStore`, `FileLedger` interfaces (including directory operations). `FileID`, `ChunkID` typed aliases. `FileMetaSummary`, `DirectoryEntry` structs.
- **`snapshots.go`** — `SnapshotManager`, `BackupLedger` interfaces.

## Coding Conventions

- **Error handling:** Wrap errors with `fmt.Errorf("context: %w", err)`. Return errors, never panic. Clean up on failure (e.g., delete partial chunks if a store fails midway).
- **Naming:** Standard Go conventions. Exported types are PascalCase, unexported are camelCase.
- **IDs & Hashes:**
  - DHT routing keys: 20 bytes (SHA-1) — `KeySize`
  - Data integrity hashes: 32 bytes (SHA-256) — `HashSize`
  - Crypto operations: 64 bytes (SHA-512) — `CryptoSize`
- **DHT key derivation:** Always use `computeChunkKey(fileHash, chunkIndex)` — appends index as little-endian uint64, then SHA-1.
- **File extensions:** `.kdht` for chunk data files, `.toml` for metadata.
- **Chunk sizing:** Dynamic based on file size, bounded by `MinBlockSize` (64KB) and `MaxBlockSize` (4MB), targeting ~1000 chunks per file. Empty files produce 0 blocks.
- **Serialization:** Protobuf for RPC messages (via gRPC), TOML for metadata persistence.
- **Dependencies:** Minimal — `BurntSushi/toml`, `google.golang.org/grpc`, `google.golang.org/protobuf`, `github.com/grpc-ecosystem/grpc-gateway/v2`, and `github.com/danmuck/smplog` (structured logging via zerolog).
- **Logging:** Uses `github.com/danmuck/smplog` with shared config loaded via `cmd/internal/logcfg`. Config resolves `SMPLOG_CONFIG` env var, then `./smplog.config.toml`, then `./local/smplog.config.toml`.

## Architecture Patterns

### Node Hierarchy

```
Node (base interface: ID, Address, NodeInfo, Start, Shutdown, Peers)
├── ServerNode (extends Node: Storage() FileLedger)
│   └── DefaultServerNode (KeyStore via FileLedger, *grpc.Server + net.Listener, optional gRPC-Gateway)
└── ClientNode (extends Node: Stub() DPSFilesClient, LocalServer() *DefaultServerNode)
    └── DefaultClientNode (activeConn *grpc.ClientConn; local mode embeds ServerNode, remote mode dials directly)
```

### Interfaces to Implement

When adding new node types or storage backends:

- **`ServerNode`** — For storage servers: `Storage() FileLedger`.
- **`ClientNode`** — For file operation clients: `Stub() (pb.DPSFilesClient, error)`, `LocalServer() *DefaultServerNode`.
- **`FileLedger`** — For storage backends: wraps chunk storage with streaming, deletion, metadata listing.
- **`RemoteHandler`** — For network chunk distribution: `StartReceiver`, `PassFileReference`, `Receive`.
- **`KademliaRouting`** — For DHT routing (future): `K()`, `A()`, `GetBucket()`, `ClosestK()`, `Size()`.

### Future Extension Interfaces (not yet implemented)

- **`RaftNode`** — Will extend `ServerNode` with: `ApplyCommand`, `CreateSnapshot`, `GetState`, `AddPeer`, `RemovePeer`.
- **`KademliaNode`** — Will extend `ClientNode` with: `FindNode`, `FindValue`, `Store` (DHT operations).

### File Storage Flow

```
Input file → calculate metadata (SHA-256, size, permissions)
  → split into chunks (dynamic size, ~1000 chunks target)
  → each chunk gets SHA-1 key (computeChunkKey) + SHA-256 hash (for integrity)
  → store chunks as local/storage/data/{key}.kdht
  → persist metadata as local/storage/metadata/{hash}.toml
  → (future) distribute chunks via DHT STORE RPCs
```

## Current State

### Working

- File chunking, storage, and reassembly (`key_store` package)
- FileLedger adapter (`KeyStoreLedger`) bridging KeyStore to the ledger interface
- gRPC transport: Upload/Download/List/Delete/UploadDir/ListDir via `DPSFiles` service
- gRPC-Gateway: HTTP/JSON proxy exposing gRPC service on a second port (`WithHTTP` option)
- Streaming file transfer via `io.Pipe` — no in-memory buffering for large files (Upload + Download)
- ServerNode backed by gRPC (`*grpc.Server` + `net.Listener`), graceful shutdown
- ClientNode with local mode (embedded ServerNode connected over gRPC) and remote mode
- Directory semantics: recursive store, list, reassemble for entire directory trees (manifests stored as MetaData entries)
- Interactive TUI client (`cmd/client`) with upload, upload-dir, download, view, delete, verify, stats
- Crash recovery via intent files (write-ahead before chunking)
- Deep integrity verification (`VerifyAll`, `VerifyFile`)
- TTL-based expiry and cleanup (`CleanupExpired`)
- Cache management with deduplication
- Concurrent access safety (RWMutex)
- Metadata persistence and loading (TOML)
- Structured logging via smplog (project-wide)
- Node creation, start/shutdown lifecycle with signal handling

### Future (Stubs)

> These are interface stubs or empty scaffolding. Do not build on these without redesign.

- Kademlia routing (interface defined, no XOR distance or bucket logic)
- UDP transport (no implementation)
- RemoteHandler (placeholder, not wired to network)
- Raft consensus (interfaces defined, no implementation)
- Snapshot/backup scheduling (interfaces defined, no implementation)
- Log replication and leader election (not started)

### Remaining Known Issues

- No TLS on gRPC connections (insecure credentials used everywhere)
- `DefaultClientNode` connects to the first remote address only (no load balancing or failover)

For detailed per-module issue tracking, see `docs/progress/buildplan.md`.

## Common Tasks

### Add a New Node Type

1. Define a struct in `src/api/nodes/` that embeds `*DefaultNode`.
2. Implement `ServerNode` or `ClientNode` interface.
3. Add a constructor following `NewServerNode` or `NewClientNode` patterns.
4. Add a `cmd/` entry point if needed.

### Add a New RPC Method

1. Add the RPC to the `DPSFiles` service in `src/api/pb/dps.proto` (with `google.api.http` annotation for gateway).
2. Run `make build-protobuf` to regenerate `src/api/pb/` (go, go-grpc, grpc-gateway outputs).
3. Implement the method in `grpcserver.Server` in `src/api/grpc/server.go`.
4. Add a client helper using the generated `pb.DPSFilesClient` stub where needed.

### Generate Test Files

```sh
go run cmd/gen_file/main.go 256MB local/upload/test_256mb.dat
# Or via Makefile:
make gen-file SIZE=256MB FILE=local/upload/test_256mb.dat
```

Files are reused if they already exist with the matching size.

### Run the Interactive Client

```sh
make client
# Or with flags:
make client ARGS="--mode local --storage local/storage"
make client ARGS="--mode remote --remotes localhost:9000"
```

### Run a Server Node

```sh
make server ARGS="--addr :9000 --http :8080 --storage local/storage"
```

### Add a New Transport Protocol

The canonical transport layer is now gRPC. To add a parallel transport (e.g., for Kademlia UDP):

1. Define a new interface or extend `src/api/nodes/nodes.go` for the new protocol.
2. Implement send/receive using the same `ledgers.FileLedger` storage backend.
3. Wire the new transport into the relevant node type.

## Testing

```sh
make test            # Run all tests (includes 256MB large file test)
go test -short ./... # Skip large file test
make test-coverage   # Run with coverage report
```

Test files follow `*_test.go` convention in their respective packages:

- `src/key_store/store_test.go` — chunking (1KB-256MB), empty file, single chunk, exact block size, persistence, corruption detection, cleanup, key consistency, streaming, TTL, deletion, cache dedup, utility functions
- `src/key_store/hardening_test.go` — concurrent stores/reads/deletes, crash recovery intents, integrity verification, error injection cleanup, stale cache pruning, non-destructive startup, reupload after restart
- `src/key_store/config_test.go` — KeyStoreConfig defaults, configurable TTL
- `src/key_store/file_ledger_test.go` — FileLedger adapter: interface satisfaction, store/list/reassemble/delete round-trip
- `src/api/nodes/routing_test.go` — node creation, start/shutdown lifecycle, router type verification
- `src/api/nodes/server_node_test.go` — ServerNode start/shutdown, gRPC upload + list round-trip
- `src/api/nodes/client_node_test.go` — ClientNode local mode (gRPC to embedded server), remote mode, LocalServer() access, error on no-server
- `src/api/grpc/server_test.go` — gRPC server via bufconn: upload+list, download by hash, delete

Test data goes in `./local/upload/` (created by tests, reused across runs). The `local/storage/` directory is used at runtime and is gitignored.

## Git

- Do not commit changes unless explicitly instructed to do so.
- Include feature size breakpoints in task lists to ask if I would like to commit the changes, giving me time to look them over before I commit them.
- By default, I make all commits
