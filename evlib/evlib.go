package evlib

import (
	"bufio"
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	wallet "cs.ubc.ca/cpsc416/BlockVote/Identity"
	blockChain "cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"fmt"
	"github.com/DistributedClocks/tracing"
	"log"
	"math/rand"
	"net"
	"net/rpc"
	"os"
	"time"
)

type (
	GetCandidatesArgs struct {
	}

	GetCandidatesReply struct {
		Candidates []Identity.Wallets
	}

	GetMinerListArgs struct {
		N_Receives int
	}

	GetMinerListReply struct {
		MinerAddrList []string
	}

	SubmitTxnArgs struct {
		txn blockChain.Transaction
	}

	SubmitTxnReply struct {
	}
)

var candiates = [2]string{"A", "B"}

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
}

// create wallet for voters
// create transcation
// sign transaction
func NewEV() *EV {
	return &EV{}
}

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
	if err != nil {
		return err
	}
	coordClient := rpc.NewClient(conn)

	// get localMinerIPPort
	lmAddr, err := net.ResolveTCPAddr("tcp", localMinerIPPort)
	if err != nil {
		return err
	}

	d.localCoordListenerAddr = lcAddr
	d.coordClient = coordClient
	d.localMinerListenerAddr = lmAddr

	// get candidates from Coord
	var candidatesReply *GetCandidatesReply
	err = d.coordClient.Call("CoordAPIClient.GetCandidates", GetCandidatesArgs{}, &candidatesReply)
	if err != nil {
		return err
	}

	// print all candidates Name
	canadiateName := make([]string, 0)
	for _, wallet := range candidatesReply.Candidates {
		canadiateName = append(canadiateName, wallet.CandidateData.CandidateName)
	}
	fmt.Println("List of canadiate:", canadiateName)

	// create ballot from user info
	ballot := createBallot()

	// create wallet for voter
	d.createVoterWallet(ballot)

	// create transaction
	txn := d.createTransaction(ballot)

	// call coord for list of active miners with length N_Receives
	var minerListReply *GetMinerListReply
	err = d.coordClient.Call("CoordAPIClient.GetMinerList", GetMinerListArgs{N_Receives}, &minerListReply)
	if err != nil {
		return err
	}

	// random pick one miner addr
	index := rand.Intn(N_Receives - 1)
	minerIPPort := minerListReply.MinerAddrList[index]

	// setup conn to miner
	d.connMinerAddr(minerIPPort)
	var submitTxnReply *SubmitTxnReply
	err = d.coordClient.Call("MinerAPIClient.SubmitTxn", SubmitTxnArgs{txn}, &submitTxnReply)
	if err != nil {
		return err
	}

	return nil
}

// Stop Stops the EV instance.
// This call always succeeds.
func (d *EV) Stop() {
	return
}

func createBallot() blockChain.Ballot {
	// enter ballot
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter your name: ")
	voterName, _ := reader.ReadString('\n')
	reader = bufio.NewReader(os.Stdin)
	fmt.Print("Enter your studentID: ")
	voterId, _ := reader.ReadString('\n')
	reader = bufio.NewReader(os.Stdin)
	fmt.Print("Vote your vote Candidate: ")
	candidateName, _ := reader.ReadString('\n')

	ballot := blockChain.Ballot{
		voterName,
		voterId,
		candidateName,
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
	haddr, err := net.ResolveTCPAddr("tcp", minerAddr)
	if err != nil {
		return err
	}
	conn, err := net.DialTCP("tcp", d.localMinerListenerAddr, haddr)
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
