package transport

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"time"
)

type TCPHandler struct {
	address  string
	listener net.Listener
	inbound  chan *RPC
	coder    Coder
	exit     chan any
}

// TCPHandler generator function
func NewTCPHandler(address string, exit chan any) *TCPHandler {
	fmt.Printf("NewTCPHandler(%s) \n", address)
	return &TCPHandler{
		address: address,
		inbound: make(chan *RPC),
		exit:    exit,
		coder:   DefaultCoder{},
	}
}

// interface

// close listener connection and inbound channel
func (h *TCPHandler) Close() error {
	fmt.Println("Close(start)")
	close(h.inbound)
	fmt.Println("Close(done)")
	return nil
}

// Listen and accept connections via TCPHandler.listener
func (h *TCPHandler) ListenAndAccept() error {
	fmt.Printf("ListenAndAccept(%s) \n", h.address)
	var err error
	h.listener, err = net.Listen("tcp", h.address)
	if err != nil {
		return err
	}

	go h.acceptConnections()

	return nil
}

// Send an RPC over a connection using the configured encoder
func (h *TCPHandler) Send(conn net.Conn, rpc *RPC) error {
	data, err := h.coder.Encode(rpc)
	if err != nil {
		return fmt.Errorf("failed to encode RPC: %w", err)
	}
	_, err = conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write RPC: %w", err)
	}
	return nil
}

// Recieve an RPC from the inbound channel
func (h *TCPHandler) ProcessRPC() <-chan *RPC {
	fmt.Println("ProcessRPC(): returning channel")
	return h.inbound
}

// private

// listener accept loop
func (h *TCPHandler) acceptConnections() {
	fmt.Printf("acceptConnections(): start \n")
	defer h.listener.Close()
	for {
		select {
		case <-h.exit:
			fmt.Printf("acceptConnections(): exit \n")
			return
		default:
			h.listener.(*net.TCPListener).SetDeadline(time.Now().Add(500 * time.Millisecond)) // Non-blocking
			conn, err := h.listener.Accept()
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
					// Timeout, continue to check exit
					continue
				}
				fmt.Printf("acceptConnections(error): %s \n", err)
				return
			}
			go h.handleConnection(conn)
		}
	}
}

// listener connection handler
func (h *TCPHandler) handleConnection(conn net.Conn) {
	defer conn.Close()
	clientAddr := conn.RemoteAddr().String()
	fmt.Printf("handleConnection(%s): start \n", clientAddr)

	reader := bufio.NewReader(conn)
	tcpConn, ok := conn.(*net.TCPConn)

Process:
	for {
		select {
		case <-h.exit:
			fmt.Printf("handleConnection(): exit \n")
			return
		default:
			if ok {
				tcpConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) // Non-blocking
			}

			data, err := reader.Peek(2)
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
					// Timeout, continue to check exit
					continue
				}
				if err == io.EOF {
					fmt.Println("Connection closed by peer.")
					return
				}
				fmt.Printf("Error reading from reader: %v\n", err)
				return
			}

			if len(data) > 0 {
				rpc, err := h.coder.Decode(reader)
				if err != nil {
					fmt.Println("handleConnection(error): ", err)
					break Process
				}
				h.inbound <- rpc
			}
		}
	}
	fmt.Printf("handleConnection(%s): connection released \n", clientAddr)
}
