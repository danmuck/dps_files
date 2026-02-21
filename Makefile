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
		if [ "$$name" = "internal" ]; then continue; fi; \
		mkdir -p .build/$$name; \
		go build -o .build/$$name/$$name ./$$dir; \
	done

# Clean up build artifacts
clean:
	rm -rf .build/

server:
	go run ./cmd/server $(ARGS)

client:
	go run ./cmd/client $(ARGS)

chain:
	go run cmd/chain/main.go

# Generate an upload file: make gen-file SIZE=256MB FILE=local/upload/test.dat
gen-file:
	go run cmd/gen_file/main.go $(SIZE) $(FILE)

# Tidy up dependencies
tidy:
	go mod tidy

build-protobuf:
	protoc \
	  -I src/api/pb \
	  -I third_party/googleapis \
	  --go_out=src/api/pb --go_opt=paths=source_relative \
	  --go-grpc_out=src/api/pb --go-grpc_opt=paths=source_relative \
	  --grpc-gateway_out=src/api/pb --grpc-gateway_opt=paths=source_relative \
	  src/api/pb/dps.proto
