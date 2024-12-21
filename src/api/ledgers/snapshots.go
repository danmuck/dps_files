package ledgers

// Snapshots represent ledger backups for a raft ledger
type Snapshot struct {
	Term     uint64
	Index    uint64
	Data     []byte // Serialized state
	Metadata map[string]string
}

// SnapshotManager is an interface for managing snapshots
// SERVER/RAFT NODE EXTENSION
type SnapshotManager interface {
	CreateSnapshot() (Snapshot, error)       // Create a snapshot
	PersistSnapshot(snapshot Snapshot) error // Persist to the blockchain ledger
	LoadSnapshot(snapshot Snapshot) error    // Load a snapshot into Raft state
	VerifySnapshot(hash string) error        // Verify a snapshot

}

// Entry represents a block in the blockchain
type BackupEntry interface{}

// The Ledger represents the blockchain backup system
// it is meant to be generic to use a non blockchain impl
type BackupLedger interface {
	// encoding scheme independent
	// TOML, JSON, etc
	Append(data []byte) error              // Append an entry to the chain
	Load(path string) error                // load ledger from filepath
	Write(path string) error               // write ledger to filepath
	Validate() error                       // validate ledger, blockchain hashes each block
	Sync() error                           // sync across raft cluster
	Rebuild(path string) error             // Rebuild chain from snapshot
	ExportSnapshot() error                 // Save snapshot to disk
	Find(key []byte) (*BackupEntry, error) // Find an entry by its key
	// 20 byte keys will find a File Reference
	// 32 byte keys will find a block by hash
}
