# Variables
MODULE_NAME := github.com/danmuck/dps_files

# Default target
all: test

# Run all tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v ./... -cover

# Build all main packages into .build/<name>/
build:
	@for dir in cmd/*/; do \
		name=$$(basename $$dir); \
		mkdir -p .build/$$name; \
		go build -o .build/$$name/$$name ./$$dir; \
	done

# Clean up build artifacts
clean:
	rm -rf .build/

server:
	go run cmd/server/main.go

client:
	go run cmd/client/main.go

chain:
	go run cmd/chain/main.go

# Generate a test file: make gen-file SIZE=256MB FILE=local/data/test.dat
gen-file:
	go run cmd/gen_file/main.go $(SIZE) $(FILE)

# Tidy up dependencies
tidy:
	go mod tidy

build-protobuf:
	protoc --go_out=. --go_opt=paths=source_relative src/api/transport/rpc.proto
