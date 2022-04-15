package blockvote

import (
	"bytes"
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/gossip"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"encoding/gob"
	"errors"
	"github.com/DistributedClocks/tracing"
	"log"
	"net/rpc"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	NCandidatesKey      = "NCandidates"
	CandidateKeyPrefix  = "cand-"
	NodeKeyPrefix       = "node-"
	BlockIDPrefix       = "block-"
	TransactionIDPrefix = "txn-"
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
	DownloadArgs struct {
	}
	DownloadReply struct {
		BlockChain   [][]byte
		LastHash     []byte
		Candidates   [][]byte
		PeerAddrList []string // not including the miner itself
	}

	RegisterArgs struct {
		Info MinerInfo
	}

	RegisterReply struct {
		PeerAddrList       []string // will include the miner itself!
		PeerGossipAddrList []string // the first address is coord!
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

	QueryResultsArgs struct {
	}

	QueryResultsReply struct {
		Votes []uint
	}
)

type Coord struct {
	// Coord state may go here
	Storage    *util.Database
	Blockchain *blockchain.BlockChain

	Candidates []*Identity.Wallets

	nlMu       sync.Mutex // lock NodeList & MinerConns
	NodeList   []NodeInfo
	MinerConns []*rpc.Client

	GossipAddr string
}

func NewCoord() *Coord {
	return &Coord{
		Storage: &util.Database{},
	}
}

func (c *Coord) Start(clientAPIListenAddr string, minerAPIListenAddr string, nCandidates uint8, ctrace *tracing.Tracer) error {
	// FIXME: comment out below if statement to test coord restart
	if _, err := os.Stat("./storage/coord"); err == nil {
		os.RemoveAll("./storage/coord")
	}

	// 1. Initialization
	// 1.1 Storage(DB)
	resume := c.InitStorage()
	defer c.Storage.Close()
	// 1.2 Candidates
	c.InitCandidates(nCandidates, resume)
	// 1.3 Blockchain
	c.InitBlockchain(resume)

	// 2. Starting API services
	coordIp := minerAPIListenAddr[0:strings.Index(minerAPIListenAddr, ":")]
	// gossip
	var existingUpdates []gossip.Update
	blockchainData, _ := c.Blockchain.Encode()
	for _, data := range blockchainData {
		existingUpdates = append(existingUpdates, gossip.NewUpdate(BlockIDPrefix, blockchain.DecodeToBlock(data).Hash, data))
	}
	queryChan, _, gossipAddr, err := gossip.Start(2,
		"Pull",
		coordIp,
		//[]string{},
		existingUpdates,
		"coord",
		true)
	if err != nil {
		return err
	}
	c.GossipAddr = gossipAddr
	// 1.4 NodeList
	c.InitNodeList(resume)

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

	// 3. receive blocks from miners
	for {
		data := <-queryChan
		// check if it is a block
		if strings.HasPrefix(data.ID, BlockIDPrefix) {
			block := blockchain.DecodeToBlock(data.Data)
			// check if it is an unseen block
			if !c.Blockchain.Exist(block.Hash) {
				// try to put it to the blockchain
				prevLastHash := c.Blockchain.GetLastHash()
				success, switched, _ := c.Blockchain.Put(*block, false)
				curLastHash := c.Blockchain.GetLastHash()
				if success {
					log.Printf("[INFO] Received valid block #%d (%x) by %s\n", block.BlockNum, block.Hash[:5], block.MinerID)
					blockchain.PrintBlock(block)
					if switched == nil {
						if bytes.Compare(prevLastHash, curLastHash) != 0 {
							log.Println("[INFO] Added new block to the current chain")
						} else {
							log.Println("[INFO] Added new block to an alternative chain")
						}
					} else {
						log.Println("[INFO] Added new block to an alternative chain")
						log.Println("[INFO] Switching to a new chain")
					}

				} else {
					log.Printf("[WARN] Rejected invalid block #%d (%x) by %s\n", block.BlockNum, block.Hash[:5], block.MinerID)
				}
			}
		}
	}

	// Wait for interrupt signal to exit
	//sigs := make(chan os.Signal, 1)
	//signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	//<-sigs
	//return nil
}

func (c *Coord) Tracker() {
	for {
		select {}
	}
}

func (c *Coord) InitStorage() (resume bool) {
	if _, err := os.Stat("./storage/coord"); err == nil {
		err := c.Storage.Load("./storage/coord")
		util.CheckErr(err, "[ERROR] error when reloading database")
		resume = true
	} else if os.IsNotExist(err) {
		err := c.Storage.New("./storage/coord", false)
		util.CheckErr(err, "[ERROR] error when creating database")
		resume = false
	} else {
		util.CheckErr(err, "[ERROR] OS error")
	}
	return resume
}

func (c *Coord) InitBlockchain(resume bool) {
	c.Blockchain = blockchain.NewBlockChain(c.Storage, c.Candidates)
	if !resume {
		err := c.Blockchain.Init()
		util.CheckErr(err, "[ERROR] error when initializing blockchain")
	} else {
		err := c.Blockchain.ResumeFromDB()
		util.CheckErr(err, "[ERROR] error when reloading blockchain")
	}
}

func (c *Coord) InitCandidates(nCandidates uint8, resume bool) {
	if !resume {
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
			c.Candidates = append(c.Candidates, can)
		}
		err := c.Storage.PutMulti(keys, values)
		util.CheckErr(err, "[ERROR] error when saving candidates")
	} else {
		values, err := c.Storage.GetAllWithPrefix(CandidateKeyPrefix)
		util.CheckErr(err, "[ERROR] error reloading candidates")
		for _, val := range values {
			cand := Identity.DecodeToWallets(val)
			c.Candidates = append(c.Candidates, cand)
		}
		if int(nCandidates) != len(c.Candidates) {
			panic("[ERROR] error reloading candidates: expect " + strconv.Itoa(int(nCandidates)) + ", got " + strconv.Itoa(len(c.Candidates)))
		}
	}
}

func (c *Coord) InitNodeList(resume bool) {
	if resume {
		values, err := c.Storage.GetAllWithPrefix(NodeKeyPrefix)
		util.CheckErr(err, "[ERROR] error reloading node list")
		for _, val := range values {
			node := NodeInfo{}
			err := gob.NewDecoder(bytes.NewReader(val)).Decode(&node)
			if err != nil {
				log.Println("[ERROR] unable to decode node info")
				log.Fatal(err)
			}
			// reconstruct node list
			c.NodeList = append(c.NodeList, node)
			// re-add gossip peer
			gossip.AddPeer(node.Property.GossipAddr)
			// reconnect
			minerConn, err := rpc.Dial("tcp", node.Property.CoordListenAddr)
			if err != nil {
				// silently digest error
				log.Println("[WARN] cannot connect to miner at", node.Property.CoordListenAddr)
			}
			c.MinerConns = append(c.MinerConns, minerConn)
		}
		// notify miners of gossip peers (b.c. gossip addr at coord changes)
		c.NotifyMiners()
	}
}

func (c *Coord) NotifyMiners() {
	var peerAddrList []string
	var peerGossipAddrList = []string{c.GossipAddr} // coord's gossip addr will always be the first!
	for _, info := range c.NodeList {
		peerAddrList = append(peerAddrList, info.Property.MinerMinerAddr)
		peerGossipAddrList = append(peerGossipAddrList, info.Property.GossipAddr)
	}
	for _, minerConn := range c.MinerConns {
		if minerConn != nil {
			args := NotifyPeerListArgs{
				PeerAddrList:       peerAddrList,
				PeerGossipAddrList: peerGossipAddrList,
			}
			reply := NotifyPeerListReply{}
			err := minerConn.Call("MinerAPICoord.NotifyPeerList", args, &reply)
			if err != nil {
				log.Println("[WARN] Unable to notify a miner")
			}
		}
	}
}

// ----- APIs for miner -----

type CoordAPIMiner struct {
	c *Coord
}

// Download provides necessary data about the system for new node. should be called before Register
func (api *CoordAPIMiner) Download(args DownloadArgs, reply *DownloadReply) error {
	// prepare reply data
	encodedBlockchain, lastHash := api.c.Blockchain.Encode()
	var peerAddrList []string
	nodeList := api.c.NodeList[:]
	for _, info := range nodeList {
		peerAddrList = append(peerAddrList, info.Property.MinerMinerAddr)
	}
	var candidates [][]byte
	for _, cand := range api.c.Candidates {
		candidates = append(candidates, cand.Encode())
	}

	*reply = DownloadReply{
		BlockChain:   encodedBlockchain,
		LastHash:     lastHash,
		Candidates:   candidates,
		PeerAddrList: peerAddrList,
	}
	return nil
}

// Register registers a new miner in the system. should be called after Download
func (api *CoordAPIMiner) Register(args RegisterArgs, reply *RegisterReply) error {
	api.c.nlMu.Lock()
	defer api.c.nlMu.Unlock()

	// add new miner to list
	newNodeInfo := NodeInfo{Property: args.Info}
	api.c.NodeList = append(api.c.NodeList, newNodeInfo)
	// write to disk first
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(newNodeInfo)
	if err != nil {
		log.Println("[WARN] node info encode error")
	}
	api.c.Storage.Put(util.DBKeyWithPrefix(NodeKeyPrefix, []byte(args.Info.MinerId)), buf.Bytes())

	// notify existing miners
	api.c.NotifyMiners() // this will not notify current miner as conn not established

	// add rpc connection
	minerConn, err := rpc.Dial("tcp", newNodeInfo.Property.CoordListenAddr)
	if err != nil {
		// silently digest error
		log.Println("[WARN] cannot connect to miner at", newNodeInfo.Property.CoordListenAddr)
	}
	api.c.MinerConns = append(api.c.MinerConns, minerConn)

	gossip.AddPeer(newNodeInfo.Property.GossipAddr)
	log.Printf("[INFO] New miner joined: %s (g: %s, co: %s, m: %s, cl:%s)", args.Info.MinerId,
		args.Info.GossipAddr, args.Info.CoordListenAddr, args.Info.MinerMinerAddr, args.Info.ClientListenAddr)

	// prepare reply data
	var peerAddrList []string
	var peerGossipAddrList = []string{api.c.GossipAddr} // coord's gossip addr will always be the first!
	for _, info := range api.c.NodeList {
		peerAddrList = append(peerAddrList, info.Property.MinerMinerAddr)
		peerGossipAddrList = append(peerGossipAddrList, info.Property.GossipAddr)
	}
	*reply = RegisterReply{
		PeerAddrList:       peerAddrList,
		PeerGossipAddrList: peerGossipAddrList,
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

func (api *CoordAPIClient) QueryResults(_ QueryResultsArgs, reply *QueryResultsReply) error {
	*reply = QueryResultsReply{Votes: api.c.Blockchain.VotingStatus()}
	return nil
}
