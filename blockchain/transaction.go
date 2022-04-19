package blockchain

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/gob"
	"log"
	"math/big"
)

type Transaction struct {
	Data      *Ballot
	ID        []byte
	Signature []byte
	PublicKey []byte
}

// ----- Transaction APIs -----

func (tx *Transaction) Hash() []byte {
	var hash [32]byte

	txCopy := *tx
	txCopy.ID = []byte{}

	hash = sha256.Sum256(txCopy.Serialize())

	return hash[:]
}

func (tx Transaction) Serialize() []byte {
	var encoded bytes.Buffer

	enc := gob.NewEncoder(&encoded)
	err := enc.Encode(tx)
	if err != nil {
		log.Panic(err)
	}

	return encoded.Bytes()
}

func DeserializeTransaction(data []byte) Transaction {
	var transaction Transaction

	decoder := gob.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&transaction); err != nil {
		log.Panic(err)
	}
	return transaction
}

func (tx *Transaction) SetID() {
	var encoded bytes.Buffer
	var hash [32]byte

	encode := gob.NewEncoder(&encoded)
	err := encode.Encode(tx)

	if err != nil {
		log.Panic(err)
	}

	hash = sha256.Sum256(encoded.Bytes())
	tx.ID = hash[:]
}

// Sign client
func (tx *Transaction) Sign(privKey ecdsa.PrivateKey) {
	txcopy := Transaction{
		Data:      tx.Data,
		ID:        tx.ID,
		Signature: nil,
		PublicKey: tx.PublicKey,
	}
	//tx.Signature = nil

	txcopy.ID = txcopy.Hash()
	//tx.PublicKey = nil

	r, s, err := ecdsa.Sign(rand.Reader, &privKey, txcopy.ID)
	if err != nil {
		log.Panic(err)
	}
	signature := append(r.Bytes(), s.Bytes()...)

	tx.Signature = signature

}

// Verify blockchain
func (tx *Transaction) Verify() bool {
	//tx.ID = tx.Hash()

	curve := elliptic.P256()

	txcopy := Transaction{
		Data:      tx.Data,
		ID:        tx.ID,
		Signature: nil,
		PublicKey: tx.PublicKey,
	}
	txcopy.ID = txcopy.Hash()

	r := big.Int{}
	s := big.Int{}

	sigLen := len(tx.Signature)
	r.SetBytes(tx.Signature[:(sigLen / 2)])
	s.SetBytes(tx.Signature[(sigLen / 2):])

	x := big.Int{}
	y := big.Int{}
	keyLen := len(tx.PublicKey)
	x.SetBytes(tx.PublicKey[:(keyLen / 2)])
	y.SetBytes(tx.PublicKey[(keyLen / 2):])

	rawPubKey := ecdsa.PublicKey{Curve: curve, X: &x, Y: &y}
	if ecdsa.Verify(&rawPubKey, txcopy.ID, &r, &s) == false {
		return false
	}

	return true
}
