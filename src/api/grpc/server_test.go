package grpcserver_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"

	grpcserver "github.com/danmuck/dps_files/src/api/grpc"
	"github.com/danmuck/dps_files/src/api/pb"
	"github.com/danmuck/dps_files/src/key_store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

func newTestServer(t *testing.T) pb.DPSFilesClient {
	t.Helper()
	dir := t.TempDir()
	ks, err := key_store.InitKeyStore(dir)
	if err != nil {
		t.Fatalf("init keystore: %v", err)
	}
	ledger := key_store.NewFileLedger(ks)

	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	pb.RegisterDPSFilesServer(srv, grpcserver.New(ledger))
	go srv.Serve(lis)
	t.Cleanup(func() { srv.GracefulStop() })

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return pb.NewDPSFilesClient(conn)
}

func TestUploadAndList(t *testing.T) {
	client := newTestServer(t)

	stream, err := client.Upload(context.Background())
	if err != nil {
		t.Fatalf("open upload stream: %v", err)
	}
	content := []byte("hello grpc world")
	if err := stream.Send(&pb.UploadChunk{Name: "hello.txt", Size: uint64(len(content))}); err != nil {
		t.Fatalf("send metadata chunk: %v", err)
	}
	if err := stream.Send(&pb.UploadChunk{Data: content}); err != nil {
		t.Fatalf("send data chunk: %v", err)
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("close upload: %v", err)
	}
	if len(resp.Hash) != 32 {
		t.Errorf("expected 32-byte hash, got %d", len(resp.Hash))
	}
	if resp.Name != "hello.txt" {
		t.Errorf("expected name hello.txt, got %s", resp.Name)
	}

	listResp, err := client.List(context.Background(), &pb.ListRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listResp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(listResp.Files))
	}
	if listResp.Files[0].Name != "hello.txt" {
		t.Errorf("expected hello.txt, got %s", listResp.Files[0].Name)
	}
}

func TestDownloadByHash(t *testing.T) {
	client := newTestServer(t)
	content := []byte("download me")

	stream, _ := client.Upload(context.Background())
	stream.Send(&pb.UploadChunk{Name: "dl.txt", Size: uint64(len(content))})
	stream.Send(&pb.UploadChunk{Data: content})
	resp, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	dl, err := client.Download(context.Background(), &pb.DownloadRequest{Hash: resp.Hash})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	var buf bytes.Buffer
	for {
		chunk, err := dl.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("recv chunk: %v", err)
		}
		buf.Write(chunk.Data)
	}
	if !bytes.Equal(buf.Bytes(), content) {
		t.Errorf("content mismatch: got %q, want %q", buf.Bytes(), content)
	}
}

func TestDeleteFile(t *testing.T) {
	client := newTestServer(t)
	content := []byte("delete me")

	stream, _ := client.Upload(context.Background())
	stream.Send(&pb.UploadChunk{Name: "todel.txt", Size: uint64(len(content))})
	stream.Send(&pb.UploadChunk{Data: content})
	resp, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	_, err = client.Delete(context.Background(), &pb.DeleteRequest{Hash: resp.Hash})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	listResp, _ := client.List(context.Background(), &pb.ListRequest{})
	if len(listResp.Files) != 0 {
		t.Errorf("expected 0 files after delete, got %d", len(listResp.Files))
	}
}

func TestVerifyEmpty(t *testing.T) {
	client := newTestServer(t)
	resp, err := client.Verify(context.Background(), &pb.VerifyRequest{})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(resp.Errors) != 0 {
		t.Errorf("expected no errors on empty store, got %d", len(resp.Errors))
	}
}

func TestExpireEmpty(t *testing.T) {
	client := newTestServer(t)
	resp, err := client.Expire(context.Background(), &pb.ExpireRequest{})
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if resp.Removed < 0 {
		t.Errorf("expected non-negative removed count, got %d", resp.Removed)
	}
}

func TestCleanShallow(t *testing.T) {
	client := newTestServer(t)
	stream, _ := client.Upload(context.Background())
	stream.Send(&pb.UploadChunk{Name: "toclean.txt", Size: 5})
	stream.Send(&pb.UploadChunk{Data: []byte("hello")})
	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatalf("upload: %v", err)
	}
	resp, err := client.Clean(context.Background(), &pb.CleanRequest{Deep: false})
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	if resp.RemovedKdht < 1 {
		t.Errorf("expected at least 1 kdht file removed, got %d", resp.RemovedKdht)
	}
	// Verify the List RPC still works after shallow clean.
	// Metadata entries persist after shallow clean; only raw .kdht chunk files are removed.
	listResp, err := client.List(context.Background(), &pb.ListRequest{})
	if err != nil {
		t.Fatalf("list after clean: %v", err)
	}
	_ = listResp // metadata entries persist after shallow clean; chunks are gone
}

func TestCleanDeep(t *testing.T) {
	client := newTestServer(t)
	stream, _ := client.Upload(context.Background())
	stream.Send(&pb.UploadChunk{Name: "todeep.txt", Size: 5})
	stream.Send(&pb.UploadChunk{Data: []byte("world")})
	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatalf("upload: %v", err)
	}
	resp, err := client.Clean(context.Background(), &pb.CleanRequest{Deep: true})
	if err != nil {
		t.Fatalf("deep clean: %v", err)
	}
	if resp.RemovedKdht < 1 {
		t.Errorf("expected at least 1 kdht file removed, got %d", resp.RemovedKdht)
	}
	if resp.RemovedMetadata < 1 {
		t.Errorf("expected at least 1 metadata file removed, got %d", resp.RemovedMetadata)
	}
	// After deep clean, List should return no files (metadata wiped and in-memory map reset).
	listResp, err := client.List(context.Background(), &pb.ListRequest{})
	if err != nil {
		t.Fatalf("list after deep clean: %v", err)
	}
	if len(listResp.Files) != 0 {
		t.Errorf("expected 0 files after deep clean, got %d", len(listResp.Files))
	}
}

func TestStats(t *testing.T) {
	client := newTestServer(t)
	resp, err := client.Stats(context.Background(), &pb.StatsRequest{})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if resp.TotalBytes < 0 {
		t.Errorf("unexpected negative total bytes")
	}
}
