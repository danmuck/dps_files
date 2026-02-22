package nodes

import (
	"bytes"
	"errors"
	"sync"
)

// All Routing tables should implement this interface
// Other interfaces defined here extend this interface
type RoutingTable interface {
	InsertNode(node Node) error        // insert a new node into the routing table
	RemoveNode(node Node) error        // remove a node from routing table
	Lookup(id []byte) (*NodeInfo, error) // lookup node by its ID
}

type DefaultRouter struct {
	localhost string
	nodes     map[string]*NodeInfo
	mu        sync.Mutex
}

func NewDefaultRouter(node *NodeInfo) (*DefaultRouter, error) {
	return &DefaultRouter{
		localhost: node.Address,
		nodes:     make(map[string]*NodeInfo),
	}, nil
}

func (r *DefaultRouter) InsertNode(node Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.nodes[node.Address()]; exists {
		return errors.New("node already exists")
	}

	r.nodes[node.Address()] = &NodeInfo{
		ID:      node.ID(),
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

func (r *DefaultRouter) Lookup(id []byte) (*NodeInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, info := range r.nodes {
		if bytes.Equal(info.ID, id) {
			return info, nil
		}
	}
	return nil, errors.New("node not found")
}
