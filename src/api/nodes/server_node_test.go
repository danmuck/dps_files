package nodes_test

import (
	"context"
	"testing"

	"github.com/danmuck/dps_files/src/api/nodes"
	"github.com/danmuck/dps_files/src/api/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func newTestServerNode(t *testing.T) (*nodes.DefaultServerNode, pb.DPSFilesClient) {
	t.Helper()
	id := make([]byte, 20)
	sn, err := nodes.NewServerNode(id, "localhost:0", t.TempDir())
	if err != nil {
		t.Fatalf("NewServerNode: %v", err)
	}
	if err := sn.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { sn.Shutdown() })

	conn, err := grpc.NewClient(sn.Addr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return sn, pb.NewDPSFilesClient(conn)
}

func TestServerNodeStartShutdown(t *testing.T) {
	_, _ = newTestServerNode(t)
}

func TestServerNodeUploadAndList(t *testing.T) {
	_, client := newTestServerNode(t)

	stream, err := client.Upload(context.Background())
	if err != nil {
		t.Fatalf("Upload open: %v", err)
	}
	data := []byte("server node test data")
	stream.Send(&pb.UploadChunk{Name: "node_test.txt", Size: uint64(len(data))})
	stream.Send(&pb.UploadChunk{Data: data})
	resp, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv: %v", err)
	}
	if len(resp.Hash) != 32 {
		t.Errorf("expected 32-byte hash, got %d", len(resp.Hash))
	}

	list, err := client.List(context.Background(), &pb.ListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Files) != 1 || list.Files[0].Name != "node_test.txt" {
		t.Errorf("unexpected list: %+v", list.Files)
	}
}
