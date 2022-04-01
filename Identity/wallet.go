package Identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"github.com/mr-tron/base58"
	"golang.org/x/crypto/ripemd160"
	"log"
)

type Wallet struct {
	PrivateKey ecdsa.PrivateKey
	PublicKey  []byte
}

const (
	checksumLength = 4
	version        = byte(0x00)
)

func NewKeyPair() (ecdsa.PrivateKey, []byte) {
	curveFunc := elliptic.P256()

	privateKey, err := ecdsa.GenerateKey(curveFunc, rand.Reader)
	if err != nil {
		log.Panic(err)
	}

	pubKey := append(privateKey.PublicKey.X.Bytes(), privateKey.PublicKey.Y.Bytes()...)
	return *privateKey, pubKey
}

func NewWallet() *Wallet {
	private, pub := NewKeyPair()
	return &Wallet{
		PrivateKey: private,
		PublicKey:  pub,
	}
}

func PublicKeyHash(pubKey []byte) []byte {

	// Sha256, ripemd160 for the key
	pubHash := sha256.Sum256(pubKey)
	hasher := ripemd160.New()
	if _, err := hasher.Write(pubHash[:]); err != nil {
		log.Panic(err)
	}
	return hasher.Sum(nil)
}

func (w Wallet) Address() []byte {
	pubHash := PublicKeyHash(w.PublicKey)

	// append the version to the head of the hashing
	versionedHash := append([]byte{version}, pubHash...)
	checksum := Checksum(versionedHash)

	fullHash := append(versionedHash, checksum...)
	address := Base58Encode(fullHash)

	return address
}

func Checksum(payload []byte) []byte {
	firstHash := sha256.Sum256(payload)
	secondHash := sha256.Sum256(firstHash[:])

	// only takes first len(checksumLength) bytes
	return secondHash[:checksumLength]
}

//func ValidateAddress(address string) bool {
//	pubKeyHash := Base58Decode([]byte(address))
//	actualChecksum := pubKeyHash[len(pubKeyHash)-checksumLength:]
//	version := pubKeyHash[0]
//	pubKeyHash = pubKeyHash[1 : len(pubKeyHash)-checksumLength]
//	targetChecksum := Checksum(append([]byte{version}, pubKeyHash...))
//
//	return bytes.Compare(actualChecksum, targetChecksum) == 0
//}

func Base58Encode(input []byte) []byte {
	return []byte(base58.Encode(input))
}

func Base58Decode(input []byte) []byte {
	decode, err := base58.Decode(string(input[:]))
	if err != nil {
		log.Panic(err)
	}
	return decode
}
