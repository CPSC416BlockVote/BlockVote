package Identity

import "os"

type Voter struct {
	VoterName string
	VoterId   string
}

type Candidate struct {
	CandidateName string
}

func CreateVoter(name string, id string) (*Wallets, error) {
	wallets := Wallets{
		Wallets:  make(map[string]*Wallet),
		UserType: VoterType,
		VoterData: Voter{
			VoterName: name,
			VoterId:   id,
		},
	}

	err := wallets.LoadFile()
	if os.IsNotExist(err) {
		wallets.SaveFile()
		err = nil
	}
	return &wallets, err
}

func CreateCandidate(name string) (*Wallets, error) {
	wallets := Wallets{
		Wallets:       make(map[string]*Wallet),
		UserType:      CandidateType,
		CandidateData: Candidate{CandidateName: name},
	}

	err := wallets.LoadFile()
	if os.IsNotExist(err) {
		wallets.SaveFile()
		err = nil
	}
	return &wallets, err
}
