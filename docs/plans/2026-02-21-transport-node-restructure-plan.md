# Transport & Node Restructure Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Restructure Node/ServerNode/ClientNode interfaces around current needs, unify transport, wire cmd/server to ServerNode+FileLedger and cmd/client to ClientNode+TUI.

**Architecture:** Bottom-up — interfaces first, then transport upgrades, then concrete ServerNode/ClientNode implementations, then cmd/ entry points, then cleanup of absorbed code. Each layer is testable before moving up.

**Tech Stack:** Go 1.25, protobuf, BurntSushi/toml, smplog (zerolog)

---

## Task 1: Restructure Node Interfaces

**Files:**
- Modify: `src/api/nodes/nodes.go`
- Modify: `src/api/ledgers/net_store.go` (add `StreamFile`, `StoreFromReader` to FileLedger)

**Step 1: Update `nodes.go` interfaces**

Replace the current `ServerNode`, `ClientNode`, and `MasterNode` definitions. Keep `Node` and `NodeState` as-is.

```go
// src/api/nodes/nodes.go
package nodes

import (
	"net/http"

	"github.com/danmuck/dps_files/src/api/ledgers"
	"github.com/danmuck/dps_files/src/api/transport"
)

type NodeState string

const (
	Follower  NodeState = "Follower"
	Candidate NodeState = "Candidate"
	Leader    NodeState = "Leader"
)

type Node interface {
	NodeInfo() transport.NodeInfo
	Address() string
	ID() []byte
	Start() error
	Shutdown() error
	Peers() []*transport.NodeInfo
}

// ServerNode manages storage and responds to RPCs.
// Future Raft methods will be added via a RaftNode extension interface.
type ServerNode interface {
	Node
	Storage() ledgers.FileLedger
	HandleRPC(rpc *transport.RPC) (*transport.RPC, error)
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// ClientNode performs file operations against ServerNodes.
// Future Kademlia methods will be added via a KademliaNode extension interface.
type ClientNode interface {
	Node
	Upload(filePath string, target *transport.NodeInfo) error
	Download(fileHash [32]byte, outputPath string, source *transport.NodeInfo) error
	Delete(fileHash [32]byte, target *transport.NodeInfo) error
	List(target *transport.NodeInfo) ([]ledgers.FileID, error)
}
```

**Step 2: Extend FileLedger with streaming methods**

The current `FileLedger` interface lacks streaming operations that ServerNode needs. Add them to `net_store.go`:

```go
// Add to the FileLedger interface in src/api/ledgers/net_store.go:

	// Streaming operations (needed by ServerNode for binary protocol)
	StoreFromReader(name string, r io.Reader, size uint64) (FileID, error)
	StreamFile(fileID FileID, w io.Writer) error
	StreamFileByName(name string, w io.Writer) error
	DeleteFile(fileID FileID) error
	ListKnownFilesMetadata() []FileMetaSummary

// Add above the interface:
type FileMetaSummary struct {
	Name string
	Hash FileID
	Size uint64
}
```

**Step 3: Run existing tests**

Run: `make test`
Expected: All 62 tests pass. The interface changes are additive — no implementations break yet because nothing implements the new interfaces.

**Step 4: Commit**

```bash
git add src/api/nodes/nodes.go src/api/ledgers/net_store.go
git commit -m "refactor: restructure Node/ServerNode/ClientNode interfaces for current needs"
```

---

## Task 2: FileLedger Adapter for KeyStore

**Files:**
- Create: `src/key_store/file_ledger.go`
- Test: `src/key_store/file_ledger_test.go`

**Step 1: Write the failing test**

```go
// src/key_store/file_ledger_test.go
package key_store

import (
	"os"
	"testing"

	"github.com/danmuck/dps_files/src/api/ledgers"
)

func TestKeyStoreImplementsFileLedger(t *testing.T) {
	dir, _ := os.MkdirTemp("", "fl-test-*")
	defer os.RemoveAll(dir)
	ks, err := InitKeyStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Compile-time interface check
	var _ ledgers.FileLedger = (*KeyStoreLedger)(nil)

	fl := NewFileLedger(ks)

	// Store a file
	data := []byte("hello file ledger")
	fid, err := fl.StoreFileLocal("test.txt", data)
	if err != nil {
		t.Fatalf("StoreFileLocal: %v", err)
	}
	if fid == (ledgers.FileID{}) {
		t.Fatal("expected non-zero FileID")
	}

	// List files
	files, err := fl.ListKnownFiles()
	if err != nil {
		t.Fatalf("ListKnownFiles: %v", err)
	}
	if len(files) != 1 || files[0] != fid {
		t.Fatalf("expected [%x], got %v", fid, files)
	}

	// Reassemble
	got, err := fl.ReassembleFileToBytes(fid)
	if err != nil {
		t.Fatalf("ReassembleFileToBytes: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("expected %q, got %q", data, got)
	}

	// Delete
	err = fl.DeleteFile(fid)
	if err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	files, _ = fl.ListKnownFiles()
	if len(files) != 0 {
		t.Fatalf("expected empty after delete, got %d", len(files))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./src/key_store/ -run TestKeyStoreImplementsFileLedger`
Expected: FAIL — `KeyStoreLedger` undefined.

**Step 3: Write the adapter**

```go
// src/key_store/file_ledger.go
package key_store

import (
	"fmt"
	"io"

	"github.com/danmuck/dps_files/src/api/ledgers"
)

// KeyStoreLedger adapts *KeyStore to the ledgers.FileLedger interface.
type KeyStoreLedger struct {
	ks *KeyStore
}

func NewFileLedger(ks *KeyStore) *KeyStoreLedger {
	return &KeyStoreLedger{ks: ks}
}

func (l *KeyStoreLedger) VerifyReferences() error {
	return l.ks.VerifyAll()
}

func (l *KeyStoreLedger) StoreFileLocal(name string, fileData []byte) (ledgers.FileID, error) {
	f, err := l.ks.StoreFileLocal(name, fileData)
	if err != nil {
		return ledgers.FileID{}, err
	}
	return ledgers.FileID(f.MetaData.Hash), nil
}

func (l *KeyStoreLedger) LoadAndStoreFileLocal(localFilePath string) (ledgers.FileID, error) {
	f, err := l.ks.LoadAndStoreFileLocal(localFilePath)
	if err != nil {
		return ledgers.FileID{}, err
	}
	return ledgers.FileID(f.MetaData.Hash), nil
}

func (l *KeyStoreLedger) LoadAndStoreFileRemote(localFilePath string, handler any) (ledgers.FileID, error) {
	rh, ok := handler.(RemoteHandler)
	if !ok {
		return ledgers.FileID{}, fmt.Errorf("handler must implement RemoteHandler")
	}
	f, err := l.ks.LoadAndStoreFileRemote(localFilePath, rh)
	if err != nil {
		return ledgers.FileID{}, err
	}
	return ledgers.FileID(f.MetaData.Hash), nil
}

func (l *KeyStoreLedger) ReassembleFileToBytes(fileID ledgers.FileID) ([]byte, error) {
	return l.ks.ReassembleFileToBytes([HashSize]byte(fileID))
}

func (l *KeyStoreLedger) ReassembleFileToPath(fileID ledgers.FileID, outputPath string) error {
	return l.ks.ReassembleFileToPath([HashSize]byte(fileID), outputPath)
}

func (l *KeyStoreLedger) ListKnownFileReferences(fileID ledgers.FileID) ([]ledgers.ChunkID, error) {
	f, err := l.ks.GetFileByHash([HashSize]byte(fileID))
	if err != nil {
		return nil, err
	}
	chunks := make([]ledgers.ChunkID, len(f.References))
	for i, ref := range f.References {
		chunks[i] = ledgers.ChunkID(ref.Key)
	}
	return chunks, nil
}

func (l *KeyStoreLedger) ListKnownFiles() ([]ledgers.FileID, error) {
	metas := l.ks.ListKnownFiles()
	ids := make([]ledgers.FileID, len(metas))
	for i, m := range metas {
		ids[i] = ledgers.FileID(m.Hash)
	}
	return ids, nil
}

func (l *KeyStoreLedger) Cleanup() error {
	return l.ks.Cleanup()
}

// Extended methods needed by ServerNode

func (l *KeyStoreLedger) StoreFromReader(name string, r io.Reader, size uint64) (ledgers.FileID, error) {
	f, err := l.ks.StoreFromReader(name, r, size)
	if err != nil {
		return ledgers.FileID{}, err
	}
	return ledgers.FileID(f.MetaData.Hash), nil
}

func (l *KeyStoreLedger) StreamFile(fileID ledgers.FileID, w io.Writer) error {
	return l.ks.StreamFile([HashSize]byte(fileID), w)
}

func (l *KeyStoreLedger) StreamFileByName(name string, w io.Writer) error {
	return l.ks.StreamFileByName(name, w)
}

func (l *KeyStoreLedger) DeleteFile(fileID ledgers.FileID) error {
	return l.ks.DeleteFile([HashSize]byte(fileID))
}

func (l *KeyStoreLedger) ListKnownFilesMetadata() []ledgers.FileMetaSummary {
	metas := l.ks.ListKnownFiles()
	summaries := make([]ledgers.FileMetaSummary, len(metas))
	for i, m := range metas {
		summaries[i] = ledgers.FileMetaSummary{
			Name: m.Name,
			Hash: ledgers.FileID(m.Hash),
			Size: m.Size,
		}
	}
	return summaries, nil
}
```

Note: `ListKnownFilesMetadata` returns two values but the signature returns one. Fix: either make it `([]ledgers.FileMetaSummary, error)` in the interface or drop the nil. Since this is a local read with no error path, keep single return:

```go
func (l *KeyStoreLedger) ListKnownFilesMetadata() []ledgers.FileMetaSummary {
	metas := l.ks.ListKnownFiles()
	summaries := make([]ledgers.FileMetaSummary, len(metas))
	for i, m := range metas {
		summaries[i] = ledgers.FileMetaSummary{
			Name: m.Name,
			Hash: ledgers.FileID(m.Hash),
			Size: m.Size,
		}
	}
	return summaries
}
```

**Step 4: Run tests**

Run: `go test -v ./src/key_store/ -run TestKeyStoreImplementsFileLedger`
Expected: PASS

Run: `make test`
Expected: All existing tests still pass.

**Step 5: Commit**

```bash
git add src/key_store/file_ledger.go src/key_store/file_ledger_test.go
git commit -m "feat: add KeyStoreLedger adapter implementing FileLedger interface"
```

---

## Task 3: Upgrade Transport — 4-byte Framing + Dial + Connection Pool

**Files:**
- Modify: `src/api/transport/encoding.go` (2-byte → 4-byte)
- Modify: `src/api/transport/tcp.go` (add Dial, connection pool)
- Modify: `src/api/transport/transport.go` (add Dial to interface)
- Modify: `src/api/transport/rpc.proto` (add UPLOAD/DOWNLOAD/LIST/DELETE commands)
- Test: `src/api/transport/tcp_handler_test.go` (update existing + add new tests)

**Step 1: Update rpc.proto**

Add file operation commands:

```proto
enum Command {
    PING = 0;
    STORE = 1;
    GET = 2;
    FIND_NODE = 3;
    FIND_VALUE = 4;
    ACK = 5;
    NODES = 6;
    VALUE = 7;
    REQUEST_VOTE = 8;
    APPEND_ENTRIES = 9;
    INSTALL_SNAPSHOT = 10;
    UPLOAD = 11;
    DOWNLOAD = 12;
    LIST = 13;
    DELETE = 14;
}
```

Run: `make build-protobuf`

**Step 2: Update encoding.go — 4-byte header**

Change `Encode` to write 4-byte big-endian length prefix. Change `Decode` to read 4-byte header.

```go
// In Encode:
header := make([]byte, 4)
binary.BigEndian.PutUint32(header, uint32(len(data)))
return append(header, data...), nil

// In Decode:
header := make([]byte, 4)
if _, err := io.ReadFull(r, header); err != nil {
    return nil, err
}
msgLen := binary.BigEndian.Uint32(header)
```

**Step 3: Update transport.go — add Dial to interface**

```go
type TransportHandler interface {
    ListenAndAccept() error
    Dial(addr string) (net.Conn, error)
    Send(conn net.Conn, rpc *RPC) error
    ProcessRPC() <-chan *RPC
    Close() error
}
```

**Step 4: Update tcp.go — add Dial and connection pool**

Add to `TCPHandler`:

```go
type TCPHandler struct {
    address  string
    listener net.Listener
    inbound  chan *RPC
    coder    Coder
    exit     chan any
    mu       sync.Mutex
    conns    map[string]net.Conn // addr → connection
}
```

Add methods:

```go
func (h *TCPHandler) Dial(addr string) (net.Conn, error) {
    h.mu.Lock()
    defer h.mu.Unlock()
    if conn, ok := h.conns[addr]; ok {
        return conn, nil
    }
    conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
    if err != nil {
        return nil, fmt.Errorf("dial %s: %w", addr, err)
    }
    h.conns[addr] = conn
    return conn, nil
}

func (h *TCPHandler) CloseConn(addr string) {
    h.mu.Lock()
    defer h.mu.Unlock()
    if conn, ok := h.conns[addr]; ok {
        conn.Close()
        delete(h.conns, addr)
    }
}
```

Update `NewTCPHandler` to initialize `conns: make(map[string]net.Conn)`.

Update `handleConnection` to use the 4-byte header (already done via `coder.Decode` — the coder handles framing).

**Step 5: Write new test for Dial + 4-byte framing**

```go
// Add to tcp_handler_test.go
func TestTCPHandler_DialAndSend(t *testing.T) {
    exit := make(chan any)
    defer close(exit)

    server := NewTCPHandler("localhost:0", exit)
    go server.ListenAndAccept()
    time.Sleep(100 * time.Millisecond)

    // Get actual listen address
    serverAddr := server.Addr()

    client := NewTCPHandler("localhost:0", exit)
    conn, err := client.Dial(serverAddr)
    if err != nil {
        t.Fatalf("Dial: %v", err)
    }

    rpc := &RPC{
        Meta:    &RPC_t{Command: Command_UPLOAD, Protocol: Protocol_Kademlia},
        Payload: []byte("test payload"),
    }
    if err := client.Send(conn, rpc); err != nil {
        t.Fatalf("Send: %v", err)
    }

    select {
    case got := <-server.ProcessRPC():
        if got.Meta.Command != Command_UPLOAD {
            t.Fatalf("expected UPLOAD, got %v", got.Meta.Command)
        }
        if string(got.Payload) != "test payload" {
            t.Fatalf("expected 'test payload', got %q", got.Payload)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("timeout waiting for RPC")
    }
}
```

Note: We need to expose the actual listen address. Add `Addr() string` method to `TCPHandler`:

```go
func (h *TCPHandler) Addr() string {
    if h.listener != nil {
        return h.listener.Addr().String()
    }
    return h.address
}
```

**Step 6: Run tests**

Run: `go test -v ./src/api/transport/ -run TestTCPHandler_DialAndSend`
Expected: PASS

Run: `make test`
Expected: All tests pass. Existing tcp_handler tests may need minor adjustment for 4-byte header if they do raw byte assertions.

**Step 7: Commit**

```bash
git add src/api/transport/
git commit -m "feat: upgrade transport to 4-byte framing, add Dial + connection pool, add file operation commands"
```

---

## Task 4: DefaultNode Cleanup

**Files:**
- Modify: `src/api/nodes/default.go` (remove Kademlia stubs, simplify constructor)
- Modify: `src/api/nodes/routing.go` (remove KademliaRouter, keep DefaultRouter)
- Modify: `src/api/nodes/nodes.go` (already done in Task 1, verify)
- Test: `src/api/nodes/routing_test.go` (update for removed KademliaRouter)

**Step 1: Clean up default.go**

Remove all Kademlia stub methods (`Send`, `Ping`, `Store`, `FindNode`, `FindValue`) from `DefaultNode`. Update constructor to use `DefaultRouter` instead of `KademliaRouter`:

```go
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

    exit := make(chan any)
    return &DefaultNode{
        pubKey:     id,
        address:    address,
        Router:     rt,
        TCPHandler: transport.NewTCPHandler(address, exit),
        exit:       exit,
    }, nil
}
```

**Step 2: Remove KademliaRouter from routing.go**

Delete the `KademliaRouter` struct, `NewKademliaRouter`, and all its methods. Keep `KademliaRouting` interface definition (it's the contract for future reimplementation). Keep `DefaultRouter` and `RoutingTable` interface.

**Step 3: Update routing_test.go**

Remove or update tests that reference `KademliaRouter`. The "router type verification" test likely checks for KademliaRouter — update it to check for DefaultRouter. Update `NewDefaultNode` calls to remove `k, a` params.

**Step 4: Run tests**

Run: `make test`
Expected: All tests pass. Any test calling `NewDefaultNode(id, addr, k, a)` needs to be updated to `NewDefaultNode(id, addr)`.

**Step 5: Commit**

```bash
git add src/api/nodes/
git commit -m "refactor: simplify DefaultNode, remove KademliaRouter stub, keep DefaultRouter"
```

---

## Task 5: Implement DefaultServerNode

**Files:**
- Create: `src/api/nodes/server_node.go`
- Test: `src/api/nodes/server_node_test.go`

**Step 1: Write failing test**

```go
// src/api/nodes/server_node_test.go
package nodes

import (
    "os"
    "testing"

    "github.com/danmuck/dps_files/src/api/ledgers"
    "github.com/danmuck/dps_files/src/api/transport"
)

func TestServerNode_StartAndShutdown(t *testing.T) {
    dir, _ := os.MkdirTemp("", "sn-test-*")
    defer os.RemoveAll(dir)

    sn, err := NewServerNode([]byte("test-server-node-id!"), "localhost:0", dir)
    if err != nil {
        t.Fatal(err)
    }

    // Verify it implements ServerNode interface
    var _ ServerNode = sn

    if err := sn.Start(); err != nil {
        t.Fatal(err)
    }
    defer sn.Shutdown()

    // Storage should be accessible
    storage := sn.Storage()
    if storage == nil {
        t.Fatal("expected non-nil storage")
    }
}

func TestServerNode_HandleRPC_Ping(t *testing.T) {
    dir, _ := os.MkdirTemp("", "sn-test-*")
    defer os.RemoveAll(dir)

    sn, _ := NewServerNode([]byte("test-server-node-id!"), "localhost:0", dir)
    sn.Start()
    defer sn.Shutdown()

    rpc := &transport.RPC{
        Meta: &transport.RPC_t{Command: transport.Command_PING},
        Sender: &transport.NodeInfo{Address: "localhost:9999"},
    }
    resp, err := sn.HandleRPC(rpc)
    if err != nil {
        t.Fatalf("HandleRPC PING: %v", err)
    }
    if resp.Meta.Command != transport.Command_ACK {
        t.Fatalf("expected ACK, got %v", resp.Meta.Command)
    }
}

func TestServerNode_HandleRPC_List(t *testing.T) {
    dir, _ := os.MkdirTemp("", "sn-test-*")
    defer os.RemoveAll(dir)

    sn, _ := NewServerNode([]byte("test-server-node-id!"), "localhost:0", dir)
    sn.Start()
    defer sn.Shutdown()

    // Store a file first
    sn.Storage().StoreFileLocal("test.txt", []byte("hello"))

    rpc := &transport.RPC{
        Meta: &transport.RPC_t{Command: transport.Command_LIST},
    }
    resp, err := sn.HandleRPC(rpc)
    if err != nil {
        t.Fatalf("HandleRPC LIST: %v", err)
    }
    // Payload should contain JSON list
    if len(resp.Payload) == 0 {
        t.Fatal("expected non-empty payload for LIST")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./src/api/nodes/ -run TestServerNode`
Expected: FAIL — `NewServerNode` undefined.

**Step 3: Implement DefaultServerNode**

```go
// src/api/nodes/server_node.go
package nodes

import (
    "encoding/json"
    "fmt"
    "net/http"

    "github.com/danmuck/dps_files/src/api/ledgers"
    "github.com/danmuck/dps_files/src/api/transport"
    "github.com/danmuck/dps_files/src/key_store"
    logs "github.com/danmuck/smplog"
)

type DefaultServerNode struct {
    *DefaultNode
    storage    ledgers.FileLedger
    httpAddr   string
    httpServer *http.Server
}

type ServerOption func(*DefaultServerNode)

func WithHTTP(addr string) ServerOption {
    return func(s *DefaultServerNode) { s.httpAddr = addr }
}

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
    }
    for _, opt := range opts {
        opt(sn)
    }
    return sn, nil
}

func (s *DefaultServerNode) Storage() ledgers.FileLedger {
    return s.storage
}

func (s *DefaultServerNode) Start() error {
    if err := s.DefaultNode.Start(); err != nil {
        return err
    }

    // Wire RPC dispatch
    go s.dispatchRPCs()

    // Optional HTTP
    if s.httpAddr != "" {
        mux := http.NewServeMux()
        // HTTP handlers will be wired in Task 7
        s.httpServer = &http.Server{Addr: s.httpAddr, Handler: mux}
        go func() {
            if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
                logs.Warnf("HTTP server error: %v", err)
            }
        }()
    }
    return nil
}

func (s *DefaultServerNode) dispatchRPCs() {
    ch := s.TCPHandler.ProcessRPC()
    for {
        select {
        case <-s.exit:
            return
        case rpc := <-ch:
            if rpc == nil {
                continue
            }
            resp, err := s.HandleRPC(rpc)
            if err != nil {
                logs.Warnf("HandleRPC error: %v", err)
                continue
            }
            if resp != nil && rpc.Sender != nil {
                conn, dialErr := s.TCPHandler.Dial(rpc.Sender.Address)
                if dialErr != nil {
                    logs.Warnf("dial back to %s: %v", rpc.Sender.Address, dialErr)
                    continue
                }
                if sendErr := s.TCPHandler.Send(conn, resp); sendErr != nil {
                    logs.Warnf("send response to %s: %v", rpc.Sender.Address, sendErr)
                }
            }
        }
    }
}

func (s *DefaultServerNode) HandleRPC(rpc *transport.RPC) (*transport.RPC, error) {
    switch rpc.Meta.Command {
    case transport.Command_PING:
        return &transport.RPC{
            Meta:   &transport.RPC_t{Command: transport.Command_ACK},
            Sender: s.nodeInfo(),
        }, nil

    case transport.Command_LIST:
        summaries := s.storage.ListKnownFilesMetadata()
        data, err := json.Marshal(summaries)
        if err != nil {
            return nil, fmt.Errorf("marshal file list: %w", err)
        }
        return &transport.RPC{
            Meta:    &transport.RPC_t{Command: transport.Command_ACK},
            Sender:  s.nodeInfo(),
            Payload: data,
        }, nil

    case transport.Command_UPLOAD:
        // Key = filename bytes, Value = file data
        name := string(rpc.Key)
        fid, err := s.storage.StoreFileLocal(name, rpc.Value)
        if err != nil {
            return nil, fmt.Errorf("store file: %w", err)
        }
        return &transport.RPC{
            Meta:   &transport.RPC_t{Command: transport.Command_ACK},
            Sender: s.nodeInfo(),
            Key:    fid[:],
        }, nil

    case transport.Command_DOWNLOAD:
        // Key = file hash (32 bytes)
        if len(rpc.Key) != 32 {
            return nil, fmt.Errorf("DOWNLOAD requires 32-byte file hash key")
        }
        var fid ledgers.FileID
        copy(fid[:], rpc.Key)
        data, err := s.storage.ReassembleFileToBytes(fid)
        if err != nil {
            return nil, fmt.Errorf("reassemble file: %w", err)
        }
        return &transport.RPC{
            Meta:   &transport.RPC_t{Command: transport.Command_ACK},
            Sender: s.nodeInfo(),
            Value:  data,
        }, nil

    case transport.Command_DELETE:
        if len(rpc.Key) != 32 {
            return nil, fmt.Errorf("DELETE requires 32-byte file hash key")
        }
        var fid ledgers.FileID
        copy(fid[:], rpc.Key)
        if err := s.storage.DeleteFile(fid); err != nil {
            return nil, fmt.Errorf("delete file: %w", err)
        }
        return &transport.RPC{
            Meta:   &transport.RPC_t{Command: transport.Command_ACK},
            Sender: s.nodeInfo(),
        }, nil

    default:
        return nil, fmt.Errorf("unhandled command: %v", rpc.Meta.Command)
    }
}

func (s *DefaultServerNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // Delegated to HTTP mux — this satisfies the interface.
    // Full HTTP handler wiring happens in Task 7.
    if s.httpServer != nil {
        s.httpServer.Handler.ServeHTTP(w, r)
    }
}

func (s *DefaultServerNode) Shutdown() error {
    if s.httpServer != nil {
        s.httpServer.Close()
    }
    return s.DefaultNode.Shutdown()
}

func (s *DefaultServerNode) nodeInfo() *transport.NodeInfo {
    info := s.NodeInfo()
    return &info
}
```

**Step 4: Run tests**

Run: `go test -v ./src/api/nodes/ -run TestServerNode`
Expected: PASS

Run: `make test`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add src/api/nodes/server_node.go src/api/nodes/server_node_test.go
git commit -m "feat: implement DefaultServerNode with HandleRPC dispatch"
```

---

## Task 6: Implement DefaultClientNode

**Files:**
- Create: `src/api/nodes/client_node.go`
- Test: `src/api/nodes/client_node_test.go`

**Step 1: Write failing test**

```go
// src/api/nodes/client_node_test.go
package nodes

import (
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/danmuck/dps_files/src/api/ledgers"
)

func TestClientNode_UploadDownloadRoundTrip(t *testing.T) {
    // Set up server
    sDir, _ := os.MkdirTemp("", "cn-server-*")
    defer os.RemoveAll(sDir)
    sn, _ := NewServerNode([]byte("server-node-id-20b!!"), "localhost:0", sDir)
    sn.Start()
    defer sn.Shutdown()
    time.Sleep(100 * time.Millisecond)

    serverAddr := sn.TCPHandler.Addr()
    serverInfo := sn.NodeInfo()

    // Set up client (remote-only mode)
    cn, err := NewClientNode([]byte("client-node-id-20b!!"), WithRemotes(serverAddr))
    if err != nil {
        t.Fatal(err)
    }
    cn.Start()
    defer cn.Shutdown()

    // Create a test file
    tmpFile := filepath.Join(t.TempDir(), "upload.txt")
    os.WriteFile(tmpFile, []byte("round trip test data"), 0644)

    // Upload
    if err := cn.Upload(tmpFile, &serverInfo); err != nil {
        t.Fatalf("Upload: %v", err)
    }

    // List
    files, err := cn.List(&serverInfo)
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(files) != 1 {
        t.Fatalf("expected 1 file, got %d", len(files))
    }

    // Download
    outPath := filepath.Join(t.TempDir(), "downloaded.txt")
    if err := cn.Download(files[0], outPath, &serverInfo); err != nil {
        t.Fatalf("Download: %v", err)
    }

    got, _ := os.ReadFile(outPath)
    if string(got) != "round trip test data" {
        t.Fatalf("expected 'round trip test data', got %q", got)
    }
}

func TestClientNode_LocalMode(t *testing.T) {
    dir, _ := os.MkdirTemp("", "cn-local-*")
    defer os.RemoveAll(dir)

    cn, err := NewClientNode([]byte("local-client-node-!!"), WithLocalStorage(dir))
    if err != nil {
        t.Fatal(err)
    }
    var _ ClientNode = cn

    cn.Start()
    defer cn.Shutdown()

    // In local mode, the embedded server should be accessible
    if cn.LocalServer() == nil {
        t.Fatal("expected non-nil local server in local mode")
    }
}
```

**Step 2: Run to verify failure**

Run: `go test -v ./src/api/nodes/ -run TestClientNode`
Expected: FAIL — `NewClientNode` undefined.

**Step 3: Implement DefaultClientNode**

```go
// src/api/nodes/client_node.go
package nodes

import (
    "encoding/json"
    "fmt"
    "os"
    "time"

    "github.com/danmuck/dps_files/src/api/ledgers"
    "github.com/danmuck/dps_files/src/api/transport"
)

type DefaultClientNode struct {
    *DefaultNode
    localServer *DefaultServerNode
    remotes     []*transport.NodeInfo
    storageDir  string // non-empty triggers local server
}

type ClientOption func(*DefaultClientNode)

func WithLocalStorage(dir string) ClientOption {
    return func(c *DefaultClientNode) { c.storageDir = dir }
}

func WithRemotes(addrs ...string) ClientOption {
    return func(c *DefaultClientNode) {
        for _, addr := range addrs {
            c.remotes = append(c.remotes, &transport.NodeInfo{Address: addr})
        }
    }
}

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
    return nil
}

func (c *DefaultClientNode) Shutdown() error {
    if c.localServer != nil {
        c.localServer.Shutdown()
    }
    return c.DefaultNode.Shutdown()
}

func (c *DefaultClientNode) LocalServer() *DefaultServerNode {
    return c.localServer
}

func (c *DefaultClientNode) Upload(filePath string, target *transport.NodeInfo) error {
    data, err := os.ReadFile(filePath)
    if err != nil {
        return fmt.Errorf("read file: %w", err)
    }
    name := filepath.Base(filePath)

    rpc := &transport.RPC{
        Meta:   &transport.RPC_t{Command: transport.Command_UPLOAD},
        Sender: c.nodeInfo(),
        Key:    []byte(name),
        Value:  data,
        RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
    }
    return c.sendAndWaitACK(target, rpc)
}

func (c *DefaultClientNode) Download(fileHash [32]byte, outputPath string, source *transport.NodeInfo) error {
    rpc := &transport.RPC{
        Meta:      &transport.RPC_t{Command: transport.Command_DOWNLOAD},
        Sender:    c.nodeInfo(),
        Key:       fileHash[:],
        RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
    }
    resp, err := c.sendAndReceive(source, rpc)
    if err != nil {
        return err
    }
    return os.WriteFile(outputPath, resp.Value, 0644)
}

func (c *DefaultClientNode) Delete(fileHash [32]byte, target *transport.NodeInfo) error {
    rpc := &transport.RPC{
        Meta:      &transport.RPC_t{Command: transport.Command_DELETE},
        Sender:    c.nodeInfo(),
        Key:       fileHash[:],
        RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
    }
    return c.sendAndWaitACK(target, rpc)
}

func (c *DefaultClientNode) List(target *transport.NodeInfo) ([]ledgers.FileID, error) {
    rpc := &transport.RPC{
        Meta:      &transport.RPC_t{Command: transport.Command_LIST},
        Sender:    c.nodeInfo(),
        RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
    }
    resp, err := c.sendAndReceive(target, rpc)
    if err != nil {
        return nil, err
    }
    var summaries []ledgers.FileMetaSummary
    if err := json.Unmarshal(resp.Payload, &summaries); err != nil {
        return nil, fmt.Errorf("unmarshal file list: %w", err)
    }
    ids := make([]ledgers.FileID, len(summaries))
    for i, s := range summaries {
        ids[i] = s.Hash
    }
    return ids, nil
}

func (c *DefaultClientNode) sendAndReceive(target *transport.NodeInfo, rpc *transport.RPC) (*transport.RPC, error) {
    conn, err := c.TCPHandler.Dial(target.Address)
    if err != nil {
        return nil, fmt.Errorf("dial %s: %w", target.Address, err)
    }
    if err := c.TCPHandler.Send(conn, rpc); err != nil {
        return nil, fmt.Errorf("send: %w", err)
    }
    // Read response from the connection
    resp, err := c.TCPHandler.ReadRPC(conn)
    if err != nil {
        return nil, fmt.Errorf("read response: %w", err)
    }
    return resp, nil
}

func (c *DefaultClientNode) sendAndWaitACK(target *transport.NodeInfo, rpc *transport.RPC) error {
    resp, err := c.sendAndReceive(target, rpc)
    if err != nil {
        return err
    }
    if resp.Meta.Command != transport.Command_ACK {
        return fmt.Errorf("expected ACK, got %v", resp.Meta.Command)
    }
    return nil
}

func (c *DefaultClientNode) nodeInfo() *transport.NodeInfo {
    info := c.NodeInfo()
    return &info
}
```

Note: This requires a `ReadRPC(conn)` method on TCPHandler. Add to `tcp.go`:

```go
func (h *TCPHandler) ReadRPC(conn net.Conn) (*RPC, error) {
    return h.coder.Decode(conn)
}
```

Also need `"path/filepath"` import in client_node.go.

**Step 4: Run tests**

Run: `go test -v ./src/api/nodes/ -run TestClientNode`
Expected: PASS

Run: `make test`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add src/api/nodes/client_node.go src/api/nodes/client_node_test.go src/api/transport/tcp.go
git commit -m "feat: implement DefaultClientNode with Upload/Download/List/Delete"
```

---

## Task 7: Wire HTTP Handlers into ServerNode

**Files:**
- Create: `src/api/nodes/http_handlers.go` (extract from cmd/httpserver)
- Test: `src/api/nodes/http_handlers_test.go`

**Step 1: Write failing test**

```go
// src/api/nodes/http_handlers_test.go
package nodes

import (
    "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "testing"
)

func TestServerNode_HTTP_UploadAndList(t *testing.T) {
    dir, _ := os.MkdirTemp("", "http-test-*")
    defer os.RemoveAll(dir)

    sn, _ := NewServerNode([]byte("http-server-node-id!"), "localhost:0", dir)

    // Test upload via HTTP
    body := strings.NewReader("http upload test data")
    req := httptest.NewRequest("PUT", "/files/test.txt", body)
    req.Header.Set("Content-Length", "20")
    w := httptest.NewRecorder()
    sn.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
        t.Fatalf("upload: expected 200, got %d: %s", w.Code, w.Body.String())
    }

    // Test list
    req = httptest.NewRequest("GET", "/files", nil)
    w = httptest.NewRecorder()
    sn.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
        t.Fatalf("list: expected 200, got %d", w.Code)
    }
    if !strings.Contains(w.Body.String(), "test.txt") {
        t.Fatalf("expected test.txt in list, got %s", w.Body.String())
    }
}
```

**Step 2: Run to verify failure**

Expected: FAIL — `ServeHTTP` doesn't handle routes yet (just delegates to nil or empty mux).

**Step 3: Implement HTTP handlers**

Extract the handler logic from `cmd/httpserver/handlers.go` into `src/api/nodes/http_handlers.go`, adapting to use `FileLedger` instead of `*KeyStore` directly. Wire the mux in `NewServerNode` so `ServeHTTP` works even without calling `Start()`.

The HTTP handlers follow the same pattern as `cmd/httpserver/handlers.go` but operate on `ledgers.FileLedger` instead of `*key_store.KeyStore`.

**Step 4: Run tests**

Run: `go test -v ./src/api/nodes/ -run TestServerNode_HTTP`
Expected: PASS

Run: `make test`
Expected: All pass.

**Step 5: Commit**

```bash
git add src/api/nodes/http_handlers.go src/api/nodes/http_handlers_test.go
git commit -m "feat: wire HTTP file server handlers into ServerNode"
```

---

## Task 8: Rewrite cmd/server

**Files:**
- Modify: `cmd/server/main.go`
- Delete: `cmd/fileserver/` (absorbed)
- Delete: `cmd/httpserver/` (absorbed)

**Step 1: Rewrite cmd/server/main.go**

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

    addr := flag.String("addr", ":9000", "TCP listen address")
    httpAddr := flag.String("http", "", "HTTP listen address (optional, e.g. :8080)")
    storageDir := flag.String("storage", "local/storage", "storage directory")
    flag.Parse()

    id := make([]byte, 20)
    rand.Read(id)

    opts := []nodes.ServerOption{}
    if *httpAddr != "" {
        opts = append(opts, nodes.WithHTTP(*httpAddr))
    }

    sn, err := nodes.NewServerNode(id, *addr, *storageDir, opts...)
    if err != nil {
        logs.Fatalf(err, "failed to create server node")
    }

    if err := sn.Start(); err != nil {
        logs.Fatalf(err, "failed to start server node")
    }

    logs.Infof("Server node listening on %s (storage: %s)", *addr, *storageDir)
    if *httpAddr != "" {
        logs.Infof("HTTP server on %s", *httpAddr)
    }

    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    <-sig

    logs.Infof("Shutting down...")
    sn.Shutdown()
}
```

**Step 2: Delete cmd/fileserver/ and cmd/httpserver/**

These are now fully absorbed into ServerNode. Remove the directories.

**Step 3: Update Makefile**

Remove `fileserver` and `httpserver` targets. Update `server` target:

```makefile
server:
    go run ./cmd/server $(ARGS)
```

**Step 4: Build and test**

Run: `make build`
Expected: Builds successfully (cmd/fileserver and cmd/httpserver dirs gone, build loop skips them).

Run: `make test`
Expected: All pass.

**Step 5: Commit**

```bash
git add cmd/server/main.go Makefile
git rm -r cmd/fileserver/ cmd/httpserver/
git commit -m "feat: rewrite cmd/server using ServerNode, absorb fileserver + httpserver"
```

---

## Task 9: Rewrite cmd/client (absorb cmd/storage TUI)

**Files:**
- Modify: `cmd/client/main.go`
- Move: `cmd/storage/*.go` → adapt TUI to use ClientNode
- Delete: `cmd/storage/` (after absorption)

**Step 1: Rewrite cmd/client/main.go**

```go
package main

import (
    "crypto/rand"
    "flag"
    "os"
    "os/signal"
    "strings"
    "syscall"

    "github.com/danmuck/dps_files/cmd/internal/logcfg"
    "github.com/danmuck/dps_files/src/api/nodes"
    logs "github.com/danmuck/smplog"
)

func main() {
    logs.Configure(logcfg.Load())

    mode := flag.String("mode", "local", "local or remote")
    storageDir := flag.String("storage", "local/storage", "local storage directory (local mode)")
    remotes := flag.String("remotes", "", "comma-separated remote server addresses")
    flag.Parse()

    id := make([]byte, 20)
    rand.Read(id)

    var opts []nodes.ClientOption
    if *mode == "local" {
        opts = append(opts, nodes.WithLocalStorage(*storageDir))
    }
    if *remotes != "" {
        addrs := strings.Split(*remotes, ",")
        opts = append(opts, nodes.WithRemotes(addrs...))
    }

    cn, err := nodes.NewClientNode(id, opts...)
    if err != nil {
        logs.Fatalf(err, "failed to create client node")
    }

    if err := cn.Start(); err != nil {
        logs.Fatalf(err, "failed to start client node")
    }
    defer cn.Shutdown()

    // Launch TUI (adapted from cmd/storage menu system)
    // The TUI files are moved into cmd/client/ and adapted
    // to use cn (ClientNode) instead of *KeyStore directly.
    runTUI(cn)
}
```

**Step 2: Adapt TUI files**

Move `cmd/storage/menu.go`, `cmd/storage/store.go`, `cmd/storage/view.go`, `cmd/storage/delete.go`, `cmd/storage/verify.go`, `cmd/storage/expire.go`, `cmd/storage/stats.go`, `cmd/storage/stream.go`, `cmd/storage/progress.go`, `cmd/storage/filesystem.go`, `cmd/storage/runtime_config.go`, `cmd/storage/remote.go` into `cmd/client/`.

Adapt each file:
- Change `package main` (stays same since it's cmd/client)
- Replace direct `*key_store.KeyStore` usage with `ClientNode` or `FileLedger` interface calls
- For local operations, use `cn.LocalServer().Storage()`
- For remote operations, use `cn.Upload/Download/List/Delete`
- The menu system stays largely the same — the mode toggle (local vs remote) maps to which path is taken

This is the largest task and should be done incrementally — move files first, then adapt one at a time.

**Step 3: Delete cmd/storage/**

After all TUI files are moved and adapted, remove cmd/storage/.

**Step 4: Update Makefile**

Remove `storage` target. Update `client`:

```makefile
client:
    go run ./cmd/client $(ARGS)
```

**Step 5: Build and test**

Run: `make build`
Expected: Builds.

Run: `make test`
Expected: All pass.

**Step 6: Manual test**

Run: `make client -- --mode local`
Expected: Interactive TUI works as before.

**Step 7: Commit**

```bash
git add cmd/client/
git rm -r cmd/storage/
git add Makefile
git commit -m "feat: rewrite cmd/client with TUI, absorb cmd/storage"
```

---

## Task 10: Update Documentation and Cleanup

**Files:**
- Modify: `CLAUDE.md`
- Modify: `AGENTS.md`
- Modify: `docs/progress/buildplan.md`
- Modify: `README.md` (if it exists)

**Step 1: Update CLAUDE.md**

- Update Directory Structure section (remove cmd/fileserver, cmd/httpserver, cmd/storage; update cmd/server and cmd/client descriptions)
- Update Key Packages section (add ServerNode, ClientNode, FileLedger adapter descriptions)
- Update Build & Run Commands (remove fileserver/httpserver/storage targets, update server/client)
- Update Current State (ServerNode and ClientNode are now functional)
- Update Common Tasks
- Update Architecture Patterns

**Step 2: Update AGENTS.md**

Update surfaces and known gaps to reflect the restructure.

**Step 3: Update buildplan.md**

Mark relevant items as complete. Add new items for any follow-up work discovered during implementation.

**Step 4: Run full test suite one final time**

Run: `make test`
Expected: All tests pass (existing 62 + new tests from Tasks 2-7).

**Step 5: Commit**

```bash
git add CLAUDE.md AGENTS.md docs/progress/buildplan.md
git commit -m "docs: update project documentation for transport/node restructure"
```

---

## Execution Order Summary

| Task | Description | Dependencies |
|------|-------------|--------------|
| 1 | Restructure interfaces | None |
| 2 | FileLedger adapter | Task 1 |
| 3 | Upgrade transport | Task 1 |
| 4 | DefaultNode cleanup | Task 1 |
| 5 | DefaultServerNode | Tasks 2, 3, 4 |
| 6 | DefaultClientNode | Tasks 3, 5 |
| 7 | HTTP handlers in ServerNode | Task 5 |
| 8 | Rewrite cmd/server | Tasks 5, 7 |
| 9 | Rewrite cmd/client (TUI) | Tasks 6, 8 |
| 10 | Documentation | Task 9 |

Tasks 2, 3, 4 can be parallelized after Task 1.
Tasks 5 and 7 can be partially parallelized.
Tasks 8 and 9 are sequential.
