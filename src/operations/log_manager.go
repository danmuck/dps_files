package operations

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
