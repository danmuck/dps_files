# dps_files Documentation Draft (Future Root README)

This document is the high-level control surface for the project.

Once finalized, this file is intended to replace the repository root `README.md`.

## What This Project Is

`dps_files` is a storage platform-in-progress that combines:

- a chunked file data plane (`key_store`)
- a Kademlia-style discovery/routing plane
- a Raft-based metadata authority plane
- a blockchain-backed snapshot history plane

The immediate goal is practical local-network storage.
The long-term goal is staged expansion into distributed cloud storage.

## Documentation-First Operating Model

Before code refactors:

- contracts in `docs/architecture/definitions/*.toml` are the source of truth
- diagrams in `docs/architecture/models/*.mmd` must match contract behavior
- execution tracking lives in `docs/progress/buildplan.md`

If implementation and contracts diverge, update contracts first (or explicitly approve divergence).

## Current Build Strategy (Staged Expansion)

- Stage 0: `local_only` storage mode
- Stage 1: `cluster_only` storage mode on local network
- Stage 2: `hybrid` mode (local cache + distributed authority)
- Stage 3: cloud-distributed cluster with replicated metadata and backup history

Design principle:

- keep stable file/chunk identity (`file_id`, `chunk_id`) across every stage
- evolve placement, replication, and control plane without breaking local correctness

## Primary Contracts To Drive Next

- `docs/architecture/definitions/file_storage.toml`
- `docs/architecture/definitions/metadata_ledgers.toml`
- `docs/architecture/definitions/transport_rpc.toml`
- `docs/architecture/definitions/node_types.toml`

Supporting contracts:

- `docs/architecture/definitions/dht_routing.toml`
- `docs/architecture/definitions/raft_consensus.toml`
- `docs/architecture/definitions/blockchain_backup.toml`

## Model Views

- `docs/architecture/models/contract_dependency_map.mmd`
- `docs/architecture/models/keystore_local_flow.mmd`
- `docs/architecture/models/keystore_remote_flow.mmd`
- `docs/architecture/models/distributed_message_flow.mmd`

## Editing Rules For High-Level Direction

When steering project direction from docs:

- change this file first for scope/priority shifts
- then update impacted contract TOMLs
- then update impacted Mermaid models
- only then plan and execute code refactors

For behavior changes, include:

- lifecycle semantics (`start/stop/status/health/config`)
- identity fields (`node_id`, `service_id`, `instance_id`)
- observability fields (`component`, `message`, `peer`, `request_id`, `trace_id`)
- failure semantics (timeouts, retries, idempotency, partial-failure policy)

## Refactor Guardrail

`key_store` local behavior is the compatibility baseline.

No refactor is acceptable if it breaks:

- deterministic chunk key derivation
- local chunk integrity validation
- metadata persistence and reload
- successful file reassembly with final hash verification

## Working Agreement

Use this draft README as the top-level planning document while contracts are being hardened.
Once stable, promote it to replace the repository root `README.md`.
