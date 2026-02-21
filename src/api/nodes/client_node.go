package nodes

import (
	"fmt"

	"github.com/danmuck/dps_files/src/api/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DefaultClientNode is a user-facing interface to one or more ServerNodes.
// All file operations go through gRPC — even when targeting a co-located local server.
type DefaultClientNode struct {
	*DefaultNode
	localServer *DefaultServerNode
	storageDir  string // non-empty → start a local ServerNode in Start()
	activeConn  *grpc.ClientConn
	remoteAddrs []string
}

// ClientOption configures optional DefaultClientNode features.
type ClientOption func(*DefaultClientNode)

// WithLocalStorage configures the client to start a co-located ServerNode backed
// by the given directory. The client connects to it over gRPC on localhost:0.
func WithLocalStorage(dir string) ClientOption {
	return func(c *DefaultClientNode) { c.storageDir = dir }
}

// WithRemotes adds remote server gRPC addresses to connect to.
func WithRemotes(addrs ...string) ClientOption {
	return func(c *DefaultClientNode) { c.remoteAddrs = append(c.remoteAddrs, addrs...) }
}

// NewClientNode creates a DefaultClientNode. ID must be 20 bytes.
func NewClientNode(id []byte, opts ...ClientOption) (*DefaultClientNode, error) {
	base, err := NewDefaultNode(id, "localhost:0")
	if err != nil {
		return nil, err
	}
	cn := &DefaultClientNode{DefaultNode: base}
	for _, opt := range opts {
		opt(cn)
	}
	return cn, nil
}

// Start initialises the local ServerNode (if configured) and establishes the
// active gRPC connection. Local server takes priority over remote addresses.
func (c *DefaultClientNode) Start() error {
	if c.storageDir != "" {
		sn, err := NewServerNode(c.ID(), "localhost:0", c.storageDir)
		if err != nil {
			return fmt.Errorf("start local server: %w", err)
		}
		if err := sn.Start(); err != nil {
			return fmt.Errorf("start local server: %w", err)
		}
		c.localServer = sn
		conn, err := grpc.NewClient(sn.Addr(),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return fmt.Errorf("dial local server: %w", err)
		}
		c.activeConn = conn
		return nil
	}
	if len(c.remoteAddrs) > 0 {
		conn, err := grpc.NewClient(c.remoteAddrs[0],
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return fmt.Errorf("dial %s: %w", c.remoteAddrs[0], err)
		}
		c.activeConn = conn
	}
	return nil
}

// Shutdown closes the active gRPC connection and stops the local server (if any).
func (c *DefaultClientNode) Shutdown() error {
	if c.activeConn != nil {
		c.activeConn.Close()
	}
	if c.localServer != nil {
		c.localServer.Shutdown()
	}
	return nil
}

// Stub returns a gRPC client stub for the active server.
// Returns an error if no server has been configured.
func (c *DefaultClientNode) Stub() (pb.DPSFilesClient, error) {
	if c.activeConn == nil {
		return nil, fmt.Errorf("no server configured: use WithLocalStorage or WithRemotes")
	}
	return pb.NewDPSFilesClient(c.activeConn), nil
}

// LocalServer returns the co-located ServerNode, or nil if not configured.
// Use this only for TUI operations that need direct KeyStore access (RawKeyStore).
func (c *DefaultClientNode) LocalServer() *DefaultServerNode {
	return c.localServer
}
