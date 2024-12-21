package nodes

import (
	"fmt"
	"sync"

	"github.com/danmuck/dps_files/src/api/transport"
)

// All Routing tables should implement this interface
// Other interfaces defined here extend this interface
type RoutingTable interface {
	InsertNode(node Node) error      // insert a new node into the routing table
	RemoveNode(node Node) error      // remove a node from routing table
	Lookup(id []byte) (*Node, error) // lookup node by its ID
}

type KademliaRouting interface {
	RoutingTable
	K()         // returns the current k value
	A()         // returns the current alpha value
	GetBucket() // returns a list of nodes in a bucket by index
	ClosestK()  // returns list of closest k nodes to a key
	Size() int  // returns the number of non-empty buckets
}

type DefaultRouter struct {
	localhost string
	nodes     map[string]transport.NodeInfo
	mu        sync.Mutex
}

func NewDefaultRouter(node *transport.NodeInfo) (*DefaultRouter, error) {
	return &DefaultRouter{
		localhost: node.Address,
		nodes:     make(map[string]transport.NodeInfo),
	}, nil
}

func (r *DefaultRouter) InsertNode(node Node) error {
	return nil
}
func (r *DefaultRouter) RemoveNode(node Node) error {
	return nil
}
func (r *DefaultRouter) Lookup(id []byte) (*Node, error) {
	return nil, nil
}

type KademliaRouter struct {
	id        []byte
	localhost string

	k       int
	size    int
	buckets [][]*transport.NodeInfo // max length of 160 			PLACEHOLDER

	mu sync.Mutex
}

func NewKademliaRouter(node *transport.NodeInfo, k int) (*KademliaRouter, error) {
	nodeID := node.Id
	if len(nodeID) != 20 || nodeID == nil {
		return nil, fmt.Errorf("bad node id: %+v", nodeID)
	}
	return &KademliaRouter{
		id:        nodeID,
		localhost: node.Address,
		k:         k,
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
func (r *KademliaRouter) Lookup(id []byte) (*Node, error) {
	return nil, nil
}
func (r *KademliaRouter) K() int {
	return -1
}
func (r *KademliaRouter) A() int {
	return -1
}
func (r *KademliaRouter) GetBucket(index int) []*Node {
	return nil
}
func (r *KademliaRouter) ClosestK(key []byte) []*Node {
	return nil
}
func (r *KademliaRouter) Size() int {
	return -1
}
