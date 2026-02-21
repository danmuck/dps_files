package nodes_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/danmuck/dps_files/src/api/nodes"
	"github.com/danmuck/dps_files/src/api/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func newTestClient(t *testing.T, opts ...nodes.ClientOption) *nodes.DefaultClientNode {
	t.Helper()
	id := make([]byte, 20)
	cn, err := nodes.NewClientNode(id, opts...)
	if err != nil {
		t.Fatalf("NewClientNode: %v", err)
	}
	if err := cn.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { cn.Shutdown() })
	return cn
}

// TestClientLocalMode_OperationsGoThroughGRPC verifies that even in local mode,
// all operations use gRPC to the embedded ServerNode.
func TestClientLocalMode_OperationsGoThroughGRPC(t *testing.T) {
	cn := newTestClient(t, nodes.WithLocalStorage(t.TempDir()))

	// The client must have an active gRPC conn (to the local server).
	stub, err := cn.Stub()
	if err != nil {
		t.Fatalf("Stub: %v", err)
	}

	// List via gRPC stub directly — should succeed.
	resp, err := stub.List(context.Background(), &pb.ListRequest{})
	if err != nil {
		t.Fatalf("List via stub: %v", err)
	}
	if len(resp.Files) != 0 {
		t.Errorf("expected empty list, got %d", len(resp.Files))
	}
}

// TestClientNoServer_ReturnsError verifies that a client with no server
// (no local storage, no remotes) returns a clear error on any operation.
func TestClientNoServer_ReturnsError(t *testing.T) {
	id := make([]byte, 20)
	cn, err := nodes.NewClientNode(id)
	if err != nil {
		t.Fatalf("NewClientNode: %v", err)
	}
	if err := cn.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer cn.Shutdown()

	_, err = cn.Stub()
	if err == nil {
		t.Error("expected error from Stub() with no server configured")
	}
}

// TestClientRemoteMode_ConnectsToRemoteServer verifies the client can connect
// to a standalone ServerNode and perform operations over gRPC.
func TestClientRemoteMode_ConnectsToRemoteServer(t *testing.T) {
	// Start a standalone server.
	sn, _ := newTestServerNode(t)

	// Client in remote-only mode (no local storage).
	cn := newTestClient(t, nodes.WithRemotes(sn.Addr()))

	stub, err := cn.Stub()
	if err != nil {
		t.Fatalf("Stub: %v", err)
	}

	// Upload a file via the stub.
	tmp := filepath.Join(t.TempDir(), "remote.txt")
	if err := os.WriteFile(tmp, []byte("remote grpc test"), 0o644); err != nil {
		t.Fatal(err)
	}

	stream, err := stub.Upload(context.Background())
	if err != nil {
		t.Fatalf("Upload stream: %v", err)
	}
	content, _ := os.ReadFile(tmp)
	stream.Send(&pb.UploadChunk{Name: "remote.txt", Size: uint64(len(content))})
	stream.Send(&pb.UploadChunk{Data: content})
	resp, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv: %v", err)
	}
	if len(resp.Hash) != 32 {
		t.Errorf("expected 32-byte hash, got %d", len(resp.Hash))
	}

	// List should show the file.
	list, err := stub.List(context.Background(), &pb.ListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Files) != 1 || list.Files[0].Name != "remote.txt" {
		t.Errorf("unexpected list: %+v", list.Files)
	}
}

// TestClientLocalMode_LocalServerAccessible verifies LocalServer() exposes the
// embedded ServerNode for TUI operations (RawKeyStore access).
func TestClientLocalMode_LocalServerAccessible(t *testing.T) {
	cn := newTestClient(t, nodes.WithLocalStorage(t.TempDir()))
	if cn.LocalServer() == nil {
		t.Error("expected LocalServer() to return non-nil in local mode")
	}
	if cn.LocalServer().RawKeyStore() == nil {
		t.Error("expected RawKeyStore() to return non-nil")
	}
}

// TestClientRemoteOnly_NoLocalServer verifies LocalServer() is nil in remote-only mode.
func TestClientRemoteOnly_NoLocalServer(t *testing.T) {
	sn, _ := newTestServerNode(t)
	cn := newTestClient(t, nodes.WithRemotes(sn.Addr()))
	if cn.LocalServer() != nil {
		t.Error("expected LocalServer() nil in remote-only mode")
	}
}

// TestClientDialBack verifies the client can also be created pointing at the
// local server using grpc.NewClient, which is what local mode does internally.
func TestClientDialBack(t *testing.T) {
	// Start server separately.
	sn, _ := newTestServerNode(t)

	// Dial it with a raw grpc.ClientConn.
	conn, err := grpc.NewClient(sn.Addr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	stub := pb.NewDPSFilesClient(conn)
	_, err = stub.List(context.Background(), &pb.ListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
}
