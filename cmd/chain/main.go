package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"strings"

	"github.com/danmuck/dps_files/src/impl"
	logs "github.com/danmuck/smplog"
)

type Blockchain struct {
	root          impl.Block
	blocks        []impl.Block
	height        uint64
	encryptionKey []byte
}

func (bc *Blockchain) ValidateChain() error {
	for i := 1; i < len(bc.blocks); i++ {
		current := bc.blocks[i]
		previous := bc.blocks[i-1]

		// Check if hashes match
		if !bytes.Equal(current.PrevHash, previous.Hash) {
			return fmt.Errorf("block %d invalid: PrevHash mismatch", i)
		}

		// Verify block hash
		if !impl.ValidateHash(current, 32) {
			return fmt.Errorf("block %d invalid: hash verification failed", i)
		}
	}
	return nil
}

func (bc *Blockchain) Append(data []byte) error {
	fmt.Printf("Appending Block data: %s \n", data)
	previousBlock := bc.blocks[len(bc.blocks)-1]
	fmt.Printf("Previous Block \n")
	previousBlock.Print()
	newBlock := impl.NewBlock(previousBlock.Index+1, data, previousBlock.Hash)
	newBlock.Print()

	bc.blocks = append(bc.blocks, *newBlock)
	return nil
}

func (bc *Blockchain) LoadChain(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open chain file: %w", err)
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&bc.blocks); err != nil {
		return fmt.Errorf("failed to decode chain data: %w", err)
	}

	return nil
}

// PrintChain prints the blockchain to the terminal.
func (bc *Blockchain) PrintChain() {
	fmt.Println("Blockchain:")
	for i, block := range bc.blocks {
		fmt.Printf("Block %d:\n", i)
		block.Print()
	}
}

func (bc *Blockchain) PrintChainDecrypted(key []byte) {
	fmt.Println("Blockchain:")
	for i, block := range bc.blocks {
		fmt.Printf("Block %d:\n", i)
		block.PrintDecrypt(key)
	}
}

// InitializeBlockchain initializes a blockchain with a genesis block.
func InitializeBlockchain(encryptionKey []byte) *Blockchain {
	genesisBlock := impl.NewBlock(0, []byte("Genesis Block Data"), []byte("genesis_hash"))
	genesisBlock.Print()
	return &Blockchain{
		blocks:        []impl.Block{*genesisBlock},
		encryptionKey: encryptionKey,
	}
}

func main() {
	logs.Configure(logs.DefaultConfig())

	// Initialize the blockchain
	encryptionKey := []byte("examplekey123456")
	// encryptionKey := []byte("some random data for my super secure key but it needs to be long enough
	// for this to actually be a thing so i am adding random data to it until it is the correct size which should be 256 bytes)
	bc := InitializeBlockchain(encryptionKey)
	bc.PrintChain()

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Blockchain initialized. Type text to append to the blockchain, or 'exit' to quit.")

	for {
		fmt.Print("> ")
		scanner.Scan()
		input := scanner.Text()
		input = strings.TrimSpace(input)

		// Exit condition
		if strings.ToLower(input) == "exit" {
			fmt.Println("Exiting...")
			break
		}

		// Append block with user input
		err := bc.Append([]byte(input))
		if err != nil {
			logs.Errorf(err, "Error appending block")
			continue
		}

		// Print the blockchain
		bc.PrintChainDecrypted(encryptionKey)
	}
	if err := bc.ValidateChain(); err != nil {
		logs.Errorf(err, "Chain validation failed")
	} else {
		logs.Info("Chain validation passed.")
	}
}
