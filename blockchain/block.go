package blockchain

type Block struct {
	PrevHash []byte
	BlockNum uint8
	Nonce    uint32
	Txns     []*Transaction
	MinerID  string
	Hash     []byte
}
