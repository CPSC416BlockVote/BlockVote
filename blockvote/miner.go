package blockvote

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/gossip"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"errors"
	"fmt"
	"github.com/DistributedClocks/tracing"
	"log"
	"strings"
	"sync"
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
	GossipAddr       string
}

// messages

type NotifyPeerListArgs struct {
	PeerAddrList       []string
	PeerGossipAddrList []string
}

type NotifyPeerListReply struct {
}

type GetBlockArgs struct {
	Hash []byte
}

type GetBlockReply struct {
	block blockchain.Block
}

type GetTxnPoolArgs struct {
}

type GetTxnPoolReply struct {
	PeerTxnPool TxnPool
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

	Info         MinerInfo
	ReceivedTxns map[string]bool
	Candidates   []Identity.Wallets
	MemoryPool   TxnPool

	mu    sync.Mutex
	cond  *sync.Cond
	start bool
}

func NewMiner() *Miner {
	return &Miner{
		Storage:      &util.Database{},
		ReceivedTxns: make(map[string]bool),
	}
}

type TxnPool struct {
	PendingTxns []blockchain.Transaction
}

func (m *Miner) Start(minerId string, coordAddr string, minerAddr string, difficulty uint8, mtrace *tracing.Tracer) error {
	err := m.Storage.New("", true)
	if err != nil {
		util.CheckErr(err, "error when creating database")
	}
	defer m.Storage.Close()

	m.cond = sync.NewCond(&m.mu)
	m.mu.Lock()
	// starting API services
	minerIP := minerAddr[0:strings.Index(minerAddr, ":")]
	// << coord
	minerAPICoord := new(MinerAPICoord)
	minerAPICoord.m = m
	coordListenAddr, err := util.NewRPCServerWithIp(minerAPICoord, minerIP)
	if err != nil {
		return errors.New("cannot start API service for coord")
	}
	m.Info.CoordListenAddr = coordListenAddr
	log.Println("[INFO] Listen to coord's API requests at", m.Info.CoordListenAddr)

	// << client
	minerAPIClient := new(MinerAPIClient)
	minerAPIClient.m = m
	clientListenAddr, err := util.NewRPCServerWithIp(minerAPIClient, minerIP)
	if err != nil {
		return errors.New("cannot start API service for client")
	}
	m.Info.ClientListenAddr = clientListenAddr
	log.Println("[INFO] Listen to clients' API requests at", m.Info.ClientListenAddr)

	// << miner
	minerAPIMiner := new(MinerAPIMiner)
	minerAPIMiner.m = m
	minerMinerAddr, err := util.NewRPCServerWithIp(minerAPIMiner, minerIP)
	if err != nil {
		return errors.New("cannot start API service for miner")
	}
	m.Info.MinerMinerAddr = minerMinerAddr
	log.Println("[INFO] Listen to miners' API requests at", m.Info.MinerMinerAddr)

	// Miner join
	coordClient, err := util.NewRPCClient(minerAddr, coordAddr)
	if err != nil {
		return errors.New("cannot create client for coord")
	}
	// download blockchain from coord
	downloadReply := DownloadReply{}
	err = coordClient.Call("CoordAPIMiner.Download", DownloadArgs{}, &downloadReply)
	if err != nil {
		return errors.New("cannot download data from coord")
	}
	// setup candidates
	for _, cand := range downloadReply.Candidates {
		wallets := Identity.DecodeToWallets(cand)
		m.Candidates = append(m.Candidates, *wallets)
	}
	// setup blockchain
	m.Blockchain = blockchain.NewBlockChain(m.Storage)
	err = m.Blockchain.ResumeFromEncodedData(downloadReply.BlockChain, downloadReply.LastHash)
	if err != nil {
		return errors.New("cannot resume blockchain")
	}
	// setup txn pool
	// ASSUME: the returned peer list cannot contain the miner itself
	// TODO: loop throught the peer list of just choose one?
	if len(downloadReply.PeerAddrList) > 0 {
		toPullMinerAddr := downloadReply.PeerAddrList[0]
		// get txn pool from the peer
		// >> miner
		minerClient, err := util.NewRPCClient(minerAddr, toPullMinerAddr)
		if err != nil {
			return errors.New("cannot create client for miner")
		}
		reply := GetTxnPoolReply{}
		minerClient.Call("MinerAPIMiner.GetTxnPool", GetTxnPoolArgs{}, &reply)
		m.MemoryPool = reply.PeerTxnPool
	}
	// setup gossip client
	var existingUpdates []gossip.Update
	blockchainData, _ := m.Blockchain.Encode()
	for _, data := range blockchainData {
		existingUpdates = append(existingUpdates, gossip.NewUpdate(BlockIDPrefix, blockchain.DecodeToBlock(data).Hash, data))
	}
	for _, txn := range m.MemoryPool.PendingTxns {
		existingUpdates = append(existingUpdates, gossip.NewUpdate(TransactionIDPrefix, txn.ID, txn.Serialize()))
	}
	_, updateChan, gossipAddr, err := gossip.Start(
		2,
		"PushPull",
		minerIP,
		//[]string{},
		existingUpdates,
		minerId,
		false)
	if err != nil {
		return err
	}
	m.Info.GossipAddr = gossipAddr

	reply := RegisterReply{}
	err = coordClient.Call("CoordAPIMiner.Register", RegisterArgs{m.Info}, &reply)
	if err != nil {
		return errors.New("cannot register as miner")
	}
	gossip.SetPeers(reply.PeerGossipAddrList)
	m.start = true
	m.cond.Broadcast()
	m.mu.Unlock()

	// miner does proof-of-work forever
	i := 0
	for {
		prevHash := m.Blockchain.LastHash
		if len(m.MemoryPool.PendingTxns) == 0 {
			// TODO: implement the select
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
			updateChan <- gossip.NewUpdate(BlockIDPrefix, block.Hash, block.Encode())

			fmt.Printf("Nonce: %d\n", block.Nonce)
			fmt.Printf("Hash: %x\n", block.Hash)
			fmt.Println()
			i++
		}
	}
	return nil
}

// ----- APIs for coord -----

type MinerAPICoord struct {
	m *Miner
}

func (api *MinerAPICoord) NotifyPeerList(args NotifyPeerListArgs, reply *NotifyPeerListReply) error {
	gossip.SetPeers(args.PeerGossipAddrList)
	return nil
}

// ----- APIs for miner -----

type MinerAPIMiner struct {
	m *Miner
}

func (api *MinerAPIMiner) GetBlock(args GetBlockArgs, reply *GetBlockReply) error {
	return nil
}

func (api *MinerAPIMiner) GetTxnPool(args GetTxnPoolArgs, reply *GetTxnPoolReply) error {
	reply.PeerTxnPool = api.m.MemoryPool
	return nil
}

// ----- APIs for client

type MinerAPIClient struct {
	m *Miner
}

func (api *MinerAPIClient) SubmitTxn(args SubmitTxnArgs, reply *SubmitTxnReply) error {
	api.m.mu.Lock()
	for !api.m.start {
		api.m.cond.Wait()
	}
	defer api.m.mu.Unlock()
	if api.m.Blockchain.ValidateTxn(&args.Txn) {
		sid := string(args.Txn.ID)
		if !api.m.ReceivedTxns[sid] {
			api.m.ReceivedTxns[sid] = true
			api.m.MemoryPool.PendingTxns = append(api.m.MemoryPool.PendingTxns, args.Txn)
		}
	}
	return nil
}
