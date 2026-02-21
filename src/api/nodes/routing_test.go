package nodes

import (
	"math/rand"
	"testing"
)

func generateTestKey() []byte {
	b := make([]byte, 20)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	return b
}

func TestNewDefaultNode(t *testing.T) {
	node, err := NewDefaultNode(generateTestKey(), "localhost:0")
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
}

func TestDefaultRouterCreation(t *testing.T) {
	node, err := NewDefaultNode(generateTestKey(), "localhost:0")
	if err != nil {
		t.Fatalf("NewDefaultNode failed: %v", err)
	}

	_, ok := node.Router.(*DefaultRouter)
	if !ok {
		t.Fatal("Router is not a DefaultRouter")
	}
}
