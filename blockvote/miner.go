package blockvote

import (
	"bytes"
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/gossip"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"encoding/gob"
	"errors"
	"log"
	"net/rpc"
	"os"
	"strings"
	"sync"
	"time"
)

type MinerConfig struct {
	MinerId           string
	CoordAddr         string
	MinerAddr         string
	TracingServerAddr string
	Secret            []byte
	TracingIdentity   string
	MaxTxn            uint8
}

type NodeInfo struct {
	NodeID           string
	CoordListenAddr  string
	MinerMinerAddr   string
	ClientListenAddr string
	GossipAddr       string
	AckAddr          string
}

// messages

type NotifyPeerListArgs struct {
	PeerAddrList       []string
	PeerGossipAddrList []string
}

type NotifyPeerListReply struct {
}

type DownloadArgs struct {
}

type DownloadReply struct {
	BlockChain [][]byte
	LastHash   []byte
	MemoryPool []blockchain.Transaction
	Peers      []gossip.Peer
}

type SubmitTxnArgs struct {
	Txn blockchain.Transaction
}

type SubmitTxnReply struct {
	Exist bool
}

type QueryTxnArgs struct {
	TxID []byte
}

type QueryTxnReply struct {
	Status blockchain.TransactionStatus
}

type QueryResultsArgs struct {
	PollID string
}

type QueryResultsReply struct {
	Results blockchain.PollMeta
}

type InvalidSignatureError struct {
}

func (e *InvalidSignatureError) Error() string {
	return "invalid signature"
}

type Miner struct {
	// Miner state may go here
	Storage    *util.Database
	Blockchain *blockchain.BlockChain

	Info         NodeInfo
	rtMu         sync.Mutex
	ReceivedTxns map[string]bool
	MemoryPool   *blockchain.MemoryPool
	Config       MinerConfig

	queryChan  <-chan gossip.Update
	updateChan chan<- gossip.Update

	TxnRecvChan      chan *blockchain.Transaction
	BlockRecvChan    chan *blockchain.Block
	ChainUpdatedChan chan int

	mu    sync.Mutex
	cond  *sync.Cond
	start bool
}

func NewMiner() *Miner {
	return &Miner{
		Storage:          &util.Database{},
		ReceivedTxns:     make(map[string]bool),
		MemoryPool:       blockchain.NewMemoryPool(200),
		TxnRecvChan:      make(chan *blockchain.Transaction, 500),
		BlockRecvChan:    make(chan *blockchain.Block, 50),
		ChainUpdatedChan: make(chan int, 50),
	}
}

type CoordConnection struct {
	mu        sync.Mutex
	Client    *rpc.Client
	MinerAddr string
	CoordAddr string
}

func NewCoordConnection(minerAddr string, coordAddr string) *CoordConnection {
	coordConnection := CoordConnection{MinerAddr: minerAddr, CoordAddr: coordAddr}
	return &coordConnection
}

func (cc *CoordConnection) Connect() {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	var err error
	for {
		cc.Client, err = util.NewRPCClient(cc.MinerAddr, cc.CoordAddr)
		if err == nil {
			break
		}
		log.Println("[INFO] Reattempting to establish connection with coord...")
	}
}

func (cc *CoordConnection) Send(serviceMethod string, args interface{}, reply interface{}) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	// check if connected
	if cc.Client == nil {
		cc.Connect()
	}

	var err error
	for {
		err = cc.Client.Call(serviceMethod, args, reply)
		if err == nil {
			break
		}
		// rpc connection is interrupted, need to reconnect
		cc.Connect()
	}
}

func (cc *CoordConnection) Close() {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.Client != nil {
		_ = cc.Client.Close()
	}
}

func (m *Miner) Start(config MinerConfig) error {
	gob.Register(blockchain.Rules{})

	m.Config = config
	m.Info.NodeID = config.MinerId

	// 1. Initialization
	resume := m.InitStorage()
	defer m.Storage.Close()

	m.cond = sync.NewCond(&m.mu)
	m.mu.Lock()

	// 2. Miner joining
	var peers []gossip.Peer
	if !resume {
		// 2.1 Contact coord and download from peers
		// 2.1.1 Connect to coord
		log.Println("[INFO] Retrieving information from coord...")
		coordConn := NewCoordConnection(config.MinerAddr, config.CoordAddr)
		coordConn.Connect()
		// 2.1.2 Get peers from coord
		reply := GetPeersReply{}
		coordConn.Send("CoordAPIMiner.GetPeers", GetPeersArgs{}, &reply)
		// 2.1.3 Download data from peers
		var dlBlockchain [][]byte
		var dlLastHash []byte
		var dlTxnPool []blockchain.Transaction
		var activeMinerPeers []gossip.Peer
		for _, p := range reply.Peers {
			if p.Type == gossip.TypeMiner && p.Active {
				activeMinerPeers = append(activeMinerPeers, p)
			}
		}
		if len(activeMinerPeers) == 0 {
			// no peers, download from coord
			log.Println("[INFO] Downloading initial system states from coord...")
			reply := GetInitialStatesReply{}
			coordConn.Send("CoordAPIMiner.GetInitialStates", GetInitialStatesArgs{}, &reply)
			dlBlockchain = reply.Blockchain
			dlLastHash = reply.LastHash
		} else {
			// peers exist, download from a peer
			log.Println("[INFO] Downloading system states from peers...")
			for len(activeMinerPeers) > 0 {
				i := 0
				for i < len(activeMinerPeers) { // attempt to download from selected peer
					log.Println("[INFO] Downloading from " + activeMinerPeers[i].Identifier + "...")
					// get txn pool from the peer
					toPullMinerAddr := activeMinerPeers[i].APIAddr
					minerClient, err := rpc.Dial("tcp", toPullMinerAddr)
					if err != nil {
						i++
						continue
					}
					reply := DownloadReply{}
					err = minerClient.Call("EntryPointAPI.Download", DownloadArgs{}, &reply)
					if err != nil {
						i++
						continue
					}
					dlBlockchain = reply.BlockChain
					dlLastHash = reply.LastHash
					dlTxnPool = reply.MemoryPool
					log.Printf("[INFO] Pool size %d (get from peer)\n", m.MemoryPool.Size())
					break
				}
				if i == len(activeMinerPeers) {
					log.Println("[INFO] Failed to download from any peer, retrieving latest peers from coord...")
					// if all peers failed, contact coord again for updated peer address list
					coordConn.Send("CoordAPIMiner.GetPeers", GetPeersArgs{}, &reply)
					activeMinerPeers = []gossip.Peer{}
					for _, p := range reply.Peers {
						if p.Type == gossip.TypeMiner && p.Active {
							activeMinerPeers = append(activeMinerPeers, p)
						}
					}
				} else {
					break
				}
			}
		}
		// 2.1.4 Set up local states
		// 2.1.4.1 Setup blockchain
		m.InitBlockchain(resume, dlBlockchain, dlLastHash)

		// 2.1.4.2 Setup txn pool (download from any of its peers)
		log.Println("[INFO] Setting up memory pool...")
		m.MemoryPool.AddTxns(dlTxnPool)
		log.Printf("[INFO] Pool size %d (get from peer)\n", m.MemoryPool.Size())

		// 2.1.4.3 Setup peers
		peers = reply.Peers
	} else {
		// 2.2 Reload from disk
		// 2.2.1 Reload blockchain
		m.InitBlockchain(resume, nil, nil)
		// 2.2.2 Reload peers
		values, err := m.Storage.GetAllWithPrefix(NodeKeyPrefix)
		util.CheckErr(err, "[ERROR] error reloading node list")
		for _, val := range values {
			node := gossip.DecodeToPeer(val)
			if node.Type == gossip.TypeTracker && !node.Active {
				node.Active = true // mark all tracker nodes as active to reduce the chance of isolation
			}
			peers = append(peers, node)
		}
	}

	// 3. Start API services
	minerIP := config.MinerAddr[0:strings.Index(config.MinerAddr, ":")]
	entryPointAPI := new(EntryPointAPI)
	entryPointAPI.e = m
	clientListenAddr, err := util.NewRPCServerWithIp(entryPointAPI, minerIP)
	if err != nil {
		return errors.New("cannot start API service for client")
	}
	m.Info.ClientListenAddr = clientListenAddr
	log.Println("[INFO] Listen to clients' API requests at", m.Info.ClientListenAddr)

	// 4. Setup gossip client
	err = m.InitGossip(minerIP, peers)
	if err != nil {
		return err
	}

	// starting internal services
	log.Println("[INFO] Starting routines...")
	go m.TxnService()
	go m.BlockService()
	go m.MiningService()
	go m.DigestUpdates()

	log.Printf("[INFO] %s joined successfully\n", config.MinerId)

	m.start = true
	m.cond.Broadcast()
	m.mu.Unlock()

	count := 0
	for {
		time.Sleep(10 * time.Second)
		count++
		if count == 1 {
			// save peer info every 10 seconds
			nodeList := gossip.GetPeers(true, false)
			for _, node := range nodeList {
				if node.Type == gossip.TypeMiner {
					_ = m.Storage.Put(util.DBKeyWithPrefix(NodeKeyPrefix, []byte(node.Identifier)), node.Encode())
				}
			}

			// TODO: might need to contact coord again if all peers are down.

		} else if count == 3 {
			// print chain every 30 seconds
			//m.PrintChain()
			count = 0
		}
	}
	return nil
}

func (m *Miner) InitStorage() (resume bool) {
	storageDir := "./storage/" + m.Info.NodeID
	if _, err := os.Stat(storageDir); err == nil {
		log.Println("[INFO] Reloading storage...")
		err := m.Storage.Load(storageDir)
		util.CheckErr(err, "[ERROR] error when reloading database")
		resume = true
	} else if os.IsNotExist(err) {
		log.Println("[INFO] Setting up storage...")
		err := m.Storage.New(storageDir, false)
		util.CheckErr(err, "[ERROR] error when creating database")
		resume = false
	} else {
		util.CheckErr(err, "[ERROR] OS error")
	}
	return resume
}

func (m *Miner) InitBlockchain(resume bool, chainData [][]byte, lastHash []byte) {
	if !resume {
		log.Println("[INFO] Setting up blockchain...")
		m.Blockchain = blockchain.NewBlockChain(m.Storage)
		err := m.Blockchain.ResumeFromEncodedData(chainData, lastHash)
		util.CheckErr(err, "[ERROR] error resuming blockchain")
	} else {
		log.Println("[INFO] Reloading blockchain...")
		m.Blockchain = blockchain.NewBlockChain(m.Storage)
		err := m.Blockchain.ResumeFromDB()
		util.CheckErr(err, "[ERROR] error reloading blockchain")
	}
}

func (m *Miner) InitGossip(ip string, peers []gossip.Peer) error {
	log.Println("[INFO] Setting up gossip client...")

	// Reconstruct existing updates (need to ensure order)
	var existingUpdates []gossip.Update

	blockBytes, _ := m.Blockchain.Encode()
	blockMap := make(map[int][]blockchain.Block) // to order blocks based on block num
	txnExistMap := make(map[string]bool)         // to avoid duplicate txns
	// Existing txn updates from blockchain (should be added before blocks)
	for _, data := range blockBytes {
		block := blockchain.DecodeToBlock(data)
		blockMap[int(block.BlockNum)] = append(blockMap[int(block.BlockNum)], *block)
		for _, txn := range block.Txns {
			if !txnExistMap[string(txn.ID)] {
				txnExistMap[string(txn.ID)] = true
				existingUpdates = append(existingUpdates, gossip.NewUpdate(gossip.TransactionIDPrefix, txn.ID, txn.Serialize()))
			}
		}
	}
	// Existing block updates from blockchain (should be added based on their positions in the blockchain)
	for i := 0; i < len(blockMap); i++ { // block num starts from 0
		for _, block := range blockMap[i] {
			existingUpdates = append(existingUpdates, gossip.NewUpdate(gossip.BlockIDPrefix, block.Hash, block.Encode()))
		}
	}
	// Existing txn update from pool
	for _, txn := range m.MemoryPool.All() {
		if !txnExistMap[string(txn.ID)] {
			txnExistMap[string(txn.ID)] = true
			existingUpdates = append(existingUpdates, gossip.NewUpdate(gossip.TransactionIDPrefix, txn.ID, txn.Serialize()))
		}
	}

	queryChan, updateChan, gossipAddr, err := gossip.Start(
		2,
		gossip.OpModePushPull,
		gossip.TriggerNewUpdate,
		ip,
		peers,
		existingUpdates,
		&gossip.Peer{
			Identifier:   m.Info.NodeID,
			APIAddr:      m.Info.ClientListenAddr,
			Type:         gossip.TypeMiner,
			Subscription: gossip.SubscribeTxn | gossip.SubscribeBlock,
		},
		true)
	m.Info.GossipAddr = gossipAddr
	m.queryChan = queryChan
	m.updateChan = updateChan
	return err
}

func (m *Miner) DigestUpdates() {
	// receive update from peers and notify respective service
	for {
		select {
		case update := <-m.queryChan:
			if strings.Contains(update.ID, gossip.BlockIDPrefix) {
				m.BlockRecvChan <- blockchain.DecodeToBlock(update.Data)
			} else if strings.Contains(update.ID, gossip.TransactionIDPrefix) {
				txn := blockchain.DeserializeTransaction(update.Data)
				m.TxnRecvChan <- &(txn)
			}
		}
	}
}

func (m *Miner) TxnService() {
	for !m.start {
	}
	for {
		txn := <-m.TxnRecvChan
		m.rtMu.Lock()
		sid := string(txn.ID)
		// check if the txn is unseen
		if !m.ReceivedTxns[sid] {
			// add unseen txn to pool
			m.ReceivedTxns[sid] = true
			m.rtMu.Unlock()
			m.MemoryPool.AddTxn(*txn)
			log.Printf("[INFO] Pool size %d (receive txn)\n", m.MemoryPool.Size())
		} else {
			m.rtMu.Unlock()
		}
	}
}

func (m *Miner) BlockService() {
	for !m.start {
	}
	for {
		block := <-m.BlockRecvChan
		// verify proof of work
		pow := blockchain.NewProof(block)
		if pow.Validate() {
			m.mu.Lock()
			prevLastHash := m.Blockchain.GetLastHash()
			success, newTxns, _ := m.Blockchain.Put(*block, false)
			curLastHash := m.Blockchain.GetLastHash()
			if success {
				if newTxns == nil { // no fork switching
					if bytes.Compare(prevLastHash, curLastHash) != 0 {
						// new block is on the current chain
						log.Printf("[INFO] New block (%x) from peers is added to the current chain\n", block.Hash[:5])
						blockchain.PrintBlock(block)
						// remove new block's txns from pool
						m.MemoryPool.RemoveTxns(block.Txns)
						log.Printf("[INFO] Pool size %d (remove included txns)\n", m.MemoryPool.Size())
						// notify mining service of new last hash
						m.ChainUpdatedChan <- 1
					} else {
						// new block is not on the current chain, just ignore it
						log.Printf("[INFO] New block (%x) from peers is added to an alternative fork\n", block.Hash[:5])
						blockchain.PrintBlock(block)
					}
				} else {
					// new longest chain!
					log.Printf("[INFO] New block (%x) from peers is added to an alternative branch\n", block.Hash[:5])
					blockchain.PrintBlock(block)
					log.Println("[INFO] Switching to a new chain")
					// TODO: we no longer recover the transactions that are in the uncle block.
					//// first, prepend old txns that get kicked out b.c. it is not on the longest chain anymore
					//for i := len(oldTxns) - 1; i >= 0; i-- {
					//	m.MemoryPool.PendingTxns = append([]blockchain.Transaction{*oldTxns[i]}, m.MemoryPool.PendingTxns...)
					//}
					// then, remove new transactions in the new fork from pool
					// this includes the txns that are in the new block
					// NOTE: this must be done second as there may be overlap between the two sets of txns
					m.MemoryPool.RemoveTxns(newTxns)
					log.Printf("[INFO] Pool size %d (switch fork)\n", m.MemoryPool.Size())
					// notify mining service of new last hash
					m.ChainUpdatedChan <- 1
				}
			}
			m.mu.Unlock()
		}
	}
}

func (m *Miner) MiningService() {
	for !m.start {
	}
	time.Sleep(3 * time.Second) // wait for a few seconds until everything is on sync
	newCycle := true
	var cycleStartTime time.Time
	var pow blockchain.ProofOfWork
	for {
		select {
		case <-m.ChainUpdatedChan:
			{
				newCycle = true
			}
		default:
			{
				if newCycle {
					// start a new mining cycle
					m.mu.Lock() // lock to prevent new block put or new txn
					cycleStartTime = time.Now()
					newCycle = false
					prevHash := m.Blockchain.GetLastHash()
					// select txns from pool
					selectedTxns := m.selectTxns()
					// validate txns
					valids := m.Blockchain.ValidatePendingTxns(selectedTxns)
					var validatedTxns []*blockchain.Transaction
					var invalidTxid [][]byte
					// only include valid txns
					for idx, valid := range valids {
						if valid {
							validatedTxns = append(validatedTxns, selectedTxns[idx])
						} else {
							invalidTxid = append(invalidTxid, selectedTxns[idx].ID)
						}
					}
					// remove invalid txns from pool
					m.MemoryPool.RemoveTxnsByTxID(invalidTxid)
					log.Printf("[INFO] Pool size %d (remove invalid txns)\n", m.MemoryPool.Size())

					// construct current block
					height := m.Blockchain.Get(prevHash).BlockNum + 1
					timestamp := time.Now().Unix() // update block timestamp
					difficulty := m.Blockchain.AdjustDifficulty(prevHash, m.Blockchain.Get(prevHash).Difficulty, timestamp, height)
					block := blockchain.Block{
						PrevHash:   prevHash,
						BlockNum:   height,
						Timestamp:  timestamp,
						Difficulty: difficulty,
						Nonce:      0,
						Txns:       validatedTxns,
						MinerID:    m.Info.NodeID,
						Hash:       []byte{},
					}
					// create a proof of work instance
					pow = *blockchain.NewProof(&block)
					m.mu.Unlock()
				} else {
					// continue mining
					if pow.Next(true) { // new block mined
						m.mu.Lock() // lock to prevent concurrent chain update and other things
						// if there is already a chain update, just discard the new block. Otherwise, safe to put
						if len(m.ChainUpdatedChan) == 0 { // no chain update
							block := *pow.Block

							// try to put new block
							success, newTxns, oldTxns := m.Blockchain.Put(block, true)
							// if there is no chain update since the start of this mining cycle, then fork switch impossible
							if newTxns != nil || oldTxns != nil { // sanity check
								log.Println("[WARN] Local put causes unexpected fork switch")
							}
							if success {
								elapsed := time.Since(cycleStartTime).Seconds()
								log.Printf("[INFO] New block (%x) mined in %v seconds\n", block.Hash[:5], elapsed)
								blockchain.PrintBlock(&block)
								// broadcast it first!
								m.updateChan <- gossip.NewUpdate(gossip.BlockIDPrefix, block.Hash, block.Encode())

								// remove included txns from pending pool
								m.MemoryPool.RemoveTxns(block.Txns)
								log.Printf("[INFO] Pool size %d (remove included txns)\n", m.MemoryPool.Size())
							}
						}
						m.mu.Unlock()
						newCycle = true
					}
				}
			}
		}
	}
}

func (m *Miner) selectTxns() (selectedTxn []*blockchain.Transaction) {
	selected := m.MemoryPool.Get(uint(m.Config.MaxTxn))
	for i := range selected {
		selectedTxn = append(selectedTxn, &selected[i])
	}
	return
}

// TODO: refactor print chain
//func (m *Miner) PrintChain() {
//	votes, txns := m.Blockchain.VotingStatus()
//
//	log.Println("[INFO] Printing...")
//	fv, err := os.Create("./" + m.Info.NodeID + "votes.txt")
//	util.CheckErr(err, "Unable to create votes.txt")
//	defer fv.Close()
//	for idx, _ := range votes {
//		fv.WriteString(fmt.Sprintf("%s,%d\n", m.Candidates[idx].CandidateData.CandidateName, votes[idx]))
//	}
//	fv.Sync()
//	ft, err := os.Create("./" + m.Info.NodeID + "txns.txt")
//	util.CheckErr(err, "Unable to create txns.txt")
//	defer ft.Close()
//	for _, txn := range txns {
//		ft.WriteString(fmt.Sprintf("%x,%s,%s\n", txn.ID, txn.Data.VoterName, txn.Data.VoterCandidate))
//	}
//	ft.Sync()
//	log.Println("[INFO] Printed.")
//}

// ----- APIs

func (m *Miner) ReceiveTxn(txn *blockchain.Transaction) bool {
	m.rtMu.Lock()
	defer m.rtMu.Unlock()
	if !m.ReceivedTxns[string(txn.ID)] {
		// internal processing
		m.TxnRecvChan <- txn
		// broadcast
		m.updateChan <- gossip.NewUpdate(gossip.TransactionIDPrefix, txn.ID, txn.Serialize())
		return false
	} else {
		return true
	}
}

func (m *Miner) CheckTxnStatus(txID []byte) blockchain.TransactionStatus {
	return m.Blockchain.TxnStatus(txID)
}

func (m *Miner) CheckResults(pollID string) blockchain.PollMeta {
	return m.Blockchain.VotingStatus(pollID)
}

func (m *Miner) Download() (encodedBlockchain [][]byte, lastHash []byte, txnPool []blockchain.Transaction, peers []gossip.Peer) {
	// prepare reply data
	m.mu.Lock() // lock ensures that blockchain data and memory pool are from the same moment
	encodedBlockchain, lastHash = m.Blockchain.Encode()
	txnPool = m.MemoryPool.All()
	m.mu.Unlock()

	peers = gossip.GetPeers(false, false) // its peers and itself
	return
}

type EntryPoint interface {
	ReceiveTxn(*blockchain.Transaction) bool
	CheckTxnStatus([]byte) blockchain.TransactionStatus
	CheckResults(string) blockchain.PollMeta
	Download() ([][]byte, []byte, []blockchain.Transaction, []gossip.Peer)
}

type EntryPointAPI struct {
	e EntryPoint
}

func (api *EntryPointAPI) Download(args DownloadArgs, reply *DownloadReply) error {
	bc, lh, pool, peers := api.e.Download()
	*reply = DownloadReply{
		BlockChain: bc,
		LastHash:   lh,
		MemoryPool: pool,
		Peers:      peers,
	}
	return nil
}

// SubmitTxn is for client to submit a transaction. This function is non-blocking.
func (api *EntryPointAPI) SubmitTxn(args SubmitTxnArgs, reply *SubmitTxnReply) error {
	// check signature first, directly reject txn with invalid signature
	if !args.Txn.Verify() {
		return &InvalidSignatureError{}
	}
	*reply = SubmitTxnReply{Exist: api.e.ReceiveTxn(&args.Txn)}
	return nil
}

// QueryTxn queries a transaction in the system and returns the number of blocks that confirm it.
func (api *EntryPointAPI) QueryTxn(args QueryTxnArgs, reply *QueryTxnReply) error {
	*reply = QueryTxnReply{Status: api.e.CheckTxnStatus(args.TxID)}
	return nil
}

func (api *EntryPointAPI) QueryResults(args QueryResultsArgs, reply *QueryResultsReply) error {
	pollMeta := api.e.CheckResults(args.PollID)
	*reply = QueryResultsReply{Results: pollMeta}
	return nil
}
