# Refactor Guard Checklist (Contracts -> Code Touchpoints)

Use this checklist before and during refactors.
Rule: no behavior/topology change is complete until contract, model, and implementation stay aligned.

## KeyStore Baseline Guardrails (Must Always Pass)

- [x] Normalize tracked documentation path references to repo-root form (`dps_files/...`) to avoid machine-specific absolute paths.
- [ ] Preserve deterministic chunk key derivation from `file_hash + chunk_index` in `dps_files/src/key_store/files.go`.
- [ ] Preserve local chunk hash verification on write/read paths in `dps_files/src/key_store/file_reference.go`.
- [ ] Preserve metadata persistence/reload compatibility in `dps_files/src/key_store/key_store.go` and `dps_files/src/key_store/metadata.go`.
- [ ] Preserve successful file reassembly and final hash validation in `dps_files/src/key_store/files.go`.
- [ ] Keep existing key_store test suite green: `dps_files/src/key_store/store_test.go`.

## Contract: file_storage.toml -> Code Touchpoints

- [ ] `storage_modes.local_only` semantics mapped to `dps_files/src/key_store/StoreFileLocal` and `dps_files/src/key_store/LoadAndStoreFileLocal`.
- [ ] `storage_modes.cluster_only` semantics mapped to `dps_files/src/key_store/LoadAndStoreFileRemote`.
- [ ] `storage_modes.hybrid` semantics implemented with local-cache fallback policy in `dps_files/src/key_store/files.go`.
- [ ] `startup_reference_validation` rules reflected in `dps_files/src/key_store/key_store.go` (`VerifyAll()` path).
- [ ] Invariants enforced or test-covered:
- [ ] `TotalBlocks` math
- [ ] chunk index continuity
- [ ] chunk hash integrity
- [ ] file hash integrity
- [ ] deterministic chunk key

## Contract: metadata_ledgers.toml -> Code Touchpoints

- [x] Typed-ID adapter for metadata API implemented via `ledgers.FileID`/`ledgers.ChunkID` in `dps_files/src/api/ledgers/net_store.go`.
- [ ] Evolve `FileLedger` from void signatures to explicit C-style signatures in `dps_files/src/api/ledgers/net_store.go`.
- [ ] Ensure key_store methods satisfy `FileLedger` target behavior in `dps_files/src/key_store/*.go`.

## Contract: transport_rpc.toml -> Code Touchpoints

- [x] gRPC service definition in `dps_files/src/api/pb/dps.proto` with HTTP gateway annotations.
- [x] Streaming upload/download via io.Pipe (no large in-memory buffers) in `dps_files/src/api/grpc/server.go`.
- [ ] Add correlation fields (`request_id`, `trace_id`) to RPC messages in `dps_files/src/api/pb/dps.proto`.
- [ ] Enforce max message sizes on inbound gRPC connections.
- [ ] Add TLS to gRPC connections before Raft log replication carries real data.

## Contract: node_types.toml -> Code Touchpoints

- [ ] Move `ClientNode` methods to explicit error-returning signatures in `dps_files/src/api/nodes/nodes.go`.
- [ ] Replace sleep-based shutdown flow with bounded synchronization in `dps_files/src/api/nodes/default.go`.

## Contract: dht_routing.toml -> Code Touchpoints

- [ ] Implement XOR distance + bucket index logic in `dps_files/src/api/nodes/routing.go`.
- [ ] Implement `KademliaRouting` interface with methods (`InsertNode`, `RemoveNode`, `ClosestK`, `GetBucket`, `Size`) in `dps_files/src/api/nodes/routing.go`.
- [ ] Add routing invariant tests in `dps_files/src/api/nodes/routing_test.go`.

## Contract: raft_consensus.toml -> Code Touchpoints

- [ ] Add consensus RPC surface in `dps_files/src/api/pb/dps.proto` (or a dedicated raft.proto).
- [ ] Implement durable log manager backend (LogManager interface will be re-introduced with Raft implementation).
- [ ] Implement snapshot manager backend (SnapshotManager interface will be re-introduced with Raft implementation).
- [ ] Implement server-node consensus behavior in `dps_files/src/api/nodes/`.

## Contract-Model Sync Gate

- [ ] Update Mermaid models in `dps_files/docs/architecture/models/` when contract behavior/topology changes.
- [ ] Confirm definitions and model names still match `dps_files/docs/index.md`.

## Validation Commands Gate

- [ ] `go test -short ./...`
- [ ] `go test -v ./src/key_store/...`
- [ ] `go test -v ./...`
- [ ] `make build`
