package ledgers

// FileID is the canonical typed file identifier (SHA-256).
type FileID [32]byte

// ChunkID is the canonical typed chunk identifier (SHA-1 routing key).
type ChunkID [20]byte

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
	UpsertFile(fileID FileID, chunks []ChunkID) error // Add/update metadata for a file
	GetFile(fileID FileID) ([]ChunkID, error)         // Retrieve metadata for a file
	DeleteFile(fileID FileID) error                   // Delete metadata for a file
	ListFiles() ([]FileID, error)                     // List all known file IDs
}

// The Ledger is the local key-store as per the kademlia distribution
type FileLedger interface {
	VerifyReferences() error
	StoreFileLocal(name string, fileData []byte) (FileID, error)
	LoadAndStoreFileLocal(localFilePath string) (FileID, error)
	LoadAndStoreFileRemote(localFilePath string, handler any) (FileID, error)
	ReassembleFileToBytes(fileID FileID) ([]byte, error)
	ReassembleFileToPath(fileID FileID, outputPath string) error
	ListKnownFileReferences(fileID FileID) ([]ChunkID, error)
	ListKnownFiles() ([]FileID, error)
	Cleanup() error
}
