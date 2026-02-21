# gRPC Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the custom TCPHandler + HTTP REST stack with a single gRPC transport, adding gRPC-Gateway for HTTP/JSON access from browsers and REST clients.

**Architecture:** Define one proto service (`DPSFiles`) in a new `src/api/pb/` package. `DefaultServerNode` runs a single gRPC listener (+ optional gRPC-Gateway HTTP listener on a second port). `DefaultClientNode` holds a `grpc.ClientConn` for remote calls; local mode calls the server's methods directly. The `src/api/transport/` package (TCPHandler, encoding, udp) and `http_handlers.go` are deleted entirely.

**Tech Stack:** `google.golang.org/grpc`, `github.com/grpc-ecosystem/grpc-gateway/v2`, `google.golang.org/protobuf` (already present). Proto plugins: `protoc-gen-go`, `protoc-gen-go-grpc`, `protoc-gen-grpc-gateway`.

---

## What Gets Deleted

| File | Reason |
|---|---|
| `src/api/transport/tcp.go` | Replaced by gRPC |
| `src/api/transport/encoding.go` | Replaced by gRPC |
| `src/api/transport/transport.go` | Replaced by gRPC |
| `src/api/transport/udp.go` | Empty stub, gone |
| `src/api/transport/rpc.proto` | Replaced by `src/api/pb/dps.proto` |
| `src/api/transport/rpc.pb.go` | Replaced by generated files in `src/api/pb/` |
| `src/api/nodes/http_handlers.go` | Replaced by gRPC-Gateway |
| `src/api/nodes/http_handlers_test.go` | Replaced by gRPC server tests |
| `src/api/transport/tcp_handler_test.go` | TCPHandler is gone |

## What Gets Simplified

| File | Change |
|---|---|
| `src/api/nodes/default.go` | Remove `TCPHandler`, `exit` channel; keep ID/address/router |
| `src/api/nodes/server_node.go` | Replace TCPHandler dispatch + http.Server with grpc.Server |
| `src/api/nodes/client_node.go` | Replace TCPHandler with `grpc.ClientConn` |
| `cmd/client/remote.go` | Replace `FileServerClient` binary protocol with gRPC stubs |
| `cmd/server/main.go` | Start gRPC server + optional gateway |
| `Makefile` | Update `build-protobuf` target |

---

## Task 1: Install Proto Plugins & Add Go Dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Install protoc plugins (one-time, dev machine setup)**

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
```

Verify all three are on PATH:
```bash
which protoc-gen-go && which protoc-gen-go-grpc && which protoc-gen-grpc-gateway
```
Expected: three paths printed.

**Step 2: Add Go module dependencies**

```bash
go get google.golang.org/grpc@latest
go get github.com/grpc-ecosystem/grpc-gateway/v2@latest
go mod tidy
```

Verify `go.mod` now contains:
```
require (
    google.golang.org/grpc vX.Y.Z
    github.com/grpc-ecosystem/grpc-gateway/v2 vX.Y.Z
    ...
)
```

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add grpc and grpc-gateway dependencies"
```

---

## Task 2: Update Makefile for gRPC protoc

**Files:**
- Modify: `Makefile`

**Step 1: Download googleapis proto files (needed for HTTP annotations)**

```bash
mkdir -p third_party/googleapis
# Download the two files grpc-gateway needs:
curl -o third_party/googleapis/annotations.proto \
  https://raw.githubusercontent.com/googleapis/googleapis/master/google/api/annotations.proto
curl -o third_party/googleapis/http.proto \
  https://raw.githubusercontent.com/googleapis/googleapis/master/google/api/http.proto
```

Place them at `third_party/googleapis/google/api/annotations.proto` and `.../http.proto`:
```bash
mkdir -p third_party/googleapis/google/api
mv third_party/googleapis/annotations.proto third_party/googleapis/google/api/
mv third_party/googleapis/http.proto third_party/googleapis/google/api/
```

**Step 2: Update `build-protobuf` in Makefile**

Old:
```makefile
build-protobuf:
	protoc --go_out=. --go_opt=paths=source_relative src/api/transport/rpc.proto
```

New:
```makefile
build-protobuf:
	protoc \
	  -I src/api/pb \
	  -I third_party/googleapis \
	  --go_out=src/api/pb --go_opt=paths=source_relative \
	  --go-grpc_out=src/api/pb --go-grpc_opt=paths=source_relative \
	  --grpc-gateway_out=src/api/pb --grpc-gateway_opt=paths=source_relative \
	  src/api/pb/dps.proto
```

**Step 3: Commit**

```bash
git add Makefile third_party/
git commit -m "chore: update build-protobuf for grpc + grpc-gateway generation"
```

---

## Task 3: Write the Proto Service Definition

**Files:**
- Create: `src/api/pb/dps.proto`

**Step 1: Create the proto file**

```protobuf
syntax = "proto3";

package dps;
option go_package = "github.com/danmuck/dps_files/src/api/pb";

import "google/api/annotations.proto";

// ─── Messages ────────────────────────────────────────────────────────────────

// UploadChunk is sent in a client-streaming Upload RPC.
// The first message sets name and size; subsequent messages carry data only.
message UploadChunk {
  string name = 1;
  uint64 size = 2;
  bytes  data = 3;
}

message UploadResponse {
  bytes  hash = 1;   // 32-byte SHA-256
  string name = 2;
  uint64 size = 3;
}

// DownloadRequest identifies a file by hash (preferred) or by name.
message DownloadRequest {
  bytes  hash = 1;   // 32-byte SHA-256; if set, name is ignored
  string name = 2;
}

// DataChunk is one piece of a server-streamed Download.
message DataChunk {
  bytes data = 1;
}

message DeleteRequest {
  bytes hash = 1;   // 32-byte SHA-256
}

message DeleteResponse {}

message ListRequest {}

message FileEntry {
  string name       = 1;
  bytes  hash       = 2;   // 32-byte SHA-256
  uint64 size       = 3;
  string entry_type = 4;   // "file" or "directory"
}

message ListResponse {
  repeated FileEntry files = 1;
}

// UploadDirRequest stores a directory tree that exists on the server's
// local filesystem (used in local mode and server-side imports).
message UploadDirRequest {
  string root_path = 1;
}

message UploadDirResponse {
  bytes hash = 1;   // root directory hash (32-byte SHA-256)
}

message ListDirRequest {
  bytes hash = 1;   // directory hash (32-byte SHA-256)
}

message DirEntry {
  string name = 1;
  string path = 2;
  bytes  hash = 3;
  string type = 4;
  uint64 size = 5;
}

message ListDirResponse {
  repeated DirEntry entries = 1;
}

// ─── Service ─────────────────────────────────────────────────────────────────

service DPSFiles {

  // Upload streams file data from client to server.
  // HTTP: POST /v1/files — body is the raw file bytes (gateway buffers for REST).
  rpc Upload(stream UploadChunk) returns (UploadResponse) {
    option (google.api.http) = {
      post: "/v1/files"
      body: "*"
    };
  }

  // Download streams file data from server to client.
  // HTTP: GET /v1/files/{hash} or GET /v1/files/name/{name}
  rpc Download(DownloadRequest) returns (stream DataChunk) {
    option (google.api.http) = {
      get: "/v1/files/{hash}"
      additional_bindings { get: "/v1/files/name/{name}" }
    };
  }

  // Delete removes a file by its SHA-256 hash.
  // HTTP: DELETE /v1/files/{hash}
  rpc Delete(DeleteRequest) returns (DeleteResponse) {
    option (google.api.http) = {
      delete: "/v1/files/{hash}"
    };
  }

  // List returns metadata for all stored files and directories.
  // HTTP: GET /v1/files
  rpc List(ListRequest) returns (ListResponse) {
    option (google.api.http) = {
      get: "/v1/files"
      additional_bindings { get: "/v1/files/" }
    };
  }

  // UploadDir stores a server-local directory tree and returns its root hash.
  // HTTP: POST /v1/dirs
  rpc UploadDir(UploadDirRequest) returns (UploadDirResponse) {
    option (google.api.http) = {
      post: "/v1/dirs"
      body: "*"
    };
  }

  // ListDir returns the immediate children of a directory by its hash.
  // HTTP: GET /v1/dirs/{hash}
  rpc ListDir(ListDirRequest) returns (ListDirResponse) {
    option (google.api.http) = {
      get: "/v1/dirs/{hash}"
    };
  }
}
```

**Step 2: Generate code**

```bash
make build-protobuf
```

Expected: three files created in `src/api/pb/`:
- `dps.pb.go` — message types
- `dps_grpc.pb.go` — server/client interfaces
- `dps.pb.gw.go` — HTTP gateway handler

**Step 3: Verify it compiles**

```bash
go build ./src/api/pb/...
```

Expected: no errors.

**Step 4: Commit**

```bash
git add src/api/pb/ third_party/
git commit -m "feat: add gRPC + grpc-gateway proto definition for DPSFiles service"
```

---

## Task 4: Implement the gRPC Server

**Files:**
- Create: `src/api/grpc/server.go`

This is the heart of the migration. It implements the generated `pb.DPSFilesServer` interface backed by `ledgers.FileLedger`. It replaces `DefaultServerNode.HandleRPC()` and all of `http_handlers.go`.

**Step 1: Write the failing test**

Create `src/api/grpc/server_test.go`:

```go
package grpcserver_test

import (
    "bytes"
    "context"
    "io"
    "net"
    "testing"

    grpcserver "github.com/danmuck/dps_files/src/api/grpc"
    "github.com/danmuck/dps_files/src/api/pb"
    "github.com/danmuck/dps_files/src/key_store"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    "google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

func newTestServer(t *testing.T) pb.DPSFilesClient {
    t.Helper()
    dir := t.TempDir()
    ks, err := key_store.InitKeyStore(dir)
    if err != nil {
        t.Fatalf("init keystore: %v", err)
    }
    ledger := key_store.NewFileLedger(ks)

    lis := bufconn.Listen(bufSize)
    srv := grpc.NewServer()
    pb.RegisterDPSFilesServer(srv, grpcserver.New(ledger))
    go srv.Serve(lis)
    t.Cleanup(func() { srv.GracefulStop() })

    conn, err := grpc.NewClient("passthrough://bufnet",
        grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
            return lis.DialContext(ctx)
        }),
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        t.Fatalf("dial bufconn: %v", err)
    }
    t.Cleanup(func() { conn.Close() })
    return pb.NewDPSFilesClient(conn)
}

func TestUploadAndList(t *testing.T) {
    client := newTestServer(t)

    // Upload a small file via streaming
    stream, err := client.Upload(context.Background())
    if err != nil {
        t.Fatalf("open upload stream: %v", err)
    }
    content := []byte("hello grpc world")
    if err := stream.Send(&pb.UploadChunk{Name: "hello.txt", Size: uint64(len(content))}); err != nil {
        t.Fatalf("send metadata chunk: %v", err)
    }
    if err := stream.Send(&pb.UploadChunk{Data: content}); err != nil {
        t.Fatalf("send data chunk: %v", err)
    }
    resp, err := stream.CloseAndRecv()
    if err != nil {
        t.Fatalf("close upload: %v", err)
    }
    if len(resp.Hash) != 32 {
        t.Errorf("expected 32-byte hash, got %d", len(resp.Hash))
    }
    if resp.Name != "hello.txt" {
        t.Errorf("expected name hello.txt, got %s", resp.Name)
    }

    // List should return the file
    listResp, err := client.List(context.Background(), &pb.ListRequest{})
    if err != nil {
        t.Fatalf("list: %v", err)
    }
    if len(listResp.Files) != 1 {
        t.Fatalf("expected 1 file, got %d", len(listResp.Files))
    }
    if listResp.Files[0].Name != "hello.txt" {
        t.Errorf("expected hello.txt, got %s", listResp.Files[0].Name)
    }
}

func TestDownloadByHash(t *testing.T) {
    client := newTestServer(t)
    content := []byte("download me")

    // Upload first
    stream, _ := client.Upload(context.Background())
    stream.Send(&pb.UploadChunk{Name: "dl.txt", Size: uint64(len(content))})
    stream.Send(&pb.UploadChunk{Data: content})
    resp, err := stream.CloseAndRecv()
    if err != nil {
        t.Fatalf("upload: %v", err)
    }

    // Download by hash
    dl, err := client.Download(context.Background(), &pb.DownloadRequest{Hash: resp.Hash})
    if err != nil {
        t.Fatalf("download: %v", err)
    }
    var buf bytes.Buffer
    for {
        chunk, err := dl.Recv()
        if err == io.EOF {
            break
        }
        if err != nil {
            t.Fatalf("recv chunk: %v", err)
        }
        buf.Write(chunk.Data)
    }
    if !bytes.Equal(buf.Bytes(), content) {
        t.Errorf("content mismatch: got %q, want %q", buf.Bytes(), content)
    }
}

func TestDeleteFile(t *testing.T) {
    client := newTestServer(t)
    content := []byte("delete me")

    stream, _ := client.Upload(context.Background())
    stream.Send(&pb.UploadChunk{Name: "todel.txt", Size: uint64(len(content))})
    stream.Send(&pb.UploadChunk{Data: content})
    resp, err := stream.CloseAndRecv()
    if err != nil {
        t.Fatalf("upload: %v", err)
    }

    _, err = client.Delete(context.Background(), &pb.DeleteRequest{Hash: resp.Hash})
    if err != nil {
        t.Fatalf("delete: %v", err)
    }

    listResp, _ := client.List(context.Background(), &pb.ListRequest{})
    if len(listResp.Files) != 0 {
        t.Errorf("expected 0 files after delete, got %d", len(listResp.Files))
    }
}
```

**Step 2: Run the test to verify it fails**

```bash
go test ./src/api/grpc/... -v -run TestUploadAndList
```

Expected: FAIL — package `src/api/grpc` does not exist.

**Step 3: Implement `src/api/grpc/server.go`**

```go
package grpcserver

import (
    "context"
    "encoding/hex"
    "fmt"
    "io"

    "github.com/danmuck/dps_files/src/api/ledgers"
    "github.com/danmuck/dps_files/src/api/pb"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

const downloadChunkSize = 1 << 20 // 1 MiB per stream message

// Server implements pb.DPSFilesServer backed by a FileLedger.
type Server struct {
    pb.UnimplementedDPSFilesServer
    storage ledgers.FileLedger
}

// New returns a Server using the given FileLedger.
func New(storage ledgers.FileLedger) *Server {
    return &Server{storage: storage}
}

// Upload receives a client-streamed file and stores it.
// First message carries name + size; subsequent messages carry data bytes.
func (s *Server) Upload(stream pb.DPSFiles_UploadServer) error {
    // Read metadata from first message.
    first, err := stream.Recv()
    if err != nil {
        return status.Errorf(codes.InvalidArgument, "receive first chunk: %v", err)
    }
    name := first.Name
    size := first.Size
    if name == "" {
        return status.Error(codes.InvalidArgument, "name is required in first chunk")
    }

    // Pipe remaining chunks into StoreFromReader so nothing is buffered in memory.
    pr, pw := io.Pipe()
    errCh := make(chan error, 1)

    // Writer goroutine: flush first data payload (may be empty), then loop.
    go func() {
        defer pw.Close()
        if len(first.Data) > 0 {
            if _, err := pw.Write(first.Data); err != nil {
                errCh <- err
                return
            }
        }
        for {
            chunk, err := stream.Recv()
            if err == io.EOF {
                errCh <- nil
                return
            }
            if err != nil {
                errCh <- err
                return
            }
            if _, err := pw.Write(chunk.Data); err != nil {
                errCh <- err
                return
            }
        }
    }()

    fid, storeErr := s.storage.StoreFromReader(name, pr, size)

    // Drain the writer goroutine.
    recvErr := <-errCh
    if storeErr != nil {
        return status.Errorf(codes.Internal, "store: %v", storeErr)
    }
    if recvErr != nil {
        return status.Errorf(codes.Internal, "receive: %v", recvErr)
    }

    return stream.SendAndClose(&pb.UploadResponse{
        Hash: fid[:],
        Name: name,
        Size: size,
    })
}

// Download streams a stored file to the client in 1 MiB chunks.
func (s *Server) Download(req *pb.DownloadRequest, stream pb.DPSFiles_DownloadServer) error {
    pr, pw := io.Pipe()

    errCh := make(chan error, 1)
    go func() {
        defer pw.Close()
        var err error
        if len(req.Hash) == 32 {
            var fid ledgers.FileID
            copy(fid[:], req.Hash)
            err = s.storage.StreamFile(fid, pw)
        } else if req.Name != "" {
            err = s.storage.StreamFileByName(req.Name, pw)
        } else {
            err = fmt.Errorf("hash or name required")
        }
        errCh <- err
    }()

    buf := make([]byte, downloadChunkSize)
    for {
        n, err := pr.Read(buf)
        if n > 0 {
            if sendErr := stream.Send(&pb.DataChunk{Data: buf[:n]}); sendErr != nil {
                pr.CloseWithError(sendErr)
                <-errCh
                return sendErr
            }
        }
        if err == io.EOF {
            break
        }
        if err != nil {
            <-errCh
            return status.Errorf(codes.Internal, "read: %v", err)
        }
    }
    if err := <-errCh; err != nil {
        return status.Errorf(codes.NotFound, "stream: %v", err)
    }
    return nil
}

// Delete removes a file by its 32-byte SHA-256 hash.
func (s *Server) Delete(_ context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
    if len(req.Hash) != 32 {
        return nil, status.Errorf(codes.InvalidArgument, "hash must be 32 bytes, got %d", len(req.Hash))
    }
    var fid ledgers.FileID
    copy(fid[:], req.Hash)
    if err := s.storage.DeleteFile(fid); err != nil {
        return nil, status.Errorf(codes.NotFound, "delete: %v", err)
    }
    return &pb.DeleteResponse{}, nil
}

// List returns metadata for all stored files and directories.
func (s *Server) List(_ context.Context, _ *pb.ListRequest) (*pb.ListResponse, error) {
    summaries := s.storage.ListKnownFilesMetadata()
    entries := make([]*pb.FileEntry, len(summaries))
    for i, sm := range summaries {
        entries[i] = &pb.FileEntry{
            Name:      sm.Name,
            Hash:      sm.Hash[:],
            Size:      sm.Size,
            EntryType: "file",
        }
    }
    return &pb.ListResponse{Files: entries}, nil
}

// UploadDir stores a directory tree that exists on the server's local filesystem.
func (s *Server) UploadDir(_ context.Context, req *pb.UploadDirRequest) (*pb.UploadDirResponse, error) {
    if req.RootPath == "" {
        return nil, status.Error(codes.InvalidArgument, "root_path is required")
    }
    fid, err := s.storage.StoreDirectory(req.RootPath)
    if err != nil {
        return nil, status.Errorf(codes.Internal, "store directory: %v", err)
    }
    return &pb.UploadDirResponse{Hash: fid[:]}, nil
}

// ListDir returns the immediate children of a directory by its hash.
func (s *Server) ListDir(_ context.Context, req *pb.ListDirRequest) (*pb.ListDirResponse, error) {
    if len(req.Hash) != 32 {
        return nil, status.Errorf(codes.InvalidArgument, "hash must be 32 bytes")
    }
    var fid ledgers.FileID
    copy(fid[:], req.Hash)
    ledgerEntries, err := s.storage.ListDirectory(fid)
    if err != nil {
        return nil, status.Errorf(codes.NotFound, "list directory: %v", err)
    }
    entries := make([]*pb.DirEntry, len(ledgerEntries))
    for i, e := range ledgerEntries {
        entries[i] = &pb.DirEntry{
            Name: e.Name,
            Path: e.Path,
            Hash: e.Hash[:],
            Type: e.Type,
            Size: e.Size,
        }
    }
    return &pb.ListDirResponse{Entries: entries}, nil
}

// hexBytes is a helper used in error messages.
func hexBytes(b []byte) string { return hex.EncodeToString(b) }
```

**Step 4: Run tests**

```bash
go test ./src/api/grpc/... -v
```

Expected: all three tests PASS.

**Step 5: Commit**

```bash
git add src/api/grpc/
git commit -m "feat: implement gRPC DPSFiles server backed by FileLedger"
```

---

## Task 5: Rewrite DefaultServerNode to use gRPC

**Files:**
- Modify: `src/api/nodes/server_node.go`
- Modify: `src/api/nodes/server_node_test.go`
- Delete: `src/api/nodes/http_handlers.go`
- Delete: `src/api/nodes/http_handlers_test.go`

**Step 1: Rewrite `server_node.go`**

Replace the entire file:

```go
package nodes

import (
    "fmt"
    "net"

    grpcserver "github.com/danmuck/dps_files/src/api/grpc"
    "github.com/danmuck/dps_files/src/api/ledgers"
    "github.com/danmuck/dps_files/src/api/pb"
    "github.com/danmuck/dps_files/src/key_store"
    logs "github.com/danmuck/smplog"
    "google.golang.org/grpc"
)

// DefaultServerNode serves files over gRPC. An optional gRPC-Gateway HTTP
// listener can be added with WithHTTP to expose a REST/JSON API on a second port.
type DefaultServerNode struct {
    *DefaultNode
    storage    ledgers.FileLedger
    grpcServer *grpc.Server
    httpAddr   string
    listener   net.Listener
}

// ServerOption configures optional DefaultServerNode features.
type ServerOption func(*DefaultServerNode)

// WithHTTP enables a gRPC-Gateway HTTP listener on the given address.
func WithHTTP(addr string) ServerOption {
    return func(s *DefaultServerNode) { s.httpAddr = addr }
}

// NewServerNode creates a DefaultServerNode backed by a KeyStore at storageDir.
func NewServerNode(id []byte, addr string, storageDir string, opts ...ServerOption) (*DefaultServerNode, error) {
    base, err := NewDefaultNode(id, addr)
    if err != nil {
        return nil, err
    }
    ks, err := key_store.InitKeyStore(storageDir)
    if err != nil {
        return nil, fmt.Errorf("init keystore: %w", err)
    }
    sn := &DefaultServerNode{
        DefaultNode: base,
        storage:     key_store.NewFileLedger(ks),
        grpcServer:  grpc.NewServer(),
    }
    pb.RegisterDPSFilesServer(sn.grpcServer, grpcserver.New(sn.storage))
    for _, opt := range opts {
        opt(sn)
    }
    return sn, nil
}

// Storage returns the node's FileLedger.
func (s *DefaultServerNode) Storage() ledgers.FileLedger {
    return s.storage
}

// RawKeyStore returns the underlying *KeyStore for TUI operations.
func (s *DefaultServerNode) RawKeyStore() *key_store.KeyStore {
    if ksl, ok := s.storage.(*key_store.KeyStoreLedger); ok {
        return ksl.KeyStore()
    }
    return nil
}

// Addr returns the address the gRPC server is bound to.
func (s *DefaultServerNode) Addr() string {
    if s.listener != nil {
        return s.listener.Addr().String()
    }
    return s.address
}

// Start binds the gRPC listener and, if configured, the gRPC-Gateway HTTP listener.
func (s *DefaultServerNode) Start() error {
    lis, err := net.Listen("tcp", s.address)
    if err != nil {
        return fmt.Errorf("listen %s: %w", s.address, err)
    }
    s.listener = lis
    go func() {
        if err := s.grpcServer.Serve(lis); err != nil {
            logs.Warnf("gRPC server stopped: %v", err)
        }
    }()

    if s.httpAddr != "" {
        go func() {
            if err := s.serveGateway(s.httpAddr, s.Addr()); err != nil {
                logs.Warnf("gRPC-Gateway stopped: %v", err)
            }
        }()
    }
    return nil
}

// Shutdown stops the gRPC server gracefully.
func (s *DefaultServerNode) Shutdown() error {
    s.grpcServer.GracefulStop()
    return nil
}
```

**Step 2: Create `src/api/nodes/gateway.go`** for the gRPC-Gateway wiring (keeps server_node.go clean):

```go
package nodes

import (
    "context"
    "net/http"

    "github.com/danmuck/dps_files/src/api/pb"
    "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

// serveGateway starts an HTTP/JSON reverse proxy that forwards to the gRPC
// server at grpcAddr. Blocks until the server fails or is closed.
func (s *DefaultServerNode) serveGateway(httpAddr, grpcAddr string) error {
    ctx := context.Background()
    mux := runtime.NewServeMux()
    opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
    if err := pb.RegisterDPSFilesHandlerFromEndpoint(ctx, mux, grpcAddr, opts); err != nil {
        return err
    }
    return http.ListenAndServe(httpAddr, mux)
}
```

**Step 3: Update `server_node_test.go`**

Replace the file — use `bufconn` just like the grpcserver test:

```go
package nodes_test

import (
    "context"
    "testing"

    "github.com/danmuck/dps_files/src/api/nodes"
    "github.com/danmuck/dps_files/src/api/pb"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func newTestServerNode(t *testing.T) (*nodes.DefaultServerNode, pb.DPSFilesClient) {
    t.Helper()
    id := make([]byte, 20)
    sn, err := nodes.NewServerNode(id, "localhost:0", t.TempDir())
    if err != nil {
        t.Fatalf("NewServerNode: %v", err)
    }
    if err := sn.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }
    t.Cleanup(func() { sn.Shutdown() })

    conn, err := grpc.NewClient(sn.Addr(),
        grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        t.Fatalf("dial: %v", err)
    }
    t.Cleanup(func() { conn.Close() })
    return sn, pb.NewDPSFilesClient(conn)
}

func TestServerNodeStartShutdown(t *testing.T) {
    _, _ = newTestServerNode(t)
    // Start + Shutdown via t.Cleanup is the test.
}

func TestServerNodeUploadAndList(t *testing.T) {
    _, client := newTestServerNode(t)

    stream, err := client.Upload(context.Background())
    if err != nil {
        t.Fatalf("Upload open: %v", err)
    }
    data := []byte("server node test data")
    stream.Send(&pb.UploadChunk{Name: "node_test.txt", Size: uint64(len(data))})
    stream.Send(&pb.UploadChunk{Data: data})
    resp, err := stream.CloseAndRecv()
    if err != nil {
        t.Fatalf("CloseAndRecv: %v", err)
    }
    if len(resp.Hash) != 32 {
        t.Errorf("expected 32-byte hash, got %d", len(resp.Hash))
    }

    list, err := client.List(context.Background(), &pb.ListRequest{})
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(list.Files) != 1 || list.Files[0].Name != "node_test.txt" {
        t.Errorf("unexpected list: %+v", list.Files)
    }
}
```

**Step 4: Delete `http_handlers.go` and `http_handlers_test.go`**

```bash
rm src/api/nodes/http_handlers.go src/api/nodes/http_handlers_test.go
```

**Step 5: Run tests**

```bash
go test ./src/api/nodes/... -v -run TestServerNode
```

Expected: PASS.

**Step 6: Commit**

```bash
git add src/api/nodes/
git commit -m "feat: replace TCPHandler+HTTP with gRPC server in DefaultServerNode"
```

---

## Task 6: Simplify DefaultNode (remove TCPHandler)

**Files:**
- Modify: `src/api/nodes/default.go`
- Modify: `src/api/nodes/nodes.go`

`DefaultNode` currently holds a `TCPHandler` and `exit` channel. Neither is needed after the gRPC migration; the gRPC server manages its own lifecycle.

**Step 1: Rewrite `default.go`**

```go
package nodes

import (
    "time"

    "github.com/danmuck/dps_files/src/api/transport"
)

// DefaultNode provides the base identity fields shared by all node types.
type DefaultNode struct {
    address string
    pubKey  []byte
    Router  RoutingTable
}

func NewDefaultNode(id []byte, address string) (*DefaultNode, error) {
    node := &transport.NodeInfo{
        Address: address,
        Id:      id,
        Time:    time.Now().UnixNano(),
    }
    rt, err := NewDefaultRouter(node)
    if err != nil {
        return nil, fmt.Errorf("failed to create router: %w", err)
    }
    return &DefaultNode{
        pubKey:  id,
        address: address,
        Router:  rt,
    }, nil
}

func (n *DefaultNode) NodeInfo() transport.NodeInfo {
    return transport.NodeInfo{Id: n.pubKey, Address: n.address, Time: time.Now().UnixNano()}
}

func (n *DefaultNode) Address() string { return n.address }
func (n *DefaultNode) ID() []byte      { return n.pubKey }
func (n *DefaultNode) PubKey() []byte  { return n.pubKey }
func (n *DefaultNode) Peers() []*transport.NodeInfo { return nil }
```

Note: `transport.NodeInfo` still comes from `src/api/pb` after the transport package is cleaned up. Update that import once Task 8 is complete.

**Step 2: Run all tests**

```bash
go test ./src/api/nodes/... -v
```

Expected: PASS.

**Step 3: Commit**

```bash
git add src/api/nodes/default.go
git commit -m "refactor: remove TCPHandler and exit channel from DefaultNode"
```

---

## Task 7: Rewrite DefaultClientNode with gRPC

**Files:**
- Modify: `src/api/nodes/client_node.go`
- Modify: `src/api/nodes/client_node_test.go`

**Step 1: Rewrite `client_node.go`**

```go
package nodes

import (
    "context"
    "fmt"
    "io"
    "os"
    "path/filepath"

    "github.com/danmuck/dps_files/src/api/ledgers"
    "github.com/danmuck/dps_files/src/api/pb"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

// DefaultClientNode performs file operations against remote ServerNodes or a
// co-located local server. Remote calls go over gRPC.
type DefaultClientNode struct {
    *DefaultNode
    localServer *DefaultServerNode
    storageDir  string
    remoteConns []*grpc.ClientConn
    remoteAddrs []string
}

// ClientOption configures optional DefaultClientNode features.
type ClientOption func(*DefaultClientNode)

// WithLocalStorage enables a co-located ServerNode backed by the given directory.
func WithLocalStorage(dir string) ClientOption {
    return func(c *DefaultClientNode) { c.storageDir = dir }
}

// WithRemotes adds remote server gRPC addresses.
func WithRemotes(addrs ...string) ClientOption {
    return func(c *DefaultClientNode) { c.remoteAddrs = append(c.remoteAddrs, addrs...) }
}

// NewClientNode creates a DefaultClientNode.
func NewClientNode(id []byte, opts ...ClientOption) (*DefaultClientNode, error) {
    base, err := NewDefaultNode(id, "localhost:0")
    if err != nil {
        return nil, err
    }
    cn := &DefaultClientNode{DefaultNode: base}
    for _, opt := range opts {
        opt(cn)
    }
    return cn, nil
}

// Start initialises the local server (if configured) and dials remotes.
func (c *DefaultClientNode) Start() error {
    if c.storageDir != "" {
        sn, err := NewServerNode(c.ID(), "localhost:0", c.storageDir)
        if err != nil {
            return fmt.Errorf("start local server: %w", err)
        }
        if err := sn.Start(); err != nil {
            return fmt.Errorf("start local server: %w", err)
        }
        c.localServer = sn
    }
    for _, addr := range c.remoteAddrs {
        conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
        if err != nil {
            return fmt.Errorf("dial %s: %w", addr, err)
        }
        c.remoteConns = append(c.remoteConns, conn)
    }
    return nil
}

// Shutdown stops the local server and closes all remote connections.
func (c *DefaultClientNode) Shutdown() error {
    if c.localServer != nil {
        c.localServer.Shutdown()
    }
    for _, conn := range c.remoteConns {
        conn.Close()
    }
    return nil
}

// LocalServer returns the co-located ServerNode, or nil if not configured.
func (c *DefaultClientNode) LocalServer() *DefaultServerNode {
    return c.localServer
}

// remoteClient returns a gRPC client for the first remote, or an error.
func (c *DefaultClientNode) remoteClient() (pb.DPSFilesClient, error) {
    if len(c.remoteConns) == 0 {
        return nil, fmt.Errorf("no remote connections configured")
    }
    return pb.NewDPSFilesClient(c.remoteConns[0]), nil
}

// Upload reads filePath and streams it to the first configured remote.
func (c *DefaultClientNode) Upload(filePath string) (ledgers.FileID, error) {
    rc, err := c.remoteClient()
    if err != nil {
        return ledgers.FileID{}, err
    }
    info, err := os.Stat(filePath)
    if err != nil {
        return ledgers.FileID{}, fmt.Errorf("stat: %w", err)
    }
    f, err := os.Open(filePath)
    if err != nil {
        return ledgers.FileID{}, fmt.Errorf("open: %w", err)
    }
    defer f.Close()

    stream, err := rc.Upload(context.Background())
    if err != nil {
        return ledgers.FileID{}, fmt.Errorf("open stream: %w", err)
    }
    // Send metadata first.
    if err := stream.Send(&pb.UploadChunk{
        Name: filepath.Base(filePath),
        Size: uint64(info.Size()),
    }); err != nil {
        return ledgers.FileID{}, fmt.Errorf("send metadata: %w", err)
    }
    // Stream file data in 1 MiB chunks.
    buf := make([]byte, 1<<20)
    for {
        n, readErr := f.Read(buf)
        if n > 0 {
            if err := stream.Send(&pb.UploadChunk{Data: buf[:n]}); err != nil {
                return ledgers.FileID{}, fmt.Errorf("send chunk: %w", err)
            }
        }
        if readErr == io.EOF {
            break
        }
        if readErr != nil {
            return ledgers.FileID{}, fmt.Errorf("read: %w", readErr)
        }
    }
    resp, err := stream.CloseAndRecv()
    if err != nil {
        return ledgers.FileID{}, fmt.Errorf("close stream: %w", err)
    }
    var fid ledgers.FileID
    copy(fid[:], resp.Hash)
    return fid, nil
}

// List returns file metadata from the first configured remote.
func (c *DefaultClientNode) List() ([]ledgers.FileMetaSummary, error) {
    rc, err := c.remoteClient()
    if err != nil {
        return nil, err
    }
    resp, err := rc.List(context.Background(), &pb.ListRequest{})
    if err != nil {
        return nil, err
    }
    out := make([]ledgers.FileMetaSummary, len(resp.Files))
    for i, f := range resp.Files {
        var fid ledgers.FileID
        copy(fid[:], f.Hash)
        out[i] = ledgers.FileMetaSummary{Name: f.Name, Hash: fid, Size: f.Size}
    }
    return out, nil
}

// ListLocal lists files on the co-located server without a network round-trip.
func (c *DefaultClientNode) ListLocal() ([]ledgers.FileMetaSummary, error) {
    if c.localServer == nil {
        return nil, fmt.Errorf("no local server configured")
    }
    return c.localServer.Storage().ListKnownFilesMetadata(), nil
}
```

**Step 2: Update `client_node_test.go`**

```go
package nodes_test

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/danmuck/dps_files/src/api/nodes"
)

func TestClientNodeLocalUploadAndList(t *testing.T) {
    id := make([]byte, 20)
    cn, err := nodes.NewClientNode(id, nodes.WithLocalStorage(t.TempDir()))
    if err != nil {
        t.Fatalf("NewClientNode: %v", err)
    }
    if err := cn.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }
    defer cn.Shutdown()

    summaries, err := cn.ListLocal()
    if err != nil {
        t.Fatalf("ListLocal: %v", err)
    }
    if len(summaries) != 0 {
        t.Errorf("expected empty list, got %d", len(summaries))
    }
}

func TestClientNodeRemoteUploadAndList(t *testing.T) {
    // Start a test server node.
    sn, client := newTestServerNode(t) // reuses helper from server_node_test.go
    _ = client

    // Create a client pointing at that server.
    id := make([]byte, 20)
    cn, err := nodes.NewClientNode(id,
        nodes.WithLocalStorage(t.TempDir()),
        nodes.WithRemotes(sn.Addr()),
    )
    if err != nil {
        t.Fatalf("NewClientNode: %v", err)
    }
    if err := cn.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }
    defer cn.Shutdown()

    // Write a temp file and upload it.
    tmp := filepath.Join(t.TempDir(), "remote_test.txt")
    if err := os.WriteFile(tmp, []byte("remote upload test"), 0o644); err != nil {
        t.Fatal(err)
    }
    fid, err := cn.Upload(tmp)
    if err != nil {
        t.Fatalf("Upload: %v", err)
    }
    if fid == (ledgers.FileID{}) {
        t.Error("expected non-zero FileID")
    }

    list, err := cn.List()
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(list) != 1 || list[0].Name != "remote_test.txt" {
        t.Errorf("unexpected list: %+v", list)
    }
}
```

**Step 3: Run tests**

```bash
go test ./src/api/nodes/... -v
```

Expected: PASS.

**Step 4: Commit**

```bash
git add src/api/nodes/client_node.go src/api/nodes/client_node_test.go
git commit -m "feat: replace TCPHandler with grpc.ClientConn in DefaultClientNode"
```

---

## Task 8: Rewrite cmd/client/remote.go with gRPC

**Files:**
- Modify: `cmd/client/remote.go`

`FileServerClient` used the old binary protocol. Replace it entirely with gRPC stubs. The same `pb.DPSFilesClient` interface is used.

**Step 1: Rewrite `remote.go`**

```go
package main

import (
    "context"
    "encoding/hex"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "time"

    "github.com/danmuck/dps_files/src/api/pb"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

// RemoteFileEntry is a file entry returned by the server List RPC.
type RemoteFileEntry struct {
    Name string
    Hash string // hex-encoded 32-byte SHA-256
    Size uint64
}

// GRPCClient dials a DPSFiles gRPC server.
type GRPCClient struct {
    addr    string
    timeout time.Duration
    conn    *grpc.ClientConn
    stub    pb.DPSFilesClient
}

// NewGRPCClient returns a client connected to addr.
func NewGRPCClient(addr string) (*GRPCClient, error) {
    conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        return nil, fmt.Errorf("dial %s: %w", addr, err)
    }
    return &GRPCClient{
        addr:    addr,
        timeout: 30 * time.Second,
        conn:    conn,
        stub:    pb.NewDPSFilesClient(conn),
    }, nil
}

// Close releases the underlying connection.
func (c *GRPCClient) Close() { c.conn.Close() }

func (c *GRPCClient) ctx() (context.Context, context.CancelFunc) {
    return context.WithTimeout(context.Background(), c.timeout)
}

// List returns all files known to the server.
func (c *GRPCClient) List() ([]RemoteFileEntry, error) {
    ctx, cancel := c.ctx()
    defer cancel()
    resp, err := c.stub.List(ctx, &pb.ListRequest{})
    if err != nil {
        return nil, fmt.Errorf("list: %w", err)
    }
    entries := make([]RemoteFileEntry, len(resp.Files))
    for i, f := range resp.Files {
        entries[i] = RemoteFileEntry{
            Name: f.Name,
            Hash: hex.EncodeToString(f.Hash),
            Size: f.Size,
        }
    }
    return entries, nil
}

// Upload sends localPath to the server and returns the 32-byte SHA-256 hash.
func (c *GRPCClient) Upload(localPath string) ([32]byte, error) {
    var hash [32]byte
    info, err := os.Stat(localPath)
    if err != nil {
        return hash, fmt.Errorf("stat: %w", err)
    }
    f, err := os.Open(localPath)
    if err != nil {
        return hash, fmt.Errorf("open: %w", err)
    }
    defer f.Close()

    // Use no deadline for large transfers.
    stream, err := c.stub.Upload(context.Background())
    if err != nil {
        return hash, fmt.Errorf("open upload stream: %w", err)
    }
    if err := stream.Send(&pb.UploadChunk{
        Name: filepath.Base(localPath),
        Size: uint64(info.Size()),
    }); err != nil {
        return hash, fmt.Errorf("send metadata: %w", err)
    }
    buf := make([]byte, 1<<20) // 1 MiB chunks
    for {
        n, readErr := f.Read(buf)
        if n > 0 {
            if err := stream.Send(&pb.UploadChunk{Data: buf[:n]}); err != nil {
                return hash, fmt.Errorf("send chunk: %w", err)
            }
        }
        if readErr == io.EOF {
            break
        }
        if readErr != nil {
            return hash, fmt.Errorf("read: %w", readErr)
        }
    }
    resp, err := stream.CloseAndRecv()
    if err != nil {
        return hash, fmt.Errorf("finish upload: %w", err)
    }
    copy(hash[:], resp.Hash)
    return hash, nil
}

// Download fetches a file by name and writes it to outputPath.
// Returns the number of bytes written.
func (c *GRPCClient) Download(name, outputPath string) (uint64, error) {
    stream, err := c.stub.Download(context.Background(), &pb.DownloadRequest{Name: name})
    if err != nil {
        return 0, fmt.Errorf("open download stream: %w", err)
    }
    if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
        return 0, fmt.Errorf("ensure output dir: %w", err)
    }
    out, err := os.Create(outputPath)
    if err != nil {
        return 0, fmt.Errorf("create output file: %w", err)
    }
    defer out.Close()

    var total uint64
    for {
        chunk, err := stream.Recv()
        if err == io.EOF {
            break
        }
        if err != nil {
            return total, fmt.Errorf("recv chunk: %w", err)
        }
        n, err := out.Write(chunk.Data)
        total += uint64(n)
        if err != nil {
            return total, fmt.Errorf("write: %w", err)
        }
    }
    return total, nil
}

// Delete removes the file identified by its 32-byte SHA-256 hash.
func (c *GRPCClient) Delete(hash [32]byte) error {
    ctx, cancel := c.ctx()
    defer cancel()
    _, err := c.stub.Delete(ctx, &pb.DeleteRequest{Hash: hash[:]})
    return err
}

// hexToHash decodes a 64-char hex string into a [32]byte.
func hexToHash(s string) ([32]byte, error) {
    var h [32]byte
    b, err := hex.DecodeString(s)
    if err != nil {
        return h, fmt.Errorf("invalid hex: %w", err)
    }
    if len(b) != 32 {
        return h, fmt.Errorf("expected 32 bytes, got %d", len(b))
    }
    copy(h[:], b)
    return h, nil
}
```

**Step 2: Update callers of `FileServerClient` → `GRPCClient`**

Search for usages:
```bash
grep -rn "FileServerClient\|NewFileServerClient\|remoteWriteFrame\|remoteReadFrame" cmd/client/
```

Update each call site. The main change is:
- `NewFileServerClient(addr)` → `NewGRPCClient(addr)` (returns `(*GRPCClient, error)` now)
- `client.List()` — same signature, same return type
- `client.Upload(path, nil)` → `client.Upload(path)` (reader arg removed)
- `client.Download(name, path, nil)` → `client.Download(name, path)` (progressWriter removed for now)

In `view.go`, `store.go`, `stream.go`, `delete.go` — update to construct `GRPCClient` and handle the `error` return from `NewGRPCClient`.

**Step 3: Build to confirm no compile errors**

```bash
go build ./cmd/client/...
```

Expected: no errors.

**Step 4: Run all tests**

```bash
go test ./... -short
```

Expected: PASS (short skips the 256MB test).

**Step 5: Commit**

```bash
git add cmd/client/
git commit -m "feat: replace binary FileServerClient with gRPC stubs in cmd/client"
```

---

## Task 9: Clean Up the transport Package

**Files:**
- Delete: `src/api/transport/tcp.go`
- Delete: `src/api/transport/encoding.go`
- Delete: `src/api/transport/transport.go`
- Delete: `src/api/transport/udp.go`
- Delete: `src/api/transport/tcp_handler_test.go`
- Delete: `src/api/transport/rpc.proto`
- Delete: `src/api/transport/rpc.pb.go`
- Keep or migrate: any `NodeInfo` usage

`transport.NodeInfo` is still referenced by `DefaultNode` and `RoutingTable`. Move `NodeInfo` into `src/api/pb/` (it's already defined there in the generated code as `pb.NodeInfo` isn't needed by the ledger layer) or define a minimal local type. The cleanest option: keep a thin `transport` package with just the `NodeInfo` struct for routing identity, or inline it into the `nodes` package.

**Decision:** Move `NodeInfo` into `src/api/nodes/nodes.go` as a local type since it's only used there. Remove the `transport` package entirely.

**Step 1: Update `nodes.go` — add NodeInfo locally**

```go
// NodeInfo identifies a node on the network.
type NodeInfo struct {
    ID      []byte
    Address string
}
```

**Step 2: Update `routing.go` to use `NodeInfo` from `nodes` not `transport`**

Change all `*transport.NodeInfo` → `*NodeInfo`.

**Step 3: Update `default.go` to remove the `transport` import**

**Step 4: Delete the transport package files**

```bash
rm src/api/transport/tcp.go \
   src/api/transport/encoding.go \
   src/api/transport/transport.go \
   src/api/transport/udp.go \
   src/api/transport/tcp_handler_test.go \
   src/api/transport/rpc.proto \
   src/api/transport/rpc.pb.go
rmdir src/api/transport   # only if empty
```

**Step 5: Build and test**

```bash
go build ./... && go test ./... -short
```

Expected: no errors, all tests pass.

**Step 6: Commit**

```bash
git add -A
git commit -m "refactor: remove transport package, move NodeInfo into nodes package"
```

---

## Task 10: Update cmd/server/main.go

**Files:**
- Modify: `cmd/server/main.go`

**Step 1: Rewrite `main.go`**

```go
package main

import (
    "crypto/rand"
    "flag"
    "os"
    "os/signal"
    "syscall"

    "github.com/danmuck/dps_files/cmd/internal/logcfg"
    "github.com/danmuck/dps_files/src/api/nodes"
    logs "github.com/danmuck/smplog"
)

func main() {
    logs.Configure(logcfg.Load())

    addr       := flag.String("addr", ":9000", "gRPC listen address")
    httpAddr   := flag.String("http", "",      "gRPC-Gateway HTTP listen address (optional)")
    storageDir := flag.String("storage", "local/storage", "storage directory")
    flag.Parse()

    id := make([]byte, 20)
    if _, err := rand.Read(id); err != nil {
        logs.Fatalf(err, "generate node ID")
    }

    var opts []nodes.ServerOption
    if *httpAddr != "" {
        opts = append(opts, nodes.WithHTTP(*httpAddr))
    }

    sn, err := nodes.NewServerNode(id, *addr, *storageDir, opts...)
    if err != nil {
        logs.Fatalf(err, "create server node")
    }
    if err := sn.Start(); err != nil {
        logs.Fatalf(err, "start server node")
    }

    logs.Infof("gRPC server listening on %s (storage: %s)", sn.Addr(), *storageDir)
    if *httpAddr != "" {
        logs.Infof("gRPC-Gateway HTTP on %s", *httpAddr)
    }

    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    <-sig

    logs.Infof("Shutting down...")
    sn.Shutdown()
}
```

**Step 2: Build**

```bash
make build
```

Expected: `.build/server/server` created, no errors.

**Step 3: Smoke test — start server, connect with grpcurl**

```bash
.build/server/server --addr :9000 --storage /tmp/dps-smoke &
SERVER_PID=$!
sleep 1
# List files (requires grpcurl: brew install grpcurl)
grpcurl -plaintext -proto src/api/pb/dps.proto localhost:9000 dps.DPSFiles/List
kill $SERVER_PID
```

Expected: `{ "files": [] }` response.

**Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: update cmd/server to use gRPC server node"
```

---

## Task 11: Run Full Test Suite

**Step 1: Run all tests including large file**

```bash
make test
```

Expected: all tests PASS, `.build/` cleaned up.

**Step 2: Run tests with coverage**

```bash
make test-coverage
```

Review coverage output. Key packages to check:
- `src/api/grpc` — aim for >80%
- `src/api/nodes` — aim for >70%

**Step 3: Commit any test fixes discovered**

```bash
git add -A
git commit -m "test: fix any test issues found in full suite run"
```

---

## Task 12: End-to-End Smoke Test (Two-Machine)

This verifies the actual cross-machine scenario that was broken.

**Step 1: On the server machine (MacBook)**

```bash
make build
.build/server/server --addr :9000 --http :8080 --storage local/storage
```

Expected: `gRPC server listening on [::]:9000` — stays running.

**Step 2: On the client machine (Mac Mini)**

```bash
make build
.build/client/client remote view --remote-addr <macbook-ip>:9000
```

Expected: list of files (empty if fresh) — no connection refused, no proto parse errors.

**Step 3: Test HTTP gateway**

```bash
curl http://<macbook-ip>:8080/v1/files
```

Expected: `{"files":[]}` JSON response.

**Step 4: Upload a file via client**

```bash
.build/client/client remote upload --remote-addr <macbook-ip>:9000
```

Follow TUI prompts, upload a file from `local/upload/`.

**Step 5: Verify it appears in List and via HTTP**

```bash
curl http://<macbook-ip>:8080/v1/files
```

Expected: uploaded file appears in JSON.

---

## Task 13: Update CLAUDE.md and docs/progress/buildplan.md

**Files:**
- Modify: `CLAUDE.md`
- Modify: `docs/progress/buildplan.md`

**Step 1: Update CLAUDE.md**

- Remove references to `TCPHandler`, `encoding.go`, `tcp.go`, `udp.go`, `http_handlers.go`
- Remove `WithHTTP` starts HTTP server (now it starts gRPC-Gateway)
- Add `src/api/pb/` package description
- Add `src/api/grpc/` package description
- Update "Remaining Known Issues" — remove dial-back and wire-format issues
- Update `build-protobuf` make target description
- Update `Add a New RPC Method` instructions to reference proto service + regeneration

**Step 2: Update buildplan.md**

Mark gRPC migration tasks complete. Note any open work (TLS, Kademlia stubs still in place).

**Step 3: Commit**

```bash
git add CLAUDE.md docs/progress/
git commit -m "docs: update CLAUDE.md and buildplan for gRPC migration"
```

---

## Dependency Summary

New packages to add:
```
google.golang.org/grpc
github.com/grpc-ecosystem/grpc-gateway/v2
```

Packages removed (no longer imported anywhere after migration):
```
# From transport package (deleted):
google.golang.org/protobuf  # still needed for pb/ generated code
# Everything else in transport/ is gone
```

gRPC itself depends on `google.golang.org/protobuf`, so the existing Protobuf dependency is preserved.

---

## Risk Notes

- **gRPC max message size:** Default is 4 MB per message. Our streaming uses 1 MiB chunks so this is fine. If you ever need to send a single unary message >4 MB, use `grpc.MaxCallRecvMsgSize` option.
- **TLS:** All connections are currently plaintext (`insecure.NewCredentials()`). Add TLS as a follow-up — the gRPC API makes this a one-line swap.
- **gRPC-Gateway streaming:** The gateway buffers streaming RPCs at the HTTP boundary. Large downloads via the HTTP gateway will be fully buffered in memory. Direct gRPC (CLI) streaming is unaffected.
- **UploadDir over gRPC:** `UploadDirRequest.root_path` is a path on the *server's* filesystem. Remote directory upload (client sends a local dir tree) is a future enhancement requiring a dedicated streaming RPC.
