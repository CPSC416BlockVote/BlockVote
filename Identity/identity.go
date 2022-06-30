package Identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/hex"
	"log"
	"math/big"
	"os"
)

type Voter struct {
	VoterName string
	VoterId   string
}

type Candidate struct {
	CandidateName string
}

type Signer struct {
	Name       string
	ID         string
	PublicKey  []byte
	PrivateKey ecdsa.PrivateKey
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

	//err := wallets.LoadFile()
	//if os.IsNotExist(err) {
	//	wallets.SaveFile()
	//	err = nil
	//}
	return &wallets, nil
}

func CreateSigner(userName string, userID string) *Signer {
	v, err := CreateVoter(userName, userID)
	if err != nil {
		log.Panic(err)
	}
	voterWallet := v
	addr := voterWallet.AddWallet()
	return &Signer{
		Name:       userName,
		ID:         userID,
		PublicKey:  voterWallet.Wallets[addr].PublicKey,
		PrivateKey: voterWallet.Wallets[addr].PrivateKey,
	}
}

func (s *Signer) Import(privKeyHex string) error {
	privKey, err := HexToPrivateKey(privKeyHex)
	if err != nil {
		return err
	}
	s.PrivateKey = *privKey
	s.PublicKey = append(privKey.PublicKey.X.Bytes(), privKey.PublicKey.Y.Bytes()...)
	return nil
}

func (s *Signer) Export() string {
	bytes := s.PrivateKey.D.Bytes()
	return hex.EncodeToString(bytes)
}

func HexToPrivateKey(hexStr string) (*ecdsa.PrivateKey, error) {
	bytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, err
	}

	k := new(big.Int)
	k.SetBytes(bytes)

	priv := new(ecdsa.PrivateKey)
	curve := elliptic.P256()
	priv.PublicKey.Curve = curve
	priv.D = k
	priv.PublicKey.X, priv.PublicKey.Y = curve.ScalarBaseMult(k.Bytes())

	return priv, nil
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
