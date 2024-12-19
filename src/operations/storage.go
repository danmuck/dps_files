package operations

type MetadataStore interface {
	AddFile(fileID string, chunks []string) error // Add metadata for a file
	GetFile(fileID string) ([]string, error)      // Retrieve metadata for a file
	DeleteFile(fileID string) error               // Delete metadata for a file
	ListFiles() ([]string, error)                 // List all file IDs
}
