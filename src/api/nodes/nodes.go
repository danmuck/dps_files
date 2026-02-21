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
type ServerNode interface {
	Node
	Storage() ledgers.FileLedger
	HandleRPC(rpc *transport.RPC) (*transport.RPC, error)
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// ClientNode performs file operations against ServerNodes.
type ClientNode interface {
	Node
	Upload(filePath string, target *transport.NodeInfo) error
	Download(fileHash [32]byte, outputPath string, source *transport.NodeInfo) error
	Delete(fileHash [32]byte, target *transport.NodeInfo) error
	List(target *transport.NodeInfo) ([]ledgers.FileID, error)
}
