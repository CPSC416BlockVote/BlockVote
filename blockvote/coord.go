package blockvote

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"errors"
	"github.com/DistributedClocks/tracing"
	"log"
	"net/rpc"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
)

const (
	NCandidatesKey     = "NCandidates"
	CandidateKeyPrefix = "cand-"
)

type CoordConfig struct {
	ClientAPIListenAddr string
	MinerAPIListenAddr  string
	TracingServerAddr   string
	NCandidates         uint8
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
		Candidates [][]byte
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

	nlMu       sync.Mutex // lock NodeList & MinerConns
	NodeList   []NodeInfo
	MinerConns []*rpc.Client
}

func NewCoord() *Coord {
	return &Coord{
		Storage: &util.Database{},
	}
}

func (c *Coord) Start(clientAPIListenAddr string, minerAPIListenAddr string, nCandidates uint8, ctrace *tracing.Tracer) error {
	// 1. Initialization
	// 1.1 Storage(DB)
	resume := c.PrepareStorage()
	defer c.Storage.Close()
	// 1.2 Blockchain
	c.Blockchain = blockchain.NewBlockChain(c.Storage)
	if !resume {
		err := c.Blockchain.Init()
		util.CheckErr(err, "[ERROR] error when initializing blockchain")
	} else {
		err := c.Blockchain.ResumeFromDB()
		util.CheckErr(err, "[ERROR] error when reloading blockchain")
	}
	// 1.3 Candidates
	c.PrepareCandidates(nCandidates, resume)

	// 2. Starting API services
	// >> miner
	coordAPIMiner := new(CoordAPIMiner)
	coordAPIMiner.c = c
	err := util.NewRPCServerWithIpPort(coordAPIMiner, minerAPIListenAddr)
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

func (c *Coord) Tracker() {
	for {
		select {}
	}
}

func (c *Coord) PrepareStorage() (resume bool) {
	// FIXME: for future implementation, check if coord restarts
	if _, err := os.Stat("./storage/coord"); err == nil {
		os.RemoveAll("./storage/coord")
	}
	err := c.Storage.New("./storage/coord", false)
	util.CheckErr(err, "[ERROR] error when creating database")
	resume = false
	return resume
}

func (c *Coord) PrepareCandidates(nCandidates uint8, resume bool) {
	// FIXME: for future implementation, reload candidates from database if resume
	var keys = [][]byte{util.DBKeyWithPrefix(NCandidatesKey, []byte{})}
	var values = [][]byte{[]byte(strconv.Itoa(int(nCandidates)))}

	for i := 0; i < int(nCandidates); i++ {
		can, err := Identity.CreateCandidate("CANDIDATE" + strconv.Itoa(i))
		if err != nil {
			util.CheckErr(err, "[ERROR] error when initializing candidates")
		}
		can.AddWallet()
		keys = append(keys, util.DBKeyWithPrefix(CandidateKeyPrefix, []byte(strconv.Itoa(i))))
		values = append(values, can.Encode())
		c.Candidates = append(c.Candidates, *can)
	}
	err := c.Storage.PutMulti(keys, values)
	util.CheckErr(err, "[ERROR] error when saving candidates")
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
	for _, minerConn := range api.c.MinerConns {
		if minerConn != nil {
			reply := NotifyPeerListReply{}
			// TODO: notifies existing miners of the new miner
			go minerConn.Call("MinerAPICoord.NotifyPeerList", NotifyPeerListArgs{}, &reply)
		}
	}
	// add rpc connection
	minerConn, err := rpc.Dial("tcp", newNodeInfo.Property.CoordListenAddr)
	if err != nil {
		// silently digest error
		log.Println("[WARN] cannot connect to miner at", newNodeInfo.Property.CoordListenAddr)
	}
	api.c.MinerConns = append(api.c.MinerConns, minerConn)

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
	var candidates [][]byte
	for _, cand := range api.c.Candidates {
		candidates = append(candidates, cand.Encode())
	}
	*reply = GetCandidatesReply{Candidates: candidates}
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
