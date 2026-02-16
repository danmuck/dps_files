# Refactor Guard Checklist (Contracts -> Code Touchpoints)

Use this checklist before and during refactors.  
Rule: no behavior/topology change is complete until contract, model, and implementation stay aligned.

## KeyStore Baseline Guardrails (Must Always Pass)

- [ ] Preserve deterministic chunk key derivation from `file_hash + chunk_index` in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/files.go`.
- [ ] Preserve local chunk hash verification on write/read paths in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/file_reference.go`.
- [ ] Preserve metadata persistence/reload compatibility in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/key_store.go` and `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/metadata.go`.
- [ ] Preserve successful file reassembly and final hash validation in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/files.go`.
- [ ] Keep existing key_store test suite green: `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/store_test.go`.

## Contract: file_storage.toml -> Code Touchpoints

- [ ] `storage_modes.local_only` semantics mapped to `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/StoreFileLocal` and `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/LoadAndStoreFileLocal`.
- [ ] `storage_modes.cluster_only` semantics mapped to `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/LoadAndStoreFileRemote`.
- [ ] `storage_modes.hybrid` semantics implemented with local-cache fallback policy in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/files.go`.
- [ ] `startup_reference_validation` rules reflected in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/key_store.go` (`verifyFileReferences` path).
- [ ] `RemoteHandler` ack/nack/completion/backpressure contract reflected in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/config.go`.
- [ ] Invariants enforced or test-covered:
- [ ] `TotalBlocks` math
- [ ] chunk index continuity
- [ ] chunk hash integrity
- [ ] file hash integrity
- [ ] deterministic chunk key

## Contract: metadata_ledgers.toml -> Code Touchpoints

- [ ] Introduce typed-ID adapter for current string-based metadata API in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/ledgers/net_store.go`.
- [ ] Evolve `MetadataStore` signatures to typed file/chunk IDs in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/ledgers/net_store.go`.
- [ ] Evolve `FileLedger` from void signatures to explicit C-style signatures in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/ledgers/net_store.go`.
- [ ] Ensure key_store methods satisfy `FileLedger` target behavior in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/key_store/*.go`.

## Contract: transport_rpc.toml -> Code Touchpoints

- [ ] Add correlation fields (`request_id`, `trace_id`) to RPC envelope in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/transport/rpc.proto` and regenerate `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/transport/rpc.pb.go`.
- [ ] Resolve frame-size mismatch (chunk payload > uint16 limit) in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/transport/encoding.go`.
- [ ] Keep transport interface compatibility while evolving framing in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/transport/transport.go`.
- [ ] Validate send/receive behavior and failure paths in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/transport/tcp.go` and `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/transport/tcp_handler_test.go`.

## Contract: node_types.toml -> Code Touchpoints

- [ ] Move `ClientNode` methods to explicit error-returning signatures in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/nodes/nodes.go`.
- [ ] Implement client operation behavior in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/nodes/default.go`.
- [ ] Replace sleep-based shutdown flow with bounded synchronization in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/nodes/default.go`.

## Contract: dht_routing.toml -> Code Touchpoints

- [ ] Implement XOR distance + bucket index logic in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/nodes/routing.go`.
- [ ] Implement `KademliaRouter` methods (`InsertNode`, `RemoveNode`, `Lookup`, `ClosestK`, `GetBucket`, `Size`) in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/nodes/routing.go`.
- [ ] Add routing invariant tests in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/nodes/routing_test.go`.

## Contract: raft_consensus.toml -> Code Touchpoints

- [ ] Add/extend consensus RPC contract surface in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/transport/rpc.proto`.
- [ ] Implement durable log manager backend aligned to `LogManager` in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/ledgers/`.
- [ ] Implement snapshot manager backend aligned to `SnapshotManager` in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/ledgers/`.
- [ ] Implement server-node consensus behavior in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/src/api/nodes/`.

## Contract-Model Sync Gate

- [ ] Update Mermaid models in `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/docs/architecture/models/` when contract behavior/topology changes.
- [ ] Confirm definitions and model names still match `/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files/docs/index.md`.

## Validation Commands Gate

- [ ] `GOCACHE=/tmp/go-build go test -v ./src/key_store`
- [ ] `GOCACHE=/tmp/go-build go test -short ./...`
- [ ] `GOCACHE=/tmp/go-build go test -v ./...`
