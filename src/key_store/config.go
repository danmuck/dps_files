package key_store

import (
	"crypto/sha256"
	"io"
	"math"
	"os"
	"path/filepath"
)

const (
	KeySize                      = 20           // 160 bits for kademlia dht routing
	HashSize                     = 32           // 256 bits (sha-256) for data integrity
	CryptoSize                   = 64           // 512 bits (sha-512) for security
	DefaultFileTTLSeconds uint64 = 24 * 60 * 60 // 24h
	MinBlockSize                 = 1 << 16      // 64kb minimum chunk
	MaxBlockSize                 = 1 << 22      // 4mb maximum chunk
	TargetBlocks                 = 1000         // aim for ~1000 chunks for large files
	LargeFileMx                  = 128          // multiplier for defining large filesize (MaxBlockSize * LargeFileMx)
	FileExtension                = ".kdht"
)

// KeyStoreConfig controls runtime behavior of a KeyStore instance.
type KeyStoreConfig struct {
	StorageDir        string // root directory for chunk and metadata storage
	VerifyOnWrite     bool   // when true, read-back and verify chunks immediately after writing
	Verbose           bool   // when true, emit progress output via fmt.Printf
	DefaultTTLSeconds uint64 // default TTL for newly stored files
}

// DefaultConfig returns a KeyStoreConfig with verbose output enabled
// and verify-on-write disabled by default.
func DefaultConfig(storageDir string) KeyStoreConfig {
	return KeyStoreConfig{
		StorageDir:        storageDir,
		VerifyOnWrite:     false,
		Verbose:           true,
		DefaultTTLSeconds: DefaultFileTTLSeconds,
	}
}

// chunkLoc maps a chunk key to its parent file and index within that file.
type chunkLoc struct {
	FileHash   [HashSize]byte
	ChunkIndex uint32
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

	// apply medium-file promotion while preserving large-file regular sizing.
	blockSize = promoteCandidateBlockSize(fileSize, blockSize)

	// clamp to min/max sizes
	if blockSize < uint64(MinBlockSize) {
		return MinBlockSize
	}
	if blockSize > uint64(MaxBlockSize) {
		return MaxBlockSize
	}

	return uint32(blockSize)
}

// promoteCandidateBlockSize applies medium-file promotion to MaxBlockSize while
// skipping large files above MaxBlockSize*LargeFileMx.
//
// The candidate is expected to come from the regular sizing calculation path.
func promoteCandidateBlockSize(fileSize uint64, candidateBlockSize uint64) uint64 {
	if fileSize == 0 {
		return 0
	}

	if fileSize <= uint64(MaxBlockSize) {
		return candidateBlockSize
	}

	largeThreshold := uint64(MaxBlockSize) * uint64(LargeFileMx)
	if fileSize > largeThreshold {
		return candidateBlockSize
	}

	maxBlocks := (fileSize + uint64(MaxBlockSize) - 1) / uint64(MaxBlockSize)
	if maxBlocks <= uint64(TargetBlocks) {
		promoted := max(candidateBlockSize, uint64(MaxBlockSize))
		if promoted > uint64(MaxBlockSize) {
			return uint64(MaxBlockSize)
		}
		return promoted
	}

	return candidateBlockSize
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
