package blockvote

import (
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"errors"
	"fmt"
	"github.com/DistributedClocks/tracing"
	"os"
)

type CoordConfig struct {
	ClientAPIListenAddr string
	MinerAPIListenAddr  string
	TracingServerAddr   string
	Secret              []byte
	TracingIdentity     string
}

type Coord struct {
	// Coord state may go here
	Storage    *util.Database
	Blockchain *blockchain.BlockChain
}

func NewCoord() *Coord {
	return &Coord{
		Storage: &util.Database{},
	}
}

func (c *Coord) Start(clientAPIListenAddr string, minerAPIListenAddr string, ctrace *tracing.Tracer) error {
	if _, err := os.Stat("./storage/coord"); err == nil {
		os.RemoveAll("./storage/coord")
	}
	err := c.Storage.New("./storage/coord", false)
	if err != nil {
		util.CheckErr(err, "error when creating databse")
	}
	defer c.Storage.Close()

	// coord can initialize the blockchain with genesis block
	c.Blockchain = blockchain.NewBlockChain(c.Storage)
	err = c.Blockchain.Init()
	if err != nil {
		fmt.Println(err)
		util.CheckErr(err, "error when initializing blockchain")
	}

	genesis := c.Blockchain.Get(c.Blockchain.LastHash)
	fmt.Println("Genesis Block:")
	fmt.Printf("PrevHash: %x\n", genesis.PrevHash)
	fmt.Printf("BlockNum: %d\n", genesis.BlockNum)
	fmt.Printf("Nonce: %d\n", genesis.Nonce)
	fmt.Println("Txns:", genesis.Txns)
	fmt.Printf("MinerID: %s\n", genesis.MinerID)
	fmt.Printf("Hash: %x\n", genesis.Hash)
	fmt.Println()

	fmt.Printf("Blockchain:\n")
	fmt.Printf("Last Hash: %x\n", c.Blockchain.LastHash)
	fmt.Println()

	// coord can store blocks using database
	var blockHashes [][]byte
	for i := 0; i < 10; i++ {
		block := blockchain.Block{
			PrevHash: c.Blockchain.LastHash,
			BlockNum: uint8(i + 1),
			Nonce:    0,
			Txns:     []*blockchain.Transaction{},
			MinerID:  "coord",
			Hash:     []byte{},
		}
		pow := blockchain.NewProof(&block)
		nonce, hash := pow.Run()
		block.Nonce = nonce
		block.Hash = hash

		blockHashes = append(blockHashes, block.Hash)
		succ := c.Blockchain.Put(block, true)
		if !succ {
			panic("Unable to put a new block")
		}
	}

	// coord can retrieve blocks from blockchain
	iterator := c.Blockchain.NewIterator(c.Blockchain.LastHash)
	for block, end := iterator.Next(); !end; block, end = iterator.Next() {
		fmt.Printf("PrevHash: %x\n", block.PrevHash)
		fmt.Printf("BlockNum: %d\n", block.BlockNum)
		fmt.Printf("Nonce: %d\n", block.Nonce)
		fmt.Println("Txns:", block.Txns)
		fmt.Printf("MinerID: %s\n", block.MinerID)
		fmt.Printf("Hash: %x\n", block.Hash)
		fmt.Println()
	}

	return errors.New("not implemented")
}
