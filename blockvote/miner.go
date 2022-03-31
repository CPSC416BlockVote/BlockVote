package blockvote

import (
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"errors"
	"fmt"
	"github.com/DistributedClocks/tracing"
)

type MinerConfig struct {
	MinerId           string
	CoordAddr         string
	MinerAddr         string
	TracingServerAddr string
	Difficulty        uint8
	Secret            []byte
	TracingIdentity   string
}

type Miner struct {
	// Miner state may go here
	Storage    *util.Database
	Blockchain *blockchain.BlockChain
}

func NewMiner() *Miner {
	return &Miner{
		Storage: &util.Database{},
	}
}

func (m *Miner) Start(minerId string, coordAddr string, minerAddr string, difficulty uint8, mtrace *tracing.Tracer) error {
	err := m.Storage.New("", true)
	if err != nil {
		util.CheckErr(err, "error when creating databse")
	}
	defer m.Storage.Close()

	m.Blockchain = blockchain.NewBlockChain(m.Storage)
	err = m.Blockchain.Init()
	if err != nil {
		fmt.Println(err)
		util.CheckErr(err, "error when initializing blockchain")
	}

	// miner can do proof-of-work
	prevHash := m.Blockchain.LastHash
	for i := 0; i < 10; i++ {
		fmt.Printf("Block #%d:\n", i+1)
		block := blockchain.Block{
			PrevHash: prevHash,
			BlockNum: uint8(i + 1),
			Nonce:    0,
			Txns:     []*blockchain.Transaction{},
			MinerID:  minerId,
			Hash:     []byte{},
		}
		pow := blockchain.NewProof(&block)
		nonce, hash := pow.Run()
		block.Nonce = nonce
		block.Hash = hash
		prevHash = hash

		fmt.Printf("Nonce: %d\n", block.Nonce)
		fmt.Printf("Hash: %x\n", block.Hash)
		fmt.Println()
	}

	return errors.New("not implemented")
}
