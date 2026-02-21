# Transport & Node Restructure Design

**Date:** 2026-02-21
**Branch:** dev (off main)
**Scope:** Node interface restructuring + transport unification. Directory semantics deferred to follow-up.

## Decisions

| Decision | Choice |
|---|---|
| Scope | Transport + Node restructuring only |
| Storage API | ServerNode exposes FileLedger; KeyStore implements it |
| Transport | Protocol-per-mode: unified 4-byte binary protobuf + optional HTTP |
| Client mode | Both local (embedded ServerNode) and remote-only modes |
| Execution | Bottom-up: interfaces → transport → ServerNode → ClientNode → cmd/ → cleanup |

## 1. Restructured Interfaces

### Node (base)

```go
type Node interface {
    ID() []byte
    Address() string
    Start() error
    Shutdown() error
    NodeInfo() transport.NodeInfo
    Peers() []*transport.NodeInfo
}
```

Unchanged from current. `MasterNode` composite removed (premature without Raft).

### ServerNode

```go
type ServerNode interface {
    Node
    Storage() FileLedger
    HandleRPC(rpc *transport.RPC) (*transport.RPC, error)
    ServeHTTP(w http.ResponseWriter, r *http.Request)  // optional HTTP mode
}
```

Replaces Raft-specific methods (`ApplyCommand`, `CreateSnapshot`, `GetState`, `AddPeer`, `RemovePeer`) with what's needed now. Raft methods return as a `RaftNode` extension interface later.

### ClientNode

```go
type ClientNode interface {
    Node
    Upload(filePath string, target *transport.NodeInfo) error
    Download(fileHash [32]byte, outputPath string, source *transport.NodeInfo) error
    Delete(fileHash [32]byte, target *transport.NodeInfo) error
    List(target *transport.NodeInfo) ([]ledgers.FileID, error)
}
```

Replaces Kademlia-specific methods (`FindNode`, `FindValue`, `Store`) with file-operation verbs. Kademlia gets added as a `KademliaNode` extension later.

### FileLedger adapter

`KeyStore` gets thin adapter methods to satisfy `FileLedger` interface, converting between concrete types and `FileID`/`ChunkID` typed aliases.

## 2. Unified Transport

### Binary protocol

- Upgrade `TCPHandler` from 2-byte to 4-byte length-prefixed frames (matches fileserver, raises limit to 4GB)
- Add `Dial(addr string) (net.Conn, error)` for outbound connections
- Add connection pool (`map[string]net.Conn`)
- Add request-response correlation via `RequestID` field (already in proto)

### Proto additions

```proto
enum Command {
    // existing Kademlia + Raft commands stay for future use
    UPLOAD = 11;
    DOWNLOAD = 12;
    LIST = 13;
    DELETE = 14;
}
```

### HTTP mode

Extract httpserver handler logic into reusable `HTTPTransport` that ServerNode can optionally mount. ClientNode uses HTTP for observability/browser access.

### Fileserver absorption

Fileserver's binary protocol logic (upload/download/list/delete) moves into ServerNode's `HandleRPC`. Streaming patterns (io.MultiReader, LimitReader) become the standard for file transfer RPCs.

## 3. Concrete Implementations

### DefaultServerNode

```go
type DefaultServerNode struct {
    *DefaultNode
    storage    ledgers.FileLedger
    tcp        *transport.TCPHandler
    httpAddr   string
    httpServer *http.Server
}
```

- `NewServerNode(id, addr, storageDir string, opts ...ServerOption)`
- `Start()` — TCP listener + optional HTTP server, dispatches RPCs to `HandleRPC`
- `HandleRPC()` — switch on Command: UPLOAD, DOWNLOAD, LIST, DELETE, PING (future: STORE, FIND_NODE, REQUEST_VOTE, etc.)
- HTTP mode reuses httpserver handler logic, delegates to `storage` FileLedger

### DefaultClientNode

```go
type DefaultClientNode struct {
    *DefaultNode
    localServer *DefaultServerNode  // nil in remote-only mode
    remotes     []*transport.NodeInfo
}
```

- `NewClientNode(id string, opts ...ClientOption)` — options for local storage dir (triggers embedded ServerNode), remote addresses
- `Upload/Download/List/Delete` — RPC to target ServerNode via binary protocol
- Local mode: embedded ServerNode handles storage, acts as both client and server

### cmd/server

```go
// flags: --addr, --storage, --http (optional)
node := nodes.NewServerNode(id, addr, storageDir, opts...)
node.Start()
// signal handling for graceful shutdown
```

Replaces old `cmd/server` demo + `cmd/fileserver` + `cmd/httpserver`.

### cmd/client

```go
// flags: --local/--remote, --storage (if local), --remotes
node := nodes.NewClientNode(id, opts...)
node.Start()
// TUI (reused from cmd/storage interactive menu)
```

Replaces old `cmd/client` demo + absorbs `cmd/storage` TUI.

## 4. What Changes

### Removed

| Item | Reason |
|---|---|
| `cmd/fileserver/` | Absorbed into ServerNode binary protocol handler |
| `cmd/httpserver/` | Absorbed into ServerNode HTTP mode |
| `cmd/storage/` | TUI absorbed into cmd/client |
| `MasterNode` interface | Premature without Raft |
| `KademliaRouter` stub | Remove, re-add when implementing Kademlia |
| Old `ServerNode` Raft methods | Replaced with current-needs interface |
| Old `ClientNode` Kademlia methods | Replaced with current-needs interface |

### Unchanged

| Item | Reason |
|---|---|
| `src/key_store/` | All existing code stays; gets FileLedger adapter |
| `src/impl/` | Block/crypto code unchanged |
| All 62 existing tests | Must pass (no feature regression) |
| `DefaultNode` base | Stays as embedding target |
| `DefaultRouter` | Stays for flat peer list routing |

## 5. Testing Strategy

- All 62 existing tests must pass throughout
- New tests per layer:
  - FileLedger adapter: verify KeyStore satisfies interface
  - TCPHandler: 4-byte framing, Dial, connection pool, request-response correlation
  - ServerNode: HandleRPC dispatch for each command
  - ClientNode: Upload/Download/List/Delete round-trip against ServerNode
  - cmd/ integration: end-to-end upload/download via both binary and HTTP

## 6. Future Extensions (not in scope)

- `RaftNode` interface extending `ServerNode` with consensus methods
- `KademliaNode` interface extending `ClientNode` with DHT methods
- `KademliaRouter` reimplementation with XOR distance and k-buckets
- Directory semantics (separate design doc)
- UDP transport
- TLS
