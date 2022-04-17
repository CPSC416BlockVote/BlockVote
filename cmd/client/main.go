package main

import (
	blockChain "cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/evlib"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"flag"
	"fmt"
	"github.com/DistributedClocks/tracing"
	"log"
	"math/rand"
	"os"
	"strconv"
	"time"
)

func main() {
	var config blockvote.ClientConfig
	err := util.ReadJSONConfig("config/client_config.json", &config)
	util.CheckErr(err, "Error reading client config: %v\n", err)

	// parse args
	flag.UintVar(&config.ClientID, "id", config.ClientID, "client ID")
	flag.Parse()
	config.TracingIdentity = "client" + strconv.Itoa(int(config.ClientID))

	// redirect output to file
	if len(os.Args) > 1 {
		f, err := os.Create("./logs/" + config.TracingIdentity + ".txt")
		if err != nil {
			log.Fatalf("error opening file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	tracer := tracing.NewTracer(tracing.TracerConfig{
		ServerAddress:  config.TracingServerAddr,
		TracerIdentity: config.TracingIdentity,
		Secret:         config.Secret,
	})

	client := evlib.NewEV()
	err = client.Start(tracer, config.ClientID, config.CoordIPPort, config.N_Receives)
	util.CheckErr(err, "Error reading client config: %v\n", err)

	// Add client operations here

	for i := 0; i < 120; i++ {
		nVoters := 100
		voter := strconv.Itoa(nVoters*int(config.ClientID-1) + rand.New(rand.NewSource(time.Now().UnixNano())).Intn(nVoters))
		ballot := blockChain.Ballot{
			VoterName:      "voter" + voter,
			VoterStudentID: voter,
			VoterCandidate: client.CandidateList[rand.Intn(10)],
		}
		blockChain.PrintBallot(&ballot)
		client.Vote(ballot.VoterName, ballot.VoterStudentID, ballot.VoterCandidate)

		if rand.New(rand.NewSource(time.Now().UnixNano())).Intn(6) == 0 {
			// 16.7% chance sending a conflicting txn immediately after
			ballot.VoterCandidate = client.CandidateList[rand.Intn(10)]
			blockChain.PrintBallot(&ballot)
			client.Vote(ballot.VoterName, ballot.VoterStudentID, ballot.VoterCandidate)
		}
		time.Sleep(time.Duration(rand.New(rand.NewSource(time.Now().UnixNano())).Intn(4000)) * time.Millisecond)
	}

	time.Sleep(20 * time.Second)
	// query which block has confirmed txn with txnID in the loop
	for voter, txn := range client.VoterTxnMap {
		fmt.Println("voter:", voter, "=>", "txnInfo:", txn.ID)
		numConfirmed, err := client.GetBallotStatus(txn.ID)
		if err != nil {
			log.Panic(err)
		}
		fmt.Println("num of Confirmed txn: ", numConfirmed)
	}

	time.Sleep(40 * time.Second)
	// query how many confirmed txn based on last txnID in the loop
	for i := 0; i < len(client.CandidateList); i++ {
		voters, err := client.GetCandVotes(client.CandidateList[i])
		if err != nil {
			log.Panic(err)
		}
		fmt.Println("checking ", client.CandidateList[i], " : ", voters)
	}

	time.Sleep(60 * time.Second)
	// query how many confirmed txn based on last txnID in the loop
	for i := 0; i < len(client.CandidateList); i++ {
		voters, err := client.GetCandVotes(client.CandidateList[i])
		if err != nil {
			log.Panic(err)
		}
		fmt.Println("checking ", client.CandidateList[i], " : ", voters)
	}

	time.Sleep(50 * time.Second)
	for i := 0; i < len(client.CandidateList); i++ {
		voters, err := client.GetCandVotes(client.CandidateList[i])
		if err != nil {
			log.Panic(err)
		}
		fmt.Println("checking ", client.CandidateList[i], " : ", voters)
	}

	for voter, txnInfo := range client.VoterTxnInfoMap {
		fmt.Println("voter:", voter, "=>", "txnInfo:", txnInfo)
	}

	//client.Stop()
}
