package transport

import "net"

type TransportHandler interface {
	ListenAndAccept() error              // listen and accept connections
	Send(conn net.Conn, rpc *RPC) error  // send an RPC message over a connection
	ProcessRPC() <-chan *RPC             // return channel of inbound RPCs
	Close() error                        // close listener and channels
}
