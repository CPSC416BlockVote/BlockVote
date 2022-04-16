package main

import (
	blockChain "cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/evlib"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"fmt"
	"github.com/DistributedClocks/tracing"
	"log"
	"math/rand"
	"time"
)

func main() {
	var config blockvote.ClientConfig
	err := util.ReadJSONConfig("config/client2_config.json", &config)
	util.CheckErr(err, "Error reading client config: %v\n", err)

	tracer := tracing.NewTracer(tracing.TracerConfig{
		ServerAddress:  config.TracingServerAddr,
		TracerIdentity: config.TracingIdentity,
		Secret:         config.Secret,
	})

	client := evlib.NewEV()
	err = client.Start(tracer, config.ClientID, config.CoordIPPort, config.LocalCoordIPPort, config.LocalMinerIPPort, config.N_Receives)
	util.CheckErr(err, "Error reading client config: %v\n", err)

	// Add client operations here
	//auto create ballots
	voterNames := [10]string{"voter10", "voter11", "voter12", "voter13", "voter14", "voter15", "voter16", "voter17", "voter18", "voter19"}
	voterIDs := [10]string{"1000", "2111", "3222", "4333", "5444", "6555", "7666", "8777", "9888", "1999"}
	for i := 0; i < len(voterNames); i++ {
		ballot := blockChain.Ballot{
			voterNames[i],
			voterIDs[i],
			client.CandidateList[rand.Intn(10)],
		}
		if i == 0 {
			err = client.Vote(ballot.VoterName, ballot.VoterStudentID, ballot.VoterCandidate)
			if err != nil {
				log.Panic(err)
			}
		}
		fmt.Println(ballot)
		err = client.Vote(ballot.VoterName, ballot.VoterStudentID, ballot.VoterCandidate)
		if err != nil {
			log.Panic(err)
		}
	}

	time.Sleep(20 * time.Second)
	// query which block has confirmed txn with first txnID in the loop
	for voter, txn := range client.VoterTxnMap {
		fmt.Println("voter:", voter, "=>", "txnID:", txn.ID)
		numConfirmed, err := client.GetBallotStatus(txn.ID)
		if err != nil {
			log.Panic(err)
		}
		fmt.Println("num of Confirmed txn: ", numConfirmed)
	}

	time.Sleep(90 * time.Second)
	// query how many confirmed txn based on last txnID in the loop
	for i := 0; i < len(client.CandidateList); i++ {
		voters, err := client.GetCandVotes(client.CandidateList[i])
		if err != nil {
			log.Panic(err)
		}
		fmt.Println("checking ", client.CandidateList[i], " : ", voters)
	}

	client.Stop()
}
