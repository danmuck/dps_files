package impl

import (
	"fmt"
	"time"
)

type Block struct {
	Hash     []byte
	PrevHash []byte
	Index    uint64

	Data  BlockData
	Time  int64
	Nonce uint64
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

		Data: BlockData{},

		Nonce: nonce,
		Time:  time.Now().UnixNano(),
	}
	_, err = b.hashBlock(data)
	if err != nil {
		fmt.Printf("NewBlock error: %v \n", err.Error())
	}
	return b
}

func (b *Block) hashBlock(data []byte) ([]byte, error) {
	b.Data.Hash = ComputeShaHash(data, 20)
	b.Data.Data = data
	b.Data.IV = nil

	hash, err := CalculateHash(b, 32)
	if err != nil {
		return nil, err
	}
	b.Hash = hash
	return hash, nil
}

// Print prints the block to the terminal.
func (b *Block) Print() {
	fmt.Printf("  Hash %x: \n", b.Hash)
	fmt.Printf("    Index: %d \n", b.Index)
	fmt.Printf("    PrevHash: %x \n", b.PrevHash)
	fmt.Printf("    Data: [%s] \n", b.Data.String())
	fmt.Printf("    Timestamp: %d \n", b.Time)
	fmt.Printf("    Nonce: %d \n", b.Nonce)
	fmt.Println()
}

// NewBlockEncrypt creates a new block with AES-GCM encrypted data.
func NewBlockEncrypt(index uint64, data []byte, prev []byte, key []byte) *Block {
	nonce, err := secureNonce64()
	if err != nil {
		return nil
	}

	b := &Block{
		Hash:     nil,
		PrevHash: prev,
		Index:    index,

		Data: BlockData{},

		Nonce: nonce,
		Time:  time.Now().UnixNano(),
	}
	_, err = b.hashBlockEncrypt(data, key)
	if err != nil {
		fmt.Printf("NewBlockEncrypt error: %v \n", err.Error())
	}
	return b
}

func (b *Block) hashBlockEncrypt(data []byte, key []byte) ([]byte, error) {
	encryptedData, iv, err := EncryptData(key[:], data)
	if err != nil {
		return nil, err
	}

	b.Data.Hash = ComputeShaHash(encryptedData, 20)
	b.Data.Data = encryptedData
	b.Data.IV = iv

	hash, err := CalculateHash(b, 32)
	if err != nil {
		return nil, err
	}
	b.Hash = hash
	return hash, nil
}

// PrintDecrypt prints the block to the terminal with decrypted data.
func (b *Block) PrintDecrypt(key []byte) {
	fmt.Printf("  Hash %x: \n", b.Hash)
	fmt.Printf("    Index: %d \n", b.Index)
	fmt.Printf("    PrevHash: %x \n", b.PrevHash)
	fmt.Printf("    Data: [%s] \n", b.Data.StringDecrypt(key))
	fmt.Printf("    Timestamp: %d \n", b.Time)
	fmt.Printf("    Nonce: %d \n", b.Nonce)
	fmt.Println()
}
