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

func NewDefaultNode(id []byte, address string) (*DefaultNode, error) {
	node := &transport.NodeInfo{
		Address: address,
		Id:      id,
		Time:    time.Now().UnixNano(),
	}
	rt, err := NewDefaultRouter(node)
	if err != nil {
		return nil, fmt.Errorf("failed to create router: %w", err)
	}

	exit := make(chan any)
	return &DefaultNode{
		pubKey:     id,
		address:    address,
		Router:     rt,
		TCPHandler: transport.NewTCPHandler(address, exit),
		exit:       exit,
	}, nil
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

