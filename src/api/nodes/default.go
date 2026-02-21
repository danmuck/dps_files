package nodes

import (
	"fmt"
	"time"

	"github.com/danmuck/dps_files/src/api/transport"
	logs "github.com/danmuck/smplog"
)

type DefaultNode struct {
	address    string
	pubKey     []byte
	Router     RoutingTable
	TCPHandler *transport.TCPHandler
	exit       chan any
}

func NewDefaultNode(id []byte, address string, k int, a int) (*DefaultNode, error) {
	node := &transport.NodeInfo{
		Address: address,
		Id:      id,
		Time:    time.Now().UnixNano(),
	}
	rt, err := NewKademliaRouter(node, k, a)
	if err != nil {
		return nil, fmt.Errorf("failed to create router: %w", err)
	}

	exit := make(chan any)
	client := &DefaultNode{
		pubKey:     id,
		address:    address,
		Router:     rt,
		TCPHandler: transport.NewTCPHandler(address, exit),
		exit:       exit,
	}

	return client, nil
}

func (n *DefaultNode) NodeInfo() transport.NodeInfo {
	return transport.NodeInfo{
		Id:      n.pubKey,
		Address: n.address,
		Time:    time.Now().UnixNano(),
	}
}

func (n *DefaultNode) Address() string {
	return n.address
}

func (n *DefaultNode) ID() []byte {
	return n.pubKey
}

func (n *DefaultNode) PubKey() []byte {
	return n.pubKey
}

func (n *DefaultNode) Start() error {
	go n.TCPHandler.ListenAndAccept()
	go func() {
		c := n.TCPHandler.ProcessRPC()
		for {
			select {
			case <-n.exit:
				logs.Debugf("handleInbound(): exiting")
				return
			case rpc := <-c:
				if rpc != nil {
					logs.Debugf("handleInbound(%s)", rpc.Sender.Address)
					continue
				}
				// default:
				// 	time.Sleep(500 * time.Millisecond)
			}
		}
	}()

	return nil
}

func (n *DefaultNode) Shutdown() error {
	close(n.exit)
	time.Sleep(2 * time.Second)
	err := n.TCPHandler.Close()
	return err
}

func (n *DefaultNode) Peers() []*transport.NodeInfo {
	return nil
}

func (n *DefaultNode) Send(addr string, messageType int, key []byte, value []byte, nodes []*transport.NodeInfo) error {
	return fmt.Errorf("kademlia Send not implemented")
}

func (n *DefaultNode) Ping(id []byte, message []byte) error {
	return fmt.Errorf("kademlia Ping not implemented")
}

func (n *DefaultNode) Store(value []byte) error {
	return fmt.Errorf("kademlia Store not implemented")
}

func (n *DefaultNode) FindNode(id []byte) ([]*transport.NodeInfo, error) {
	return nil, fmt.Errorf("kademlia FindNode not implemented")
}

func (n *DefaultNode) FindValue(id []byte) ([]byte, []*transport.NodeInfo, error) {
	return nil, nil, fmt.Errorf("kademlia FindValue not implemented")
}
