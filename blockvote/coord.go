package blockvote

import (
	"bytes"
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/gossip"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"encoding/gob"
	"errors"
	"fmt"
	"github.com/DistributedClocks/tracing"
	"log"
	"os"
	"strconv"
	"strings"
)

const (
	NCandidatesKey     = "NCandidates"
	CandidateKeyPrefix = "cand-"
	NodeKeyPrefix      = "node-"
)

type CoordConfig struct {
	ClientAPIListenAddr string
	MinerAPIListenAddr  string
	TracingServerAddr   string
	NCandidates         uint8
	Secret              []byte
	TracingIdentity     string
}

// messages

type (
	DownloadArgs struct {
	}
	DownloadReply struct {
		BlockChain [][]byte
		LastHash   []byte
		Candidates [][]byte
		MemoryPool TxnPool
		Peers      []gossip.Peer
	}

	GetCandidatesArgs struct {
	}

	GetCandidatesReply struct {
		Candidates [][]byte
	}

	GetEntryPointsArgs struct {
	}

	GetEntryPointsReply struct {
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

	EntryPointIpPort string

	GossipAddr string
	UpdateChan chan<- gossip.Update
}

func NewCoord() *Coord {
	return &Coord{
		Storage: &util.Database{},
	}
}

func (c *Coord) Start(clientAPIListenAddr string, minerAPIListenAddr string, nCandidates uint8, ctrace *tracing.Tracer) error {
	// 1. Initialization
	// 1.1 Storage(DB)
	resume := c.InitStorage()
	if resume {
		log.Println("[INFO] Restarting...")
	}
	defer c.Storage.Close()
	// 1.2 Candidates
	c.InitCandidates(nCandidates, resume)
	// 1.3 Blockchain
	c.InitBlockchain(resume)

	// 2. Starting API services
	coordIp := minerAPIListenAddr[0:strings.Index(minerAPIListenAddr, ":")]

	// >> entry point
	entryPointAPI := new(EntryPointAPI)
	entryPointAPI.e = c
	entryPointListenAddr, err := util.NewRPCServerWithIp(entryPointAPI, coordIp)
	if err != nil {
		return errors.New("cannot start entry point API service")
	}
	c.EntryPointIpPort = entryPointListenAddr
	log.Println("[INFO] Listen to clients' API requests at", c.EntryPointIpPort)

	// gossip
	var existingUpdates []gossip.Update
	blockchainData, _ := c.Blockchain.Encode()
	for _, data := range blockchainData {
		existingUpdates = append(existingUpdates, gossip.NewUpdate(gossip.BlockIDPrefix, blockchain.DecodeToBlock(data).Hash, data))
	}
	queryChan, updateChan, gossipAddr, err := gossip.Start(2,
		"Pull",
		coordIp,
		[]gossip.Peer{},
		existingUpdates,
		gossip.Peer{
			Identifier: "coord",
			APIAddr:    c.EntryPointIpPort,
			Active:     true,
		},
		true)
	if err != nil {
		return err
	}
	c.GossipAddr = gossipAddr
	c.UpdateChan = updateChan
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
		if strings.HasPrefix(data.ID, gossip.BlockIDPrefix) {
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
			//c.NodeList = append(c.NodeList, node)
		}
		// notify miners of gossip peers (b.c. gossip addr at coord changes)
		//c.NotifyMiners()
	}
}

func (c *Coord) PrintChain() {
	votes, txns := c.Blockchain.VotingStatus()
	fv, err := os.Create("./votes.txt")
	util.CheckErr(err, "Unable to create votes.txt")
	defer fv.Close()
	for idx, _ := range votes {
		fv.WriteString(fmt.Sprintf("%s,%d\n", c.Candidates[idx].CandidateData.CandidateName, votes[idx]))
	}
	fv.Sync()

	ft, err := os.Create("./txns.txt")
	util.CheckErr(err, "Unable to create txns.txt")
	defer ft.Close()
	for _, txn := range txns {
		ft.WriteString(fmt.Sprintf("%x,%s,%s\n", txn.ID, txn.Data.VoterName, txn.Data.VoterCandidate))
	}
	ft.Sync()
}

// ----- APIs for miner -----

type CoordAPIMiner struct {
	c *Coord
}

// Download provides necessary data about the system for new node. should be called before Register
func (api *CoordAPIMiner) Download(args DownloadArgs, reply *DownloadReply) error {
	// prepare reply data
	encodedBlockchain, lastHash := api.c.Blockchain.Encode()
	var candidates [][]byte
	for _, cand := range api.c.Candidates {
		candidates = append(candidates, cand.Encode())
	}

	*reply = DownloadReply{
		BlockChain: encodedBlockchain,
		LastHash:   lastHash,
		Candidates: candidates,
		Peers:      append(gossip.GetPeers(), gossip.Identity),
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

func (api *CoordAPIClient) GetEntryPoints(args GetEntryPointsArgs, reply *GetEntryPointsReply) error {
	// access points consist of api listeners of coord and all miners
	accessPoints := []string{api.c.EntryPointIpPort}
	for _, miner := range gossip.GetPeers() {
		accessPoints = append(accessPoints, miner.APIAddr)
	}

	*reply = GetEntryPointsReply{MinerAddrList: accessPoints}
	return nil
}

func (c *Coord) ReceiveTxn(txn *blockchain.Transaction) {
	// broadcast
	c.UpdateChan <- gossip.NewUpdate(gossip.TransactionIDPrefix, txn.ID, txn.Serialize())
}

func (c *Coord) CheckTxn(txID []byte) int {
	return c.Blockchain.TxnStatus(txID)
}

func (c *Coord) CheckResults() []uint {
	votes, _ := c.Blockchain.VotingStatus()
	return votes
}
