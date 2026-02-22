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
	Name      string
	Hash      string // hex-encoded 32-byte SHA-256
	Size      uint64
	EntryType string
}

// IsDirectory returns true when this entry is a directory manifest.
func (e RemoteFileEntry) IsDirectory() bool { return e.EntryType == "directory" }

// VerifyIssue is a single integrity error returned by the remote Verify RPC.
type VerifyIssue struct {
	ChunkIndex uint64
	FileName   string
	Err        string
}

// RemoteCleanResult holds counts from the remote Clean RPC.
type RemoteCleanResult struct {
	RemovedKDHT     int64
	RemovedMetadata int64
	RemovedCache    int64
}

// RemoteStats holds storage statistics from the remote Stats RPC.
type RemoteStats struct {
	DataBytes     uint64
	MetadataBytes uint64
	CacheBytes    uint64
	TotalBytes    uint64
	FileCount     int64
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
			Name:      f.Name,
			Hash:      hex.EncodeToString(f.Hash),
			Size:      f.Size,
			EntryType: f.EntryType,
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

	size := uint64(info.Size())
	pr := newProgressReader(f, size, "upload", true)
	defer pr.Finish()

	// No deadline for large transfers.
	stream, err := c.stub.Upload(context.Background())
	if err != nil {
		return hash, fmt.Errorf("open upload stream: %w", err)
	}
	if err := stream.Send(&pb.UploadChunk{
		Name: filepath.Base(localPath),
		Size: size,
	}); err != nil {
		return hash, fmt.Errorf("send metadata: %w", err)
	}
	buf := make([]byte, 1<<20) // 1 MiB chunks
	for {
		n, readErr := pr.Read(buf)
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
// size is a hint for the progress bar (0 = unknown).
// Returns bytes written.
func (c *GRPCClient) Download(name, outputPath string, size uint64) (uint64, error) {
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

	pw := newProgressWriter(out, size, "download", true)
	defer pw.Finish()

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return pw.Written(), fmt.Errorf("recv chunk: %w", err)
		}
		if _, err := pw.Write(chunk.Data); err != nil {
			return pw.Written(), fmt.Errorf("write: %w", err)
		}
	}
	return pw.Written(), nil
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

// Verify runs a remote integrity scan and returns any chunk errors.
func (c *GRPCClient) Verify() ([]VerifyIssue, error) {
	ctx, cancel := c.ctx()
	defer cancel()
	resp, err := c.stub.Verify(ctx, &pb.VerifyRequest{})
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	issues := make([]VerifyIssue, len(resp.Errors))
	for i, e := range resp.Errors {
		issues[i] = VerifyIssue{
			ChunkIndex: e.ChunkIndex,
			FileName:   e.FileName,
			Err:        e.Error,
		}
	}
	return issues, nil
}

// Expire triggers TTL expiry on the remote server and returns the count removed.
func (c *GRPCClient) Expire() (int64, error) {
	ctx, cancel := c.ctx()
	defer cancel()
	resp, err := c.stub.Expire(ctx, &pb.ExpireRequest{})
	if err != nil {
		return 0, fmt.Errorf("expire: %w", err)
	}
	return resp.Removed, nil
}

// Clean removes chunk data (and optionally metadata + cache) from the remote server.
func (c *GRPCClient) Clean(deep bool) (RemoteCleanResult, error) {
	ctx, cancel := c.ctx()
	defer cancel()
	resp, err := c.stub.Clean(ctx, &pb.CleanRequest{Deep: deep})
	if err != nil {
		return RemoteCleanResult{}, fmt.Errorf("clean: %w", err)
	}
	return RemoteCleanResult{
		RemovedKDHT:     resp.RemovedKdht,
		RemovedMetadata: resp.RemovedMetadata,
		RemovedCache:    resp.RemovedCache,
	}, nil
}

// UploadDirManifest sends a client-assembled DirectoryManifest JSON blob to
// the server for storage. Returns the 32-byte root manifest hash.
func (c *GRPCClient) UploadDirManifest(manifestJSON []byte) ([32]byte, error) {
	var hash [32]byte
	ctx, cancel := c.ctx()
	defer cancel()
	resp, err := c.stub.UploadDir(ctx, &pb.UploadDirRequest{Manifest: manifestJSON})
	if err != nil {
		return hash, fmt.Errorf("upload dir manifest: %w", err)
	}
	copy(hash[:], resp.Hash)
	return hash, nil
}

// RemoteStorageStats fetches storage usage from the remote server.
func (c *GRPCClient) RemoteStorageStats() (RemoteStats, error) {
	ctx, cancel := c.ctx()
	defer cancel()
	resp, err := c.stub.Stats(ctx, &pb.StatsRequest{})
	if err != nil {
		return RemoteStats{}, fmt.Errorf("stats: %w", err)
	}
	return RemoteStats{
		DataBytes:     resp.DataBytes,
		MetadataBytes: resp.MetadataBytes,
		CacheBytes:    resp.CacheBytes,
		TotalBytes:    resp.TotalBytes,
		FileCount:     resp.FileCount,
	}, nil
}
