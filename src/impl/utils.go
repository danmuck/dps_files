package impl

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/gob"
	"io"
)

// compute computes and returns the key for obj
//
// obj: [bytes to compute]
//
// size: [20, 32, 64] size of hash in bytes
//
// note: defaults to 20 bytes for SHA1
func ComputeShaHash(obj []byte, size int) []byte {
	switch size {
	case sha1.Size:
		sha_1 := sha1.Sum(obj)
		return sha_1[:]
	case sha256.Size:
		sha_1 := sha256.Sum256(obj)
		return sha_1[:]
	case sha512.Size:
		sha_1 := sha512.Sum512(obj)
		return sha_1[:]
	default:
		sha_1 := sha1.Sum(obj)
		return sha_1[:]
	}
}

// Generate a nonce for Block creation
// note: random 64 bit unsigned integer
func secureNonce64() (uint64, error) {
	var nonce uint64
	err := binary.Read(rand.Reader, binary.LittleEndian, &nonce)
	if err != nil {
		return 0, err
	}
	return nonce, nil
}

// Helper function to calculate hash of a struct
// (excluding the Hash field if it is a Block)
//
//	any: [any type data]
//
// size: [20, 32, 64] (length of sha hash in bytes)
func CalculateHash(any interface{}, size int) ([]byte, error) {
	tmp := any
	block, ok := any.(Block)
	if ok {
		block.Hash = nil
		var buf bytes.Buffer
		encoder := gob.NewEncoder(&buf)
		err := encoder.Encode(block)
		if err != nil {
			return nil, err
		}

		hash := ComputeShaHash(buf.Bytes(), size)
		return hash, nil
	}

	// Serialize the struct
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	err := encoder.Encode(tmp)
	if err != nil {
		return nil, err
	}

	hash := ComputeShaHash(buf.Bytes(), size)
	return hash, nil
}

// Validate the hash of a Block by rehashing it
func ValidateHash(s interface{}, size int) bool {
	// Compute the expected hash
	expectedHash, err := CalculateHash(s, size)
	hash, ok := s.(Block)
	if !ok || err != nil {
		return false
	}
	// Compare the block's stored hash with the computed hash
	return bytes.Equal(hash.Hash, expectedHash)
}

// Encrypts data using AES-GCM
func EncryptData(key, plaintext []byte) (ciphertext, iv []byte, err error) {
	// Create a new AES cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}

	// Generate a random initialization vector
	iv = make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}

	// Use GCM mode for encryption
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}

	// Encrypt the data
	ciphertext = aesGCM.Seal(nil, iv, plaintext, nil)
	return ciphertext, iv, nil
}

// Decrypts data using AES-GCM
func DecryptData(key, ciphertext, iv []byte) ([]byte, error) {
	// Create a new AES cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// Use GCM mode for decryption
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Decrypt the data
	plaintext, err := aesGCM.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
