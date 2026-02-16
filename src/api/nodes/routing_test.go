package nodes

import (
	"math/rand"
	"testing"
	"time"
)

func generateTestKey() []byte {
	b := make([]byte, 20)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	return b
}

func TestNewDefaultNode(t *testing.T) {
	node, err := NewDefaultNode(generateTestKey(), "localhost:0", 5, 3)
	if err != nil {
		t.Fatalf("NewDefaultNode failed: %v", err)
	}

	if node.Address() != "localhost:0" {
		t.Errorf("Expected address localhost:0, got %s", node.Address())
	}
	if len(node.ID()) != 20 {
		t.Errorf("Expected 20-byte ID, got %d bytes", len(node.ID()))
	}
	if node.Router == nil {
		t.Error("Router is nil")
	}
	if node.TCPHandler == nil {
		t.Error("TCPHandler is nil")
	}
}

func TestNewDefaultNodeBadID(t *testing.T) {
	_, err := NewDefaultNode([]byte{1, 2, 3}, "localhost:0", 5, 3)
	if err == nil {
		t.Error("Expected error for bad node ID, got nil")
	}
}

func TestDefaultNodeStartShutdown(t *testing.T) {
	node, err := NewDefaultNode(generateTestKey(), "localhost:0", 5, 3)
	if err != nil {
		t.Fatalf("NewDefaultNode failed: %v", err)
	}

	if err := node.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	if err := node.Shutdown(); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}

func TestKademliaRouterCreation(t *testing.T) {
	node, err := NewDefaultNode(generateTestKey(), "localhost:0", 20, 3)
	if err != nil {
		t.Fatalf("NewDefaultNode failed: %v", err)
	}

	router, ok := node.Router.(*KademliaRouter)
	if !ok {
		t.Fatal("Router is not a KademliaRouter")
	}

	if router.k != 20 {
		t.Errorf("Expected k=20, got %d", router.k)
	}
	if router.a != 3 {
		t.Errorf("Expected a=3, got %d", router.a)
	}
}
