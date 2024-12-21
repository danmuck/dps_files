package transport

import "net"

type TransportHandler interface {
	ListenAndAccept() error            // listen
	Send(conn net.Conn, rpc RPC) error // Send a message to another node
	ProcessRPC() <-chan *RPC           // Process an RPC
	Close()
}
