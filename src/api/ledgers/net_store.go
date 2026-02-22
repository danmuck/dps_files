package ledgers

import "io"

type FileID [32]byte
type ChunkID [20]byte

type FileMetaSummary struct {
	Name      string `json:"name"`
	Hash      FileID `json:"hash"`
	Size      uint64 `json:"size"`
	EntryType  string `json:"entry_type,omitempty"`
	ParentHash FileID `json:"parent_hash,omitempty"`
}

// DirectoryEntry represents a child in a directory manifest.
type DirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Hash FileID `json:"hash"`
	Type string `json:"type"`
	Size uint64 `json:"size"`
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

	// Directory operations
	StoreDirectory(rootPath string) (FileID, error)
	StoreDirectoryManifest(manifestJSON []byte) (FileID, error)
	ListDirectory(dirID FileID) ([]DirectoryEntry, error)
	ReassembleDirectory(dirID FileID, outputRoot string) error
}
