package blockchain

type Transaction struct {
	Data      *Ballot
	TXID      []byte
	Signature []byte
	PublicKey []byte
}
