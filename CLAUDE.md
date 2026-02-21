# CLAUDE.md — dps_files Project Guide

> **Before every pass, read `AGENTS.md` first.** It defines execution policy, documentation governance, buildlog discipline, and canonical doc rules that apply to all work in this repository.

## Execution Discipline

- After any code or documentation update, review `docs/progress/` artifacts before concluding the pass.
- Update the relevant `docs/progress` buildplan/checklist entries in the same pass so tracker state matches repository state.

## Project Overview

**dps_files** is a decentralized file storage system written in Go. It combines three distributed-systems primitives:

- **Kademlia DHT** — Peer discovery and distributed chunk routing using XOR-distance-based k-buckets.
- **Raft Consensus** — A root cluster of server nodes maintains authoritative metadata via leader election and log replication.
- **Blockchain Backup Ledger** — Periodic snapshots of Raft state are sealed into an append-only chain for tamper-evident history.

Files are split into fixed-size chunks, each assigned a 20-byte SHA-1 DHT key (via `computeChunkKey`). Chunks are stored locally as `.kdht` files and (when networking is complete) distributed across DHT participants. Metadata is persisted as `.toml` files.

## Directory Structure

```
cmd/
  server/main.go      — Server node entry point (TCP listener demo)
  client/main.go      — Client node entry point (Protobuf RPC demo)
  chain/main.go       — Blockchain demo with AES-GCM encryption
  storage/             — File chunking integration flow (interactive CLI)
  gen_file/main.go    — Test file generator (size-aware, reuses existing files)
  fileserver/          — TCP file server (4-byte length-prefixed binary protocol)
  httpserver/          — HTTP file server (PUT/GET/Range/DELETE)
  internal/logcfg/    — Shared smplog config loader

src/
  api/
    nodes/             — Node interfaces (ServerNode, ClientNode, MasterNode), DefaultNode, routing tables
    transport/         — TransportHandler interface, TCPHandler, Protobuf encoding, rpc.proto
    ledgers/           — Interfaces for LogManager, MetadataStore, FileLedger, SnapshotManager, BackupLedger
  impl/                — Block, BlockData, crypto utilities (SHA, AES-GCM)
  key_store/           — KeyStore, File, FileReference, MetaData, RemoteHandler, chunking pipeline

tools/gen_text/        — Python test file generator (legacy, outputs should target local/upload)
docs/progress/         — Build plan and progress tracking
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
make server                                # go run cmd/server/main.go
make client                                # go run cmd/client/main.go
make chain                                 # go run cmd/chain/main.go
make storage ARGS="..."                    # go run ./cmd/storage (interactive CLI)
make fileserver ARGS="..."                 # go run ./cmd/fileserver (TCP file server)
make httpserver ARGS="..."                 # go run ./cmd/httpserver (HTTP file server)
make gen-file SIZE=256MB FILE=local/upload/test.dat # generate test file
make tidy                                  # go mod tidy
make build-protobuf                        # protoc → src/api/transport/rpc.pb.go
make clean                                 # rm -rf .build/
```

## Key Packages & Files

### `key_store` — Local File Storage Pipeline (FUNCTIONAL)
- **`key_store.go`** — `KeyStore` struct: manages chunk storage directory, metadata persistence, file operations, verification. Includes streaming (`StreamFile`, `StreamFileByName`, `StreamChunkRange`), TTL expiry (`CleanupExpired`), cache management, and `StoreFromReader`.
- **`files.go`** — `File` struct, `StoreFileLocal`, `LoadAndStoreFileLocal`, `LoadAndStoreFileRemote`, `StoreFromReader`, `ReassembleFileToBytes`, `ReassembleFileToPath`. Contains `computeChunkKey` — the canonical DHT key derivation.
- **`file_reference.go`** — `FileReference` struct: per-chunk metadata (key, hash, index, location, protocol).
- **`metadata.go`** — `MetaData` struct: per-file metadata. TOML serialization to `local/storage/metadata/`.
- **`config.go`** — `KeyStoreConfig`, `DefaultConfig()`, `CalculateBlockSize()` with promotion logic, `HashFile()`, `CopyFile()`, `ValidateSHA256()`. Constants (`KeySize=20`, `HashSize=32`, `CryptoSize=64`, block size limits), `RemoteHandler` interface, `DefaultRemoteHandler`.
- **`verify.go`** — `VerifyAll()`, `VerifyFile()`: deep integrity scanning of all stored chunks.
- **`intent.go`** — Crash recovery via intent files (write-ahead before chunking).
- **`string.go`** — String/formatting helpers.

### `impl` — Blockchain & Crypto (FUNCTIONAL)
- **`block.go`** — `Block` struct (all fields exported for gob encoding) with `NewBlock()` / `NewBlockEncrypt()`, hash validation, chain verification.
- **`block_data.go`** — `BlockData` struct (Hash, Data, IV).
- **`utils.go`** — `EncryptData()` / `DecryptData()` (AES-256-GCM), `CalculateHash()` (SHA-1/256/512), `ValidateHash()`. Handles both `*Block` and `Block` value types.

### `nodes` — Node Types & Routing (SCAFFOLDING)
- **`nodes.go`** — Interfaces: `Node`, `ServerNode`, `ClientNode`, `MasterNode`. `NodeState` enum (Follower/Candidate/Leader).
- **`default.go`** — `DefaultNode`: basic implementation with address, ID, peers list, transport binding. Returns `*DefaultNode` (pointer).
- **`routing.go`** — `RoutingTable` and `KademliaRouting` interfaces (return `*transport.NodeInfo`). `DefaultRouter` (map-based) works; `KademliaRouter` is a stub.

### `transport` — Network & RPC (PARTIAL)
- **`transport.go`** — `TransportHandler` interface: `ListenAndAccept`, `Send(*RPC)`, `ProcessRPC`, `Close() error`.
- **`tcp.go`** — `TCPHandler`: non-blocking accept, 2-byte length-prefixed Protobuf messages, `Send()` encodes via `Coder.Encode()`.
- **`encoding.go`** — `Coder` interface, `DefaultCoder` using Protobuf marshal/unmarshal.
- **`rpc.proto`** — Defines `RPC`, `RPCT`, `NodeInfo`, `Protocol` (Raft/Kademlia), `Command` (PING/STORE/GET/FIND_NODE/FIND_VALUE/ACK/NODES/VALUE).
- **`udp.go`** — Empty placeholder.

### `ledgers` — Consensus & Backup Interfaces (INTERFACES ONLY)
- **`net_store.go`** — `LogManager`, `MetadataStore`, `FileLedger` interfaces.
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
- **Serialization:** Protobuf for RPC messages, TOML for metadata persistence, gob for Block hashing.
- **Dependencies:** Minimal — `BurntSushi/toml`, `google.golang.org/protobuf`, and `github.com/danmuck/smplog` (structured logging via zerolog).
- **Logging:** Uses `github.com/danmuck/smplog` with shared config loaded via `cmd/internal/logcfg`. Config resolves `SMPLOG_CONFIG` env var, then `./smplog.config.toml`, then `./local/smplog.config.toml`.

## Architecture Patterns

### Interfaces to Implement
When adding new node types or storage backends, implement these interfaces:

- **`ServerNode`** — For Raft cluster participants: `ApplyCommand`, `CreateSnapshot`, `GetState`, `AddPeer`, `RemovePeer`.
- **`ClientNode`** — For DHT participants: `Send`, `Ping`, `Store`, `FindNode`, `FindValue`.
- **`RemoteHandler`** — For network chunk distribution: `StartReceiver`, `PassFileReference`, `Receive`.
- **`KademliaRouting`** — For DHT routing: `K() int`, `A() int`, `GetBucket(int) []*NodeInfo`, `ClosestK([]byte) []*NodeInfo`, `Size() int`.
- **`TransportHandler`** — For new transport protocols: implement alongside `TCPHandler`.

### Dual-Ledger Model
1. **Raft log** — Authoritative, replicated metadata store for the root cluster.
2. **Blockchain** — Periodic snapshots of Raft state sealed into tamper-evident blocks.

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
- Streaming file serving (TCP binary protocol + HTTP REST)
- Crash recovery via intent files (write-ahead before chunking)
- Deep integrity verification (`VerifyAll`, `VerifyFile`)
- TTL-based expiry and cleanup (`CleanupExpired`)
- Cache management with deduplication
- Concurrent access safety (RWMutex)
- Metadata persistence and loading (TOML)
- Structured logging via smplog (project-wide)
- AES-256-GCM encryption/decryption (`impl` package)
- Blockchain block creation and chain validation (hash covers all exported fields)
- TCP transport with Protobuf encoding and Send/Receive
- Basic node creation, start/shutdown lifecycle

### Future (Stubs)

> These are interface stubs or empty scaffolding. They will be reworked once key_store is fully complete. Do not build on these without redesign.

- Kademlia routing (interface defined, no XOR distance or bucket logic)
- UDP transport (empty file)
- RemoteHandler (placeholder, not wired to network)
- Raft consensus (interfaces defined, no implementation)
- Snapshot/backup scheduling (interfaces defined, no implementation)
- Blockchain Chain struct (Block works, but no Chain/persistence/Append/Validate)
- Log replication and leader election (not started)

### Remaining Known Issues
- `TCPHandler` shutdown uses `time.Sleep` instead of context cancellation
- No TLS on TCP connections
- Hardcoded addresses and node IDs in `cmd/` entry points
- 2-byte message length header limits messages to 65KB

For detailed per-module issue tracking, see `docs/progress/buildplan.md`.

## Common Tasks

### Add a New Node Type
1. Define a struct in `src/api/nodes/` that embeds `*DefaultNode`.
2. Implement `ServerNode` or `ClientNode` interface.
3. Add a constructor following `NewDefaultNode(id, addr, k, a)` pattern — returns `(*DefaultNode, error)`.
4. Add a `cmd/` entry point if needed.

### Add a New RPC Method
1. Add the command to the `Command` enum in `src/api/transport/rpc.proto`.
2. Run `make build-protobuf` to regenerate `rpc.pb.go`.
3. Add handler logic in the node's RPC processing loop.

### Generate Test Files
```sh
go run cmd/gen_file/main.go 256MB local/upload/test_256mb.dat
# Or via Makefile:
make gen-file SIZE=256MB FILE=local/upload/test_256mb.dat
```
Files are reused if they already exist with the matching size.

### Run the File Storage Test
```sh
make storage
# Chunks appear in local/storage/data/, metadata in local/storage/metadata/
```

### Add a New Transport Protocol
1. Create a new file in `src/api/transport/` (e.g., `udp.go`).
2. Implement the `TransportHandler` interface.
3. Follow `TCPHandler` patterns: channel-based inbound queue, length-prefixed messages.

## Testing

```sh
make test            # Run all tests (includes 256MB large file test)
go test -short ./... # Skip large file test
make test-coverage   # Run with coverage report
```

Test files follow `*_test.go` convention in their respective packages (62 tests across 5 files):
- `src/key_store/store_test.go` — 26 tests: chunking (1KB-256MB), empty file, single chunk, exact block size, persistence, corruption detection, cleanup, key consistency, streaming, TTL, deletion, cache dedup, utility functions, and more
- `src/key_store/hardening_test.go` — 28 tests: concurrent stores/reads/deletes, crash recovery intents, integrity verification, error injection cleanup, stale cache pruning, non-destructive startup, reupload after restart
- `src/key_store/config_test.go` — 2 tests: KeyStoreConfig defaults, configurable TTL
- `src/api/nodes/routing_test.go` — 4 tests: node creation, bad ID rejection, start/shutdown lifecycle, router type verification
- `src/api/transport/tcp_handler_test.go` — 2 tests: listener init + connect, full send/receive round-trip

Test data goes in `./local/upload/` (created by tests, reused across runs). The `local/storage/` directory is used at runtime and is gitignored.
