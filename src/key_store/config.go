package key_store

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const (
	KeySize       = 20      // 160 bits for kademlia dht routing
	HashSize      = 32      // 256 bits (sha-256) for data integrity
	CryptoSize    = 64      // 512 bits (sha-512) for security
	MinBlockSize  = 1 << 16 // 64kb minimum chunk
	MaxBlockSize  = 1 << 22 // 4mb maximum chunk
	TargetBlocks  = 1000    // aim for ~1000 chunks for large files
	FileExtension = ".kdht"
	PRINT_BLOCKS  = 500
	VERIFY        = false
)

// KeyStoreConfig controls runtime behavior of a KeyStore instance.
type KeyStoreConfig struct {
	StorageDir    string // root directory for chunk and metadata storage
	VerifyOnWrite bool   // when true, read-back and verify chunks immediately after writing
	Verbose       bool   // when true, emit progress output via fmt.Printf
}

// DefaultConfig returns a KeyStoreConfig with verbose output enabled
// and verify-on-write matching the VERIFY constant.
func DefaultConfig(storageDir string) KeyStoreConfig {
	return KeyStoreConfig{
		StorageDir:    storageDir,
		VerifyOnWrite: VERIFY,
		Verbose:       true,
	}
}

// chunkLoc maps a chunk key to its parent file and index within that file.
type chunkLoc struct {
	FileHash   [HashSize]byte
	ChunkIndex uint32
}

// this reciever must be implemented on the server side
// it needs to implement a channel that will first be initialized
// with a MetaData{}, followed by all FileReference{} for the file.
//
// each FileReference{} is passed just before it's []byte{} data
//
// these need to be processed and stored across the dht
// and removed from memory as the files are stored remotely
type RemoteHandler interface {

	// StartReceiver() takes a File MetaData and prepares to process
	// the file as it is passed block by block
	StartReceiver(md *MetaData)
	// PassFileReference() takes a FileReference pointer which acts
	// as a header for the data that will follow it
	PassFileReference(fr *FileReference, d []byte)
	// Receive() will need to be implemented in place of the current
	// default go-routine inside StartReceiver()
	// NOTE: it takes the place of make(chan interface{}) in a real impl
	Receive() <-chan any
}

type DefaultRemoteHandler struct {
	stream chan any
	ready  chan struct{} // signals receiver goroutine is ready
	mu     sync.Mutex
}

func (h *DefaultRemoteHandler) Receive() <-chan any {
	if h.stream == nil {
		h.stream = make(chan any)
	}
	return h.stream
}

func (h *DefaultRemoteHandler) StartReceiver(md *MetaData) {
	ch := h.Receive()
	h.ready = make(chan struct{})

	// This needs to be implemented to actually process the data.
	// Currently this serves as a placeholder that prints output
	// for testing purposes.
	go func() {
		blocks := md.TotalBlocks
		var index uint32 = 0
		close(h.ready) // signal that receiver is listening
		for data := range ch {
			switch tmp := data.(type) {
			case []byte:
				_ = tmp
				index++
				if index == blocks {
					fmt.Printf("Final Block Data Received: %d/%d \n", index, blocks)
					return
				}

			case *MetaData:
				fmt.Printf("MetaData: %+v \n", tmp)

			case *FileReference:
				// FileReference is a header; the next message will be []byte data
				fmt.Printf("FileReference: %d/%d \n", tmp.FileIndex+1, blocks)

			default:
				fmt.Printf("Hit Default Case??: %+v \n", tmp)
			}
		}
	}()

	// Wait for receiver goroutine to be ready before returning
	<-h.ready
}

func (h *DefaultRemoteHandler) PassFileReference(fr *FileReference, d []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stream <- fr
	h.stream <- d
}

func PrintMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("Alloc = %v MiB", m.Alloc/1024/1024)
	fmt.Printf("\tTotalAlloc = %v MiB", m.TotalAlloc/1024/1024)
	fmt.Printf("\tSys = %v MiB", m.Sys/1024/1024)
	fmt.Printf("\tNumGC = %v\n", m.NumGC)
}

// calculate optimal block size based on file size
func CalculateBlockSize(fileSize uint64) uint32 {
	if fileSize == 0 {
		return 0
	}

	// for small files, use the file size as the block size (single chunk)
	if fileSize < uint64(MinBlockSize) {
		return uint32(fileSize)
	}

	// calculate block size to achieve target number of blocks
	blockSize := fileSize / uint64(TargetBlocks)

	// round to nearest power of 2 for efficiency
	power := math.Log2(float64(blockSize))
	blockSize = uint64(math.Pow(2, math.Round(power)))

	// clamp to min/max sizes
	if blockSize < uint64(MinBlockSize) {
		return MinBlockSize
	}
	if blockSize > uint64(MaxBlockSize) {
		return MaxBlockSize
	}

	return uint32(blockSize)
}

func HashFile(filePath string) ([32]byte, int64, error) {
	// open the file
	file, err := os.Open(filePath)
	if err != nil {
		return [32]byte{}, 0, err
	}
	defer file.Close()

	// get the file size
	fileInfo, err := file.Stat()
	if err != nil {
		return [32]byte{}, 0, err
	}
	fileSize := fileInfo.Size()

	// create a new sha256 hash
	hasher := sha256.New()

	// read the file in chunks and update the hash
	_, err = io.Copy(hasher, file)
	if err != nil {
		return [32]byte{}, 0, err
	}

	// Convert the hash to [32]byte
	var hash [32]byte
	copy(hash[:], hasher.Sum(nil))

	// Return the hash and file size
	return hash, fileSize, nil

	// buf := make([]byte, 4096) // 4kb buffer
	// for {
	// 	n, err := file.Read(buf)
	// 	if err != nil && err != io.EOF {
	// 		return [32]byte{}, 0, err
	// 	}
	// 	if n == 0 {
	// 		break
	// 	}
	// 	hasher.Write(buf[:n]) // update hash with the read chunk
	// }

	// // convert the hash to [32]byte
	// var hash [32]byte
	// copy(hash[:], hasher.Sum(nil))

	// // return the hash and file size
	// return hash, fileSize, nil
}

func CopyFile(srcPath, dstPath string) error {
	// check if dstpath is a directory
	if fileInfo, err := os.Stat(dstPath); err == nil && fileInfo.IsDir() {
		// if it's a directory, append the source file's name to the destination path
		srcFileName := filepath.Base(srcPath)
		dstPath = filepath.Join(dstPath, srcFileName)
	}

	// open the source file
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// create or overwrite the destination file
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// copy the contents of the source file to the destination
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	return nil
}

func ValidateSHA256(a, b []byte) bool {
	hash1 := sha256.Sum256(a)
	hash2 := sha256.Sum256(b)
	return hash1 == hash2
}

// convert byte slice to fixed size array
func SliceToArray20(b []byte) ([KeySize]byte, error) {
	if len(b) != KeySize {
		return [KeySize]byte{}, fmt.Errorf("invalid hash length: got %d, want %d", len(b), KeySize)
	}
	var arr [KeySize]byte
	copy(arr[:], b)
	return arr, nil
}

// compute computes and returns the key for obj
func ShaCheckSum(obj []byte, bytes int) []byte {
	switch bytes {
	case KeySize:
		sha_1 := sha1.Sum(obj)
		return sha_1[:]
	case HashSize:
		sha_1 := sha256.Sum256(obj)
		return sha_1[:]
	case CryptoSize:
		sha_1 := sha512.Sum512(obj)
		return sha_1[:]
	default:
		sha_1 := sha1.Sum(obj)
		return sha_1[:]
	}
}
