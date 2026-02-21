package key_store

import (
	"fmt"
	"io"

	"github.com/danmuck/dps_files/src/api/ledgers"
)

// KeyStoreLedger adapts *KeyStore to the ledgers.FileLedger interface.
type KeyStoreLedger struct {
	ks *KeyStore
}

func NewFileLedger(ks *KeyStore) *KeyStoreLedger {
	return &KeyStoreLedger{ks: ks}
}

func (l *KeyStoreLedger) VerifyReferences() error {
	errs := l.ks.VerifyAll()
	if len(errs) > 0 {
		return fmt.Errorf("verify found %d errors; first: %s", len(errs), errs[0].Err)
	}
	return nil
}

func (l *KeyStoreLedger) StoreFileLocal(name string, fileData []byte) (ledgers.FileID, error) {
	f, err := l.ks.StoreFileLocal(name, fileData)
	if err != nil {
		return ledgers.FileID{}, err
	}
	return ledgers.FileID(f.MetaData.FileHash), nil
}

func (l *KeyStoreLedger) LoadAndStoreFileLocal(localFilePath string) (ledgers.FileID, error) {
	f, err := l.ks.LoadAndStoreFileLocal(localFilePath)
	if err != nil {
		return ledgers.FileID{}, err
	}
	return ledgers.FileID(f.MetaData.FileHash), nil
}

func (l *KeyStoreLedger) LoadAndStoreFileRemote(localFilePath string, handler any) (ledgers.FileID, error) {
	rh, ok := handler.(RemoteHandler)
	if !ok {
		return ledgers.FileID{}, fmt.Errorf("handler must implement RemoteHandler")
	}
	f, err := l.ks.LoadAndStoreFileRemote(localFilePath, rh)
	if err != nil {
		return ledgers.FileID{}, err
	}
	return ledgers.FileID(f.MetaData.FileHash), nil
}

func (l *KeyStoreLedger) ReassembleFileToBytes(fileID ledgers.FileID) ([]byte, error) {
	return l.ks.ReassembleFileToBytes([HashSize]byte(fileID))
}

func (l *KeyStoreLedger) ReassembleFileToPath(fileID ledgers.FileID, outputPath string) error {
	return l.ks.ReassembleFileToPath([HashSize]byte(fileID), outputPath)
}

func (l *KeyStoreLedger) ListKnownFileReferences(fileID ledgers.FileID) ([]ledgers.ChunkID, error) {
	f, err := l.ks.GetFileByHash([HashSize]byte(fileID))
	if err != nil {
		return nil, err
	}
	chunks := make([]ledgers.ChunkID, len(f.References))
	for i, ref := range f.References {
		chunks[i] = ledgers.ChunkID(ref.Key)
	}
	return chunks, nil
}

func (l *KeyStoreLedger) ListKnownFiles() ([]ledgers.FileID, error) {
	metas := l.ks.ListKnownFiles()
	ids := make([]ledgers.FileID, len(metas))
	for i, m := range metas {
		ids[i] = ledgers.FileID(m.FileHash)
	}
	return ids, nil
}

func (l *KeyStoreLedger) Cleanup() error {
	return l.ks.Cleanup()
}

func (l *KeyStoreLedger) StoreFromReader(name string, r io.Reader, size uint64) (ledgers.FileID, error) {
	f, err := l.ks.StoreFromReader(name, r, size)
	if err != nil {
		return ledgers.FileID{}, err
	}
	return ledgers.FileID(f.MetaData.FileHash), nil
}

func (l *KeyStoreLedger) StreamFile(fileID ledgers.FileID, w io.Writer) error {
	return l.ks.StreamFile([HashSize]byte(fileID), w)
}

func (l *KeyStoreLedger) StreamFileByName(name string, w io.Writer) error {
	return l.ks.StreamFileByName(name, w)
}

func (l *KeyStoreLedger) DeleteFile(fileID ledgers.FileID) error {
	return l.ks.DeleteFile([HashSize]byte(fileID))
}

func (l *KeyStoreLedger) ListKnownFilesMetadata() []ledgers.FileMetaSummary {
	metas := l.ks.ListKnownFiles()
	summaries := make([]ledgers.FileMetaSummary, len(metas))
	for i, m := range metas {
		summaries[i] = ledgers.FileMetaSummary{
			Name: m.FileName,
			Hash: ledgers.FileID(m.FileHash),
			Size: m.TotalSize,
		}
	}
	return summaries
}
