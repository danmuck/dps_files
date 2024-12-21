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
	// for small files, use minimum chunk size
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
