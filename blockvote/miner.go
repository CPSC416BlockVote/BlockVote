package blockvote

import (
	"bytes"
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	fchecker "cs.ubc.ca/cpsc416/BlockVote/fcheck"
	"cs.ubc.ca/cpsc416/BlockVote/gossip"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"errors"
	"github.com/DistributedClocks/tracing"
	"log"
	"math"
	"net/rpc"
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

type MinerInfo struct {
	MinerId          string
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

func (m *Miner) Start(minerId string, coordAddr string, minerAddr string, difficulty uint8, maxTxn uint8, mtrace *tracing.Tracer) error {
	m.MaxTxn = maxTxn
	m.Info.MinerId = minerId
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

	// fcheck
	ackPort, _, err := fchecker.Start(fchecker.StartStruct{
		LocalIP: minerIP,
	})
	if err != nil {
		return errors.New("cannot start fcheck")
	}
	m.Info.AckAddr = minerIP + ":" + ackPort
	defer fchecker.Stop()

	// Miner join
	log.Println("[INFO] Retrieving infomation from coord...")
	coordClient, err := util.NewRPCClient(minerAddr, coordAddr)
	for err != nil {
		log.Println("[INFO] Reattempting to establish connection with coord...")
		coordClient, err = util.NewRPCClient(minerAddr, coordAddr)
	}
	// download blockchain from coord
	downloadReply := DownloadReply{}
	err = coordClient.Call("CoordAPIMiner.Download", DownloadArgs{}, &downloadReply)
	for err != nil {
		log.Println("[INFO] Reattempting to download data from coord...")
		for {
			// rpc connection is interrupted, need to reconnect
			coordClient, err = util.NewRPCClient(minerAddr, coordAddr)
			if err == nil {
				break
			}
		}
		err = coordClient.Call("CoordAPIMiner.Download", DownloadArgs{}, &downloadReply)
	}

	// setup candidates
	log.Println("[INFO] Setting up candidates...")
	for _, cand := range downloadReply.Candidates {
		wallets := Identity.DecodeToWallets(cand)
		m.Candidates = append(m.Candidates, *wallets)
	}

	// setup blockchain
	log.Println("[INFO] Setting up blockchain...")
	var candidates []*Identity.Wallets
	for _, cand := range downloadReply.Candidates {
		candidates = append(candidates, Identity.DecodeToWallets(cand))
	}
	m.Blockchain = blockchain.NewBlockChain(m.Storage, candidates)
	err = m.Blockchain.ResumeFromEncodedData(downloadReply.BlockChain, downloadReply.LastHash)
	if err != nil {
		return errors.New("cannot resume blockchain")
	}

	// setup txn pool (download from any of its peers)
	log.Println("[INFO] Setting up memory pool...")
	for len(downloadReply.PeerAddrList) > 0 { // only need to download txn pool if there are existing miners
		i := 0
		for i < len(downloadReply.PeerAddrList) { // attempt to download txn pool from selected peer
			// get txn pool from the peer
			toPullMinerAddr := downloadReply.PeerAddrList[i]
			minerClient, err := rpc.Dial("tcp", toPullMinerAddr)
			if err != nil {
				i++
				continue
			}
			reply := GetTxnPoolReply{}
			err = minerClient.Call("MinerAPIMiner.GetTxnPool", GetTxnPoolArgs{}, &reply)
			if err != nil {
				i++
				continue
			}
			m.MemoryPool = reply.PeerTxnPool
			log.Printf("[INFO] Pool size %d (get from peer)\n", len(m.MemoryPool.PendingTxns))
			break
		}
		if i == len(downloadReply.PeerAddrList) {
			// if all peers failed, contact coord again for updated peer address list
			err = coordClient.Call("CoordAPIMiner.Download", DownloadArgs{}, &downloadReply)
			for err != nil {
				for {
					// rpc connection is interrupted, need to reconnect
					coordClient, err = util.NewRPCClient(minerAddr, coordAddr)
					if err == nil {
						break
					}
				}
				err = coordClient.Call("CoordAPIMiner.Download", DownloadArgs{}, &downloadReply)
			}
		} else {
			break
		}
	}

	// setup gossip client
	log.Println("[INFO] Setting up gossip client...")
	var existingUpdates []gossip.Update
	blockchainData, _ := m.Blockchain.Encode()
	for _, data := range blockchainData {
		existingUpdates = append(existingUpdates, gossip.NewUpdate(BlockIDPrefix, blockchain.DecodeToBlock(data).Hash, data))
	}
	for _, txn := range m.MemoryPool.PendingTxns {
		existingUpdates = append(existingUpdates, gossip.NewUpdate(TransactionIDPrefix, txn.ID, txn.Serialize()))
	}
	queryChan, updateChan, gossipAddr, err := gossip.Start(
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
	m.queryChan = queryChan
	m.updateChan = updateChan

	// starting internal services
	log.Println("[INFO] Starting routines...")
	go m.TxnService()
	go m.BlockService()
	go m.MiningService()

	log.Println("[INFO] Registering...")
	reply := RegisterReply{}
	err = coordClient.Call("CoordAPIMiner.Register", RegisterArgs{m.Info}, &reply)
	for err != nil {
		for {
			// rpc connection is interrupted, need to reconnect
			coordClient, err = util.NewRPCClient(minerAddr, coordAddr)
			if err == nil {
				break
			}
		}
		err = coordClient.Call("CoordAPIMiner.Register", RegisterArgs{m.Info}, &reply)
	}
	gossip.SetPeers(reply.PeerGossipAddrList)

	log.Printf("[INFO] %s joined successfully\n", minerId)
	m.start = true
	m.cond.Broadcast()
	m.mu.Unlock()

	// receive update from peers and notify respective service
	for {
		select {
		case update := <-queryChan:
			if strings.Contains(update.ID, BlockIDPrefix) {
				m.BlockRecvChan <- blockchain.DecodeToBlock(update.Data)
			} else if strings.Contains(update.ID, TransactionIDPrefix) {
				txn := blockchain.DeserializeTransaction(update.Data)
				m.TxnRecvChan <- &(txn)
			}
		}
	}
	return nil
}

func (m *Miner) TxnService() {
	for !m.start {
	}
	for {
		txn := <-m.TxnRecvChan
		m.mu.Lock()
		sid := string(txn.ID)
		// check if the txn is unseen
		if !m.ReceivedTxns[sid] {
			// add unseen txn to pool
			m.ReceivedTxns[sid] = true
			m.MemoryPool.PendingTxns = append(m.MemoryPool.PendingTxns, *txn)
			log.Printf("[INFO] Pool size %d (receive txn)\n", len(m.MemoryPool.PendingTxns))
		}
		m.mu.Unlock()
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
					// first, add old txns that get kicked out b.c. it is not on the longest chain anymore
					for _, oldTxn := range oldTxns {
						m.MemoryPool.PendingTxns = append(m.MemoryPool.PendingTxns, *oldTxn)
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
							invalidTxid = append(invalidTxid[:0], invalidTxid[1:]...)
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
						MinerID:  m.Info.MinerId,
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
								m.updateChan <- gossip.NewUpdate(BlockIDPrefix, block.Hash, block.Encode())

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

func (m *Miner) updateBlockChainAndTxnPool(block blockchain.Block, own bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	success, newTxn, oldTxn := m.Blockchain.Put(block, own)
	if success {
		// the block has been added to the blockchain
		existID := make(map[string]bool)
		for _, txn := range block.Txns {
			existID[string(txn.ID)] = true
			m.ReceivedTxns[string(txn.ID)] = true
		}
		if newTxn != nil && oldTxn != nil {
			// switched fork
			for _, txn := range newTxn {
				existID[string(txn.ID)] = true
				m.ReceivedTxns[string(txn.ID)] = true
			}
			// add uncommitted txns to pool
			for _, txn := range oldTxn {
				if !existID[string(txn.ID)] {
					m.MemoryPool.PendingTxns = append(m.MemoryPool.PendingTxns, *txn)
					m.ReceivedTxns[string(txn.ID)] = true
				}
			}
		}
		// remove the committed txns from pool
		for i, txn := range m.MemoryPool.PendingTxns {
			if existID[string(txn.ID)] {
				m.MemoryPool.PendingTxns = append(m.MemoryPool.PendingTxns[:i], m.MemoryPool.PendingTxns[i+1:]...)
			}
		}
	}
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

// SubmitTxn is for client to submit a transaction. This function is non-blocking.
func (api *MinerAPIClient) SubmitTxn(args SubmitTxnArgs, reply *SubmitTxnReply) error {
	// internal processing
	api.m.TxnRecvChan <- &(args.Txn)
	// broadcast
	api.m.updateChan <- gossip.NewUpdate(TransactionIDPrefix, args.Txn.ID, args.Txn.Serialize())

	return nil
}
