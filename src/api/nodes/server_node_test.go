package nodes

import (
	"os"
	"testing"
	"time"

	"github.com/danmuck/dps_files/src/api/transport"
)

func TestServerNode_StartAndShutdown(t *testing.T) {
	dir, _ := os.MkdirTemp("", "sn-test-*")
	defer os.RemoveAll(dir)

	sn, err := NewServerNode([]byte("test-server-node-id!"), "localhost:0", dir)
	if err != nil {
		t.Fatal(err)
	}
	// Compile-time interface check.
	var _ ServerNode = sn

	if err := sn.Start(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	if sn.Storage() == nil {
		t.Fatal("expected non-nil storage")
	}

	if err := sn.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestServerNode_HandleRPC_Ping(t *testing.T) {
	dir, _ := os.MkdirTemp("", "sn-test-*")
	defer os.RemoveAll(dir)

	sn, err := NewServerNode([]byte("test-server-node-id!"), "localhost:0", dir)
	if err != nil {
		t.Fatal(err)
	}

	rpc := &transport.RPC{
		Meta:   &transport.RPCT{Command: transport.Command_PING},
		Sender: &transport.NodeInfo{Address: "localhost:9999"},
	}
	resp, err := sn.HandleRPC(rpc)
	if err != nil {
		t.Fatalf("HandleRPC PING: %v", err)
	}
	if resp.Meta.Command != transport.Command_ACK {
		t.Fatalf("expected ACK, got %v", resp.Meta.Command)
	}
}

func TestServerNode_HandleRPC_StoreAndList(t *testing.T) {
	dir, _ := os.MkdirTemp("", "sn-test-*")
	defer os.RemoveAll(dir)

	sn, err := NewServerNode([]byte("test-server-node-id!"), "localhost:0", dir)
	if err != nil {
		t.Fatal(err)
	}

	// Upload
	uploadRPC := &transport.RPC{
		Meta:  &transport.RPCT{Command: transport.Command_UPLOAD},
		Key:   []byte("test.txt"),
		Value: []byte("hello server node"),
	}
	resp, err := sn.HandleRPC(uploadRPC)
	if err != nil {
		t.Fatalf("HandleRPC UPLOAD: %v", err)
	}
	if resp.Meta.Command != transport.Command_ACK {
		t.Fatalf("expected ACK, got %v", resp.Meta.Command)
	}
	if len(resp.Key) != 32 {
		t.Fatalf("expected 32-byte file hash, got %d bytes", len(resp.Key))
	}

	// List
	listRPC := &transport.RPC{
		Meta: &transport.RPCT{Command: transport.Command_LIST},
	}
	listResp, err := sn.HandleRPC(listRPC)
	if err != nil {
		t.Fatalf("HandleRPC LIST: %v", err)
	}
	if len(listResp.Payload) == 0 {
		t.Fatal("expected non-empty payload for LIST")
	}

	// Download
	downloadRPC := &transport.RPC{
		Meta: &transport.RPCT{Command: transport.Command_DOWNLOAD},
		Key:  resp.Key,
	}
	dlResp, err := sn.HandleRPC(downloadRPC)
	if err != nil {
		t.Fatalf("HandleRPC DOWNLOAD: %v", err)
	}
	if string(dlResp.Value) != "hello server node" {
		t.Fatalf("expected 'hello server node', got %q", dlResp.Value)
	}

	// Delete
	deleteRPC := &transport.RPC{
		Meta: &transport.RPCT{Command: transport.Command_DELETE},
		Key:  resp.Key,
	}
	delResp, err := sn.HandleRPC(deleteRPC)
	if err != nil {
		t.Fatalf("HandleRPC DELETE: %v", err)
	}
	if delResp.Meta.Command != transport.Command_ACK {
		t.Fatalf("expected ACK, got %v", delResp.Meta.Command)
	}
}

func TestServerNode_HandleRPC_UnhandledCommand(t *testing.T) {
	dir, _ := os.MkdirTemp("", "sn-test-*")
	defer os.RemoveAll(dir)

	sn, err := NewServerNode([]byte("test-server-node-id!"), "localhost:0", dir)
	if err != nil {
		t.Fatal(err)
	}

	rpc := &transport.RPC{
		Meta: &transport.RPCT{Command: transport.Command_FIND_NODE},
	}
	_, err = sn.HandleRPC(rpc)
	if err == nil {
		t.Fatal("expected error for unhandled command")
	}
}
