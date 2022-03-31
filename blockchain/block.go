package blockchain

import (
	"bytes"
	"encoding/gob"
	"log"
)

type Block struct {
	PrevHash []byte
	BlockNum uint8
	Nonce    uint32
	Txns     []*Transaction
	MinerID  string
	Hash     []byte
}

// ----- Block APIs -----

// Genesis makes current block a genesis block
func (b *Block) Genesis() {
	b.PrevHash = []byte{}
	b.BlockNum = 0
	b.Txns = []*Transaction{}
	b.MinerID = "Coord"
	// get nonce and hash from POW
	pow := NewProof(b)
	nonce, hash := pow.Run()
	b.Nonce = nonce
	b.Hash = hash
}

// Encode encodes current block instance into bytes
func (b *Block) Encode() []byte {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(b)
	if err != nil {
		log.Println("[WARN] block encode error")
	}
	return buf.Bytes()
}

// DecodeToBlock decodes bytes to a new block instance
func DecodeToBlock(data []byte) *Block {
	block := Block{}
	err := gob.NewDecoder(bytes.NewReader(data)).Decode(&block)
	if err != nil {
		log.Println("[ERROR] block decode error")
		log.Fatal(err)
	}
	return &block
}

// ----- Utility Functions -----
