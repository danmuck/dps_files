# dps_files Build Plan

> Organized by module stage. Each stage can be worked independently once its dependencies are met.
> Complete Stage 1 first — it is the most functional module and sets patterns for the rest.

---

## Stage 1: KeyStore — Verification, Hardening & Performance

**Current state:** The most complete module. File chunking (local), metadata persistence (TOML), reassembly, hash verification, and cleanup all work. Tests cover 1KB–256MB files plus edge cases (empty, single chunk, corruption). `StoreFileLocal` and `LoadAndStoreFileLocal` now produce identical DHT keys via `computeChunkKey`. `DefaultRemoteHandler` is a placeholder that prints to stdout but has correct synchronization. Empty files are handled correctly (0 blocks).

**Key files:**
- `src/key_store/key_store.go` — KeyStore struct, init, memory/disk persistence, cleanup, verification
- `src/key_store/files.go` — File struct, StoreFileLocal, LoadAndStoreFileLocal, LoadAndStoreFileRemote, reassembly, `computeChunkKey`
- `src/key_store/file_reference.go` — FileReference struct, StoreFileReference, LoadFileReferenceData, DeleteFileReference
- `src/key_store/metadata.go` — MetaData struct, PrepareMetaData, TOML serialization
- `src/key_store/config.go` — Constants, RemoteHandler interface, DefaultRemoteHandler, utility functions
- `src/key_store/string.go` — String formatting helpers
- `src/key_store/store_test.go` — 10 tests: large file, small parametric, empty, single chunk, exact block, persistence, corruption, cleanup, key consistency

### Phase 1A: Bug Fixes & Correctness
- [x] Fix `StoreFileLocal` vs `LoadAndStoreFileLocal` DHT key calculation mismatch — unified via `computeChunkKey`
- [x] Remove `StoreFileRemote` — dead copy-paste removed
- [x] Fix `LoadAndStoreFileRemote` race condition — removed goroutine wrapper on `StartReceiver`, added ready signal
- [x] Fix `LoadAndStoreFileRemote` — `PassFileReference` no longer launched as goroutine
- [x] Fix `DefaultRemoteHandler.StartReceiver` off-by-one — index now increments on `[]byte` (data) case
- [x] Fix `verifyFileReferences` — now only moves metadata for files with missing chunks, not all files
- [x] Fix `fileToMemory` variable shadowing — renamed to `metadataPath`
- [x] Fix `LoadLocalFileToMemory` variable shadowing and ineffective `ref = nil` — uses index assignment
- [x] Fix `CalculateBlockSize` / `PrepareMetaData` divide-by-zero on empty files
- [x] Migrate runtime/test filesystem paths to local/storage/data (runtime chunk store) and local/upload (input files)
- [x] Fix `cmd/key_store` CLI mode/output — `make storage` now runs `go run ./cmd/key_store`, defaults to `run`, reports automated indexing from `local/upload`, prompts for file index (`all`/`clean` supported), persists `.kdht` chunks in `local/storage/data` by default, and gates reassembly behind `--reassemble`
- [x] Add configurable default TTL for `cmd/key_store` via `--ttl-seconds`, wired through `RuntimeConfig.TTLSeconds` into `KeyStoreConfig` and applied to local/remote store metadata (runtime default set explicitly, independent from library default)
- [x] Refactor `cmd/key_store` runtime defaults to a top-level config struct (`RuntimeConfig`); CLI flags now act as run-time overrides on that default config
- [x] Refactor `cmd/key_store` into smaller files and add explicit menu actions for `upload` (from `local/upload`, with indexed file list shown before selection), `store` (direct filepath), `clean` (`.kdht` only), `deep-clean` (`.kdht` + metadata + cache), and `view` (inspect metadata + optional reassembly to `local/storage/copy.*`); `InitKeyStore` now runs at process startup
- [x] Improve `view` action metadata entry formatting in `cmd/key_store/view.go` (readable multi-line layout, human-readable size/TTL, last-chunk size, truncated hash)
- [x] Prevent `.cache` metadata duplication, upsert cache metadata on successful store, and skip storing files whose hash is already present in cache (CLI now reports skip instead of fatal exit)
- [x] Validate cache entries before hash-cache skip decisions: local (`file` protocol / `local/storage/*`) references are checked for chunk existence, stale cache entries are pruned on startup and hash-check paths, and uploads now proceed when cache metadata points to missing local data
- [x] Make startup non-destructive: `InitKeyStoreWithConfig` no longer moves/prunes metadata/cache on boot; stale local references are validated at upload-time and missing-data hashes are evicted from in-memory indexes so upload reprocesses chunks
- [x] Add internal block-size promotion utility with `LargeFileMx` guard in `config.go` and wire it into `CalculateBlockSize`
- [ ] Fix `Cleanup` — only removes chunks tracked in memory; if the process crashed mid-store, orphaned `.kdht` files on disk are never cleaned up

### Phase 1B: Testing
- [x] Fix `TestLargeFileChunking` — now generates a 256MB test file and reuses it if present
- [x] Add test: empty file (0 bytes) — `TestEmptyFile`
- [x] Add test: file smaller than `MinBlockSize` — `TestSingleChunkFile`
- [x] Add test: file exactly equal to one block size — `TestExactBlockSizeFile`
- [x] Add test: store → cleanup → verify all files removed — `TestCleanupRemovesAllFiles`
- [x] Add test: store → reload KeyStore from disk — `TestKeyStorePersistence`
- [x] Add test: key consistency between `StoreFileLocal` and `LoadAndStoreFileLocal` — `TestStoreFileLocalAndLoadAndStoreFileLocalProduceSameKeys`
- [x] Add test: chunk corruption detected — `TestChunkCorruptionDetected`
- [x] Add tests: error-injection cleanup paths for failed metadata persistence — `TestStoreFileLocalErrorInjectionCleansChunks`, `TestLoadAndStoreFileLocalErrorInjectionCleansChunks`
- [x] Add test: stale local cache metadata is pruned and does not block upload — `TestLoadAndStoreFileLocalPrunesDeadLocalCacheEntry`
- [x] Add tests: startup is non-destructive and missing-data reupload works after restart — `TestInitKeyStoreDoesNotPruneStorageOnStartup`, `TestLoadAndStoreFileLocalReuploadsMissingDataAfterRestart`
- [x] Add focused unit tests for block-size promotion utility and `LargeFileMx` threshold behavior — `TestPromoteCandidateBlockSize`
- [x] Add `CalculateBlockSize` integration tests for promotion + large-file guard behavior — `TestCalculateBlockSizePromotionIntegration`

### Phase 1C: Cleanup & Performance
- [ ] Replace all `fmt.Printf` in key_store with `log/slog` structured logging (file hash, chunk index, operation as context fields)
- [x] Make `VERIFY` a runtime field on `KeyStore` instead of a compile-time const (implemented via `KeyStoreConfig.VerifyOnWrite`)
- [ ] Make `PRINT_BLOCKS` a runtime field or remove progress printing from library code (move to cmd/)
- [ ] Extract shared chunking logic from `StoreFileLocal` and `LoadAndStoreFileLocal` into a private helper to eliminate duplication
- [ ] Add `context.Context` parameter to `StoreFileLocal` and `LoadAndStoreFileLocal` for cancellation support
- [ ] Deduplicate: if a file with the same SHA-256 hash is already stored, return the existing `File` instead of re-chunking

---

## Stage 2: Transport & RPC — Wire Protocol Completion

**Current state:** `TCPHandler` can accept TCP connections, encode/send RPCs via Protobuf, and push decoded RPCs into a channel. `DefaultCoder` handles Protobuf encode/decode with a 2-byte (uint16) length header, limiting messages to 65KB. `Send()` now correctly encodes via `Coder.Encode()`. `TransportHandler` interface signatures are consistent (`Send(*RPC)`, `Close() error`). Tests verify listener, connect, and full send/receive round-trip. No RPC dispatch, no UDP, no TLS.

**Key files:**
- `src/api/transport/transport.go` — `TransportHandler` interface (corrected signatures)
- `src/api/transport/tcp.go` — `TCPHandler`: accept loop, connection handler, `Send()` uses encoder
- `src/api/transport/encoding.go` — `Coder` interface, `DefaultCoder` (Protobuf + 2-byte header)
- `src/api/transport/udp.go` — Empty placeholder
- `src/api/transport/rpc.proto` — Protobuf definitions (RPC, RPCT, NodeInfo, Protocol, Command)
- `src/api/transport/rpc.pb.go` — Generated Protobuf code
- `src/api/transport/tcp_handler_test.go` — 2 tests: listener + connect, full send/receive round-trip

### Phase 2A: Fix Existing TCP
- [x] Fix `TCPHandler.Send()` — now encodes via `Coder.Encode()` and writes the result
- [x] Fix `TransportHandler` interface signatures — `Send(*RPC)`, `Close() error`
- [ ] Upgrade length header from `uint16` (65KB max) to `uint32` (4GB max) to support chunk-sized messages
- [ ] Add `TCPHandler.Dial(addr)` method to initiate outbound connections (currently only accepts inbound)
- [ ] Add connection pooling or reuse — currently each `handleConnection` runs independently with no way to send responses back
- [ ] Replace `fmt.Printf` / `fmt.Fprintf(os.Stderr, ...)` with `log/slog`

### Phase 2B: RPC Dispatch
- [ ] Implement an RPC handler registry: map `Command` enum → handler function
- [ ] Implement request-response correlation: add a nonce/request-ID field to `rpc.proto`, match responses to pending requests
- [ ] Implement `PING` / `ACK` handler as the first working RPC round-trip
- [ ] Add RPC timeout: if no response within N seconds, return an error to the caller

### Phase 2C: UDP Transport
- [ ] Implement `UDPHandler` in `udp.go` — same `TransportHandler` interface as TCP
- [ ] UDP is preferred for Kademlia RPCs (small messages, connectionless); TCP for Raft (reliable, ordered)
- [ ] Add message size validation: reject messages larger than UDP-safe threshold (~1400 bytes)

### Phase 2D: Security
- [ ] Add TLS support to `TCPHandler` (required before Raft log replication carries real data)
- [ ] Validate all inbound message sizes before allocating buffers (prevent memory exhaustion)
- [ ] Add rate limiting on inbound connections per remote address

### Phase 2E: Testing
- [x] Add test: listener init and client connect — `TestTCPHandlerListenAndAccept`
- [x] Add test: full encode/send/receive round-trip — `TestTCPHandlerSendReceive`
- [ ] Add test: encode → decode round-trip for every `Command` type
- [ ] Add test: oversized message is rejected cleanly
- [ ] Add test: concurrent connections (10+ clients sending simultaneously)
- [ ] Add test: clean shutdown — close exit channel, verify all goroutines exit and listener is released

---

## Stage 3: Kademlia DHT — Routing & Distributed Storage

**Current state:** `KademliaRouter` struct exists with a `buckets` field (`[][]*NodeInfo`) but every method is a stub returning `nil` or `-1`. Interfaces are now consistent — `RoutingTable.Lookup` returns `(*transport.NodeInfo, error)`, `KademliaRouting` methods return typed values. `DefaultRouter` is a simple map-based router that works. `DefaultNode` returns `*DefaultNode` from constructor (no more panic). `DefaultNode.Send` signature matches `ClientNode.Send`. Empty method bodies for Kademlia RPCs.

**Key files:**
- `src/api/nodes/routing.go` — `RoutingTable`, `KademliaRouting` interfaces (corrected return types), `DefaultRouter`, `KademliaRouter` (stubs)
- `src/api/nodes/nodes.go` — `Node`, `ClientNode`, `ServerNode`, `MasterNode` interfaces
- `src/api/nodes/default.go` — `DefaultNode` struct (returns `*DefaultNode`, no panic), `Start()`, `Shutdown()`, `ID()`
- `src/api/nodes/routing_test.go` — 4 tests: creation, bad ID, start/shutdown, router type

**Depends on:** Stage 2 (transport must work for RPCs)

### Phase 3A: Core Algorithms
- [ ] Implement `XORDistance(a, b []byte) []byte` — bitwise XOR of two 20-byte node IDs
- [ ] Implement `PrefixLength(distance []byte) int` — count leading zero bits (determines bucket index)
- [ ] Implement k-bucket struct: ordered list of up to `k` contacts, LRU eviction policy (ping least-recently-seen before evicting)
- [ ] Initialize `KademliaRouter.buckets` as 160 k-buckets (one per possible prefix length)
- [ ] Implement `InsertNode`: calculate XOR distance → determine bucket → insert or update position
- [ ] Implement `RemoveNode`: find and remove from correct bucket
- [ ] Implement `ClosestK(key)`: collect `k` closest nodes across buckets by XOR distance
- [ ] Implement `Lookup(id)`: return single closest node or exact match

### Phase 3B: Kademlia RPCs
- [ ] Implement `PING` handler: respond with `ACK` to confirm liveness, update routing table
- [ ] Implement `STORE` handler: accept a key-value pair and persist it locally (ties into KeyStore)
- [ ] Implement `FIND_NODE` handler: return `k` closest nodes to the requested ID
- [ ] Implement `FIND_VALUE` handler: return value if held locally, otherwise return `k` closest nodes
- [ ] Wire `DefaultNode.Send/Ping/Store/FindNode/FindValue` to transport layer

### Phase 3C: Iterative Lookups
- [ ] Implement iterative `NodeLookup`: alpha-concurrent queries, converging on target, short-list management
- [ ] Implement iterative `ValueLookup`: like NodeLookup but returns immediately when value is found
- [ ] Implement node join: given a bootstrap address, perform `FindNode(self.ID)` to populate routing table

### Phase 3D: Maintenance & Cleanup
- [ ] Add periodic bucket refresh: for each bucket not accessed in 1 hour, perform lookup on a random ID in that bucket's range
- [ ] Add key republishing: periodically re-store keys to ensure they survive node churn

### Phase 3E: Testing
- [x] Add test: node creation with valid/invalid IDs — `TestNewDefaultNode`, `TestNewDefaultNodeBadID`
- [x] Add test: start/shutdown lifecycle — `TestDefaultNodeStartShutdown`
- [x] Add test: router type verification — `TestKademliaRouterCreation`
- [ ] Add test: XOR distance correctness (known vectors)
- [ ] Add test: k-bucket insert, eviction at capacity, LRU ordering
- [ ] Add test: `ClosestK` returns correct nodes sorted by distance
- [ ] Add test: 3-node network — node A stores value, node C retrieves it via node B
- [ ] Add test: node join populates routing table from bootstrap peer

---

## Stage 4: Raft Consensus — Leader Election & Log Replication

**Current state:** Only interfaces exist. `ServerNode` defines `ApplyCommand`, `CreateSnapshot`, `GetState`, `AddPeer`, `RemovePeer` but nothing implements them. `NodeState` enum (Follower/Candidate/Leader) is defined. `LogManager` interface defines `Append`, `GetEntry`, `LastLogIndex`, `Commit` with no implementation. `LogEntry` struct has `Index`, `Term`, `Command`. `SnapshotManager` interface defines `CreateSnapshot`, `PersistSnapshot`, `LoadSnapshot`, `VerifySnapshot` with no implementation. There is zero Raft code.

**Key files:**
- `src/api/nodes/nodes.go` — `ServerNode` interface, `NodeState` enum
- `src/api/ledgers/net_store.go` — `LogManager`, `MetadataStore`, `FileLedger` interfaces, `LogEntry` struct
- `src/api/ledgers/snapshots.go` — `SnapshotManager`, `BackupLedger` interfaces, `Snapshot` struct

**Depends on:** Stage 2 (reliable TCP transport for log replication)

### Phase 4A: Persistent State & Log
- [ ] Implement `RaftState` struct: `currentTerm`, `votedFor`, `log []LogEntry`, persisted to disk
- [ ] Implement `LogManager`: append, get by index, truncate, commit index tracking
- [ ] Add write-ahead log persistence (append-only file for crash recovery)

### Phase 4B: Leader Election
- [ ] Implement election timer: randomized timeout (150-300ms), reset on heartbeat
- [ ] Implement `RequestVote` RPC: candidate requests vote, follower grants if term is newer and log is up-to-date
- [ ] Implement state transitions: Follower → Candidate → Leader (or back to Follower on higher term)
- [ ] Implement `AppendEntries` as heartbeat: empty entries from leader to prevent elections

### Phase 4C: Log Replication
- [ ] Implement `AppendEntries` with log entries: leader sends uncommitted entries, followers append
- [ ] Implement next/matchIndex tracking per follower
- [ ] Implement commit advancement: leader commits once majority has replicated
- [ ] Implement state machine apply: committed entries are applied in order

### Phase 4D: Snapshots & Membership
- [ ] Implement `CreateSnapshot`: serialize committed state, compact log
- [ ] Implement `InstallSnapshot` RPC: leader sends snapshot to lagging followers
- [ ] Add single-server membership changes (add/remove one node at a time)

### Phase 4E: Testing
- [ ] Add test: 3-node cluster elects a leader within timeout
- [ ] Add test: leader failure triggers re-election, new leader emerges
- [ ] Add test: log replication — client sends command to leader, all followers receive it
- [ ] Add test: network partition — split cluster, verify no split-brain commits
- [ ] Add test: snapshot creation and install on a new follower

---

## Stage 5: Chain & Ledgers — Blockchain Backup System

**Current state:** `Block` struct works with all fields exported (Data, Time, Nonce) so gob encoding covers full content. `CalculateHash` and `ValidateHash` handle both `*Block` and `Block` value types. AES-GCM encryption/decryption is functional. `cmd/chain/main.go` demo works with correct hash size (32) and no nil-pointer crash on validation. `BackupLedger` interface is defined but not implemented. No chain struct or persistence.

**Key files:**
- `src/impl/block.go` — `Block` struct (exported fields), `NewBlock`, `NewBlockEncrypt`, hash and print methods
- `src/impl/block_data.go` — `BlockData` struct (Data, Hash, IV fields)
- `src/impl/utils.go` — `ComputeShaHash`, `CalculateHash`, `ValidateHash` (handles *Block and Block), `EncryptData`, `DecryptData`
- `src/api/ledgers/snapshots.go` — `BackupLedger`, `SnapshotManager` interfaces, `Snapshot` struct
- `cmd/chain/main.go` — Interactive blockchain demo (fixed: hash size, nil error handling)

### Phase 5A: Chain Structure
- [x] Fix `CalculateHash` / `gob` issue — Block fields are now exported, gob covers all content
- [x] Fix `CalculateHash` / `ValidateHash` — handles both `*Block` and `Block` type assertions
- [x] Fix `cmd/chain/main.go` — `ValidateHash` called with correct size (32), nil error handled
- [ ] Create a `Chain` struct: holds `[]*Block`, genesis block, chain height, persistence path
- [ ] Implement `Append`: validate previous hash linkage, add block
- [ ] Implement `Validate`: walk the full chain verifying each block's hash and prev-hash linkage

### Phase 5B: Persistence
- [ ] Implement `Write`: serialize chain to disk (gob, JSON, or custom binary format)
- [ ] Implement `Load`: deserialize chain from disk and validate on load
- [ ] Implement `Find`: lookup by 20-byte key (FileReference) or 32-byte key (Block hash)

### Phase 5C: Raft Integration
- [ ] Implement `BackupScheduler`: periodic timer on the Raft leader triggers `CreateSnapshot`
- [ ] Implement `PersistSnapshot`: serialize Raft snapshot into a new `Block` and append to chain
- [ ] Implement `Sync`: replicate chain state across the Raft cluster
- [ ] Implement `Rebuild`: reconstruct chain from a snapshot file

### Phase 5D: Testing
- [ ] Add test: create genesis → append 10 blocks → validate chain passes
- [ ] Add test: tamper with a block's data → validate chain fails
- [ ] Add test: encrypt/decrypt round-trip for block data
- [ ] Add test: chain persistence — write to disk, load from disk, validate matches

---

## Stage 6: Integration & End-to-End Pipeline

**Depends on:** Stages 1-5

- [ ] End-to-end: store a file → chunk locally → distribute chunks via DHT `STORE` → verify all chunks retrievable via `FIND_VALUE`
- [ ] End-to-end: Raft cluster of 3 nodes reaches consensus on a file metadata update
- [ ] End-to-end: Raft leader creates a blockchain backup block, followers validate the chain
- [ ] End-to-end: retrieve a file by hash → resolve chunks via DHT → reassemble → verify integrity matches original
- [ ] Add CLI or config-driven node startup (replace hardcoded addresses and node IDs in `cmd/`)
- [ ] Connect `RemoteHandler` to transport layer so `LoadAndStoreFileRemote` actually distributes chunks over the network
- [ ] Wire `FileLedger` interface to `KeyStore` (KeyStore already implements most of the behavior, just needs the interface)

---

## Stage 7: Hardening & Production Readiness

**Depends on:** Stages 1-6

- [ ] Extract all magic numbers into a TOML config file or `config.go` constants
- [ ] Security audit: validate all external input at API boundaries, enforce max message sizes
- [ ] Add graceful shutdown across all components (context-based cancellation)
- [ ] Write package-level `doc.go` files for each package
- [x] Add architecture diagrams (Mermaid) showing data flow: file → chunks → DHT → Raft → chain
- [ ] Benchmark critical paths: chunking throughput, DHT lookup latency, Raft commit latency
- [ ] Add CI pipeline (GitHub Actions): `make test`, `make build`, lint
