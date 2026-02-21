package transport

import "net"

type TransportHandler interface {
	ListenAndAccept() error              // listen and accept connections
	Dial(addr string) (net.Conn, error)  // dial a remote address (with connection pooling)
	Send(conn net.Conn, rpc *RPC) error  // send an RPC message over a connection
	ProcessRPC() <-chan *RPC             // return channel of inbound RPCs
	Close() error                        // close listener and channels
}
