package api

type Block interface{}
type Blockchain interface {

	// Load chain from a given filepath
	// encoding scheme independent
	// TOML, JSON, etc
	LoadChain(path string) error

	// Write chain to a given filepath
	// encoding scheme independent
	// TOML, JSON, etc
	WriteChain(path string) error

	// Validate the chain by hashing each block
	// and comparing it to the previous blocks hash
	ValidateChain() error

	// Sync the chain across the network
	// if using raft server, update the log
	Sync() error

	// Append an entry to the chain
	Append(data []byte) error

	// Find an entry by its key
	// 20 byte keys will find a File Reference
	// 32 byte keys will find a block by hash
	Find(key []byte) (*Block, error)

	// Save snapshot to disk
	ExportSnapshot() error

	// Rebuild chain from snapshot
	RebuildChain(path string) error
}
