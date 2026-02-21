package nodes

import (
	"github.com/danmuck/dps_files/src/api/ledgers"
)

type NodeState string

const (
	Follower  NodeState = "Follower"
	Candidate NodeState = "Candidate"
	Leader    NodeState = "Leader"
)

// NodeInfo identifies a node on the network.
type NodeInfo struct {
	ID      []byte
	Address string
}

type Node interface {
	NodeInfo() NodeInfo
	Address() string
	ID() []byte
	Start() error
	Shutdown() error
	Peers() []*NodeInfo
}

// ServerNode manages storage and serves gRPC RPCs.
type ServerNode interface {
	Node
	Storage() ledgers.FileLedger
}

// ClientNode performs file operations against ServerNodes.
type ClientNode interface {
	Node
	Upload(filePath string, target *NodeInfo) error
	Download(fileHash [32]byte, outputPath string, source *NodeInfo) error
	Delete(fileHash [32]byte, target *NodeInfo) error
	List(target *NodeInfo) ([]ledgers.FileID, error)
}
