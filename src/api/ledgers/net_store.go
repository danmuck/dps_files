package ledgers

type LogEntry struct {
	Index   uint64 // Log index
	Term    uint64 // Term in which the entry was added
	Command []byte // Command data
}

type LogManager interface {
	Append(entry LogEntry) error             // Append a new log entry
	GetEntry(index uint64) (LogEntry, error) // Retrieve a log entry
	LastLogIndex() uint64                    // Get the last log index
	Commit(index uint64) error               // Commit an entry
}

type MetadataStore interface {
	AddFile(fileID string, chunks []string) error // Add metadata for a file
	GetFile(fileID string) ([]string, error)      // Retrieve metadata for a file
	DeleteFile(fileID string) error               // Delete metadata for a file
	ListFiles() ([]string, error)                 // List all file IDs
}

// The Ledger is the local key-store as per the kademlia distribution
type FileLedger interface {
	VerifyReferences()        // Verify References
	FileToDisk()              // Store a file to disk
	FileFromDisk()            // Retrieve a file from disk
	FileToCache()             // Store a file in memory
	FileFromCache()           // Retrieve file from memory
	ListKnownFileReferences() // Get all locally stored file references
	Cleanup()                 // Remove all locally stored files and references
	ListKnownFiles()          // returns list of metadata that have partial references stored locally
}
