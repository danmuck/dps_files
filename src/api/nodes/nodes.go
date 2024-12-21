package nodes

import (
	"github.com/danmuck/dps_files/src/api/transport"
)

type NodeState string

// Raft node stats
const (
	Follower  NodeState = "Follower"
	Candidate NodeState = "Candidate"
	Leader    NodeState = "Leader"
)

// Generic Node interface
// This interface is the base Node for a peer in the network
// ServerNodes extend this to participate in the Raft Backup Cluster
// ClientNodes extend this it participate in the Kademlia Storage Network
type Node interface {
	NodeInfo() transport.NodeInfo // returns the NodeInfo for this Node
	Address() string              // listener address for node
	ID() []byte                   // node id, relevent for kademlia
	Start() error                 // start node and participate in the network
	Shutdown() error              // shutdown node and handle closing states
	Peers() []*transport.NodeInfo // return a list of all known nodes
}

// RaftNode interface for managing the Raft node lifecycle
type ServerNode interface {
	Node
	ApplyCommand(command []byte) error // Apply a command to the log
	CreateSnapshot() error             // Create a blockchain-backed snapshot
	GetState() NodeState               // Return the current state
	AddPeer(peerID string) error       // Add a new peer to the cluster
	RemovePeer(peerID string) error    // Remove a peer from the cluster
}

// KademliaNode interface for managing the Kademlia node lifecycle
type ClientNode interface {
	Node
	Send(addr string, message_t int, key []byte, value []byte, nodes []*transport.NodeInfo)
	Ping(id []byte, message []byte) // Ping a client
	Store(value []byte)             // Store a value in the ledger
	FindNode(id []byte)             // Find node by ID
	FindValue(id []byte)            // Find value from ledger by key
}

type MasterNode interface {
	ServerNode
	ClientNode
}
