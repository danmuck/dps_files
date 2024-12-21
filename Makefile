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

# Build the project
build:
	go build -o bin/project ./...

# Clean up build artifacts
clean:
	rm -rf bin/

server:
	go run cmd/server/main.go

client:
	go run cmd/client/main.go

chain:
	go run cmd/chain/main.go

# Tidy up dependencies
tidy:
	go mod tidy
