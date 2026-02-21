package nodes

import (
	"github.com/danmuck/dps_files/src/api/ledgers"
	"github.com/danmuck/dps_files/src/api/pb"
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

// ClientNode is a user-facing interface that performs file operations against ServerNodes.
// All operations go through a gRPC connection — even in local mode.
type ClientNode interface {
	Node
	// Stub returns the gRPC client stub for the active server connection.
	Stub() (pb.DPSFilesClient, error)
	// LocalServer returns the embedded ServerNode, or nil if not in local mode.
	LocalServer() *DefaultServerNode
}
