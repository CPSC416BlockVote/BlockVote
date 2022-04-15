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
	"net"
	"net/rpc"
	"os"
	"strings"
	"time"
)

type EV struct {
	// Add EV instance state here.
	//ListCandidate          []*Identity.Wallets
	//Voters                 []*Identity.Wallets
	localCoordListenerAddr *net.TCPAddr
	localMinerListenerAddr *net.TCPAddr
	coordClient            *rpc.Client
	minerClient            *rpc.Client
	voterWallet            wallet.Wallets
	voterWalletAddr        string
	N_Receives             int
	candidateList          []string
	connCoord              *net.TCPConn
	coordAddr              *net.TCPAddr
	minerIpPort            string
}

// create wallet for voters
// create transcation
// sign transaction
func NewEV() *EV {
	return &EV{}
}

// ----- evlib APIs -----
var quit chan bool

// Start Starts the instance of EV to use for connecting to the system with the given coord's IP:port.
func (d *EV) Start(localTracer *tracing.Tracer, clientId string, coordIPPort string, localCoordIPPort string, localMinerIPPort string, N_Receives int) error {

	// setup conn to coord
	lcAddr, err := net.ResolveTCPAddr("tcp", localCoordIPPort)
	if err != nil {
		return err
	}

	cAddr, err := net.ResolveTCPAddr("tcp", coordIPPort)
	if err != nil {
		return err
	}

	conn, err := net.DialTCP("tcp", lcAddr, cAddr)
	d.connCoord = conn
	if err != nil {
		return err
	}
	coordClient := rpc.NewClient(conn)

	// get localMinerIPPort
	lmAddr, err := net.ResolveTCPAddr("tcp", localMinerIPPort)
	if err != nil {
		return err
	}
	d.coordAddr = cAddr
	d.localCoordListenerAddr = lcAddr
	d.coordClient = coordClient
	d.localMinerListenerAddr = lmAddr
	d.N_Receives = N_Receives

	// get candidates from Coord
	var candidatesReply *blockvote.GetCandidatesReply
	err = d.coordClient.Call("CoordAPIClient.GetCandidates", blockvote.GetCandidatesArgs{}, &candidatesReply)
	if err != nil {
		return err
	}

	// print all candidates Name
	canadiateName := make([]string, 0)
	for _, cand := range candidatesReply.Candidates {
		wallets := wallet.DecodeToWallets(cand)
		canadiateName = append(canadiateName, wallets.CandidateData.CandidateName)
	}
	d.candidateList = canadiateName
	fmt.Println("List of candidate:", canadiateName)

	quit = make(chan bool)
	go func() {
		// call coord for list of active miners with length N_Receives
		for {
			var minerListReply *blockvote.GetMinerListReply
			err := d.coordClient.Call("CoordAPIClient.GetMinerList", blockvote.GetMinerListArgs{}, &minerListReply)
			if err != nil {
				log.Panic(err)
			}

			// random pick one miner addr
			index := 0
			if d.N_Receives > 1 {
				index = rand.Intn(d.N_Receives - 1)
			}
			d.minerIpPort = minerListReply.MinerAddrList[index]
			time.Sleep(500 * time.Millisecond)
			select {
			case <-quit:
				// end
				return
			default:
				// Do other stuff
			}
		}
	}()

	// use terminal to auto or manually crate ballot
	//ballot := createBallot()
	//err = d.Vote(ballot.VoterName, ballot.VoterStudentID, ballot.VoterCandidate)
	//if err != nil {
	//	return err
	//}

	// auto create ballots
	voterNames := [10]string{"voter0", "voter1", "voter2", "voter3", "voter4", "voter5", "voter6", "voter7", "voter8", "voter9"}
	voterIDs := [10]string{"0000", "1111", "2222", "3333", "4444", "5555", "6666", "7777", "8888", "9999"}
	txnID := []byte("")
	for i := 0; i < len(voterNames); i++ {
		ballot := blockChain.Ballot{
			voterNames[rand.Intn(9)],
			voterIDs[rand.Intn(9)],
			d.candidateList[rand.Intn(9)],
		}
		fmt.Println(ballot)
		txnID, err = d.Vote(ballot.VoterName, ballot.VoterStudentID, ballot.VoterCandidate)
		if err != nil {
			return err
		}
	}
	time.Sleep(30 * time.Second)
	// query how many confirmed txn based on last txnID in the loop
	numConfirmed, err := d.GetBallotStatus(txnID)
	if err != nil {
		return err
	}
	fmt.Println("num of Confirmed txn: ", numConfirmed)
	// query how many confirmed txn based on last txnID in the loop
	for i := 0; i < len(d.candidateList); i++ {
		fmt.Println("checking ", d.candidateList[i])
		voters, err := d.GetCandVotes(d.candidateList[i])
		if err != nil {
			return err
		}
		fmt.Println(voters)
	}
	return nil
}

// Vote API provides the functionality of voting
func (d *EV) Vote(from, fromID, to string) ([]byte, error) {

	ballot := blockChain.Ballot{
		VoterName:      from,
		VoterStudentID: fromID,
		VoterCandidate: to,
	}
	// create wallet for voter
	d.createVoterWallet(ballot)

	// create transaction
	txn := d.createTransaction(ballot)

	// setup conn to miner
	d.connMinerAddr(d.minerIpPort)
	var submitTxnReply *blockvote.SubmitTxnReply
	err := d.minerClient.Call("MinerAPIClient.SubmitTxn", blockvote.SubmitTxnArgs{Txn: txn}, &submitTxnReply)
	if err != nil {
		return []byte(""), err
	}

	return txn.ID, nil
}

// GetBallotStatus API checks the status of a transaction and returns the number of blocks that confirm it
func (d *EV) GetBallotStatus(TxID []byte) (int, error) {
	d.connCoordAddr()
	var queryTxnReply *blockvote.QueryTxnReply
	err := d.coordClient.Call("CoordAPIClient.QueryTxn", blockvote.QueryTxnArgs{
		TxID: TxID,
	}, &queryTxnReply)
	if err != nil {
		return -1, err
	}
	return queryTxnReply.NumConfirmed, nil
}

// GetCandVotes API retrieve the number of votes a candidate has.
func (d *EV) GetCandVotes(candidate string) (uint, error) {
	d.connCoordAddr()
	if len(d.candidateList) == 0 {
		return 0, errors.New("Empty Candidates.\n")
	}

	var queryResultReply *blockvote.QueryResultsReply
	err := d.coordClient.Call("CoordAPIClient.QueryResults", blockvote.QueryResultsArgs{}, &queryResultReply)
	if err != nil {
		return 0, err
	}

	idx := 0
	for i, cand := range d.candidateList {
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
	d.minerClient.Close()
	d.connCoord.Close()
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

// connect with miner addr with timeout to retry
func (d *EV) connMinerAddr(minerAddr string) error {
	// setup connection to the miner
	maddr, err := net.ResolveTCPAddr("tcp", minerAddr)
	if err != nil {
		return err
	}
	conn, err := net.DialTCP("tcp", d.localMinerListenerAddr, maddr)
	if err != nil {
		return err
	}
	// timeout
	readAndWriteTimeout := 5 * time.Second
	err = conn.SetDeadline(time.Now().Add(readAndWriteTimeout))
	if err != nil {
		return err
	}
	err = conn.SetLinger(0)
	if err != nil {
		return err
	}
	d.minerClient = rpc.NewClient(conn)
	return nil
}

func (d *EV) connCoordAddr() error {
	// setup connection to the miner
	conn, err := net.DialTCP("tcp", d.localCoordListenerAddr, d.coordAddr)
	if err != nil {
		return err
	}
	// timeout
	readAndWriteTimeout := 5 * time.Second
	err = conn.SetDeadline(time.Now().Add(readAndWriteTimeout))
	if err != nil {
		return err
	}
	err = conn.SetLinger(0)
	if err != nil {
		return err
	}
	d.coordClient = rpc.NewClient(conn)
	return nil
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
