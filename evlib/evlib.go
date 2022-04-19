package evlib

import (
	"bufio"
	wallet "cs.ubc.ca/cpsc416/BlockVote/Identity"
	blockChain "cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"errors"
	"fmt"
	"github.com/DistributedClocks/tracing"
	"log"
	"math/rand"
	"net/rpc"
	"os"
	"strings"
	"sync"
	"time"
)

type TxnInfo struct {
	txn        blockChain.Transaction
	submitTime time.Time
	confirmed  bool
}

type EV struct {
	// Add EV instance state here.
	rw               sync.RWMutex // mutex for arrays
	connRw           sync.RWMutex // mutex for connections
	voterWallet      wallet.Wallets
	voterWalletAddr  string
	CandidateList    []string
	minerIpPort      string
	coordIPPort      string
	localMinerIPPort string
	localCoordIPPort string
	coordClient      *rpc.Client
	//minerClient      *rpc.Client
	//VoterTxnInfoMap map[string]TxnInfo
	//VoterTxnMap     map[string]blockChain.Transaction
	TxnInfos      []TxnInfo
	MinerAddrList []string

	ComplainCoordChan chan int // for all operations to complain about coord unavailability
	ComplainMinerChan chan int // for all operations to complain about no miner available
}

func NewEV() *EV {
	return &EV{
		ComplainCoordChan: make(chan int, 1000),
		ComplainMinerChan: make(chan int, 1000),
	}
}

// ----- evlib APIs -----
type VoterNameID struct {
	Name string
	ID   string
}

var quit chan bool
var voterInfo []VoterNameID
var thread = 35 * time.Second

func (d *EV) connectCoord() {
	// setup conn to coord
	client, err := rpc.Dial("tcp", d.coordIPPort)
	for err != nil {
		time.Sleep(3 * time.Second)
		client, err = rpc.Dial("tcp", d.coordIPPort)
	}
	d.coordClient = client
}

func (d *EV) connectMiner() (conn *rpc.Client) {
	// setup conn to miner
	for {
		d.rw.RLock()
		minerList := d.MinerAddrList[:] // make a copy
		d.rw.RUnlock()
		if len(minerList) > 0 {
			// randomly select a miner
			minerIpPort := minerList[rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(minerList))]
			// connect to it
			rpcClient, err := rpc.Dial("tcp", minerIpPort)
			if err != nil {
				// remove failed miner
				d.rw.Lock()
				d.MinerAddrList = sliceMinerList(minerIpPort, d.MinerAddrList)
				d.rw.Unlock()
			} else {
				conn = rpcClient
				return
			}
		} else {
			// no available miners, retrieve latest list from coord
			log.Println("[WARN] No miner available. Please wait...")
			d.ComplainMinerChan <- 1
			time.Sleep(time.Second)
		}
	}
}

// Start Starts the instance of EV to use for connecting to the system with the given coord's IP:port.
func (d *EV) Start(localTracer *tracing.Tracer, clientId uint, coordIPPort string) error {
	voterInfo = make([]VoterNameID, 0)
	d.coordIPPort = coordIPPort

	// setup conn to coord
	d.connectCoord()

	// get candidates from Coord
	log.Println("[INFO] Retrieving candidates from coord...")
	var candidatesReply *blockvote.GetCandidatesReply
	for {
		err := d.coordClient.Call("CoordAPIClient.GetCandidates", blockvote.GetCandidatesArgs{}, &candidatesReply)
		if err == nil {
			break
		} else {
			d.connectCoord()
		}
	}

	log.Println("[INFO] Retrieving miner list from coord...")
	// no need to retry when failed.
	var minerListReply *blockvote.GetMinerListReply
	err := d.coordClient.Call("CoordAPIClient.GetMinerList", blockvote.GetMinerListArgs{}, &minerListReply)
	if err == nil {
		d.MinerAddrList = minerListReply.MinerAddrList
	}

	// print all candidates Name
	canadiateName := make([]string, 0)
	for _, cand := range candidatesReply.Candidates {
		wallets := wallet.DecodeToWallets(cand)
		canadiateName = append(canadiateName, wallets.CandidateData.CandidateName)
	}
	d.CandidateList = canadiateName
	log.Println("List of candidate:", canadiateName)

	// Start internal services
	go d.CoordConnManager()
	go d.MinerListManager()

	quit = make(chan bool)
	go func() {
		// call coord for list of active miners with length N_Receives
		for {
			//var minerListReply *blockvote.GetMinerListReply
			//for {
			//	d.connRw.RLock()
			//	err := d.coordClient.Call("CoordAPIClient.GetMinerList", blockvote.GetMinerListArgs{}, &minerListReply)
			//	d.connRw.RUnlock()
			//	if err == nil && len(minerListReply.MinerAddrList) > 0 {
			//		break
			//	} else if err != nil {
			//		d.ComplainCoordChan <- 1
			//		time.Sleep(2 * time.Second)
			//	} else {
			//		log.Println("[WARN] No available miner. Waiting...")
			//		time.Sleep(5 * time.Second)
			//	}
			//}
			//
			//// random pick one miner addr
			//index := 0
			//d.MinerAddrList = minerListReply.MinerAddrList
			//if len(d.MinerAddrList) > 1 {
			//	index = rand.Intn(len(d.MinerAddrList) - 1)
			//}
			//d.minerIpPort = d.MinerAddrList[index]

			d.rw.RLock()
			allTxns := d.TxnInfos[:]
			d.rw.RUnlock()

			for idx, txnInfo := range allTxns {
				if !txnInfo.confirmed && time.Now().Sub(txnInfo.submitTime) > thread {
					// start query status
					var queryTxnReply *blockvote.QueryTxnReply
					d.connRw.RLock()
					err = d.coordClient.Call("CoordAPIClient.QueryTxn", blockvote.QueryTxnArgs{
						TxID: txnInfo.txn.ID,
					}, &queryTxnReply)
					d.connRw.RUnlock()
					if err == nil {
						if queryTxnReply.NumConfirmed > -1 {
							d.rw.Lock()
							d.TxnInfos[idx].confirmed = true // we can do this b.c. TxnInfos is append only
							d.rw.Unlock()
						} else {
							//log.Printf("[INFO] Resubmitting %x", txnInfo.txn.ID)
							d.submitTxn(txnInfo.txn)
							d.rw.Lock()
							d.TxnInfos[idx].submitTime = time.Now() // we can do this b.c. TxnInfos is append only
							d.rw.Unlock()
						}
					} else {
						// try again in the next cycle!
						d.ComplainCoordChan <- 1
					}
				}
			}

			select {
			case <-quit:
				// end
				return
			default:
				// Do other stuff
				time.Sleep(10 * time.Second)
			}
		}
	}()
	return nil
}

func (d *EV) CoordConnManager() {
	for {
		select {
		case <-d.ComplainCoordChan:
			{
				d.connRw.Lock()
				log.Println("[INFO] Reconnecting to coord...")
				d.connectCoord()
				d.connRw.Unlock()
				// digest remaining complains
				for {
					select {
					case <-d.ComplainCoordChan:
						continue
					default:
						break
					}
					break
				}
			}
		default:
			continue
		}
	}
}

func (d *EV) MinerListManager() {
	for {
		select {
		case <-d.ComplainMinerChan:
			{
				log.Println("[INFO] Retrieving miner list from coord...")
				var minerListReply *blockvote.GetMinerListReply
				for {
					// retrieve miner list
					d.connRw.RLock()
					err := d.coordClient.Call("CoordAPIClient.GetMinerList", blockvote.GetMinerListArgs{}, &minerListReply)
					d.connRw.RUnlock()
					if err == nil {
						d.rw.Lock()
						d.MinerAddrList = minerListReply.MinerAddrList
						d.rw.Unlock()
						break
					} else {
						// coord failed, complain about it and wait
						d.ComplainCoordChan <- 1
						time.Sleep(2 * time.Second)
					}
				}
				// digest remaining complains
				for {
					select {
					case <-d.ComplainMinerChan:
						continue
					default:
						break
					}
					break
				}
			}
		default:
			continue
		}
	}
}

// helper function for checking the existence of voter
func findVoterExist(from, to string) bool {
	for _, v := range voterInfo {
		if v.Name == from && v.ID == to {
			return true
		}
	}
	return false
}

// helper function for remove minerList
func sliceMinerList(mAddr string, minerList []string) []string {
	for i, v := range minerList {
		if mAddr == v {
			minerList = append(minerList[:i], minerList[i+1:]...)
			return minerList
		}
	}
	return minerList
}

// Vote API provides the functionality of voting
func (d *EV) Vote(ballot blockChain.Ballot) []byte {
	// create wallet for voter, only when such voter is not exist
	//if !findVoterExist(ballot.VoterName, ballot.VoterStudentID) {
	d.createVoterWallet(ballot)
	voterInfo = append(voterInfo, VoterNameID{
		Name: ballot.VoterName,
		ID:   ballot.VoterStudentID,
	})
	//}

	// create transaction
	txn := d.createTransaction(ballot)

	var submitTxnReply *blockvote.SubmitTxnReply
	for {
		// connect to miner
		conn := d.connectMiner()
		err := conn.Call("MinerAPIClient.SubmitTxn", blockvote.SubmitTxnArgs{Txn: txn}, &submitTxnReply)
		conn.Close()
		if err == nil {
			d.rw.Lock()
			d.TxnInfos = append(d.TxnInfos, TxnInfo{
				txn:        txn,
				submitTime: time.Now(),
				confirmed:  false,
			})
			d.rw.Unlock()
			break
		} else {
			log.Println("[WARN] Fail in SubmitTxn, retrying...")
		}
	}
	return txn.ID
}

func (d *EV) submitTxn(txn blockChain.Transaction) {

	var submitTxnReply *blockvote.SubmitTxnReply
	for {
		// setup conn to miner
		conn := d.connectMiner()
		err := conn.Call("MinerAPIClient.SubmitTxn", blockvote.SubmitTxnArgs{Txn: txn}, &submitTxnReply)
		conn.Close()
		if err == nil {
			break
		} else {
			log.Println("[WARN] Fail in SubmitTxn, retrying...")
		}
	}
}

// GetBallotStatus API checks the status of a transaction and returns the number of blocks that confirm it
func (d *EV) GetBallotStatus(TxID []byte) (int, error) {
	//retry := 0
	var queryTxnReply *blockvote.QueryTxnReply
	for {
		d.connRw.RLock()
		err := d.coordClient.Call("CoordAPIClient.QueryTxn", blockvote.QueryTxnArgs{
			TxID: TxID,
		}, &queryTxnReply)
		d.connRw.RUnlock()
		if err == nil {
			break
		} else {
			d.ComplainCoordChan <- 1
			time.Sleep(2 * time.Second)
		}
	}
	return queryTxnReply.NumConfirmed, nil
}

// GetCandVotes API retrieve the number of votes a candidate has.
func (d *EV) GetCandVotes(candidate string) (uint, error) {
	if len(d.CandidateList) == 0 {
		return 0, errors.New("Empty Candidates.\n")
	}
	var queryResultReply *blockvote.QueryResultsReply
	for {
		d.connRw.RLock()
		err := d.coordClient.Call("CoordAPIClient.QueryResults", blockvote.QueryResultsArgs{}, &queryResultReply)
		d.connRw.RUnlock()
		if err == nil {
			break
		} else {
			d.ComplainCoordChan <- 1
			time.Sleep(2 * time.Second)
		}
	}

	idx := 0
	//fmt.Println(queryResultReply)
	for i, cand := range d.CandidateList {
		if cand == candidate {
			idx = i
		}
	}
	return queryResultReply.Votes[idx], nil
}

// Stop Stops the EV instance.
// This call always succeeds.
func (d *EV) Stop() {
	quit <- true
	d.coordClient.Close()
	//d.minerClient.Close()
	return
}

// ----- evlib utility functions -----

func createBallot() blockChain.Ballot {
	// enter ballot
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter your name: ")
	voterName, _ := reader.ReadString('\n')

	reader = bufio.NewReader(os.Stdin)
	fmt.Print("Enter your studentID: ")
	voterId, _ := reader.ReadString('\n')
	// ^[0-9]{8}
	isIDValid := false
	candidateName := ""
	for !isIDValid {
		reader = bufio.NewReader(os.Stdin)
		fmt.Print("Vote your vote Candidate: ")
		candidateName, _ = reader.ReadString('\n')
	}

	ballot := blockChain.Ballot{
		strings.TrimRight(voterName, "\r\n"),
		strings.TrimRight(voterId, "\r\n"),
		strings.TrimRight(candidateName, "\r\n"),
	}
	return ballot
}

func (d *EV) createVoterWallet(ballot blockChain.Ballot) {
	v, err := wallet.CreateVoter(ballot.VoterName, ballot.VoterStudentID)
	if err != nil {
		log.Panic(err)
	}
	d.voterWallet = *v
	addr := d.voterWallet.AddWallet()
	d.voterWalletAddr = addr
	d.voterWallet.SaveFile()
}

func (d *EV) createTransaction(ballot blockChain.Ballot) blockChain.Transaction {
	txn := blockChain.Transaction{
		Data:      &ballot,
		ID:        nil,
		Signature: nil,
		PublicKey: d.voterWallet.Wallets[d.voterWalletAddr].PublicKey,
	}
	// client sign with private key
	txn.Sign(d.voterWallet.Wallets[d.voterWalletAddr].PrivateKey)
	return txn
}

//Client - Coord Interaction
//Clients need to contact coord before they issue transactions
//or when they check the status of the transactions.
//Before issuing a transaction, a client should retrieve a list of
//active miners from coord to select miners to send the transaction.
//However, a client should not contact coord whenever it wants to
//issue a transaction. To check the status of a transaction, clients
//should send the query to coord, and the coord will use its local
//copy of the blockchain to return the result.
//
//Client - Miner Interaction
//After receiving a list of active miners from coord, the client will
//select N_RECEIVERS miners to submit its transaction. When the client
//cannot find its transaction after a set timeout, it will resubmit the
//same transaction again.
