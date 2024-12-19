package impl

import (
	"fmt"
	"time"
)

type Block struct {
	Hash     []byte
	PrevHash []byte
	Index    uint64

	data  BlockData
	time  int64
	nonce uint64
}

func NewBlock(index uint64, data []byte, prev []byte) *Block {
	nonce, err := secureNonce64()
	if err != nil {
		return nil
	}

	b := &Block{
		Hash:     nil,
		PrevHash: prev,
		Index:    index,

		data: BlockData{},

		nonce: nonce,
		time:  time.Now().UnixNano(),
	}
	fmt.Printf("\n\nHashing New Block: \n  data: %s \n\n", data)
	_, err = b.hashBlock(data)
	if err != nil {
		fmt.Printf("error: %v \n", err.Error())
	}
	return b
}

func (b *Block) hashBlock(data []byte) ([]byte, error) {
	fmt.Printf("Hashing Block %x: \n", b.Hash)
	fmt.Printf("Hashing data ... \n")
	b.data.Hash = ComputeShaHash(data, 20)
	b.data.Data = data
	b.data.IV = nil

	fmt.Printf("Hashing block ... \n")
	hash, err := CalculateHash(b, 32)
	if err != nil {
		return nil, err
	}
	b.Hash = hash
	fmt.Printf("Hash: %v \n", b.Hash)
	return hash, nil
}

// PrintChain prints the blockchain to the terminal.
func (b *Block) Print() {
	fmt.Printf("  Hash %x: \n", b.Hash)
	fmt.Printf("    Index: %d \n", b.Index)
	fmt.Printf("    PrevHash: %x \n", b.PrevHash)
	fmt.Printf("    Hash: %x \n", b.Hash)
	fmt.Printf("    Data: [%s] \n", b.data.String())
	fmt.Printf("    Timestamp: %d \n", b.time)
	fmt.Printf("    Nonce: %d \n", b.nonce)
	fmt.Println()
}

// Encrypted Mirrors

func NewBlockEncrypt(index uint64, data []byte, prev []byte, key []byte) *Block {
	nonce, err := secureNonce64()
	if err != nil {
		return nil
	}

	b := &Block{
		Hash:     nil,
		PrevHash: prev,
		Index:    index,

		data: BlockData{},

		nonce: nonce,
		time:  time.Now().UnixNano(),
	}
	fmt.Printf("\n\nHashing New Block: \n  data: %s \n  key: %s \n\n", data, key)
	_, err = b.hashBlockEncrypt(data, key)
	if err != nil {
		fmt.Printf("error: %v \n", err.Error())
	}
	return b
}

func (b *Block) hashBlockEncrypt(data []byte, key []byte) ([]byte, error) {
	fmt.Printf("Hashing Block %x: \n", b.Hash)
	fmt.Printf("Encrypting data ... \n")
	encryptedData, iv, err := EncryptData(key[:], data)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Hashing data ... \n")

	b.data.Hash = ComputeShaHash(encryptedData, 20)
	b.data.Data = encryptedData
	b.data.IV = iv

	fmt.Printf("Hashing block ... \n")
	hash, err := CalculateHash(b, 32)
	if err != nil {
		return nil, err
	}
	b.Hash = hash
	fmt.Printf("Hash: %v \n", b.Hash)
	return hash, nil
}

// PrintChain prints the blockchain to the terminal decrypted.
func (b *Block) PrintDecrypt(key []byte) {
	fmt.Printf("  Hash %x: \n", b.Hash)
	fmt.Printf("    Index: %d \n", b.Index)
	fmt.Printf("    PrevHash: %x \n", b.PrevHash)
	fmt.Printf("    Hash: %x \n", b.Hash)
	fmt.Printf("    Data: [%s] \n", b.data.StringDecrypt(key)) // Assuming decrypted data is available
	fmt.Printf("    Timestamp: %d \n", b.time)
	fmt.Printf("    Nonce: %d \n", b.nonce)
	fmt.Println()
}
