package api

import (
	"github.com/coreos/etcd/raft"
)

type NodeState string

const (
	Follower  NodeState = "Follower"
	Candidate NodeState = "Candidate"
	Leader    NodeState = "Leader"
)

// RaftNode interface for managing the Raft node lifecycle.
type ServerNode interface {
	Start(peers []raft.Peer) error     // Start the Raft node
	Stop() error                       // Stop the Raft node
	ApplyCommand(command []byte) error // Apply a command to the log
	CreateSnapshot() error             // Create a blockchain-backed snapshot
	GetState() NodeState               // Return the current state
	AddPeer(peerID string) error       // Add a new peer to the cluster
	RemovePeer(peerID string) error    // Remove a peer from the cluster
}

// KademliaNode interface for managing the Kademlia node lifecycle.
type ClientNode interface {
	Ping(id []byte, message []byte) // Ping a client
	Store(value []byte)             // Store a value in the ledger
	FindNode(id []byte)             // Find node by ID
	FindValue(id []byte)            // Find value from ledger by key
	Peers() []*string               // Return a list of all known clients
	Send(addr string, message_t int, key []byte, value []byte, nodes []*string)
	Shutdown() error // Stop the Kademlia node
}
