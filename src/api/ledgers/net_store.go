package ledgers

import "io"

type FileID [32]byte
type ChunkID [20]byte

type LogEntry struct {
	Index   uint64
	Term    uint64
	Command []byte
}

type FileMetaSummary struct {
	Name string `json:"name"`
	Hash FileID `json:"hash"`
	Size uint64 `json:"size"`
}

type LogManager interface {
	Append(entry LogEntry) error
	GetEntry(index uint64) (LogEntry, error)
	LastLogIndex() uint64
	Commit(index uint64) error
}

type MetadataStore interface {
	UpsertFile(fileID FileID, chunks []ChunkID) error
	GetFile(fileID FileID) ([]ChunkID, error)
	DeleteFile(fileID FileID) error
	ListFiles() ([]FileID, error)
}

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

	// Streaming operations for ServerNode
	StoreFromReader(name string, r io.Reader, size uint64) (FileID, error)
	StreamFile(fileID FileID, w io.Writer) error
	StreamFileByName(name string, w io.Writer) error
	DeleteFile(fileID FileID) error
	ListKnownFilesMetadata() []FileMetaSummary
}
