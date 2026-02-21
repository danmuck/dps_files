package grpcserver

import (
	"context"
	"fmt"
	"io"

	"github.com/danmuck/dps_files/src/api/ledgers"
	"github.com/danmuck/dps_files/src/api/pb"
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
	recvErr := <-errCh

	if storeErr != nil {
		return status.Errorf(codes.Internal, "store: %v", storeErr)
	}
	if recvErr != nil {
		return status.Errorf(codes.Internal, "receive: %v", recvErr)
	}

	return stream.SendAndClose(&pb.UploadResponse{
		Hash: fid[:],
		Name: name,
		Size: size,
	})
}

// Download streams a stored file to the client in 1 MiB chunks.
func (s *Server) Download(req *pb.DownloadRequest, stream pb.DPSFiles_DownloadServer) error {
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

	buf := make([]byte, downloadChunkSize)
	for {
		n, err := pr.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&pb.DataChunk{Data: buf[:n]}); sendErr != nil {
				pr.CloseWithError(sendErr)
				<-errCh
				return sendErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			<-errCh
			return status.Errorf(codes.Internal, "read: %v", err)
		}
	}
	if err := <-errCh; err != nil {
		return status.Errorf(codes.NotFound, "stream: %v", err)
	}
	return nil
}

// Delete removes a file by its 32-byte SHA-256 hash.
func (s *Server) Delete(_ context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	if len(req.Hash) != 32 {
		return nil, status.Errorf(codes.InvalidArgument, "hash must be 32 bytes, got %d", len(req.Hash))
	}
	var fid ledgers.FileID
	copy(fid[:], req.Hash)
	if err := s.storage.DeleteFile(fid); err != nil {
		return nil, status.Errorf(codes.NotFound, "delete: %v", err)
	}
	return &pb.DeleteResponse{}, nil
}

// List returns metadata for all stored files and directories.
func (s *Server) List(_ context.Context, _ *pb.ListRequest) (*pb.ListResponse, error) {
	summaries := s.storage.ListKnownFilesMetadata()
	entries := make([]*pb.FileEntry, len(summaries))
	for i, sm := range summaries {
		entries[i] = &pb.FileEntry{
			Name:      sm.Name,
			Hash:      sm.Hash[:],
			Size:      sm.Size,
			EntryType: "file",
		}
	}
	return &pb.ListResponse{Files: entries}, nil
}

// UploadDir stores a directory tree that exists on the server's local filesystem.
func (s *Server) UploadDir(_ context.Context, req *pb.UploadDirRequest) (*pb.UploadDirResponse, error) {
	if req.RootPath == "" {
		return nil, status.Error(codes.InvalidArgument, "root_path is required")
	}
	fid, err := s.storage.StoreDirectory(req.RootPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "store directory: %v", err)
	}
	return &pb.UploadDirResponse{Hash: fid[:]}, nil
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
		return nil, status.Errorf(codes.NotFound, "list directory: %v", err)
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
