package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/danmuck/dps_files/src/api/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RemoteFileEntry is a file entry returned by the server List RPC.
type RemoteFileEntry struct {
	Name string
	Hash string // hex-encoded 32-byte SHA-256
	Size uint64
}

// GRPCClient wraps a pb.DPSFilesClient stub with convenience methods
// matching the surface previously provided by FileServerClient.
type GRPCClient struct {
	conn    *grpc.ClientConn
	stub    pb.DPSFilesClient
	timeout time.Duration
}

// NewGRPCClient dials addr and returns a GRPCClient. Call Close() when done.
func NewGRPCClient(addr string) (*GRPCClient, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return &GRPCClient{
		conn:    conn,
		stub:    pb.NewDPSFilesClient(conn),
		timeout: 30 * time.Second,
	}, nil
}

// Close releases the underlying connection.
func (c *GRPCClient) Close() { c.conn.Close() }

func (c *GRPCClient) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), c.timeout)
}

// List returns all files known to the server.
func (c *GRPCClient) List() ([]RemoteFileEntry, error) {
	ctx, cancel := c.ctx()
	defer cancel()
	resp, err := c.stub.List(ctx, &pb.ListRequest{})
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	entries := make([]RemoteFileEntry, len(resp.Files))
	for i, f := range resp.Files {
		entries[i] = RemoteFileEntry{
			Name: f.Name,
			Hash: hex.EncodeToString(f.Hash),
			Size: f.Size,
		}
	}
	return entries, nil
}

// Upload sends localPath to the server and returns the 32-byte SHA-256 hash.
func (c *GRPCClient) Upload(localPath string) ([32]byte, error) {
	var hash [32]byte
	info, err := os.Stat(localPath)
	if err != nil {
		return hash, fmt.Errorf("stat: %w", err)
	}
	f, err := os.Open(localPath)
	if err != nil {
		return hash, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	// No deadline for large transfers.
	stream, err := c.stub.Upload(context.Background())
	if err != nil {
		return hash, fmt.Errorf("open upload stream: %w", err)
	}
	if err := stream.Send(&pb.UploadChunk{
		Name: filepath.Base(localPath),
		Size: uint64(info.Size()),
	}); err != nil {
		return hash, fmt.Errorf("send metadata: %w", err)
	}
	buf := make([]byte, 1<<20) // 1 MiB chunks
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			if err := stream.Send(&pb.UploadChunk{Data: buf[:n]}); err != nil {
				return hash, fmt.Errorf("send chunk: %w", err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return hash, fmt.Errorf("read file: %w", readErr)
		}
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return hash, fmt.Errorf("finish upload: %w", err)
	}
	copy(hash[:], resp.Hash)
	return hash, nil
}

// Download fetches a file by name and writes it to outputPath.
// Returns bytes written.
func (c *GRPCClient) Download(name, outputPath string) (uint64, error) {
	// No deadline — large files.
	stream, err := c.stub.Download(context.Background(), &pb.DownloadRequest{Name: name})
	if err != nil {
		return 0, fmt.Errorf("open download stream: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return 0, fmt.Errorf("ensure output dir: %w", err)
	}
	out, err := os.Create(outputPath)
	if err != nil {
		return 0, fmt.Errorf("create output file: %w", err)
	}
	defer out.Close()
	var total uint64
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return total, fmt.Errorf("recv chunk: %w", err)
		}
		n, err := out.Write(chunk.Data)
		total += uint64(n)
		if err != nil {
			return total, fmt.Errorf("write: %w", err)
		}
	}
	return total, nil
}

// Delete removes the file identified by its 32-byte SHA-256 hash.
func (c *GRPCClient) Delete(hash [32]byte) error {
	ctx, cancel := c.ctx()
	defer cancel()
	_, err := c.stub.Delete(ctx, &pb.DeleteRequest{Hash: hash[:]})
	return err
}

// hexToHash decodes a 64-char hex string into a [32]byte.
func hexToHash(s string) ([32]byte, error) {
	var h [32]byte
	b, err := hex.DecodeString(s)
	if err != nil {
		return h, fmt.Errorf("invalid hex: %w", err)
	}
	if len(b) != 32 {
		return h, fmt.Errorf("expected 32 bytes, got %d", len(b))
	}
	copy(h[:], b)
	return h, nil
}
