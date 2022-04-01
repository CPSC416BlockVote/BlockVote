package evlib

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	wallet "cs.ubc.ca/cpsc416/BlockVote/Identity"
	blockChain "cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"fmt"
	"github.com/DistributedClocks/tracing"
	"log"
)

var candiates = [2]string{"A", "B"}

type EV struct {
	// Add EV instance state here.
	ListCandidate []Identity.Wallets
	Voters        []Identity.Wallets
}

// create wallet for voters
// create transcation
// sign transaction
func NewEV() *EV {
	ListCandidates := make([]Identity.Wallets, len(candiates))
	for _, val := range candiates {
		can, err := wallet.CreateCandidate(val)
		if err != nil {
			log.Panic(err)
		}
		ListCandidates = append(ListCandidates, *can)

		can.SaveFile()
	}
	v, err := wallet.CreateVoter("hhh", "111")
	if err != nil {
		log.Panic(err)
	}
	return &EV{
		ListCandidates,
		[]Identity.Wallets{*v},
	}
}

// Start Starts the instance of EV to use for connecting to the system with the given coord's IP:port.
func (d *EV) Start(localTracer *tracing.Tracer, clientId string, coordIPPort string) error {
	ballot := blockChain.Ballot{
		d.Voters[0].VoterData.VoterName,
		d.Voters[0].VoterData.VoterId,
		d.ListCandidate[1].CandidateData.CandidateName,
	}
	addr := d.Voters[0].AddWallet()
	d.Voters[0].SaveFile()
	txn := blockChain.Transaction{
		Data:      &ballot,
		ID:        nil,
		Signature: nil,
		PublicKey: d.Voters[0].Wallets[addr].PublicKey,
	}
	txn.Sign(d.Voters[0].Wallets[addr].PrivateKey)
	ret := txn.Verify()
	if ret {
		fmt.Println("Verify success")
	} else {
		fmt.Println("Verify fail")
	}
	return nil
}

// Stop Stops the EV instance.
// This call always succeeds.
func (d *EV) Stop() {
	return
}
