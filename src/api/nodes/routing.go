package nodes

import (
	"bytes"
	"errors"
	"sync"

	"github.com/danmuck/dps_files/src/api/transport"
)

// All Routing tables should implement this interface
// Other interfaces defined here extend this interface
type RoutingTable interface {
	InsertNode(node Node) error                    // insert a new node into the routing table
	RemoveNode(node Node) error                    // remove a node from routing table
	Lookup(id []byte) (*transport.NodeInfo, error) // lookup node by its ID
}

type KademliaRouting interface {
	RoutingTable
	K() int                                    // returns the current k value (replication factor)
	A() int                                    // returns the current alpha value (concurrency)
	GetBucket(index int) []*transport.NodeInfo // returns a list of nodes in a bucket by index
	ClosestK(key []byte) []*transport.NodeInfo // returns list of closest k nodes to a key
	Size() int                                 // returns the number of non-empty buckets
}

////////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////

type DefaultRouter struct {
	localhost string
	nodes     map[string]*transport.NodeInfo
	mu        sync.Mutex
}

func NewDefaultRouter(node *transport.NodeInfo) (*DefaultRouter, error) {
	return &DefaultRouter{
		localhost: node.Address,
		nodes:     make(map[string]*transport.NodeInfo),
	}, nil
}

func (r *DefaultRouter) InsertNode(node Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.nodes[node.Address()]; exists {
		return errors.New("node already exists")
	}

	r.nodes[node.Address()] = &transport.NodeInfo{
		Id:      node.ID(),
		Address: node.Address(),
	}
	return nil
}

func (r *DefaultRouter) RemoveNode(node Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.nodes[node.Address()]; !exists {
		return errors.New("node not found")
	}

	delete(r.nodes, node.Address())
	return nil
}

func (r *DefaultRouter) Lookup(id []byte) (*transport.NodeInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, info := range r.nodes {
		if bytes.Equal(info.GetId(), id) {
			return info, nil
		}
	}
	return nil, errors.New("node not found")
}

