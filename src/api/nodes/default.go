package nodes

import "fmt"

type DefaultNode struct {
	address string
	pubKey  []byte
	Router  RoutingTable
}

func NewDefaultNode(id []byte, address string) (*DefaultNode, error) {
	node := &NodeInfo{
		Address: address,
		ID:      id,
	}
	rt, err := NewDefaultRouter(node)
	if err != nil {
		return nil, fmt.Errorf("failed to create router: %w", err)
	}

	return &DefaultNode{
		pubKey:  id,
		address: address,
		Router:  rt,
	}, nil
}

func (n *DefaultNode) NodeInfo() NodeInfo {
	return NodeInfo{
		ID:      n.pubKey,
		Address: n.address,
	}
}

func (n *DefaultNode) Address() string {
	return n.address
}

func (n *DefaultNode) ID() []byte {
	return n.pubKey
}

func (n *DefaultNode) Peers() []*NodeInfo {
	return nil
}
