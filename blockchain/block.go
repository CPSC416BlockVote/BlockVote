package blockchain

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"time"
)

type Block struct {
	BlockNum   uint
	Difficulty float64
	Hash       []byte
	MinerID    string
	Nonce      uint32
	PrevHash   []byte
	Timestamp  int64
	Txns       []*Transaction
}

// ----- Block APIs -----

// Genesis makes current block a genesis block
func (b *Block) Genesis() {
	b.PrevHash = []byte{}
	b.BlockNum = 0
	b.Difficulty = InitialDifficulty // initial difficulty is set to 8 leading zeros
	b.Timestamp = time.Now().Unix()
	b.Txns = []*Transaction{}
	b.MinerID = "Coord"
	// get nonce and hash from POW
	pow := NewProof(b)
	pow.Run()
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

func PrintBlock(block *Block) {
	str := ""
	str += fmt.Sprintf("Block #%d (%x)\n", block.BlockNum, block.Hash[:5])
	str += fmt.Sprintf("\tPrevHash:\t %x\n", block.PrevHash[:5])
	str += fmt.Sprintf("\tDifficulty:\t %.2f\n", block.Difficulty)
	str += fmt.Sprintf("\tNonce:\t\t %d\n", block.Nonce)
	str += fmt.Sprintf("\tMinerID:\t %s\n", block.MinerID)
	str += fmt.Sprintf("\tTimestamp:\t %s\n", time.Unix(block.Timestamp, 0).String())
	str += fmt.Sprintf("\tTxns:\t\t %d\n", len(block.Txns))
	for _, txn := range block.Txns {
		str += fmt.Sprintf("\t    status %d\t	%s\n", txn.Receipt.Code, txn.Data.ToString())
	}
	log.Print(str)
}
