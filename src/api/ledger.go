package api

// The Ledger is the local key-store as per the kademlia distribution
type Ledger interface {

	// Create and store a snapshot on the blockchain
	StoreSnapshot(snapshot []byte) (string, error)

	// Verify a snapshot
	VerifySnapshot(hash string) error

	// Verify References
	VerifyReferences()

	// Store a file to disk
	FileToDisk()

	// Retrieve a file from disk
	FileFromDisk()

	// Store a file in memory
	FileToCache()

	// Retrieve file from memory
	FileFromCache()

	// Get all locally stored file references
	ListKnownFileReferences()

	// Get all known file references
	// this will be a list of metadata that have
	// references stored locally
	ListKnownFiles()

	// Remove all locally stored files and references
	Cleanup()
}
