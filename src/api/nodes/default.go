package nodes

import (
	"fmt"
	"time"

	"github.com/danmuck/dps_files/src/api/transport"
)

type DefaultNode struct {
	id         []byte
	address    string
	Router     RoutingTable
	TCPHandler *transport.TCPHandler
	exit       chan interface{}
}

func NewDefaultNode(id []byte, address string, k int, a int) (DefaultNode, error) {
	node := &transport.NodeInfo{
		Address: address,
		Id:      id,
		Time:    time.Now().UnixNano(),
	}
	rt, err := NewKademliaRouter(node, k)
	if err != nil {
		panic(err)
	}
	if a > k {
		a = k
	}

	exit := make(chan interface{})
	client := &DefaultNode{
		id:         id,
		address:    address,
		Router:     rt,
		TCPHandler: transport.NewTCPHandler(address, exit),
		exit:       exit,
	}

	return *client, nil
}

func (n *DefaultNode) NodeInfo() transport.NodeInfo {
	return transport.NodeInfo{
		Id:      n.id,
		Address: n.address,
		Time:    time.Now().UnixNano(),
	}
}

func (n *DefaultNode) Address() string {
	return n.address
}

func (n *DefaultNode) ID() []byte {
	return n.id
}

func (n *DefaultNode) Start() error {
	go n.TCPHandler.ListenAndAccept()
	go func() {
		c := n.TCPHandler.ProcessRPC()
		for {
			select {
			case <-n.exit:
				fmt.Printf("handleInbound(): exiting \n")
				return
			case rpc := <-c:
				if rpc != nil {
					fmt.Printf("handleInbound(%s) \n", rpc.Sender.Address)
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

func (n *DefaultNode) Send(addr string, message_t int, key []byte, value []byte, nodes []*string) {

}

func (n *DefaultNode) Ping(id []byte, message []byte) {

}

func (n *DefaultNode) Store(value []byte) {

}

func (n *DefaultNode) FindNode(id []byte) {

}

func (n *DefaultNode) FindValue(id []byte) {

}
