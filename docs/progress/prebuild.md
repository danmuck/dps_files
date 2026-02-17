# KeyStore Storage Engine — Prebuild Task List

> Goal: Bulletproof local storage engine that stands alone, connects over direct TCP, and later plugs into DHT with backups/replication.

## Phase 1: Streaming & Lookup (Current)

- [x] **1.1 StreamFile** — `StreamFile(key, io.Writer) error` streams chunks directly to any writer (HTTP response, TCP conn) without buffering the full file. Replaces need for ReassembleFileToBytes in serving paths.
- [x] **1.2 Filename index** — `filesByName map[string][HashSize]byte` populated on init/store, enables `GetFileByName(name) (*File, error)` lookup. File server can't work with hash-only access.
- [x] **1.3 StreamChunkRange** — `StreamChunkRange(key, start, end, io.Writer) error` serves a range of chunks. Enables HTTP Range requests and resumable transfers.

## Phase 2: Efficiency & Robustness

- [x] **2.1 Eliminate dual-map redundancy** — `references map[[KeySize]byte]FileReference` was replaced with `chunkIndex map[[KeySize]byte]chunkLoc`, and chunk lookups now resolve through `files[fileHash].References[chunkIndex]`.
- [x] **2.2 Runtime config** — Added `KeyStoreConfig` + `InitKeyStoreWithConfig`; write verification and verbose output are now runtime-configurable per KeyStore instance.
- [x] **2.3 Small file passthrough (closed as won't-do)** — Kept canonical `computeChunkKey(fileHash, chunkIndex)` for all chunks to preserve consistent DHT addressing and avoid a special-case path.
- [x] **2.4 TTL enforcement** — Implemented lazy TTL checks on access (`fileFromMemory`) and batch expiration cleanup (`CleanupExpired`), plus explicit file deletion (`DeleteFile`).

## Phase 3: Serving Layer

- [x] **3.1 TCP file server** — Minimal TCP server in `cmd/fileserver/` that uses KeyStore directly. Upload files, download by name or hash, list files. 4-byte length-prefixed binary protocol.
- [x] **3.2 HTTP file server** — HTTP wrapper in `cmd/httpserver/` for browser/curl access. PUT/GET by name, GET by hash, Range header support via StreamChunkRange, JSON file listing, DELETE by hash.
- [x] **3.3 Upload streaming** — `StoreFromReader(name string, r io.Reader, size uint64) (*File, error)` — accepts data from a connection via temp-file spill, delegates to LoadAndStoreFileLocal.

## Phase 4: Hardening

- [ ] **4.1 Concurrent access testing** — Stress test parallel reads/writes to same KeyStore. Verify lock correctness under contention.
- [ ] **4.2 Crash recovery** — Partial writes leave orphaned chunks. Add write-ahead intent file so recovery can complete or rollback interrupted stores.
- [ ] **4.3 Integrity scan** — `VerifyAll() []error` reads and re-hashes every chunk on disk. Detects bit rot, returns list of corrupted chunks.

## Phase 5: Network Integration (Future)

- [ ] **5.1 RemoteHandler real impl** — Wire RemoteHandler to TCP transport. Stream chunks to remote KeyStore instances.
- [ ] **5.2 Replication** — Store chunks on N peers via DHT STORE. Read from closest peer with fallback.
- [ ] **5.3 Raft metadata** — Root cluster maintains authoritative file metadata. KeyStore becomes the local storage backend.
- [ ] **5.4 Blockchain snapshots** — Periodic Raft state sealed into append-only chain.

## Issues to Address Along the Way

- `fmt.Printf` everywhere — swap to `log.Logger` or structured logger when adding TCP server
- `fileFromMemory` prints on every call — noisy for a server, gate behind verbosity config
- `LoadFileReferenceData` hashes on every read — consider optional skip for trusted-local reads
- TOML hex encoding doubles hash storage — acceptable for now, revisit if metadata size matters
