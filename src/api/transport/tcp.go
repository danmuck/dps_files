package transport

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	logs "github.com/danmuck/smplog"
)

type TCPHandler struct {
	address  string
	listener net.Listener
	inbound  chan *RPC
	coder    Coder
	exit     chan any
	mu       sync.Mutex
	conns    map[string]net.Conn
}

// TCPHandler generator function
func NewTCPHandler(address string, exit chan any) *TCPHandler {
	logs.Debugf("NewTCPHandler(%s)", address)
	return &TCPHandler{
		address: address,
		inbound: make(chan *RPC),
		exit:    exit,
		coder:   DefaultCoder{},
		conns:   make(map[string]net.Conn),
	}
}

// interface

// Dial connects to a remote address, reusing an existing connection if available.
func (h *TCPHandler) Dial(addr string) (net.Conn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if conn, ok := h.conns[addr]; ok {
		return conn, nil
	}
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	h.conns[addr] = conn
	return conn, nil
}

// Addr returns the listener's address if available, otherwise the configured address.
func (h *TCPHandler) Addr() string {
	if h.listener != nil {
		return h.listener.Addr().String()
	}
	return h.address
}

// ReadRPC decodes a single RPC from the given connection.
func (h *TCPHandler) ReadRPC(conn net.Conn) (*RPC, error) {
	return h.coder.Decode(conn)
}

// close listener connection and inbound channel
func (h *TCPHandler) Close() error {
	logs.Debugf("Close(start)")
	close(h.inbound)
	logs.Debugf("Close(done)")
	return nil
}

// Listen and accept connections via TCPHandler.listener
func (h *TCPHandler) ListenAndAccept() error {
	logs.Debugf("ListenAndAccept(%s)", h.address)
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
	logs.Debugf("ProcessRPC(): returning channel")
	return h.inbound
}

// private

// listener accept loop
func (h *TCPHandler) acceptConnections() {
	logs.Debugf("acceptConnections(): start")
	defer h.listener.Close()
	for {
		select {
		case <-h.exit:
			logs.Debugf("acceptConnections(): exit")
			return
		default:
			h.listener.(*net.TCPListener).SetDeadline(time.Now().Add(500 * time.Millisecond)) // Non-blocking
			conn, err := h.listener.Accept()
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
					// Timeout, continue to check exit
					continue
				}
				logs.Warnf("acceptConnections error: %s", err)
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
	logs.Debugf("handleConnection(%s): start", clientAddr)

	reader := bufio.NewReader(conn)
	tcpConn, ok := conn.(*net.TCPConn)

Process:
	for {
		select {
		case <-h.exit:
			logs.Debugf("handleConnection(): exit")
			return
		default:
			if ok {
				tcpConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) // Non-blocking
			}

			data, err := reader.Peek(4)
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
					// Timeout, continue to check exit
					continue
				}
				if err == io.EOF {
					logs.Debugf("Connection closed by peer.")
					return
				}
				logs.Warnf("Error reading from reader: %v", err)
				return
			}

			if len(data) > 0 {
				rpc, err := h.coder.Decode(reader)
				if err != nil {
					logs.Warnf("handleConnection error: %v", err)
					break Process
				}
				h.inbound <- rpc
			}
		}
	}
	logs.Debugf("handleConnection(%s): connection released", clientAddr)
}
