package main

import (
	"bytes"
	blockChain "cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/evlib"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"time"
)

type Record struct {
	TxID      []byte
	Name      string
	Candidate string
}

func main() {
	var config blockvote.ClientConfig
	err := util.ReadJSONConfig("config/client_config.json", &config)
	util.CheckErr(err, "Error reading client config: %v\n", err)

	// parse args
	var redirect bool
	flag.UintVar(&config.ClientID, "id", config.ClientID, "client ID")
	flag.BoolVar(&redirect, "redirect", false, "redirect outputs to file")
	flag.Parse()

	// redirect output to file
	if redirect {
		f, err := os.Create("./logs/" + config.TracingIdentity + ".txt")
		if err != nil {
			log.Fatalf("error opening file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	client := evlib.NewEV()
	err = client.Start(nil, config.ClientID, config.CoordIPPort)
	util.CheckErr(err, "Error reading client config: %v\n", err)

	// Add client operations here
	var validRecords []Record
	var invalidRecords []Record
	var conflictRecords []Record
	voterRecords := make(map[string][]Record)
	nVoters := 90
	for i := 0; i < 100; i++ {
		voterID := strconv.Itoa(nVoters*int(config.ClientID-1) + rand.New(rand.NewSource(time.Now().UnixNano())).Intn(nVoters))
		voterName := "voter" + voterID
		var candidate string
		valid := true
		if rand.New(rand.NewSource(time.Now().UnixNano())).Intn(15) != 0 {
			candidate = client.CandidateList[rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(client.CandidateList))]
		} else {
			// 6.67% chance to vote for invalid candidate
			candidate = "CANDIDATE" + strconv.Itoa(len(client.CandidateList)+rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(client.CandidateList)))
			valid = false
		}
		ballot := blockChain.Ballot{
			VoterName:      voterName,
			VoterStudentID: voterID,
			VoterCandidate: candidate,
		}
		blockChain.PrintBallot(&ballot)
		txid := client.Vote(ballot)
		if valid {
			dup := false
			for _, r := range voterRecords[voterName] {
				if bytes.Compare(r.TxID, txid) == 0 {
					dup = true
					break
				}
			}
			if !dup {
				voterRecords[voterName] = append(voterRecords[voterName], Record{
					TxID:      txid,
					Name:      voterName,
					Candidate: candidate,
				})
			}
		} else {
			invalidRecords = append(invalidRecords, Record{
				TxID:      txid,
				Name:      voterName,
				Candidate: candidate,
			})
		}

		if rand.New(rand.NewSource(time.Now().UnixNano())).Intn(10) == 0 {
			// 10% chance sending a conflicting txn immediately after
			candidate = client.CandidateList[rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(client.CandidateList))]
			ballot.VoterCandidate = candidate
			blockChain.PrintBallot(&ballot)
			txid = client.Vote(ballot)
			dup := false
			for _, r := range voterRecords[voterName] {
				if bytes.Compare(r.TxID, txid) == 0 {
					dup = true
					break
				}
			}
			if !dup {
				voterRecords[voterName] = append(voterRecords[voterName], Record{
					TxID:      txid,
					Name:      voterName,
					Candidate: candidate,
				})
			}
		}
		time.Sleep(time.Duration(rand.New(rand.NewSource(time.Now().UnixNano())).Intn(1000)) * time.Millisecond)
	}

	// write to file
	for _, records := range voterRecords {
		if len(records) == 1 {
			validRecords = append(validRecords, records[0])
		} else {
			for _, record := range records {
				conflictRecords = append(conflictRecords, record)
			}
		}
	}
	fv, err := os.Create("./client" + strconv.Itoa(int(config.ClientID)) + "valid.txt")
	util.CheckErr(err, "Unable to create valid.txt")
	defer fv.Close()
	for _, record := range validRecords {
		fv.WriteString(fmt.Sprintf("%x,%s,%s\n", record.TxID, record.Name, record.Candidate))
	}
	fv.Sync()
	fi, err := os.Create("./client" + strconv.Itoa(int(config.ClientID)) + "invalid.txt")
	util.CheckErr(err, "Unable to create invalid.txt")
	defer fi.Close()
	for _, record := range invalidRecords {
		fi.WriteString(fmt.Sprintf("%x,%s,%s\n", record.TxID, record.Name, record.Candidate))
	}
	fi.Sync()
	fc, err := os.Create("./client" + strconv.Itoa(int(config.ClientID)) + "conflict.txt")
	util.CheckErr(err, "Unable to create invalid.txt")
	defer fc.Close()
	for _, record := range conflictRecords {
		fc.WriteString(fmt.Sprintf("%x,%s,%s\n", record.TxID, record.Name, record.Candidate))
	}
	fc.Sync()

	log.Println("All voters have voted. Sleeping...")

	time.Sleep(45 * time.Second)

	for i := 0; i < 15; i++ {
		n := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(4)
		if n <= 1 {
			if len(validRecords) > 0 {
				record := validRecords[rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(validRecords))]
				log.Println("valid, voter:", record.Name, "=>", "txnInfo:", record.TxID)
				numConfirmed, _ := client.GetBallotStatus(record.TxID)
				log.Println("num of Confirmed txn: ", numConfirmed)
			}
		} else if n == 2 {
			if len(conflictRecords) > 0 {
				record := conflictRecords[rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(conflictRecords))]
				log.Println("conflict, voter:", record.Name, "=>", "txnInfo:", record.TxID)
				numConfirmed, _ := client.GetBallotStatus(record.TxID)
				log.Println("num of Confirmed txn: ", numConfirmed)
			}
		} else if n == 3 {
			if len(invalidRecords) > 0 {
				record := invalidRecords[rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(invalidRecords))]
				log.Println("invalid, voter:", record.Name, "=>", "txnInfo:", record.TxID)
				numConfirmed, _ := client.GetBallotStatus(record.TxID)
				log.Println("num of Confirmed txn: ", numConfirmed)
			}
		}
	}

	for i := 0; i < len(client.CandidateList); i++ {
		voters, err := client.GetCandVotes(client.CandidateList[i])
		if err != nil {
			log.Panic(err)
		}
		log.Println("checking ", client.CandidateList[i], " : ", voters)
	}

	client.Stop()
}
