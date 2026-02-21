package nodes

import (
	"fmt"
	"os"

	"github.com/danmuck/dps_files/src/api/ledgers"
)

// DefaultClientNode embeds DefaultNode and performs file operations against
// remote ServerNodes or an optional co-located local server.
type DefaultClientNode struct {
	*DefaultNode
	localServer *DefaultServerNode
	remotes     []*NodeInfo
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
			c.remotes = append(c.remotes, &NodeInfo{Address: addr})
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

// Start optionally starts a co-located local ServerNode.
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
	}
	return nil
}

// Shutdown stops the local server (if any).
func (c *DefaultClientNode) Shutdown() error {
	if c.localServer != nil {
		c.localServer.Shutdown()
	}
	return nil
}

// LocalServer returns the co-located ServerNode, or nil if not configured.
func (c *DefaultClientNode) LocalServer() *DefaultServerNode {
	return c.localServer
}

// Upload reads filePath and sends it to the target server.
// Remote upload via gRPC will be wired in Task 7.
func (c *DefaultClientNode) Upload(filePath string, target *NodeInfo) error {
	_, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	// TODO(Task 7): send via gRPC to target.Address
	return fmt.Errorf("remote upload not yet implemented")
}

// Download requests a file by hash from the source server.
// Remote download via gRPC will be wired in Task 7.
func (c *DefaultClientNode) Download(fileHash [32]byte, outputPath string, source *NodeInfo) error {
	_ = fileHash
	_ = outputPath
	_ = source
	// TODO(Task 7): fetch via gRPC from source.Address
	return fmt.Errorf("remote download not yet implemented")
}

// Delete requests deletion of a file by hash on the target server.
// Remote delete via gRPC will be wired in Task 7.
func (c *DefaultClientNode) Delete(fileHash [32]byte, target *NodeInfo) error {
	_ = fileHash
	_ = target
	// TODO(Task 7): delete via gRPC on target.Address
	return fmt.Errorf("remote delete not yet implemented")
}

// List requests the file list from the target server.
// Remote list via gRPC will be wired in Task 7.
func (c *DefaultClientNode) List(target *NodeInfo) ([]ledgers.FileID, error) {
	_ = target
	// TODO(Task 7): list via gRPC from target.Address
	return nil, fmt.Errorf("remote list not yet implemented")
}

// ListLocal is a convenience method that lists files on the co-located server
// without a network round-trip.
func (c *DefaultClientNode) ListLocal() ([]ledgers.FileID, error) {
	if c.localServer == nil {
		return nil, fmt.Errorf("no local server configured")
	}
	summaries := c.localServer.Storage().ListKnownFilesMetadata()
	ids := make([]ledgers.FileID, len(summaries))
	for i, s := range summaries {
		ids[i] = s.Hash
	}
	return ids, nil
}
