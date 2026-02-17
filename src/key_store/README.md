# KDHT File Chunking Test Program

A test utility for the KDHT (Kademlia DHT) file chunking and reassembly system. This program tests the ability to break files into chunks, store them, and reassemble them correctly.

## Setup

1. Create required directories:
```bash
mkdir -p local/upload local/storage/data local/storage/.cache local/storage/metadata
```

2. Add test files to `local/upload/` directory
   - Files are auto-indexed from that folder at runtime

## Usage

1. Navigate to the test directory:
```bash
cd cmd/_test
```

2. Run the test program:
```bash
go run main.go run
# Optional end-to-end reassembly/copy validation:
go run main.go run --reassemble
# Optional short-lived TTL (seconds) for expiry testing:
go run main.go run --ttl-seconds 15
```

## Configuration Options

`cmd/key_store/main.go` uses a top-level `defaultRuntimeConfig` struct as the runtime default.
Edit that struct to set baseline behavior (upload path, run mode, cleanup toggles, and default KeyStore config including TTL).

CLI flags:
- `--reassemble` enables output file reassembly/validation (overrides config default for that run).
- `--ttl-seconds N` overrides the configured default metadata TTL for newly stored files.

## Program Flow

1. Creates a storage directory for chunks
2. For each test file:
   - Reads original file
   - Calculates original hash
   - Breaks file into chunks
   - Stores chunks with verification
   - Writes/updates a cache metadata entry in `local/storage/.cache/`
   - Reassembles file
   - Verifies reassembled file matches original

## Output Locations

- Original files: `./local/upload/`
- Chunked storage: `./local/storage/data/`
- Reassembled files: `./local/upload/copy.<filename>` (only when `--reassemble` is set)

## Example Run

```bash
# Add a test file
cp myimage.jpg local/upload/image.jpg

# Run the test
go run main.go run

# Check results
ls local/upload/copy.image.jpg
```

## Expected Output

The program will show:
- Original file details
- Chunking progress
- Verification steps
- Reassembly progress
- Final hash verification

Example output:
```
Original file size: 1048576 bytes
Original file hash: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

File details:
Total size: 1048576 bytes
Chunk size: 65536 bytes
Total chunks: 16
...
```

## Error Handling

- If files don't exist in local/upload/, the program will error
- Verification failures will stop the process
- If a file hash already exists in `local/storage/.cache/`, store is skipped to avoid duplicate cache/store entries
- Use the prompt entry `clean` to remove all `.kdht` files from local/storage/data/

## Notes

- The program requires write permissions in both `local/upload/` and `local/storage/` directories
- Large files will be chunked into approximately 1000 pieces
- Each chunk is individually verified during storage and reassembly
