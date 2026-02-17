# AGENTS.md — dps_files Project Instructions

This header is repository-specific and sits above the generic AGENTS footer content.

## Instruction Precedence

- `CLAUDE.md` is the primary source for project-specific implementation guidance.
- Use this file as the execution policy and documentation governance layer.
- If this header and the generic footer conflict, follow this header for `dps_files` behavior and keep generic governance rules unless explicitly overridden.

## Project Overview

`dps_files` is a decentralized file storage system in Go combining:

- **Kademlia DHT** for peer discovery and chunk routing (XOR distance / k-buckets).
- **Raft consensus** for authoritative replicated metadata in a root cluster.
- **Blockchain backup ledger** for tamper-evident snapshots of Raft state.

Files are split into chunks, each chunk gets a 20-byte SHA-1 DHT key, chunk payloads are stored as `.kdht`, and metadata is persisted as TOML.

## Current Architecture Surfaces

- `cmd/server/main.go`: server node entry point (TCP listener demo)
- `cmd/client/main.go`: client node entry point (Protobuf RPC demo)
- `cmd/chain/main.go`: blockchain demo (AES-GCM)
- `cmd/key_store/main.go`: file chunking integration flow
- `src/api/nodes/`: node interfaces and routing scaffolding
- `src/api/transport/`: transport interfaces, TCP handler, protobuf encoding, `rpc.proto`
- `src/api/ledgers/`: ledger/snapshot interfaces
- `src/impl/`: block and crypto primitives
- `src/key_store/`: local chunking/storage/reassembly pipeline
- `local/upload/`: operator upload/test input files
- `local/storage/`: runtime layout (`data/` chunks, `.cache/`, `metadata/`)

## Build And Run

- `make test` / `make test-coverage`
- `make build`
- `make server`, `make client`, `make chain`
- `make tidy`
- `make build-protobuf`
- `go run cmd/key_store/main.go`

## Contracts And Interfaces

Prioritize interfaces/contracts already defined in code and `CLAUDE.md`:

- `ServerNode`, `ClientNode`, `MasterNode`
- `TransportHandler`
- `RemoteHandler`
- `KademliaRouting`
- `LogManager`, `MetadataStore`, `FileLedger`, `SnapshotManager`, `BackupLedger`

When changing behavior, keep lifecycle and RPC semantics explicit: start/stop/status/health/version/config, identity fields (`node_id`, `service_id`, `instance_id`), and observability fields (`component`, `message`, `peer`, `request_id`, `trace_id`).

## Delivery Expectations For This Repo

- Prefer C-style clarity and explicit error propagation.
- Make retries, timeouts, idempotency, and partial-failure behavior explicit.
- Keep docs, diagrams, and code aligned for topology and message flow changes.
- Treat scaffolding areas as incomplete unless verified in code/tests.

## Known Implementation Gaps (from `CLAUDE.md`)

- Kademlia routing logic is incomplete.
- UDP transport is a placeholder.
- Raft and snapshot/backup systems are interface-only.
- Some transport and hashing paths contain correctness risks noted in `CLAUDE.md`.

## Required Reference

Read `CLAUDE.md` before implementing architecture, transport, routing, or storage behavior.

# AGENTS.md

These instructions are project-agnostic and are intended to bootstrap documentation governance and buildlog discipline in any repository.

## Canonical Docs Rules

- `docs/` is the source of truth for architecture, contracts, and behavior.
- Canonical contract docs live in `docs/architecture/definitions/*.toml`.
- Canonical diagrams live in `docs/architecture/models/*.mmd`.
- `docs/index.md` defines documentation navigation and precedence.
- If implementation and canonical docs diverge, treat docs as authoritative unless explicit approval is given to update canonical docs.
- Changes to any file under `docs/architecture/` require explicit user approval in the active thread.
- Keep canonical docs up to date on every pass when behavior, topology, interfaces, invariants, or lifecycle semantics change.

## Buildplan Rules (`docs/progress`)

- `docs/progress/` is the active buildplan and execution tracker.
- After any code or documentation update, review relevant `docs/progress/*` files before ending the pass.
- In the same pass, update the affected buildplan/checklist entries to reflect current status (do not defer progress tracking).
- Maintain phase/task files with ordered grouping names:
- `mvp_p1`, `mvp_p2`, `mvp_p3`, ...
- or `<subprojectacronym>_phase1`, `<subprojectacronym>_phase2`, ...
- Keep a single plan index file in `docs/progress/`:
- `<subprojectacronym>_buildplan.md` (preferred)
- or `mvp_buildplan.md`

## Buildlog Rules (`local/buildlogs`)

- Build logs are required for every user prompt.
- Create exactly one new TOML log file per prompt.
- Do not append new prompts to prior log files.
- Naming format is strict: `YYYY-MM-DD_HH:MM.toml` (EST/New York).
- Every log must include:
- initial prompt
- files changed
- justification for each change
- completed progress checklist tasks
- validation commands/notes
- follow-up items

## Required Directories (Create If Missing)

- `docs/`
- `docs/index.md`
- `docs/architecture/`
- `docs/architecture/definitions/`
- `docs/architecture/models/`
- `docs/progress/`
- `docs/progress/buildlog/`
- `local/buildlogs/`

## Bootstrap Rule for New Projects

- If `docs/` or `docs/architecture/` is missing:
- scan the codebase first
- infer current boundaries/interfaces/workflows from code
- create first-pass canonical TOML contracts in `docs/architecture/definitions/`
- create first-pass Mermaid diagrams in `docs/architecture/models/`
- create first-pass buildplan files in `docs/progress/`
- mark first-pass docs as provisional and refine in subsequent passes

## Diagram Requirements

- Use Mermaid (`.mmd`) for text-native, version-controllable diagrams.
- Keep at least:
- one system boundary diagram
- one core workflow/message-flow diagram
- Update diagrams whenever behavior or topology changes.

## TOML Structure Templates

Use these as baseline skeletons; keep keys explicit and stable.

### File: `docs/architecture/definitions/_template.toml`

[meta]
name = "<contract_name>"
version = "v1"
status = "draft" # draft | active | deprecated
owner = "<owner_or_team>"

[scope]
purpose = "<what this contract governs>"
in_scope = "<what is included>"
out_of_scope = "<what is excluded>"

[entities]
primary = "<primary entity>"
related = ["<related_entity_1>", "<related_entity_2>"]

[lifecycle]
create = "<create/start semantics>"
update = "<update/reconcile semantics>"
delete = "<delete/stop semantics>"
status = "<status semantics>"
health = "<health semantics>"
config = "<config semantics>"

[contracts]
inputs = ["<input_contract_ref>"]
outputs = ["<output_contract_ref>"]
errors = ["<error_contract_ref>"]

[reliability]
timeouts = "<timeout policy reference>"
retries = "<retry policy reference>"
idempotency = "<idempotency policy reference>"

[observability]
required_fields = [
"component",
"message",
"peer",
"request_id",
"trace_id",
]

[[interfaces]]
id = "<interface_id>"
request = "<request_shape_or_ref>"
response = "<response_shape_or_ref>"
notes = "<behavioral notes>"

### File: `docs/progress/buildlog/template.toml`

[meta]
log_id = "YYYY-MM-DD_HH:MM"
created_at_est = "YYYY-MM-DDTHH:MM-05:00"
updated_at_est = "YYYY-MM-DDTHH:MM-05:00"
agent = "codex"
status = "in_progress" # in_progress | completed | blocked

[scope]
kind = "new_scope" # initial | follow_up | new_scope
workstream = "<short_workstream_label>"
scope_reason = "<why_this_log_exists>"

[prompt]
initial = """
<original_user_prompt>
"""

[[prompt_followups]]
at_est = "YYYY-MM-DDTHH:MM-05:00"
text = """
<follow_up_prompt>
"""

[summary]
goal = "<goal_for_this_pass>"
result = "<outcome_summary>"

[[changes]]
path = "<file_path>"
change_type = "modify" # add | modify | delete
summary = "<what_changed>"
justification = "<why_change_was_needed>"

[[progress_tasks_completed]]
checklist_file = "docs/progress/<checklist_file>.md"
task = "<exact_task_text>"
status = "completed"

[validation]
commands = [

# "tool or test command"

]
notes = "<validation_notes>"

[follow_up]
open_items = [

# "<next_item>"

]

### File: `local/buildlogs/YYYY-MM-DD_HH:MM.toml` (instance shape)

[meta]
log_id = "YYYY-MM-DD_HH:MM"
created_at_est = "YYYY-MM-DDTHH:MM-05:00"
updated_at_est = "YYYY-MM-DDTHH:MM-05:00"
agent = "codex"
status = "completed"

[scope]
kind = "new_scope"
workstream = "<workstream>"
scope_reason = "<reason>"

[prompt]
initial = """
<prompt_text>
"""

[summary]
goal = "<goal>"
result = "<result>"

[[changes]]
path = "<path>"
change_type = "modify"
summary = "<summary>"
justification = "<justification>"

[validation]
commands = []
notes = "<notes>"

[follow_up]
open_items = []
