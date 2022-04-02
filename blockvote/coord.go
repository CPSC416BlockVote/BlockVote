package blockvote

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"errors"
	"github.com/DistributedClocks/tracing"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type CoordConfig struct {
	ClientAPIListenAddr string
	MinerAPIListenAddr  string
	TracingServerAddr   string
	Secret              []byte
	TracingIdentity     string
}

type NodeInfo struct {
	Property MinerInfo
}

// messages

type (
	RegisterArgs struct {
		Info MinerInfo
	}

	RegisterReply struct {
		BlockChain   [][]byte
		LastHash     []byte
		Candidates   []Identity.Wallets
		PeerAddrList []string
	}

	GetCandidatesArgs struct {
	}

	GetCandidatesReply struct {
		Candidates []Identity.Wallets
	}

	GetMinerListArgs struct {
	}

	GetMinerListReply struct {
		MinerAddrList []string
	}

	QueryTxnArgs struct {
		TxID []byte
	}

	QueryTxnReply struct {
		NumConfirmed int
	}
)

type Coord struct {
	// Coord state may go here
	Storage    *util.Database
	Blockchain *blockchain.BlockChain

	Candidates []Identity.Wallets

	nlMu     sync.Mutex
	NodeList []NodeInfo
}

func NewCoord() *Coord {
	return &Coord{
		Storage: &util.Database{},
	}
}

func (c *Coord) Start(clientAPIListenAddr string, minerAPIListenAddr string, ctrace *tracing.Tracer) error {
	// 1. Initialization
	// 1.1 Storage(DB)
	if _, err := os.Stat("./storage/coord"); err == nil {
		os.RemoveAll("./storage/coord")
	}
	err := c.Storage.New("./storage/coord", false)
	util.CheckErr(err, "[ERROR] error when creating database")
	defer c.Storage.Close()
	// 1.2 Blockchain
	c.Blockchain = blockchain.NewBlockChain(c.Storage)
	err = c.Blockchain.Init()
	util.CheckErr(err, "[ERROR] error when initializing blockchain")
	// TODO: 1.3 Candidates

	// 2. Starting API services
	// >> miner
	coordAPIMiner := new(CoordAPIMiner)
	coordAPIMiner.c = c
	err = util.NewRPCServerWithIpPort(coordAPIMiner, minerAPIListenAddr)
	if err != nil {
		return errors.New("cannot start API service for miner")
	}
	log.Println("[INFO] Listen to miners' API requests at", minerAPIListenAddr)

	// >> client
	coordAPIClient := new(CoordAPIClient)
	coordAPIClient.c = c
	err = util.NewRPCServerWithIpPort(coordAPIClient, clientAPIListenAddr)
	if err != nil {
		return errors.New("cannot start API service for client")
	}
	log.Println("[INFO] Listen to clients' API requests at", clientAPIListenAddr)

	// Wait for interrupt signal to exit
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	return nil
}

// ----- APIs for miner -----

type CoordAPIMiner struct {
	c *Coord
}

// Register registers a new miner in the system and returns necessary data about the system
func (api *CoordAPIMiner) Register(args RegisterArgs, reply *RegisterReply) error {
	api.c.nlMu.Lock()
	defer api.c.nlMu.Unlock()

	// add to list
	newNodeInfo := NodeInfo{Property: args.Info}
	api.c.NodeList = append(api.c.NodeList, newNodeInfo)

	// prepare reply data
	encodedBlockchain, lastHash := api.c.Blockchain.Encode()
	var peerAddrList []string
	for _, info := range api.c.NodeList {
		peerAddrList = append(peerAddrList, info.Property.MinerMinerAddr)
	}

	*reply = RegisterReply{
		BlockChain:   encodedBlockchain,
		LastHash:     lastHash,
		Candidates:   api.c.Candidates,
		PeerAddrList: peerAddrList,
	}

	return nil
}

// ----- APIs for client -----

type CoordAPIClient struct {
	c *Coord
}

func (api *CoordAPIClient) GetCandidates(args GetCandidatesArgs, reply *GetCandidatesReply) error {
	*reply = GetCandidatesReply{Candidates: api.c.Candidates}
	return nil
}

func (api *CoordAPIClient) GetMinerList(args GetMinerListArgs, reply *GetMinerListReply) error {
	api.c.nlMu.Lock()
	defer api.c.nlMu.Unlock()

	var minerAddrList []string
	for _, info := range api.c.NodeList {
		minerAddrList = append(minerAddrList, info.Property.ClientListenAddr)
	}

	*reply = GetMinerListReply{MinerAddrList: minerAddrList}
	return nil
}

// QueryTxn queries a transaction in the system and returns the number of blocks that confirm it.
func (api *CoordAPIClient) QueryTxn(args QueryTxnArgs, reply *QueryTxnReply) error {
	*reply = QueryTxnReply{NumConfirmed: api.c.Blockchain.TxnStatus(args.TxID)}
	return nil
}
