package operations

type Message struct {
	From    string
	To      string
	Type    string
	Payload []byte
}

type Transport interface {
	Send(message Message) error                 // Send a message to another node
	Receive() (Message, error)                  // Receive a message
	RegisterNode(nodeID string, address string) // Register a node
}
