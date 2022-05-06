package blockvote

import (
	"bytes"
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/gossip"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"errors"
	"fmt"
	"log"
	"math"
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
	Difficulty        uint8
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
	Candidates [][]byte
	MemoryPool TxnPool
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
	NumConfirmed int
}

type QueryResultsArgs struct {
}

type QueryResultsReply struct {
	Votes []uint
}

type Miner struct {
	// Miner state may go here
	Storage    *util.Database
	Blockchain *blockchain.BlockChain

	Info         NodeInfo
	rtMu         sync.Mutex
	ReceivedTxns map[string]bool
	Candidates   []Identity.Wallets
	MemoryPool   TxnPool
	MaxTxn       uint8

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
		TxnRecvChan:      make(chan *blockchain.Transaction, 500),
		BlockRecvChan:    make(chan *blockchain.Block, 50),
		ChainUpdatedChan: make(chan int, 50),
	}
}

type TxnPool struct {
	PendingTxns []blockchain.Transaction
}

func (m *Miner) Start(minerId string, coordAddr string, minerAddr string, maxTxn uint8) error {
	m.MaxTxn = maxTxn
	m.Info.NodeID = minerId
	err := m.Storage.New("", true)
	if err != nil {
		util.CheckErr(err, "error when creating database")
	}
	defer m.Storage.Close()

	m.cond = sync.NewCond(&m.mu)
	m.mu.Lock()
	// starting API services
	minerIP := minerAddr[0:strings.Index(minerAddr, ":")]

	// << client
	entryPointAPI := new(EntryPointAPI)
	entryPointAPI.e = m
	clientListenAddr, err := util.NewRPCServerWithIp(entryPointAPI, minerIP)
	if err != nil {
		return errors.New("cannot start API service for client")
	}
	m.Info.ClientListenAddr = clientListenAddr
	log.Println("[INFO] Listen to clients' API requests at", m.Info.ClientListenAddr)

	// Miner join
	log.Println("[INFO] Retrieving information from coord...")
	coordClient, err := util.NewRPCClient(minerAddr, coordAddr)
	for err != nil {
		log.Println("[INFO] Reattempting to establish connection with coord...")
		coordClient, err = util.NewRPCClient(minerAddr, coordAddr)
	}
	// get peers from coord
	reply := GetPeersReply{}
	err = coordClient.Call("CoordAPIMiner.GetPeers", GetPeersArgs{}, &reply)
	for err != nil {
		log.Println("[INFO] Reattempting to get peers from coord...")
		for {
			// rpc connection is interrupted, need to reconnect
			coordClient, err = util.NewRPCClient(minerAddr, coordAddr)
			if err == nil {
				break
			}
		}
		err = coordClient.Call("CoordAPIMiner.GetPeers", GetPeersArgs{}, &reply)
	}
	// download data from peers
	var dlCandidates [][]byte
	var dlBlockchain [][]byte
	var dlLastHash []byte
	var dlTxnPool TxnPool
	var minerPeers []gossip.Peer
	for _, p := range reply.Peers {
		if p.Type == gossip.TypeMiner && p.Active {
			minerPeers = append(minerPeers, p)
		}
	}
	if len(minerPeers) == 0 {
		// no peers, download from coord
		log.Println("[INFO] Downloading initial system states from coord...")
		reply := GetInitialStatesReply{}
		err = coordClient.Call("CoordAPIMiner.GetInitialStates", GetInitialStatesArgs{}, &reply)
		for err != nil {
			for {
				// rpc connection is interrupted, need to reconnect
				coordClient, err = util.NewRPCClient(minerAddr, coordAddr)
				if err == nil {
					break
				}
			}
			err = coordClient.Call("CoordAPIMiner.GetInitialStates", GetInitialStatesArgs{}, &reply)
		}
		dlCandidates = reply.Candidates
		dlBlockchain = reply.Blockchain
		dlLastHash = reply.LastHash
	} else {
		// peers exist, download from a peer
		log.Println("[INFO] Downloading system states from peers...")
		for len(minerPeers) > 0 {
			i := 0
			for i < len(minerPeers) { // attempt to download from selected peer
				// get txn pool from the peer
				toPullMinerAddr := minerPeers[i].APIAddr
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
				dlCandidates = reply.Candidates
				dlBlockchain = reply.BlockChain
				dlLastHash = reply.LastHash
				dlTxnPool = reply.MemoryPool
				log.Printf("[INFO] Pool size %d (get from peer)\n", len(m.MemoryPool.PendingTxns))
				break
			}
			if i == len(minerPeers) {
				// if all peers failed, contact coord again for updated peer address list
				err = coordClient.Call("CoordAPIMiner.GetPeers", GetPeersArgs{}, &reply)
				for err != nil {
					for {
						// rpc connection is interrupted, need to reconnect
						coordClient, err = util.NewRPCClient(minerAddr, coordAddr)
						if err == nil {
							break
						}
					}
					err = coordClient.Call("CoordAPIMiner.GetPeers", GetPeersArgs{}, &reply)
				}
				minerPeers = []gossip.Peer{}
				for _, p := range reply.Peers {
					if p.Type == gossip.TypeMiner && p.Active {
						minerPeers = append(minerPeers, p)
					}
				}
			} else {
				break
			}
		}
	}
	// setup candidates
	log.Println("[INFO] Setting up candidates...")
	for _, cand := range dlCandidates {
		wallets := Identity.DecodeToWallets(cand)
		m.Candidates = append(m.Candidates, *wallets)
	}

	// setup blockchain
	log.Println("[INFO] Setting up blockchain...")
	var candidates []*Identity.Wallets
	for _, cand := range dlCandidates {
		candidates = append(candidates, Identity.DecodeToWallets(cand))
	}
	m.Blockchain = blockchain.NewBlockChain(m.Storage, candidates)
	err = m.Blockchain.ResumeFromEncodedData(dlBlockchain, dlLastHash)
	if err != nil {
		return errors.New("cannot resume blockchain")
	}

	// setup txn pool (download from any of its peers)
	log.Println("[INFO] Setting up memory pool...")
	m.MemoryPool = dlTxnPool
	log.Printf("[INFO] Pool size %d (get from peer)\n", len(m.MemoryPool.PendingTxns))

	// setup gossip client
	log.Println("[INFO] Setting up gossip client...")
	var existingUpdates []gossip.Update
	blockchainData, _ := m.Blockchain.Encode()
	for _, data := range blockchainData { // existing block updates
		existingUpdates = append(existingUpdates, gossip.NewUpdate(gossip.BlockIDPrefix, blockchain.DecodeToBlock(data).Hash, data))
	}
	for _, txn := range m.MemoryPool.PendingTxns { // existing txn update from pool
		existingUpdates = append(existingUpdates, gossip.NewUpdate(gossip.TransactionIDPrefix, txn.ID, txn.Serialize()))
	}
	iter := m.Blockchain.NewIterator(m.Blockchain.GetLastHash())
	for block, end := iter.Next(); !end; block, end = iter.Next() { // existing txn update from the longest chain
		for _, txn := range block.Txns {
			exist := false
			for idx, pendingTxn := range m.MemoryPool.PendingTxns { // check duplicate
				if bytes.Compare(txn.ID, pendingTxn.ID) == 0 {
					m.MemoryPool.PendingTxns = append(m.MemoryPool.PendingTxns[:idx], m.MemoryPool.PendingTxns[idx+1:]...)
					exist = true
					break
				}
			}
			if !exist {
				existingUpdates = append(existingUpdates, gossip.NewUpdate(gossip.TransactionIDPrefix, txn.ID, txn.Serialize()))
			}
		}
	}
	queryChan, updateChan, gossipAddr, err := gossip.Start(
		2,
		"PushPull",
		gossip.TriggerNewUpdate,
		minerIP,
		reply.Peers,
		existingUpdates,
		gossip.Peer{
			Identifier: minerId,
			APIAddr:    clientListenAddr,
			Active:     true,
			Type:       gossip.TypeMiner,
		},
		true)
	if err != nil {
		return err
	}
	m.Info.GossipAddr = gossipAddr
	m.queryChan = queryChan
	m.updateChan = updateChan

	// starting internal services
	log.Println("[INFO] Starting routines...")
	go m.TxnService()
	go m.BlockService()
	go m.MiningService()
	go m.DigestUpdates()

	log.Printf("[INFO] %s joined successfully\n", minerId)

	m.start = true
	m.cond.Broadcast()
	m.mu.Unlock()

	for {
		m.PrintChain()
		time.Sleep(time.Minute)
	}
	return nil
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
			m.mu.Lock()
			m.MemoryPool.PendingTxns = append(m.MemoryPool.PendingTxns, *txn)
			m.mu.Unlock()
			log.Printf("[INFO] Pool size %d (receive txn)\n", len(m.MemoryPool.PendingTxns))
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
			success, newTxns, oldTxns := m.Blockchain.Put(*block, false)
			curLastHash := m.Blockchain.GetLastHash()
			if success {
				if newTxns == nil { // no fork switching
					if bytes.Compare(prevLastHash, curLastHash) != 0 {
						// new block is on the current chain
						log.Printf("[INFO] New block (%x) from peers is added to the current chain\n", block.Hash[:5])
						blockchain.PrintBlock(block)
						// remove new block's txns from pool
						for i := 0; i < len(m.MemoryPool.PendingTxns); {
							rm := false
							for j := 0; j < len(block.Txns); j++ {
								if bytes.Compare(m.MemoryPool.PendingTxns[i].ID, block.Txns[j].ID) == 0 {
									rm = true
								}
							}
							if rm {
								m.MemoryPool.PendingTxns = append(m.MemoryPool.PendingTxns[:i], m.MemoryPool.PendingTxns[i+1:]...)
							} else {
								i++
							}
						}
						log.Printf("[INFO] Pool size %d (remove included txns)\n", len(m.MemoryPool.PendingTxns))
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
					// first, prepend old txns that get kicked out b.c. it is not on the longest chain anymore
					for i := len(oldTxns) - 1; i >= 0; i-- {
						m.MemoryPool.PendingTxns = append([]blockchain.Transaction{*oldTxns[i]}, m.MemoryPool.PendingTxns...)
					}
					// then, remove new transactions in the new fork from pool
					// this includes the txns that are in the new block
					// NOTE: this must be done second as there may be overlap between the two sets of txns
					for i := 0; i < len(m.MemoryPool.PendingTxns); {
						rm := false
						for j := 0; j < len(newTxns); j++ {
							if bytes.Compare(m.MemoryPool.PendingTxns[i].ID, newTxns[j].ID) == 0 {
								rm = true
							}
						}
						if rm {
							m.MemoryPool.PendingTxns = append(m.MemoryPool.PendingTxns[:i], m.MemoryPool.PendingTxns[i+1:]...)
						} else {
							i++
						}
					}
					log.Printf("[INFO] Pool size %d (switch fork)\n", len(m.MemoryPool.PendingTxns))
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
					valids := m.Blockchain.ValidateTxns(selectedTxns)
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
					for i := 0; i < len(m.MemoryPool.PendingTxns) && len(invalidTxid) > 0; {
						if bytes.Compare(invalidTxid[0], m.MemoryPool.PendingTxns[i].ID) == 0 {
							invalidTxid = invalidTxid[1:]
							m.MemoryPool.PendingTxns = append(m.MemoryPool.PendingTxns[:i], m.MemoryPool.PendingTxns[i+1:]...)
						} else {
							i++
						}
					}
					log.Printf("[INFO] Pool size %d (remove invalid txns)\n", len(m.MemoryPool.PendingTxns))
					// construct current block
					height := m.Blockchain.Get(m.Blockchain.GetLastHash()).BlockNum + 1
					block := blockchain.Block{
						PrevHash: prevHash,
						BlockNum: height,
						Nonce:    0,
						Txns:     validatedTxns,
						MinerID:  m.Info.NodeID,
						Hash:     []byte{},
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
								for i := 0; i < len(m.MemoryPool.PendingTxns); {
									rm := false
									for j := 0; j < len(block.Txns); j++ {
										if bytes.Compare(m.MemoryPool.PendingTxns[i].ID, block.Txns[j].ID) == 0 {
											rm = true
											break
										}
									}
									if rm {
										m.MemoryPool.PendingTxns = append(m.MemoryPool.PendingTxns[:i], m.MemoryPool.PendingTxns[i+1:]...)
									} else {
										i++
									}
								}
								log.Printf("[INFO] Pool size %d (remove included txns)\n", len(m.MemoryPool.PendingTxns))
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
	for i := 0; i < int(math.Min(float64(m.MaxTxn), float64(len(m.MemoryPool.PendingTxns)))); i++ {
		txn := m.MemoryPool.PendingTxns[i] // make a copy first. avoid pointing to the slot in slice.
		selectedTxn = append(selectedTxn, &txn)
	}
	return
}

func (m *Miner) PrintChain() {
	votes, txns := m.Blockchain.VotingStatus()
	fv, err := os.Create("./" + m.Info.NodeID + "votes.txt")
	util.CheckErr(err, "Unable to create votes.txt")
	defer fv.Close()
	for idx, _ := range votes {
		fv.WriteString(fmt.Sprintf("%s,%d\n", m.Candidates[idx].CandidateData.CandidateName, votes[idx]))
	}
	fv.Sync()
	ft, err := os.Create("./" + m.Info.NodeID + "txns.txt")
	util.CheckErr(err, "Unable to create txns.txt")
	defer ft.Close()
	for _, txn := range txns {
		ft.WriteString(fmt.Sprintf("%x,%s,%s\n", txn.ID, txn.Data.VoterName, txn.Data.VoterCandidate))
	}
	ft.Sync()
}

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

func (m *Miner) CheckTxn(txID []byte) int {
	return m.Blockchain.TxnStatus(txID)
}

func (m *Miner) CheckResults() []uint {
	votes, _ := m.Blockchain.VotingStatus()
	return votes
}

func (m *Miner) Download() (encodedBlockchain [][]byte, lastHash []byte, candidates [][]byte, txnPool TxnPool, peers []gossip.Peer) {
	// prepare reply data
	encodedBlockchain, lastHash = m.Blockchain.Encode()
	for _, cand := range m.Candidates {
		candidates = append(candidates, cand.Encode())
	}
	m.mu.Lock()
	txnPool = m.MemoryPool
	m.mu.Unlock()

	peers = append(gossip.GetPeers(), gossip.Identity) // its peers and itself
	return
}

type EntryPoint interface {
	ReceiveTxn(*blockchain.Transaction) bool
	CheckTxn([]byte) int
	CheckResults() []uint
	Download() ([][]byte, []byte, [][]byte, TxnPool, []gossip.Peer)
}

type EntryPointAPI struct {
	e EntryPoint
}

func (api *EntryPointAPI) Download(args DownloadArgs, reply *DownloadReply) error {
	bc, lh, cands, pool, peers := api.e.Download()
	*reply = DownloadReply{
		BlockChain: bc,
		LastHash:   lh,
		Candidates: cands,
		MemoryPool: pool,
		Peers:      peers,
	}
	return nil
}

// SubmitTxn is for client to submit a transaction. This function is non-blocking.
func (api *EntryPointAPI) SubmitTxn(args SubmitTxnArgs, reply *SubmitTxnReply) error {
	*reply = SubmitTxnReply{Exist: api.e.ReceiveTxn(&args.Txn)}
	return nil
}

// QueryTxn queries a transaction in the system and returns the number of blocks that confirm it.
func (api *EntryPointAPI) QueryTxn(args QueryTxnArgs, reply *QueryTxnReply) error {
	*reply = QueryTxnReply{NumConfirmed: api.e.CheckTxn(args.TxID)}
	return nil
}

func (api *EntryPointAPI) QueryResults(_ QueryResultsArgs, reply *QueryResultsReply) error {
	votes := api.e.CheckResults()
	*reply = QueryResultsReply{Votes: votes}
	return nil
}
