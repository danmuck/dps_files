package grpcserver

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/danmuck/dps_files/src/api/ledgers"
	"github.com/danmuck/dps_files/src/api/pb"
	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const downloadChunkSize = 1 << 20 // 1 MiB per stream message

// Server implements pb.DPSFilesServer backed by a FileLedger.
type Server struct {
	pb.UnimplementedDPSFilesServer
	storage ledgers.FileLedger
}

// New returns a Server using the given FileLedger.
func New(storage ledgers.FileLedger) *Server {
	return &Server{storage: storage}
}

// managedStore type-asserts storage to *key_store.KeyStoreLedger to expose
// management operations. Returns Unimplemented if the backend is not KeyStoreLedger.
func (s *Server) managedStore() (*key_store.KeyStore, error) {
	ledger, ok := s.storage.(*key_store.KeyStoreLedger)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "management RPCs require KeyStoreLedger backend")
	}
	return ledger.KeyStore(), nil
}

// Upload receives a client-streamed file and stores it via StoreFromReader.
// First message carries name + size; subsequent messages carry data bytes.
func (s *Server) Upload(stream pb.DPSFiles_UploadServer) error {
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "receive first chunk: %v", err)
	}
	name := first.Name
	size := first.Size
	if name == "" {
		return status.Error(codes.InvalidArgument, "name is required in first chunk")
	}
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return status.Error(codes.InvalidArgument, "invalid file name")
	}

	logs.Infof("Upload started: name=%q size=%d", name, size)

	pr, pw := io.Pipe()
	errCh := make(chan error, 1)

	go func() {
		defer pw.Close()
		if len(first.Data) > 0 {
			if _, err := pw.Write(first.Data); err != nil {
				errCh <- err
				return
			}
		}
		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				errCh <- nil
				return
			}
			if err != nil {
				errCh <- err
				return
			}
			if _, err := pw.Write(chunk.Data); err != nil {
				errCh <- err
				return
			}
		}
	}()

	fid, storeErr := s.storage.StoreFromReader(name, pr, size)
	// If StoreFromReader returned early (error or unexpected EOF), unblock the
	// goroutine so it can exit and send on errCh.  Without this, a disk error
	// or size mismatch leaves the goroutine blocked on pw.Write and <-errCh
	// deadlocks, eventually forcing the gRPC transport to kill the connection.
	_ = pr.CloseWithError(storeErr)
	recvErr := <-errCh

	if storeErr != nil {
		logs.Errorf(storeErr, "Upload store failed: name=%q", name)
		return status.Errorf(codes.Internal, "store failed")
	}
	if recvErr != nil && recvErr != io.ErrClosedPipe {
		logs.Errorf(recvErr, "Upload receive failed: name=%q", name)
		return status.Errorf(codes.Internal, "upload receive failed")
	}

	logs.Infof("Upload complete: name=%q hash=%x size=%d", name, fid, size)
	return stream.SendAndClose(&pb.UploadResponse{
		Hash: fid[:],
		Name: name,
		Size: size,
	})
}

// Download streams a stored file to the client in 1 MiB chunks.
func (s *Server) Download(req *pb.DownloadRequest, stream pb.DPSFiles_DownloadServer) error {
	logs.Debugf("Download request: name=%q hash=%x", req.Name, req.Hash)

	pr, pw := io.Pipe()
	errCh := make(chan error, 1)

	go func() {
		defer pw.Close()
		var err error
		if len(req.Hash) == 32 {
			var fid ledgers.FileID
			copy(fid[:], req.Hash)
			err = s.storage.StreamFile(fid, pw)
		} else if req.Name != "" {
			err = s.storage.StreamFileByName(req.Name, pw)
		} else {
			err = fmt.Errorf("hash or name required")
		}
		errCh <- err
	}()

	var sent uint64
	buf := make([]byte, downloadChunkSize)
	for {
		n, err := pr.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&pb.DataChunk{Data: buf[:n]}); sendErr != nil {
				pr.CloseWithError(sendErr)
				<-errCh
				return sendErr
			}
			sent += uint64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			<-errCh
			return status.Errorf(codes.Internal, "download read failed")
		}
	}
	if err := <-errCh; err != nil {
		logs.Errorf(err, "Download stream failed: name=%q", req.Name)
		return status.Errorf(codes.NotFound, "file not found")
	}
	logs.Infof("Download complete: name=%q sent=%d bytes", req.Name, sent)
	return nil
}

// Delete removes a file by its 32-byte SHA-256 hash.
func (s *Server) Delete(_ context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	if len(req.Hash) != 32 {
		return nil, status.Errorf(codes.InvalidArgument, "hash must be 32 bytes, got %d", len(req.Hash))
	}
	var fid ledgers.FileID
	copy(fid[:], req.Hash)
	logs.Infof("Delete: hash=%x", req.Hash)
	if err := s.storage.DeleteFile(fid); err != nil {
		logs.Errorf(err, "Delete failed: hash=%x", req.Hash)
		return nil, status.Errorf(codes.NotFound, "file not found")
	}
	logs.Infof("Delete complete: hash=%x", req.Hash)
	return &pb.DeleteResponse{}, nil
}

// List returns metadata for all stored files and directories.
func (s *Server) List(_ context.Context, _ *pb.ListRequest) (*pb.ListResponse, error) {
	summaries := s.storage.ListKnownFilesMetadata()
	logs.Debugf("List: returning %d file(s)", len(summaries))
	entries := make([]*pb.FileEntry, len(summaries))
	for i, sm := range summaries {
		entries[i] = &pb.FileEntry{
			Name:       sm.Name,
			Hash:       sm.Hash[:],
			Size:       sm.Size,
			EntryType:  sm.EntryType,
			ParentHash: sm.ParentHash[:],
		}
	}
	return &pb.ListResponse{Files: entries}, nil
}

// UploadDir stores a directory tree. If req.Manifest is set, the client has
// already traversed its local filesystem and assembled the manifest JSON; the
// server stores it directly. Otherwise req.RootPath is read from the server's
// own filesystem (legacy server-local path).
func (s *Server) UploadDir(_ context.Context, req *pb.UploadDirRequest) (*pb.UploadDirResponse, error) {
	const maxManifestSize = 100 << 20 // 100 MiB
	if len(req.Manifest) > maxManifestSize {
		return nil, status.Errorf(codes.InvalidArgument,
			"manifest too large: %d bytes (max %d)", len(req.Manifest), maxManifestSize)
	}
	if len(req.Manifest) > 0 {
		fid, err := s.storage.StoreDirectoryManifest(req.Manifest)
		if err != nil {
			logs.Errorf(err, "UploadDir manifest store failed")
			return nil, status.Errorf(codes.Internal, "store directory failed")
		}
		return &pb.UploadDirResponse{Hash: fid[:]}, nil
	}
	if req.RootPath != "" {
		return nil, status.Error(codes.InvalidArgument,
			"server-side root_path is disabled; provide a manifest instead")
	}
	return nil, status.Error(codes.InvalidArgument, "manifest is required")
}

// ListDir returns the immediate children of a directory by its hash.
func (s *Server) ListDir(_ context.Context, req *pb.ListDirRequest) (*pb.ListDirResponse, error) {
	if len(req.Hash) != 32 {
		return nil, status.Errorf(codes.InvalidArgument, "hash must be 32 bytes")
	}
	var fid ledgers.FileID
	copy(fid[:], req.Hash)
	ledgerEntries, err := s.storage.ListDirectory(fid)
	if err != nil {
		logs.Errorf(err, "ListDir failed: hash=%x", req.Hash)
		return nil, status.Errorf(codes.NotFound, "directory not found")
	}
	entries := make([]*pb.DirEntry, len(ledgerEntries))
	for i, e := range ledgerEntries {
		entries[i] = &pb.DirEntry{
			Name: e.Name,
			Path: e.Path,
			Hash: e.Hash[:],
			Type: e.Type,
			Size: e.Size,
		}
	}
	return &pb.ListDirResponse{Entries: entries}, nil
}

// Verify runs a full integrity scan and returns any chunk errors found.
func (s *Server) Verify(_ context.Context, _ *pb.VerifyRequest) (*pb.VerifyResponse, error) {
	ks, err := s.managedStore()
	if err != nil {
		return nil, err
	}
	chunkErrs := ks.VerifyAll()
	protoErrs := make([]*pb.VerifyError, len(chunkErrs))
	for i, ce := range chunkErrs {
		protoErrs[i] = &pb.VerifyError{
			ChunkIndex: uint64(ce.ChunkIndex),
			FileName:   ce.FileName,
			Error:      ce.Err.Error(),
		}
	}
	logs.Infof("Verify complete: %d chunk error(s) found", len(chunkErrs))
	return &pb.VerifyResponse{Errors: protoErrs}, nil
}

// Expire sweeps TTL-expired files and returns the number removed.
func (s *Server) Expire(_ context.Context, _ *pb.ExpireRequest) (*pb.ExpireResponse, error) {
	ks, err := s.managedStore()
	if err != nil {
		return nil, err
	}
	removed := ks.CleanupExpired()
	logs.Infof("Expire complete: removed=%d", removed)
	return &pb.ExpireResponse{Removed: int64(removed)}, nil
}

// Clean removes stored data. If req.Deep is true, also removes metadata and cache.
func (s *Server) Clean(_ context.Context, req *pb.CleanRequest) (*pb.CleanResponse, error) {
	ks, err := s.managedStore()
	if err != nil {
		return nil, err
	}
	if req.Deep {
		result, cleanErr := ks.DeepClean()
		if cleanErr != nil {
			return nil, status.Errorf(codes.Internal, "deep clean: %v", cleanErr)
		}
		logs.Infof("Clean(deep) complete: kdht=%d meta=%d cache=%d", result.RemovedKDHT, result.RemovedMetadata, result.RemovedCache)
		return &pb.CleanResponse{
			RemovedKdht:     int64(result.RemovedKDHT),
			RemovedMetadata: int64(result.RemovedMetadata),
			RemovedCache:    int64(result.RemovedCache),
		}, nil
	}
	// Shallow clean: .kdht only. Count before removal for the response.
	kdhtPattern := filepath.Join(ks.StorageDir(), "data", "*.kdht")
	kdhtFiles, globErr := filepath.Glob(kdhtPattern)
	if globErr != nil {
		return nil, status.Errorf(codes.Internal, "glob kdht: %v", globErr)
	}
	if cleanErr := ks.CleanupKDHT(); cleanErr != nil {
		return nil, status.Errorf(codes.Internal, "cleanup kdht: %v", cleanErr)
	}
	logs.Infof("Clean complete: removed_kdht=%d", len(kdhtFiles))
	return &pb.CleanResponse{RemovedKdht: int64(len(kdhtFiles))}, nil
}

// Stats returns byte-level storage usage for the server's storage root.
func (s *Server) Stats(_ context.Context, _ *pb.StatsRequest) (*pb.StatsResponse, error) {
	ks, err := s.managedStore()
	if err != nil {
		return nil, err
	}
	storageDir := ks.StorageDir()
	entries, readErr := os.ReadDir(storageDir)
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, status.Errorf(codes.Internal, "read storage dir: %v", readErr)
	}
	var dataBytes, metaBytes, cacheBytes, otherBytes uint64
	for _, entry := range entries {
		entryPath := filepath.Join(storageDir, entry.Name())
		size, sizeErr := dirSize(entryPath)
		if sizeErr != nil {
			return nil, status.Errorf(codes.Internal, "stat %s: %v", entry.Name(), sizeErr)
		}
		switch entry.Name() {
		case "data":
			dataBytes += size
		case "metadata":
			metaBytes += size
		case ".cache":
			cacheBytes += size
		default:
			otherBytes += size
		}
	}
	summaries := s.storage.ListKnownFilesMetadata()
	logs.Debugf("Stats: data=%d meta=%d cache=%d files=%d", dataBytes, metaBytes, cacheBytes, len(summaries))
	return &pb.StatsResponse{
		DataBytes:     dataBytes,
		MetadataBytes: metaBytes,
		CacheBytes:    cacheBytes,
		TotalBytes:    dataBytes + metaBytes + cacheBytes + otherBytes,
		FileCount:     int64(len(summaries)),
	}, nil
}

// dirSize returns the total byte size of all files under path.
func dirSize(path string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Size() > 0 {
			total += uint64(info.Size())
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	return total, nil
}
