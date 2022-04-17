package blockchain

import (
	"log"
)

type Ballot struct {
	VoterName      string
	VoterStudentID string
	VoterCandidate string
}

func PrintBallot(ballot *Ballot) {
	log.Printf("%s (%s) -> %s\n", ballot.VoterName, ballot.VoterStudentID, ballot.VoterCandidate)
}
