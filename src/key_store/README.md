# KDHT File Chunking Test Program

A test utility for the KDHT (Kademlia DHT) file chunking and reassembly system. This program tests the ability to break files into chunks, store them, and reassemble them correctly.

## Setup

1. Create required directories:
```bash
mkdir -p local/data
```

2. Add test files to `local/data/` directory
   - Files must match the TEST_FILES configuration in main.go
   - Current configuration:
```go
var TEST_FILES = map[int]string{
    0: "image.jpg",
    1: "142_MB.dmg",
    2: "ubuntu.iso",
}
```

## Usage

1. Navigate to the test directory:
```bash
cd cmd/_test
```

2. Run the test program:
```bash
go run main.go run
```

## Configuration Options

In `main.go`, you can modify these constants:

```go
const CLEAN = true       // Cleanup previous test files
const RUN_ALL = true     // Test all configured files
const FILE = TEST_FILES[0]  // Default file when RUN_ALL is false
```

## Program Flow

1. Creates a storage directory for chunks
2. For each test file:
   - Reads original file
   - Calculates original hash
   - Breaks file into chunks
   - Stores chunks with verification
   - Reassembles file
   - Verifies reassembled file matches original

## Output Locations

- Original files: `./local/data/`
- Chunked storage: `./local/storage/`
- Reassembled files: `./local/data/copy.<filename>`

## Example Run

```bash
# Add a test file
cp myimage.jpg local/data/image.jpg

# Run the test
go run main.go run

# Check results
ls local/data/copy.image.jpg
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

- If files don't exist in local/data/, the program will error
- Verification failures will stop the process
- Use CLEAN=true to remove previous test files

## Notes

- The program requires write permissions in both `local/data/` and `local/storage/` directories
- Large files will be chunked into approximately 1000 pieces
- Each chunk is individually verified during storage and reassembly
