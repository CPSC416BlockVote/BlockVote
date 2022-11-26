package evlib

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	blockChain "cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"errors"
	"fmt"
	"github.com/DistributedClocks/tracing"
	"log"
	"math/rand"
	"net/rpc"
	"strconv"
	"sync"
	"time"
)

type TxnInfo struct {
	txn        blockChain.Transaction
	submitTime time.Time
	confirmed  bool
}

type EV struct {
	// AddTxns EV instance state here.
	rw               sync.RWMutex // mutex for arrays
	connRw           sync.RWMutex // mutex for connections
	ifRw             sync.RWMutex // mutex for info
	CandidateList    []string
	minerIpPort      string
	coordIPPort      string
	localMinerIPPort string
	localCoordIPPort string
	coordClient      *rpc.Client
	TxnInfos         []TxnInfo
	MinerAddrList    []string

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

const (
	SubmitRetry       = 3
	ConnectionTimeOut = 10 * time.Second
	CheckTxnRetry     = 0
	CheckTxnWait      = 2 * time.Minute
	CheckPollRetry    = 0
)

var quit chan bool

func (d *EV) connectCoord() {
	// setup conn to coord
	client, err := rpc.Dial("tcp", d.coordIPPort)
	for err != nil {
		time.Sleep(3 * time.Second)
		client, err = rpc.Dial("tcp", d.coordIPPort)
	}
	d.coordClient = client
}

func (d *EV) connectMiner() (conn *rpc.Client, err error) {
	// setup conn to miner
	start := time.Now() // start timer
	for time.Since(start) < ConnectionTimeOut {
		d.rw.RLock()
		minerList := make([]string, len(d.MinerAddrList))
		copy(minerList, d.MinerAddrList) // make a copy
		d.rw.RUnlock()
		if len(minerList) > 0 {
			// randomly select a miner
			minerIpPort := minerList[rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(minerList))]
			// connect to it
			conn, err = rpc.Dial("tcp", minerIpPort)
			if err != nil {
				// remove failed miner
				d.rw.Lock()
				d.MinerAddrList = sliceMinerList(minerIpPort, d.MinerAddrList)
				d.rw.Unlock()
			} else {
				return
			}
		} else {
			// no available miners, retrieve latest list from coord
			//log.Println("[WARN] No miner available. Please wait...")
			d.ComplainMinerChan <- 1
			time.Sleep(time.Second)
		}
	}

	if err != nil {
		// connection error
		return
	} else {
		// no available miner error
		return nil, errors.New("no available miner")
	}
}

// Start Starts the instance of EV to use for connecting to the system with the given coord's IP:port.
func (d *EV) Start(localTracer *tracing.Tracer, coordIPPort string) error {
	rand.Seed(time.Now().Unix())

	// setup conn to coord
	d.coordIPPort = coordIPPort
	d.connectCoord()

	log.Println("[INFO] Retrieving miner list from coord...")
	// no need to retry when failed.
	var minerListReply *blockvote.GetEntryPointsReply
	err := d.coordClient.Call("CoordAPIClient.GetEntryPoints", blockvote.GetEntryPointsArgs{}, &minerListReply)
	if err == nil {
		d.MinerAddrList = minerListReply.MinerAddrList
	}

	// Start internal services
	go d.CoordConnManager()
	go d.MinerListManager()

	// Wait for interrupt signal to exit
	//sigs := make(chan os.Signal, 1)
	//signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	//<-sigs
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
				var minerListReply *blockvote.GetEntryPointsReply
				for {
					// retrieve miner list
					d.connRw.RLock()
					err := d.coordClient.Call("CoordAPIClient.GetEntryPoints", blockvote.GetEntryPointsArgs{}, &minerListReply)
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

func (d *EV) LaunchPoll(signer *Identity.Signer, pollID string, rules blockChain.Rules) ([]byte, error) {
	payload := blockChain.Payload{
		Method:   blockChain.PayLoadMethodLaunch,
		UserName: signer.Name,
		UserID:   signer.ID,
		PollID:   pollID,
		Extra:    rules,
	}
	return d.initiateTransaction(signer, &payload)
}

func (d *EV) CastVote(signer *Identity.Signer, pollID string, options []string) ([]byte, error) {
	payload := blockChain.Payload{
		Method:   blockChain.PayloadMethodVote,
		UserName: signer.Name,
		UserID:   signer.ID,
		PollID:   pollID,
		Extra:    options,
	}
	return d.initiateTransaction(signer, &payload)
}

func (d *EV) TerminatePoll(signer *Identity.Signer, pollID string) ([]byte, error) {
	payload := blockChain.Payload{
		Method:   blockChain.PayloadMethodTerminate,
		UserName: signer.Name,
		UserID:   signer.ID,
		PollID:   pollID,
		Extra:    nil,
	}
	return d.initiateTransaction(signer, &payload)
}

func (d *EV) initiateTransaction(signer *Identity.Signer, payload *blockChain.Payload) ([]byte, error) {
	// create and sign transaction
	txn := blockChain.NewTransaction(payload, rand.Uint32(), signer.PublicKey)
	txn.Sign(signer.PrivateKey)

	// submit transaction
	err := d.submitTxn(txn)
	if err != nil {
		log.Println(fmt.Sprintf("[ERROR] Unable to submit transaction %x: %s",
			txn.ID[:5], err.Error()))
	} else {
		log.Println(fmt.Sprintf("[INFO] Transaction %x submitted.",
			txn.ID[:5]))
	}
	return txn.ID, err
}

func (d *EV) submitTxn(txn *blockChain.Transaction) error {
	var submitTxnReply *blockvote.SubmitTxnReply
	var err error
	var conn *rpc.Client
	for retry := 0; retry < SubmitRetry; retry++ {
		// setup conn to miner
		conn, err = d.connectMiner()
		if err != nil {
			log.Println(fmt.Sprintf("[WARN] Transaction %x failed to submit: %s (attempt %d of %d)",
				txn.ID[:5], err.Error(), retry+1, SubmitRetry))
			continue
		}
		err = conn.Call("EntryPointAPI.SubmitTxn", blockvote.SubmitTxnArgs{Txn: *txn}, &submitTxnReply)
		conn.Close()
		if err != nil {
			if _, ok := err.(*blockvote.InvalidSignatureError); !ok {
				log.Println(fmt.Sprintf("[WARN] Transaction %x failed to submit: %s (attempt %d of %d)",
					txn.ID[:5], err.Error(), retry+1, SubmitRetry))
				continue
			}
		}
		return err // this can pass the invalid signature error back to caller
	}
	return err
}

// CheckTxnStatus API checks the status of a transaction and returns the number of blocks that confirm it
func (d *EV) CheckTxnStatus(TxID []byte) (blockChain.TransactionStatus, error) {
	start := time.Now()

	var err error
	var conn *rpc.Client
	var queryTxnReply *blockvote.QueryTxnReply

	for retry := 0; retry < CheckTxnRetry && time.Since(start) < CheckTxnWait; retry++ {
		conn, err = d.connectMiner()
		if err != nil {
			continue
		}

		err = conn.Call("EntryPointAPI.QueryTxn", blockvote.QueryTxnArgs{
			TxID: TxID,
		}, &queryTxnReply)

		if err != nil {
			_ = conn.Close()
			continue
		}

		// success
		for queryTxnReply.Status.Confirmed == -1 && time.Since(start) < CheckTxnWait && err == nil {
			// check again if it is still not confirmed
			time.Sleep(5 * time.Second)
			err = conn.Call("EntryPointAPI.QueryTxn", blockvote.QueryTxnArgs{
				TxID: TxID,
			}, &queryTxnReply)
		}
		_ = conn.Close()
		if err != nil {
			continue
		} else {
			// either txn is confirmed, or not confirmed but time limit reached, so just return result
			return queryTxnReply.Status, nil
		}
	}

	// failure
	return blockChain.TransactionStatus{}, err // pass whatever error back to caller
}

// CheckPollStatus API retrieve the number of votes a candidate has.
func (d *EV) CheckPollStatus(pollID string) (blockChain.PollMeta, error) {
	var queryResultReply *blockvote.QueryResultsReply
	var err error
	var conn *rpc.Client
	for retry := 0; retry < CheckPollRetry; retry++ {
		conn, err = d.connectMiner()
		if err != nil {
			continue
		}

		err = conn.Call("EntryPointAPI.QueryResults",
			blockvote.QueryResultsArgs{PollID: pollID}, &queryResultReply)
		_ = conn.Close()
		if err != nil {
			continue
		}

		// success
		return queryResultReply.Results, nil
	}

	// failure
	return blockChain.PollMeta{}, err // pass whatever error back to caller
}

// Stop Stops the EV instance.
// This call always succeeds.
func (d *EV) Stop() {
	log.Println("[INFO] Stopping...")
	for {
		d.rw.RLock()
		allTxns := d.TxnInfos[:]
		d.rw.RUnlock()
		// check unconfirmed transactions
		count := 0
		for _, txn := range allTxns {
			if !txn.confirmed {
				count++
			}
		}
		if count == 0 {
			break
		}
		log.Println("[INFO] Waiting for all transactions to be submitted... " +
			"(" + strconv.Itoa(count) + "/" + strconv.Itoa(len(allTxns)) + " remaining)")
		time.Sleep(5 * time.Second)
	}
	quit <- true
	d.coordClient.Close()
	return
}

// ----- evlib utility functions -----

//func createBallot() blockChain.Ballot {
//	// enter ballot
//	reader := bufio.NewReader(os.Stdin)
//	fmt.Print("Enter your name: ")
//	voterName, _ := reader.ReadString('\n')
//
//	reader = bufio.NewReader(os.Stdin)
//	fmt.Print("Enter your studentID: ")
//	voterId, _ := reader.ReadString('\n')
//	// ^[0-9]{8}
//	isIDValid := false
//	candidateName := ""
//	for !isIDValid {
//		reader = bufio.NewReader(os.Stdin)
//		fmt.Print("Vote your vote Candidate: ")
//		candidateName, _ = reader.ReadString('\n')
//	}
//
//	ballot := blockChain.Ballot{
//		strings.TrimRight(voterName, "\r\n"),
//		strings.TrimRight(voterId, "\r\n"),
//		strings.TrimRight(candidateName, "\r\n"),
//	}
//	return ballot
//}

//func (d *EV) createTransaction(ballot blockChain.Ballot) (*blockChain.Transaction, error) {
//	VoterWallet, VoterWalletAddr := d.findWalletAndAddr(ballot)
//	if VoterWalletAddr == "" {
//		return blockChain.Transaction{}, errors.New("Not such a voter exists.\n")
//	}
//
//	txn := blockChain.NewTransaction(&ballot, VoterWallet.Wallets[VoterWalletAddr].PublicKey)
//	// client sign with private key
//	txn.Sign(VoterWallet.Wallets[VoterWalletAddr].PrivateKey)
//	return txn, nil
//}

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
