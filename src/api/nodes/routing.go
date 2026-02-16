package nodes

import (
	"bytes"
	"errors"
	"fmt"
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

////////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////

type KademliaRouter struct {
	id        []byte
	localhost string

	k       int
	a       int
	size    int
	buckets [][]*transport.NodeInfo // max length of 160 			PLACEHOLDER

	mu sync.Mutex
}

func NewKademliaRouter(node *transport.NodeInfo, k int, a int) (*KademliaRouter, error) {
	nodeID := node.Id
	if len(nodeID) != 20 || nodeID == nil {
		return nil, fmt.Errorf("bad node id: %+v", nodeID)
	}
	if k <= 0 {
		return nil, fmt.Errorf("bad k value: %d", k)
	}
	if a <= 0 || a > k {
		return nil, fmt.Errorf("bad a value: %d (k=%d)", a, k)
	}
	return &KademliaRouter{
		id:        nodeID,
		localhost: node.Address,
		k:         k,
		a:         a,
		size:      0,
		buckets:   make([][]*transport.NodeInfo, 0, 160),
	}, nil
}

func (r *KademliaRouter) InsertNode(node Node) error {
	return nil
}

func (r *KademliaRouter) RemoveNode(node Node) error {
	return nil
}

func (r *KademliaRouter) Lookup(id []byte) (*transport.NodeInfo, error) {
	return nil, nil
}

func (r *KademliaRouter) K() int {
	return r.k
}

func (r *KademliaRouter) A() int {
	return r.a
}

func (r *KademliaRouter) GetBucket(index int) []*transport.NodeInfo {
	return nil
}

func (r *KademliaRouter) ClosestK(key []byte) []*transport.NodeInfo {
	return nil
}

func (r *KademliaRouter) Size() int {
	return r.size
}
