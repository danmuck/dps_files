# dps_files Build Plan

> Organized by module stage. Each stage can be worked independently once its dependencies are met.
> Complete Stage 1 first — it is the most functional module and sets patterns for the rest.

---

## Stage 1: KeyStore — Verification, Hardening & Performance

**Current state:** The most complete module. File chunking (local), metadata persistence (TOML), reassembly, hash verification, streaming, TTL expiry, crash recovery, deep integrity verification, cache management with deduplication, and concurrent access safety all work. Tests across 4 files cover 1KB–256MB files plus edge cases (empty, single chunk, corruption, concurrent access, crash recovery). `StoreFileLocal` and `LoadAndStoreFileLocal` produce identical DHT keys via `computeChunkKey`. Empty files are handled correctly (0 blocks). Logging uses `github.com/danmuck/smplog` via shared config loader (`cmd/internal/logcfg`).

**Key files:**
- `src/key_store/key_store.go` — KeyStore struct, init, memory/disk persistence, cleanup, verification
- `src/key_store/files.go` — File struct, StoreFileLocal, LoadAndStoreFileLocal, LoadAndStoreFileRemote, reassembly, `computeChunkKey`
- `src/key_store/file_reference.go` — FileReference struct, StoreFileReference, LoadFileReferenceData, DeleteFileReference
- `src/key_store/metadata.go` — MetaData struct, PrepareMetaData, TOML serialization
- `src/key_store/config.go` — KeyStoreConfig, DefaultConfig(), CalculateBlockSize() with promotion logic, HashFile(), CopyFile(), ValidateSHA256(), constants
- `src/key_store/verify.go` — VerifyAll(), VerifyFile(): deep integrity scanning
- `src/key_store/intent.go` — Crash recovery via intent files (write-ahead before chunking)
- `src/key_store/string.go` — String formatting helpers
- `src/key_store/store_test.go` — chunking, persistence, corruption, cleanup, key consistency, streaming, TTL, deletion, cache dedup, utility functions
- `src/key_store/hardening_test.go` — concurrent stores/reads/deletes, crash recovery, integrity verification, error injection, stale cache pruning, non-destructive startup
- `src/key_store/config_test.go` — KeyStoreConfig defaults, configurable TTL

### Phase 1A: Bug Fixes & Correctness
- [x] Fix `StoreFileLocal` vs `LoadAndStoreFileLocal` DHT key calculation mismatch — unified via `computeChunkKey`
- [x] Remove `StoreFileRemote` — dead copy-paste removed
- [x] Fix `LoadAndStoreFileRemote` race condition — removed goroutine wrapper on `StartReceiver`, added ready signal
- [x] Fix `LoadAndStoreFileRemote` — `PassFileReference` no longer launched as goroutine
- [x] Fix `fileToMemory` variable shadowing — renamed to `metadataPath`
- [x] Fix `CalculateBlockSize` / `PrepareMetaData` divide-by-zero on empty files
- [x] Migrate runtime/test filesystem paths to local/storage/data (runtime chunk store) and local/upload (input files)
- [x] Add configurable default TTL wired through `KeyStoreConfig`
- [x] Add internal block-size promotion utility with `LargeFileMx` guard in `config.go`
- [x] Fix `existingFileByHash` — re-upload of an expired file refreshes TTL; all four upload paths covered
- [x] Fix `Cleanup` — scans `chunkDataDir` on disk and removes every `.kdht` file found
- [x] Normalize `smplog` config discovery across all `cmd/*` entrypoints with shared loader (`cmd/internal/logcfg`)
- [x] Make startup non-destructive: `InitKeyStoreWithConfig` no longer moves/prunes metadata/cache on boot

### Phase 1B: Testing
- [x] Fix `TestLargeFileChunking` — generates 256MB test file and reuses if present
- [x] Add test: empty file (0 bytes) — `TestEmptyFile`
- [x] Add test: file smaller than `MinBlockSize` — `TestSingleChunkFile`
- [x] Add test: file exactly equal to one block size — `TestExactBlockSizeFile`
- [x] Add test: store → cleanup → verify all files removed — `TestCleanupRemovesAllFiles`
- [x] Add test: store → reload KeyStore from disk — `TestKeyStorePersistence`
- [x] Add test: key consistency between `StoreFileLocal` and `LoadAndStoreFileLocal`
- [x] Add test: chunk corruption detected — `TestChunkCorruptionDetected`
- [x] Add tests: error-injection cleanup paths for failed metadata persistence
- [x] Add test: stale local cache metadata is pruned and does not block upload
- [x] Add tests: startup is non-destructive and missing-data reupload works after restart
- [x] Add focused unit tests for block-size promotion utility and `LargeFileMx` threshold behavior
- [x] Add tests: streaming to writer — `TestStreamFile`, `TestStreamFileByName`, `TestStreamChunkRange`, `TestStreamFileDetectsCorruption`
- [x] Add tests: filename index lookup and persistence
- [x] Add tests: KeyStoreConfig defaults and configurable TTL
- [x] Add tests: TTL expiry, CleanupExpired, DeleteFile
- [x] Add tests: StoreFromReader success and size-mismatch error
- [x] Add tests: concurrent stores, reads, store-and-read, deletes under contention
- [x] Add tests: crash-recovery intent file creation and committed-file safety
- [x] Add tests: VerifyAll detects corruption and passes on clean store
- [x] Add tests: CleanupKDHTAndMetaData, MoveToCache deduplicates by hash
- [x] Add tests: hash-cache skip decisions for StoreFileLocal and LoadAndStoreFileLocal

### Phase 1C: Cleanup & Performance
- [x] Ensure all key_store library code uses `smplog` consistently
- [x] Make `VERIFY` a runtime field on `KeyStore` instead of a compile-time const
- [x] Extract shared chunking logic from `StoreFileLocal` and `LoadAndStoreFileLocal`
- [x] Add directory semantics: `StoreDirectory`, `ListDirectory`, `ReassembleDirectory` on KeyStore and FileLedger
- [x] Add TUI directory upload action (`upload-dir`) and `[DIR]` display in view
- [ ] Add `context.Context` parameter to `StoreFileLocal` and `LoadAndStoreFileLocal` for cancellation support
- [x] Deduplicate: `existingFileByHash` called at the top of all store entry points

---

## Stage 2: Transport & RPC — gRPC Migration (COMPLETE)

> **STATUS: COMPLETE** — The hand-rolled TCP transport package has been replaced by gRPC. All file operations (Upload, Download, List, Delete, UploadDir, ListDir) are served via a generated `DPSFiles` gRPC service. An optional gRPC-Gateway proxy exposes the same service as HTTP/JSON on a second port.

**Current state:** `src/api/transport/` package deleted entirely. `NodeInfo` moved to `src/api/nodes/nodes.go`. `src/api/pb/` contains generated code from `dps.proto`. `src/api/grpc/server.go` implements `pb.DPSFilesServer` backed by `ledgers.FileLedger`. `DefaultServerNode` holds `*grpc.Server` + `net.Listener`. `DefaultClientNode` holds one `*grpc.ClientConn`. Upload/Download use `io.Pipe` for streaming — no large in-memory buffers.

**Key files:**
- `src/api/pb/dps.proto` — `DPSFiles` service definition with HTTP annotations
- `src/api/pb/dps.pb.go` — Generated message types
- `src/api/pb/dps_grpc.pb.go` — Generated `DPSFilesClient`, `DPSFilesServer`, `RegisterDPSFilesServer`
- `src/api/pb/dps.pb.gw.go` — Generated `RegisterDPSFilesHandlerFromEndpoint` (grpc-gateway)
- `src/api/grpc/server.go` — `grpcserver.Server`: Upload, Download, Delete, List, UploadDir, ListDir
- `src/api/grpc/server_test.go` — bufconn tests: upload+list, download by hash, delete
- `src/api/nodes/server_node.go` — `DefaultServerNode` with `*grpc.Server`, `Addr()`, `WithHTTP`
- `src/api/nodes/client_node.go` — `DefaultClientNode` with `activeConn`, `Stub()`, `LocalServer()`
- `src/api/nodes/gateway.go` — `serveGateway`: HTTP/JSON reverse proxy via grpc-gateway

### Phase 2A: gRPC Migration
- [x] Install proto plugins and add Go module dependencies (grpc, protobuf, grpc-gateway)
- [x] Update `make build-protobuf` to invoke protoc with go, go-grpc, and grpc-gateway plugins
- [x] Write `dps.proto` service definition with HTTP annotations for all six operations
- [x] Generate `src/api/pb/` code (dps.pb.go, dps_grpc.pb.go, dps.pb.gw.go)
- [x] Implement `grpcserver.Server` in `src/api/grpc/server.go` backed by `ledgers.FileLedger`
- [x] Rewrite `DefaultServerNode` to hold `*grpc.Server` + `net.Listener`
- [x] Simplify `DefaultNode` to identity only (address, pubKey, Router)
- [x] Rewrite `DefaultClientNode` to hold `*grpc.ClientConn`; local mode connects to embedded server over gRPC
- [x] Delete `src/api/transport/` package entirely; move `NodeInfo` to `src/api/nodes/nodes.go`

### Phase 2B: Testing
- [x] Add grpc server tests via bufconn — `TestUploadAndList`, `TestDownloadByHash`, `TestDeleteFile`
- [x] Update server_node_test.go — gRPC upload + list via real TCP listener
- [x] Update client_node_test.go — local mode gRPC, remote mode, LocalServer() access, no-server error

---

## Stage 3: v1 Production Hardening & Benchmarking  [CURRENT]

> **STATUS: CURRENT** — Establish performance baselines, add context/cancellation support, and harden the v1 fileserver for real-world use before beginning distributed work.

### Phase 3A: Benchmarks
- [ ] Add Go benchmark functions for chunking throughput (`BenchmarkStoreFileLocal`, `BenchmarkStoreFromReader`)
- [ ] Add benchmarks for reassembly (`BenchmarkReassembleFileToBytes`, `BenchmarkReassembleFileToPath`)
- [ ] Add benchmarks for gRPC Upload and Download round-trips (via bufconn)
- [ ] Capture and commit baseline benchmark results (`go test -bench=. -benchmem`)

### Phase 3B: v1 Stability
- [ ] Add `context.Context` to `StoreFileLocal` and `LoadAndStoreFileLocal` for cancellation support
- [ ] Add TLS support to gRPC connections (required before Raft log replication carries real data)
- [ ] Add graceful shutdown with context-based cancellation propagation through ServerNode → KeyStore

### Phase 3C: Observability
- [ ] Add structured metrics: upload/download request counts, transfer throughput, error rates via smplog
- [ ] Write package-level `doc.go` files for each package (key_store, grpcserver, nodes, ledgers, impl)

### Phase 3D: Load Testing
- [ ] Add multi-client concurrent upload test (N goroutines, large files, verify integrity)
- [ ] Add concurrent mixed upload+download stress test
- [ ] Profile hot paths with pprof; optimize based on benchmark results

### Phase 3E: Security
- [ ] Security audit: validate all external input at gRPC boundaries, enforce max message sizes
- [ ] Extract all magic numbers into config.go constants or TOML config file

---

## Stage 4: Kademlia DHT — Routing & Distributed Storage

> **STATUS: FUTURE** — Interface stubs removed. Will be designed from scratch when distribution work begins.

**Current state:** `KademliaRouting` interface removed (was stubs only). `DefaultRouter` is a simple map-based router that works for current needs. `RoutingTable` interface defines `InsertNode`, `RemoveNode`, `Lookup`. `DefaultNode` is identity-only. `NodeInfo` is defined in `src/api/nodes/nodes.go`.

**Key files:**
- `src/api/nodes/routing.go` — `RoutingTable` interface, `DefaultRouter` (map-based, functional)
- `src/api/nodes/nodes.go` — `Node`, `ClientNode`, `ServerNode` interfaces; `NodeInfo` struct
- `src/api/nodes/default.go` — `DefaultNode` struct (identity only: address, pubKey, Router)
- `src/api/nodes/routing_test.go` — 4 tests: creation, bad ID, start/shutdown, router type

**Depends on:** Stage 2 (transport must work for RPCs)

### Phase 4A: Core Algorithms
- [ ] Implement `XORDistance(a, b []byte) []byte` — bitwise XOR of two 20-byte node IDs
- [ ] Implement `PrefixLength(distance []byte) int` — count leading zero bits (determines bucket index)
- [ ] Implement k-bucket struct: ordered list of up to `k` contacts, LRU eviction policy
- [ ] Re-introduce `KademliaRouting` interface with real XOR-distance semantics
- [ ] Implement `InsertNode`: calculate XOR distance → determine bucket → insert or update position
- [ ] Implement `RemoveNode`: find and remove from correct bucket
- [ ] Implement `ClosestK(key)`: collect `k` closest nodes across buckets by XOR distance
- [ ] Implement `Lookup(id)`: return single closest node or exact match

### Phase 4B: Kademlia RPCs
- [ ] Implement `PING`, `STORE`, `FIND_NODE`, `FIND_VALUE` handlers
- [ ] Wire to transport layer

### Phase 4C: Iterative Lookups
- [ ] Implement iterative `NodeLookup` and `ValueLookup`
- [ ] Implement node join from bootstrap address

### Phase 4D: Maintenance & Cleanup
- [ ] Add periodic bucket refresh
- [ ] Add key republishing

### Phase 4E: Testing
- [x] Add test: node creation with valid/invalid IDs — `TestNewDefaultNode`, `TestNewDefaultNodeBadID`
- [x] Add test: start/shutdown lifecycle — `TestDefaultNodeStartShutdown`
- [x] Add test: router type verification — `TestKademliaRouterCreation`
- [ ] Add test: XOR distance correctness (known vectors)
- [ ] Add test: k-bucket insert, eviction at capacity, LRU ordering
- [ ] Add test: `ClosestK` returns correct nodes sorted by distance
- [ ] Add test: 3-node network — node A stores value, node C retrieves it via node B
- [ ] Add test: node join populates routing table from bootstrap peer

---

## Stage 5: Raft Consensus — Leader Election & Log Replication

> **STATUS: FUTURE** — Interfaces removed. Will be designed from scratch when consensus work begins.

**Current state:** `LogManager`, `MetadataStore` interfaces removed from `net_store.go`. `SnapshotManager`, `BackupLedger` interfaces removed (snapshots.go deleted). Only `NodeState` enum (Follower/Candidate/Leader) and `ServerNode` interface stubs remain. There is zero Raft code.

**Key files:**
- `src/api/nodes/nodes.go` — `ServerNode` interface, `NodeState` enum

**Depends on:** Stage 2 (reliable gRPC transport for log replication)

### Phase 5A: Persistent State & Log
- [ ] Implement `RaftState` struct: `currentTerm`, `votedFor`, `log []LogEntry`, persisted to disk
- [ ] Re-introduce `LogManager` interface with actual implementation
- [ ] Add write-ahead log persistence (append-only file for crash recovery)

### Phase 5B: Leader Election
- [ ] Implement election timer: randomized timeout (150-300ms), reset on heartbeat
- [ ] Implement `RequestVote` RPC
- [ ] Implement state transitions: Follower → Candidate → Leader

### Phase 5C: Log Replication
- [ ] Implement `AppendEntries` with log entries
- [ ] Implement commit advancement: leader commits once majority has replicated
- [ ] Implement state machine apply: committed entries applied in order

### Phase 5D: Snapshots & Membership
- [ ] Re-introduce `SnapshotManager` interface with actual implementation
- [ ] Implement `CreateSnapshot` and `InstallSnapshot` RPC
- [ ] Add single-server membership changes

### Phase 5E: Testing
- [ ] Add test: 3-node cluster elects a leader within timeout
- [ ] Add test: leader failure triggers re-election
- [ ] Add test: log replication — client sends command to leader, all followers receive it
- [ ] Add test: network partition — verify no split-brain commits
- [ ] Add test: snapshot creation and install on a new follower

---

## Stage 6: Chain & Ledgers — Blockchain Backup System

> **STATUS: FUTURE** — Interfaces removed. Will be designed from scratch when blockchain work begins.

**Current state:** `Block` struct works with all fields exported. `CalculateHash` and `ValidateHash` handle both `*Block` and `Block` value types. AES-GCM encryption/decryption is functional. `cmd/chain/main.go` demo works. `BackupLedger` and `SnapshotManager` interfaces removed (snapshots.go deleted). No chain struct or persistence.

**Key files:**
- `src/impl/block.go` — `Block` struct (exported fields), `NewBlock`, `NewBlockEncrypt`, hash methods
- `src/impl/block_data.go` — `BlockData` struct (Data, Hash, IV fields)
- `src/impl/utils.go` — `CalculateHash`, `ValidateHash`, `EncryptData`, `DecryptData`
- `cmd/chain/main.go` — Interactive blockchain demo

### Phase 6A: Chain Structure
- [x] Fix `CalculateHash` / `gob` issue — Block fields are now exported
- [x] Fix `CalculateHash` / `ValidateHash` — handles both `*Block` and `Block` type assertions
- [ ] Create a `Chain` struct: holds `[]*Block`, genesis block, chain height, persistence path
- [ ] Implement `Append`: validate previous hash linkage, add block
- [ ] Implement `Validate`: walk the full chain verifying each block's hash and prev-hash linkage

### Phase 6B: Persistence
- [ ] Implement `Write` and `Load` (gob or binary format)
- [ ] Implement `Find`: lookup by 20-byte key or 32-byte hash
- [ ] Re-introduce `BackupLedger` interface with actual implementation

### Phase 6C: Raft Integration
- [ ] Implement `BackupScheduler`: periodic timer triggers `CreateSnapshot` on Raft leader
- [ ] Implement `PersistSnapshot`: serialize Raft snapshot into a new `Block` and append to chain

### Phase 6D: Testing
- [ ] Add test: create genesis → append 10 blocks → validate chain passes
- [ ] Add test: tamper with a block's data → validate chain fails
- [ ] Add test: chain persistence — write to disk, load from disk, validate matches

---

## Stage 7: Integration & End-to-End Pipeline

**Depends on:** Stages 1-6

- [ ] End-to-end: store a file → chunk locally → distribute chunks via DHT `STORE` → verify all chunks retrievable via `FIND_VALUE`
- [ ] End-to-end: Raft cluster of 3 nodes reaches consensus on a file metadata update
- [ ] End-to-end: Raft leader creates a blockchain backup block, followers validate the chain
- [ ] End-to-end: retrieve a file by hash → resolve chunks via DHT → reassemble → verify integrity
- [ ] Add CLI or config-driven node startup (replace hardcoded addresses and node IDs in `cmd/`)
- [ ] Connect distributed chunk distribution to transport layer so `LoadAndStoreFileRemote` actually distributes chunks over the network

---

## Stage 8: Production Readiness & CI

**Depends on:** Stages 1-7

- [ ] Add CI pipeline (GitHub Actions): `make test`, `make build`, lint
- [x] Add architecture diagrams (Mermaid) showing data flow
- [ ] Performance baselines documented and regression-guarded (moved to Stage 3 as first priority)
