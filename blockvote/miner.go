package blockvote

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"errors"
	"fmt"
	"github.com/DistributedClocks/tracing"
	"log"
	"strings"
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

type MinerInfo struct {
	CoordListenAddr  string
	MinerMinerAddr   string
	ClientListenAddr string
}

// messages

type NotifyPeerListArgs struct {
}

type NotifyPeerListReply struct {
}

type GetBlockArgs struct {
	Hash []byte
}

type GetBlockReply struct {
	block blockchain.Block
}

type SubmitTxnArgs struct {
	Txn blockchain.Transaction
}

type SubmitTxnReply struct {
}

type Miner struct {
	// Miner state may go here
	Storage    *util.Database
	Blockchain *blockchain.BlockChain

	Info MinerInfo

	Candidates []Identity.Wallets
	MemoryPool TxnPool
}

func NewMiner() *Miner {
	return &Miner{
		Storage: &util.Database{},
	}
}

type TxnPool struct {
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

	// starting API services
	minerIP := minerAddr[0:strings.Index(minerAddr, ":")]
	// >> coord
	minerAPICoord := new(MinerAPICoord)
	minerAPICoord.m = m
	coordListenAddr, err := util.NewRPCServerWithIp(minerAPICoord, minerIP)
	if err != nil {
		return errors.New("cannot start API service for coord")
	}
	m.Info.CoordListenAddr = coordListenAddr
	log.Println("[INFO] Listen to coord's API requests at", m.Info.CoordListenAddr)

	// >> client
	minerAPIClient := new(MinerAPIClient)
	minerAPIClient.m = m
	clientListenAddr, err := util.NewRPCServerWithIp(minerAPIClient, minerIP)
	if err != nil {
		return errors.New("cannot start API service for client")
	}
	m.Info.ClientListenAddr = clientListenAddr
	log.Println("[INFO] Listen to clients' API requests at", m.Info.ClientListenAddr)

	// >> miner
	minerAPIMiner := new(MinerAPIMiner)
	minerAPIMiner.m = m
	minerMinerAddr, err := util.NewRPCServerWithIp(minerAPIMiner, minerIP)
	if err != nil {
		return errors.New("cannot start API service for miner")
	}
	m.Info.MinerMinerAddr = minerMinerAddr
	log.Println("[INFO] Listen to miners' API requests at", m.Info.MinerMinerAddr)

	return errors.New("not implemented")
}

// ----- APIs for coord -----

type MinerAPICoord struct {
	m *Miner
}

func (api *MinerAPICoord) NotifyPeerList(args NotifyPeerListArgs, reply *NotifyPeerListReply) error {
	return nil
}

// ----- APIs for miner -----

type MinerAPIMiner struct {
	m *Miner
}

func (api *MinerAPIMiner) GetBlock(args GetBlockArgs, reply *GetBlockReply) error {
	return nil
}

// ----- APIs for client

type MinerAPIClient struct {
	m *Miner
}

func (api *MinerAPIClient) SubmitTxn(args SubmitTxnArgs, reply *SubmitTxnReply) error {
	return nil
}
