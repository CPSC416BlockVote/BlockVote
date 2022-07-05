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

const (
	TxnStatusSuccess          = 0
	TxnStatusInvalidSignature = -1
	TxnStatusInvalidStatus    = -2
)

type Transaction struct {
	Data      *Payload
	Nonce     uint32
	ID        []byte
	Signature []byte
	PublicKey []byte
	Receipt   TransactionReceipt // status code (set by miner)
}

// TransactionReceipt TODO: use this struct to replace Status field
type TransactionReceipt struct {
	Code     int  // status code
	BlockNum uint // block number of the block that mines this transaction
	MinerID  string
}

type TransactionStatus struct {
	Confirmed int                // number of blocks that confirm the transaction
	Receipt   TransactionReceipt // status code of the transaction
}

// ----- Transaction APIs -----

func NewTransaction(data *Payload, nonce uint32, publicKey []byte) *Transaction {
	txn := Transaction{Data: data, Nonce: nonce, PublicKey: publicKey}
	txn.ID = txn.Hash()
	return &txn
}

func (tx *Transaction) Hash() []byte {
	var hash [32]byte

	txCopy := *tx
	// only hash data, nonce and public key, remove other fields
	txCopy.ID = []byte{}
	txCopy.Signature = []byte{}
	txCopy.Receipt = TransactionReceipt{}

	hash = sha256.Sum256(txCopy.Serialize())

	return hash[:]
}

func (tx Transaction) Serialize() []byte {
	var encoded bytes.Buffer

	gob.Register(Rules{})
	enc := gob.NewEncoder(&encoded)
	err := enc.Encode(tx)
	if err != nil {
		log.Panic(err)
	}

	return encoded.Bytes()
}

func DeserializeTransaction(data []byte) Transaction {
	var transaction Transaction

	gob.Register(Rules{})
	decoder := gob.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&transaction); err != nil {
		log.Panic(err)
	}
	return transaction
}

// Sign client
func (tx *Transaction) Sign(privKey ecdsa.PrivateKey) {
	txcopy := Transaction{
		Data:      tx.Data,
		Nonce:     tx.Nonce,
		ID:        nil,
		Signature: nil,
		PublicKey: tx.PublicKey,
		Receipt:   TransactionReceipt{},
	}

	txcopy.ID = txcopy.Hash()

	r, s, err := ecdsa.Sign(rand.Reader, &privKey, txcopy.ID)
	if err != nil {
		log.Panic(err)
	}
	signature := append(r.Bytes(), s.Bytes()...)

	tx.Signature = signature

}

// Verify transaction signature
func (tx *Transaction) Verify() bool {
	curve := elliptic.P256()

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
	if ecdsa.Verify(&rawPubKey, tx.ID, &r, &s) == false {
		//// second phase verification process
		//rawPubHash := append(rawPubKey.X.Bytes(), rawPubKey.Y.Bytes()...)
		//if bytes.Compare(rawPubHash, tx.PublicKey) == 0 {
		//	return true
		//}
		return false
	}

	return true
}

func (tx *Transaction) Validate(handler *QueryHandler) (bool, int) {
	// 1. verify signature
	if !tx.Verify() {
		return false, TxnStatusInvalidSignature
	}
	// 2. verify data (compute status code)
	// any transaction with valid signature is valid, but not necessarily succeeded
	return true, tx.Data.Validate(tx, handler)
}

func (tx *Transaction) Success() bool {
	return tx.Receipt.Code == TxnStatusSuccess
}
