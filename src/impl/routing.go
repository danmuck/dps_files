package impl

type RoutingTable interface {
	Store(chunkID string, data []byte) error // Store a chunk
	Retrieve(chunkID string) ([]byte, error) // Retrieve a chunk
	Delete(chunkID string) error             // Delete a chunk
}
