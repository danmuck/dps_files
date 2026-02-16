package transport

import (
	"net"
	"testing"
	"time"
)

func TestTCPHandlerListenAndAccept(t *testing.T) {
	exit := make(chan any)
	handler := NewTCPHandler("localhost:0", exit)

	if err := handler.ListenAndAccept(); err != nil {
		t.Fatalf("ListenAndAccept failed: %v", err)
	}

	// Verify we can connect to it
	addr := handler.listener.Addr().String()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect to handler: %v", err)
	}
	conn.Close()

	// Clean shutdown
	close(exit)
	time.Sleep(600 * time.Millisecond) // wait for accept loop deadline
	handler.Close()
}

func TestTCPHandlerSendReceive(t *testing.T) {
	exit := make(chan any)
	handler := NewTCPHandler("localhost:0", exit)

	if err := handler.ListenAndAccept(); err != nil {
		t.Fatalf("ListenAndAccept failed: %v", err)
	}

	addr := handler.listener.Addr().String()

	// Connect a client
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send an RPC from the client side
	rpc := &RPC{
		Meta: &RPCT{
			Protocol: Protocol_Kademlia,
			Command:  Command_PING,
		},
		Sender: &NodeInfo{
			Address: "localhost:9999",
			Id:      []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
		},
		Payload: []byte("hello"),
	}

	coder := DefaultCoder{}
	encoded, err := coder.Encode(rpc)
	if err != nil {
		t.Fatalf("Failed to encode RPC: %v", err)
	}

	if _, err := conn.Write(encoded); err != nil {
		t.Fatalf("Failed to write RPC: %v", err)
	}

	// Read from inbound channel
	select {
	case received := <-handler.ProcessRPC():
		if received == nil {
			t.Fatal("Received nil RPC")
		}
		if received.Meta.Command != Command_PING {
			t.Errorf("Expected PING command, got %v", received.Meta.Command)
		}
		if string(received.Payload) != "hello" {
			t.Errorf("Expected payload 'hello', got '%s'", received.Payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for RPC")
	}

	close(exit)
	time.Sleep(600 * time.Millisecond)
	handler.Close()
}
