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
	err := util.ReadJSONConfig("config/client_config.json", &config)
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
	voterNames := [10]string{"voter0", "voter1", "voter2", "voter3", "voter4", "voter5", "voter6", "voter7", "voter8", "voter9"}
	voterIDs := [10]string{"0000", "1111", "2222", "3333", "4444", "5555", "6666", "7777", "8888", "9999"}
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

	time.Sleep(60 * time.Second)
	// query which block has confirmed txn with first txnID in the loop
	for voter, txnInfo := range client.VoterTxnInfoMap {
		fmt.Println("voter:", voter, "=>", "txnInfo:", txnInfo)
		//numConfirmed, err := client.GetBallotStatus(txnInfo.txn.ID)
		//if err != nil {
		//	log.Panic(err)
		//}
		//fmt.Println("num of Confirmed txn: ", numConfirmed)
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

	client.Stop()
}
