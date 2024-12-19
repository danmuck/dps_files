package server

// import (
// 	"github.com/coreos/etcd/raft"
// 	"github.com/danmuck/dps_files/operations"
// )

// type Node struct {
// 	ID         uint64
// 	Node       raft.Node
// 	Storage    *raft.MemoryStorage
// 	LogManager operations.CandidateLogManager
// 	Metadata   operations.MetadataStore
// 	Transport  operations.Transport
// 	Snapshots  operations.SnapshotManager
// }

// func (n *Node) Start(peers []raft.Peer) error {
// 	config := &raft.Config{
// 		ID:              n.ID,
// 		ElectionTick:    10,
// 		HeartbeatTick:   1,
// 		Storage:         n.Storage,
// 		MaxSizePerMsg:   4096,
// 		MaxInflightMsgs: 256,
// 	}

// 	n.Node = raft.StartNode(config, peers)
// 	go n.processRaftEvents()
// 	return nil
// }

// func (n *Node) processRaftEvents() {
// 	for rd := range n.Node.Ready() {
// 		// Apply log entries
// 		for _, entry := range rd.CommittedEntries {
// 			n.LogManager.Append(operations.LogEntry{
// 				Index:   entry.Index,
// 				Term:    entry.Term,
// 				Command: entry.Data,
// 			})
// 		}

// 		// Persist snapshot
// 		if !raft.IsEmptySnap(rd.Snapshot) {
// 			snapshot := operations.Snapshot{
// 				Term:  rd.Snapshot.Metadata.Term,
// 				Index: rd.Snapshot.Metadata.Index,
// 				Data:  rd.Snapshot.Data,
// 				Metadata: map[string]string{
// 					"blockchain_hash": operations.computeBlockchainHash(rd.Snapshot.Data),
// 				},
// 			}
// 			n.Snapshots.PersistSnapshot(snapshot)
// 		}

// 		// Send messages
// 		for _, msg := range rd.Messages {
// 			n.Transport.Send(operations.Message{
// 				From:    msg.From,
// 				To:      msg.To,
// 				Type:    msg.Type.String(),
// 				Payload: msg.Entries,
// 			})
// 		}

// 		n.Node.Advance()
// 	}
// }
