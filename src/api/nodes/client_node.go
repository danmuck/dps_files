package nodes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/danmuck/dps_files/src/api/ledgers"
	"github.com/danmuck/dps_files/src/api/transport"
)

// DefaultClientNode embeds DefaultNode and performs file operations against
// remote ServerNodes or an optional co-located local server.
type DefaultClientNode struct {
	*DefaultNode
	localServer *DefaultServerNode
	remotes     []*transport.NodeInfo
	storageDir  string
}

// ClientOption configures optional DefaultClientNode features.
type ClientOption func(*DefaultClientNode)

// WithLocalStorage enables a co-located ServerNode backed by the given directory.
func WithLocalStorage(dir string) ClientOption {
	return func(c *DefaultClientNode) { c.storageDir = dir }
}

// WithRemotes adds remote server addresses to the client's target list.
func WithRemotes(addrs ...string) ClientOption {
	return func(c *DefaultClientNode) {
		for _, addr := range addrs {
			c.remotes = append(c.remotes, &transport.NodeInfo{Address: addr})
		}
	}
}

// NewClientNode creates a DefaultClientNode. The ID must be exactly 20 bytes.
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

// Start initialises the TCP listener (for receiving dial-back responses) and
// optionally starts a co-located local ServerNode.
func (c *DefaultClientNode) Start() error {
	// Start TCP listener so remote servers can dial back with responses.
	if err := c.DefaultNode.Start(); err != nil {
		return err
	}

	if c.storageDir != "" {
		sn, err := NewServerNode(c.ID(), "localhost:0", c.storageDir)
		if err != nil {
			return fmt.Errorf("start local server: %w", err)
		}
		if err := sn.Start(); err != nil {
			return fmt.Errorf("start local server: %w", err)
		}
		c.localServer = sn
	}
	return nil
}

// Shutdown stops the local server (if any) and the base node.
func (c *DefaultClientNode) Shutdown() error {
	if c.localServer != nil {
		c.localServer.Shutdown()
	}
	return c.DefaultNode.Shutdown()
}

// LocalServer returns the co-located ServerNode, or nil if not configured.
func (c *DefaultClientNode) LocalServer() *DefaultServerNode {
	return c.localServer
}

// Upload reads filePath and sends it to the target server via UPLOAD RPC.
func (c *DefaultClientNode) Upload(filePath string, target *transport.NodeInfo) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	name := filepath.Base(filePath)

	rpc := &transport.RPC{
		Meta:      &transport.RPCT{Command: transport.Command_UPLOAD},
		Sender:    c.nodeInfo(),
		Key:       []byte(name),
		Value:     data,
		RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
	}
	return c.send(target, rpc)
}

// Download requests a file by hash from the source server via DOWNLOAD RPC.
func (c *DefaultClientNode) Download(fileHash [32]byte, outputPath string, source *transport.NodeInfo) error {
	rpc := &transport.RPC{
		Meta:      &transport.RPCT{Command: transport.Command_DOWNLOAD},
		Sender:    c.nodeInfo(),
		Key:       fileHash[:],
		RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
	}
	return c.send(source, rpc)
}

// Delete requests deletion of a file by hash on the target server.
func (c *DefaultClientNode) Delete(fileHash [32]byte, target *transport.NodeInfo) error {
	rpc := &transport.RPC{
		Meta:      &transport.RPCT{Command: transport.Command_DELETE},
		Sender:    c.nodeInfo(),
		Key:       fileHash[:],
		RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
	}
	return c.send(target, rpc)
}

// List requests the file list from the target server via LIST RPC.
func (c *DefaultClientNode) List(target *transport.NodeInfo) ([]ledgers.FileID, error) {
	rpc := &transport.RPC{
		Meta:      &transport.RPCT{Command: transport.Command_LIST},
		Sender:    c.nodeInfo(),
		RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
	}
	if err := c.send(target, rpc); err != nil {
		return nil, err
	}
	// In the current architecture the server dials back with the response.
	// A full request-response correlation (matching RequestID on the inbound
	// channel) will be added once the transport layer supports it.
	// For now callers can use the local server path for synchronous operations.
	return nil, nil
}

// ListLocal is a convenience method that lists files on the co-located server
// without a network round-trip.
func (c *DefaultClientNode) ListLocal() ([]ledgers.FileID, error) {
	if c.localServer == nil {
		return nil, fmt.Errorf("no local server configured")
	}
	listRPC := &transport.RPC{
		Meta: &transport.RPCT{Command: transport.Command_LIST},
	}
	resp, err := c.localServer.HandleRPC(listRPC)
	if err != nil {
		return nil, err
	}
	var summaries []ledgers.FileMetaSummary
	if err := json.Unmarshal(resp.Payload, &summaries); err != nil {
		return nil, fmt.Errorf("unmarshal file list: %w", err)
	}
	ids := make([]ledgers.FileID, len(summaries))
	for i, s := range summaries {
		ids[i] = s.Hash
	}
	return ids, nil
}

// send dials the target and sends an RPC. The current transport model has the
// server dial back with the response, so this method is fire-and-forget.
func (c *DefaultClientNode) send(target *transport.NodeInfo, rpc *transport.RPC) error {
	conn, err := c.TCPHandler.Dial(target.Address)
	if err != nil {
		return fmt.Errorf("dial %s: %w", target.Address, err)
	}
	if err := c.TCPHandler.Send(conn, rpc); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}

// nodeInfo returns a pointer to this node's transport.NodeInfo.
func (c *DefaultClientNode) nodeInfo() *transport.NodeInfo {
	info := c.NodeInfo()
	return &info
}
