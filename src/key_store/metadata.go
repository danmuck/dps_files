package key_store

import (
	"time"
)

type MetaData struct {
	FileHash    [HashSize]byte   `toml:"file_hash"`
	TotalSize   uint64           `toml:"total_size"`
	FileName    string           `toml:"file_name"`
	Modified    int64            `toml:"modified"`
	Permissions uint32           `toml:"permissions"`
	Signature   [CryptoSize]byte `toml:"signature"`
	TTL         uint64           `toml:"ttl"`
	BlockSize   uint32           `toml:"chunk_size"`
	TotalBlocks uint32           `toml:"total_chunks"`
	EntryType   string           `toml:"entry_type,omitempty"`  // "file" (default/empty) or "directory"
	ParentHash  [HashSize]byte   `toml:"parent_hash,omitempty"` // hash of parent directory manifest; zero for root
}

// IsDirectory returns true if this entry is a directory manifest.
func (md MetaData) IsDirectory() bool {
	return md.EntryType == "directory"
}

func PrepareMetaData(name string, data []byte) (metadata MetaData, e error) {
	metadata.TotalSize = uint64(len(data))
	metadata.TTL = DefaultFileTTLSeconds
	metadata.FileName = name
	metadata.Modified = time.Now().UnixNano()
	metadata.Permissions = DEFAULT_PERMISSIONS
	metadata.BlockSize = CalculateBlockSize(metadata.TotalSize)

	// calculate total chunks with proper rounding up
	if metadata.BlockSize > 0 {
		metadata.TotalBlocks = uint32((metadata.TotalSize + uint64(metadata.BlockSize) - 1) / uint64(metadata.BlockSize))
	}

	return metadata, nil
}
