package blockchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"log"
	"math"
	"math/big"
)

type ProofOfWork struct {
	Block  *Block
	Target *big.Int
}

const NumZeros = 4

// NewProof creates a new ProofOfWork structure
func NewProof(b *Block) *ProofOfWork {
	target := big.NewInt(1)
	target.Lsh(target, uint(255-NumZeros))
	//fmt.Printf("Target: %x\n", target)
	pow := &ProofOfWork{b, target}
	return pow
}

// Run executes proof of work to find the nonce that makes block hash has NumZeros leading zeros
func (pow *ProofOfWork) Run() (uint32, []byte) {
	var hash [32]byte
	var intHash big.Int

	nonce := uint32(0)
	for nonce < math.MaxUint32 {
		data := pow.BlockToBytes(nonce)
		hash = sha256.Sum256(data)
		intHash.SetBytes(hash[:])
		if intHash.Cmp(pow.Target) == -1 { // find the nonce
			break
		} else {
			nonce++
		}
	}
	fmt.Printf("Nonce: %v\n", nonce)
	fmt.Printf("Hash: %x\n", hash)
	return nonce, hash[:]
}

// Validate checks whether the nonce is correct
func (pow *ProofOfWork) Validate() bool {
	var intHash big.Int

	data := pow.BlockToBytes(pow.Block.Nonce)
	hash := sha256.Sum256(data)
	intHash.SetBytes(hash[:])

	return intHash.Cmp(pow.Target) == -1
}

// ---------------------------

func (pow *ProofOfWork) BlockToBytes(nonce uint32) []byte {
	data := bytes.Join(
		[][]byte{
			pow.Block.PrevHash,
			NumToBytes(uint32(pow.Block.BlockNum)),
			NumToBytes(nonce),
			pow.HashTxns(),
			[]byte(pow.Block.MinerID),
		},
		[]byte{},
	)
	return data
}

func NumToBytes(num uint32) []byte {
	buff := new(bytes.Buffer)
	err := binary.Write(buff, binary.BigEndian, num)
	if err != nil {
		log.Println("[WARN] error when converting uint32 to bytes.", err)
	}
	return buff.Bytes()
}

func (pow *ProofOfWork) HashTxns() []byte {
	var txBytes [][]byte
	var txHash [32]byte

	for _, tx := range pow.Block.Txns {
		var encoded bytes.Buffer
		err := gob.NewEncoder(&encoded).Encode(tx)
		if err != nil {
			log.Println("[WARN] error when converting txns to bytes.", err)
		}
		txBytes = append(txBytes, encoded.Bytes())
	}
	txHash = sha256.Sum256(bytes.Join(txBytes, []byte{}))
	return txHash[:]
}
