package operations

type Snapshot struct {
	Term     uint64
	Index    uint64
	Data     []byte // Serialized state
	Metadata map[string]string
}

type SnapshotManager interface {
	CreateSnapshot() (Snapshot, error)       // Create a snapshot
	PersistSnapshot(snapshot Snapshot) error // Persist to the blockchain ledger
	LoadSnapshot(snapshot Snapshot) error    // Load a snapshot into Raft state
}
