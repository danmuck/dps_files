package impl

import "fmt"

type BlockData struct {
	Hash []byte
	Data []byte
	IV   []byte
}

func (bd *BlockData) String() string {
	return fmt.Sprintf("Hash: %x -- Data: %s", bd.Hash, bd.Data)
}

func (bd *BlockData) StringDecrypt(key []byte) string {
	if bd.IV == nil {
		return bd.String()
	}
	d, err := DecryptData(key, bd.Data, bd.IV)
	if err != nil {
		return "StringDecrypt(): error"
	}
	return fmt.Sprintf("Hash: %x -- Data: %s", bd.Hash, d)
}
