package Identity

import (
	"bytes"
	"crypto/elliptic"
	"encoding/gob"
	"fmt"
	"io/ioutil"
	"log"
	"os"
)

type Wallets struct {
	UserType      string
	Wallets       map[string]*Wallet
	VoterData     Voter
	CandidateData Candidate
}

const (
	VoterType     = "Vot"
	CandidateType = "Can"
	walletFile    = "./tmp/ws_%s.data"
)

func (ws Wallets) SerializeDependOnType() []byte {
	var encoded bytes.Buffer

	enc := gob.NewEncoder(&encoded)
	if ws.UserType == VoterType {
		if err := enc.Encode(ws.VoterData); err != nil {
			log.Panic(err)
		}
	} else if ws.UserType == CandidateType {
		if err := enc.Encode(ws.CandidateData); err != nil {
			log.Panic(err)
		}
	} else {
		log.Panic("Undefined Type.\n")
	}
	return encoded.Bytes()
}

func (ws *Wallets) AddWallet() string {
	wallet := NewWallet()
	address := fmt.Sprintf("%s", wallet.Address())
	ws.Wallets[address] = wallet
	return address
}

func (ws *Wallets) GetAllAddresses() []string {
	var addresses []string

	for address := range ws.Wallets {
		addresses = append(addresses, address)
	}

	return addresses
}

func (ws Wallets) GetWallet(address string) Wallet {
	return *ws.Wallets[address]
}

func (ws *Wallets) LoadFile() error {

	walletFile := fmt.Sprintf(walletFile, ws.UserType)
	if ws.UserType == VoterType {
		walletFile = fmt.Sprintf(walletFile, ws.VoterData.VoterName, ws.VoterData.VoterId)
	} else if ws.UserType == CandidateType {
		walletFile = fmt.Sprintf(walletFile, ws.CandidateData.CandidateName)
	}
	if _, err := os.Stat(walletFile); os.IsNotExist(err) {
		return err
	}

	var wallets Wallets

	fileContent, err := ioutil.ReadFile(walletFile)
	if err != nil {
		return err
	}

	gob.Register(elliptic.P256())
	decoder := gob.NewDecoder(bytes.NewReader(fileContent))

	if err = decoder.Decode(&wallets); err != nil {
		return err
	}

	ws.Wallets = wallets.Wallets
	ws.UserType = wallets.UserType
	if ws.UserType == VoterType {
		ws.VoterData = wallets.VoterData
		ws.CandidateData = Candidate{}
	} else if ws.UserType == CandidateType {
		ws.VoterData = Voter{}
		ws.CandidateData = wallets.CandidateData
	}
	return nil
}

func (ws *Wallets) SaveFile() {

	walletFile := fmt.Sprintf(walletFile, ws.UserType)
	if ws.UserType == VoterType {
		walletFile = fmt.Sprintf(walletFile, ws.VoterData.VoterName, ws.VoterData.VoterId)
	} else if ws.UserType == CandidateType {
		walletFile = fmt.Sprintf(walletFile, ws.CandidateData.CandidateName)
	}

	gob.Register(elliptic.P256())
	var content bytes.Buffer
	encoder := gob.NewEncoder(&content)

	if err := encoder.Encode(ws); err != nil {
		log.Panic(err)
	}

	if err := ioutil.WriteFile(walletFile, content.Bytes(), 0644); err != nil {
		log.Panic(err)
	}

}

func (ws *Wallets) Encode() []byte {
	var buf bytes.Buffer
	gob.Register(elliptic.P256())
	err := gob.NewEncoder(&buf).Encode(ws)
	if err != nil {
		log.Println("[WARN] wallet encode error")
	}
	return buf.Bytes()
}
