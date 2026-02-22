# key_store

Local file chunking, storage, and reassembly pipeline for dps_files.

## What it does

Files are split into fixed-size chunks (64KB–4MB, targeting ~1000 chunks per file). Each chunk gets a deterministic 20-byte SHA-1 DHT routing key via `computeChunkKey(file_hash, chunk_index)` and is stored as a `.kdht` file. File metadata is persisted as TOML. All operations are safe for concurrent access.

## Usage

Run the interactive TUI client (`cmd/client`) which wraps this package via the `FileLedger` interface over gRPC:

```sh
make client ARGS="--mode local --storage local/storage"
```

Or start a standalone server and connect remotely:

```sh
make server ARGS="--addr :9000 --storage local/storage"
make client ARGS="--mode remote --remotes localhost:9000"
```

## Key files

| File | Purpose |
|------|---------|
| `key_store.go` | `KeyStore` struct: init, memory/disk persistence, cleanup, TTL expiry, streaming, verification |
| `files.go` | `StoreFileLocal`, `LoadAndStoreFileLocal`, `LoadAndStoreFileRemote`, `StoreFromReader`, reassembly, `computeChunkKey` |
| `file_reference.go` | `FileReference` struct: per-chunk metadata, `StoreFileReference`, `LoadFileReferenceData`, `DeleteFileReference` |
| `metadata.go` | `MetaData` struct, `PrepareMetaData`, TOML serialization |
| `directory.go` | `StoreDirectory`, `ListDirectory`, `ReassembleDirectory` — recursive directory ingest/browse/download |
| `config.go` | `KeyStoreConfig`, `DefaultConfig`, `CalculateBlockSize`, `HashFile`, `CopyFile`, `ValidateSHA256`, constants |
| `file_ledger.go` | `KeyStoreLedger` adapter: wraps `*KeyStore` to implement `ledgers.FileLedger` |
| `verify.go` | `VerifyAll`, `VerifyFile`: deep integrity scanning |
| `intent.go` | Crash recovery via intent files (write-ahead before chunking) |
| `string.go` | String/formatting helpers |

## Storage layout

```
local/storage/
  data/           *.kdht chunk files (keyed by 20-byte SHA-1 routing key)
  metadata/       *.toml file metadata (keyed by 32-byte SHA-256 file hash)
  .cache/         deduplicated metadata cache entries
  .intents/       crash-recovery intent files (cleared on successful store)
```

## Tests

```sh
make test            # includes 256MB large file test
go test -short ./... # skip large file test
```

Test files: `store_test.go`, `hardening_test.go`, `config_test.go`, `file_ledger_test.go`.
