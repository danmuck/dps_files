# dps_files Documentation Index

This `docs/` tree is the canonical source of architecture contracts and models.

## Precedence

1. `docs/README.md` (high-level direction and scope)
2. `docs/architecture/definitions/*.toml` (contract truth)
3. `docs/architecture/models/*.mmd` (system and message-flow models)
4. `docs/progress/*.md` (execution plan and status)

If implementation diverges from contracts, treat contract docs as authoritative until explicitly revised.

## Contract Definitions

- `docs/architecture/definitions/file_storage.toml`
- `docs/architecture/definitions/file_storage.old.toml` (legacy snapshot)
- `docs/architecture/definitions/transport_rpc.toml`
- `docs/architecture/definitions/dht_routing.toml`
- `docs/architecture/definitions/node_types.toml`
- `docs/architecture/definitions/metadata_ledgers.toml`
- `docs/architecture/definitions/raft_consensus.toml`
- `docs/architecture/definitions/blockchain_backup.toml`

## Architecture Models

- `docs/architecture/models/contract_dependency_map.mmd`
- `docs/architecture/models/keystore_local_flow.mmd`
- `docs/architecture/models/keystore_remote_flow.mmd`
- `docs/architecture/models/distributed_message_flow.mmd`
- `docs/architecture/models/kademlia_iterative_lookup_flow.mmd`
- `docs/architecture/models/raft_election_replication_flow.mmd`

## Progress Tracking

- `docs/progress/buildplan.md`
- `docs/progress/refactor_guard_checklist.md`
