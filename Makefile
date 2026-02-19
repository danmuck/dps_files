# Variables
MODULE_NAME := github.com/danmuck/dps_files

# Default target
all: test

# Run all tests: build executables, run tests, remove .build/ on success
test:
	clear; $(MAKE) build && go test -v ./... && rm -rf .build/

# Run tests with coverage: build executables, run tests, remove .build/ on success
test-coverage:
	clear; $(MAKE) build && go test -v ./... -cover && rm -rf .build/

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

storage:
	clear; go run ./cmd/storage $(ARGS)

server:
	go run cmd/server/main.go

client:
	go run cmd/client/main.go

chain:
	go run cmd/chain/main.go

# Generate an upload file: make gen-file SIZE=256MB FILE=local/upload/test.dat
gen-file:
	go run cmd/gen_file/main.go $(SIZE) $(FILE)

# Tidy up dependencies
tidy:
	go mod tidy

fileserver:
	go run ./cmd/fileserver $(ARGS)

httpserver:
	go run ./cmd/httpserver $(ARGS)

build-protobuf:
	protoc --go_out=. --go_opt=paths=source_relative src/api/transport/rpc.proto
