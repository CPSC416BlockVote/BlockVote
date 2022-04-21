package blockchain

import (
	"bytes"
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
)

var LastHashKey = []byte("LastHash")

const BlockKeyPrefix = "block-"
const NumConfirmed = 4

type BlockChain struct {
	mu         sync.Mutex
	LastHash   []byte // should not be accessed without locking (unsafe). should not be accessed directly from outside
	DB         *util.Database
	Candidates []*Identity.Wallets
}

type ChainIterator struct {
	LastHash    []byte
	CurrentHash []byte
	Index       int
	BlockChain  *BlockChain
}

// ----- BlockChain APIs -----

func NewBlockChain(DB *util.Database, candidates []*Identity.Wallets) *BlockChain {
	return &BlockChain{DB: DB, Candidates: candidates}
}

// Init initializes the blockchain with genesis block. For coord use only.
func (bc *BlockChain) Init() error {
	// check key
	if bc.DB.KeyExist(LastHashKey) {
		return errors.New("blockchain has already been initialized")
	}

	// generate genesis block
	genesis := Block{}
	genesis.Genesis()

	// store genesis block
	err := bc.DB.PutMulti(
		[][]byte{DBKeyForBlock(genesis.Hash), LastHashKey},
		[][]byte{genesis.Encode(), genesis.Hash})
	if err != nil {
		return err
	}

	// update last hash
	bc.LastHash = genesis.Hash
	return nil
}

// ResumeFromDB resumes a blockchain from database. For coord use only.
func (bc *BlockChain) ResumeFromDB() error {
	lastHash, err := bc.DB.Get(LastHashKey)
	if err != nil {
		return err
	}

	// update last hash
	bc.LastHash = lastHash
	return nil
}

// ResumeFromEncodedData resumes a blockchain from byte data. For miner use only.
func (bc *BlockChain) ResumeFromEncodedData(blocks [][]byte, lastHash []byte) error {
	// save last hash & every block to DB
	// (all blocks are assumed valid)
	var keys [][]byte
	for _, blockBytes := range blocks {
		block := DecodeToBlock(blockBytes)
		keys = append(keys, DBKeyForBlock(block.Hash))
	}
	keys = append(keys, LastHashKey)
	values := append(blocks, lastHash)
	err := bc.DB.PutMulti(keys, values)
	if err != nil {
		return err
	}

	// update last hash
	bc.LastHash = lastHash
	return nil
}

// GetLastHash provides a safe way to read the last hash of the blockchain from outside
func (bc *BlockChain) GetLastHash() []byte {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.LastHash[:]
}

// Encode encodes all the blocks in the blockchain into a 2D byte array.
func (bc *BlockChain) Encode() ([][]byte, []byte) {
	// lock to ensure block data and last hash consistency
	bc.mu.Lock()
	defer bc.mu.Unlock()

	blocks, err := bc.DB.GetAllWithPrefix(BlockKeyPrefix)
	if err != nil {
		log.Println("[ERROR] Unable to fetch all block data from database:")
		log.Fatal(err)
	}
	return blocks, bc.LastHash[:]
}

// Exist returns if a block exists in the blockchain
func (bc *BlockChain) Exist(hash []byte) bool {
	key := DBKeyForBlock(hash)
	return bc.DB.KeyExist(key)
}

// Get gets a block by hash
func (bc *BlockChain) Get(hash []byte) *Block {
	data, err := bc.DB.Get(DBKeyForBlock(hash))
	if err != nil {
		log.Println("[ERROR] Unable to fetch the block from DB:")
		log.Fatal(err)
	}
	block := DecodeToBlock(data)
	return block
}

// Put adds a new block to the blockchain
func (bc *BlockChain) Put(block Block, owned bool) (success bool, newTxns []*Transaction, oldTxns []*Transaction) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	// sanity check
	if len(block.PrevHash) == 0 || block.BlockNum == 0 || len(block.Hash) == 0 || len(block.MinerID) == 0 {
		log.Println("[WARN] Block has missing values and will not be added to the chain.")
		success = false
		return
	}
	if !bc.Exist(block.PrevHash) {
		log.Printf("[WARN] Previous block (%x) does not exist and "+
			"the block (%x) will not be added to the chain.\n", block.PrevHash[:5], block.Hash[:5])
		success = false
		return
	}
	if bc.Exist(block.Hash) {
		log.Println("[WARN] Block already exists and will not be added to the chain.")
		success = false
		return
	}

	// validate
	if !owned {
		// validate pow
		pow := NewProof(&block)
		if !pow.Validate() {
			log.Println("invalid pow")
			success = false
			return
		}
		// validate txns (use the chain that the block is on, not necessarily the longest)
		for _, valid := range bc._ValidateTxns(block.Txns, false, block.PrevHash) {
			if !valid {
				log.Println("invalid txns")
				success = false
				return
			}
		}
		//for _, txn := range block.Txns {
		//	if !bc._ValidateTxn(txn, false) {
		//		success = false
		//		return
		//	}
		//}
	}

	// save to db
	err := bc.DB.Put(DBKeyForBlock(block.Hash), block.Encode())
	if err != nil {
		log.Println("[ERROR] Unable to save the block:")
		log.Fatal(err)
	}

	// check chain
	if bytes.Compare(block.PrevHash, bc.LastHash) == 0 {
		err = bc.DB.Put(LastHashKey, block.Hash)
		if err != nil {
			log.Println("[ERROR] Unable to save last hash:")
			log.Fatal(err)
		}
		bc.LastHash = block.Hash
	} else {
		// possible new fork, check length
		if block.BlockNum > bc.Get(bc.LastHash).BlockNum {
			// switch fork (newTxns and oldTxns won't be nil when switching to a new fork, but the length may be zero)
			newTxns, oldTxns = bc.CheckoutFork(block.Hash)
		}
	}
	success = true
	return
}

// CheckoutFork checks out a different fork and returns any difference between two forks. internal use only
func (bc *BlockChain) CheckoutFork(lastHashNew []byte) (newTxns []*Transaction, oldTxns []*Transaction) {
	// NOTE: this function will not acquire lock and therefore can only be called internally.
	//bc.mu.Lock()
	//defer bc.mu.Unlock()

	if bytes.Compare(lastHashNew, bc.LastHash) == 0 {
		log.Println("[WARN] Attempting to checkout the same fork")
		return
	}

	iterNew, iterOld := bc.NewIterator(lastHashNew), bc.NewIterator(bc.LastHash)
	var blockHashesNew [][]byte
	var blockHashesOld [][]byte

	// collect all block hashes
	for block, end := iterNew.Next(); !end; block, end = iterNew.Next() {
		blockHashesNew = append([][]byte{block.Hash}, blockHashesNew...)
	}
	for block, end := iterOld.Next(); !end; block, end = iterOld.Next() {
		blockHashesOld = append([][]byte{block.Hash}, blockHashesOld...)
	}

	// find first different
	i := 0
	for ; i < int(math.Min(float64(len(blockHashesNew)), float64(len(blockHashesOld)))); i++ {
		if bytes.Compare(blockHashesNew[i], blockHashesOld[i]) != 0 {
			break
		}
	}

	// collect txns
	newTxns = []*Transaction{}
	for _, hash := range blockHashesNew[i:] {
		block := bc.Get(hash)
		for _, txn := range block.Txns {
			newTxns = append(newTxns, txn)
		}
	}
	oldTxns = []*Transaction{}
	for _, hash := range blockHashesOld[i:] {
		block := bc.Get(hash)
		for _, txn := range block.Txns {
			oldTxns = append(oldTxns, txn)
		}
	}

	// set last hash
	err := bc.DB.Put(LastHashKey, lastHashNew)
	if err != nil {
		log.Println("[ERROR] Unable to save last hash:")
		log.Fatal(err)
	}
	bc.LastHash = lastHashNew

	return newTxns, oldTxns
}

// NewIterator returns a chain iterator
func (bc *BlockChain) NewIterator(hash []byte) *ChainIterator {
	return &ChainIterator{
		LastHash:    hash,
		CurrentHash: hash,
		Index:       -1,
		BlockChain:  bc,
	}
}

// INTERNAL USE ONLY
func (bc *BlockChain) _ValidateTxn(txn *Transaction, lock bool, fork []byte) bool {
	// when fork is nil, default to validate on the longest chain
	// 1. verify signature
	if !txn.Verify() {
		log.Println("txn has invalid signature")
		log.Println(txn.Data, fmt.Sprintf("%x, %x", txn.Signature, txn.PublicKey))
		return false
	}
	// 2. validate data
	validCand := false
	for _, cand := range bc.Candidates {
		// 2.1 candidates cannot vote
		if bytes.Compare(txn.PublicKey, cand.Wallets[cand.GetAddress()].PublicKey) == 0 {
			log.Println("candidates cannot vote")
			log.Println(txn.Data)
			return false
		}
		// 2.2 voter can only vote for candidates
		if txn.Data.VoterCandidate == cand.CandidateData.CandidateName {
			validCand = true
		}
	}
	if !validCand {
		log.Println("voter can only vote for candidates")
		log.Println(txn.Data)
		return false
	}
	// 2.3: voter can only vote once
	var iter *ChainIterator
	if lock && fork == nil {
		bc.mu.Lock()
		iter = bc.NewIterator(bc.LastHash)
		bc.mu.Unlock()
	} else {
		if fork == nil {
			iter = bc.NewIterator(bc.LastHash)
		} else {
			iter = bc.NewIterator(fork)
		}
	}

	for block, end := iter.Next(); !end; block, end = iter.Next() {
		for _, pastTxn := range block.Txns {
			if bytes.Compare(pastTxn.PublicKey, txn.PublicKey) == 0 {
				log.Println("voter has voted")
				log.Println(txn.Data)
				return false
			}
		}
	}
	return true
}

// INTERNAL USE ONLY
func (bc *BlockChain) _ValidateTxns(txns []*Transaction, lock bool, fork []byte) (res []bool) {
	// check conflicting txns (first received wins)
	// NOTE: txns should be sorted by when they were received. earlier txns should appear in front
	// when fork is nil, default to validate on the longest chain
	if lock {
		bc.mu.Lock()
	}
	voterMap := make(map[string]bool)
	for _, txn := range txns {
		if voterMap[fmt.Sprintf("%x", txn.PublicKey)] {
			res = append(res, false)
			log.Println("voter has voted in the same block")
			log.Println(txn.Data)
		} else {
			res = append(res, bc._ValidateTxn(txn, false, fork))
			if res[len(res)-1] {
				voterMap[fmt.Sprintf("%x", txn.PublicKey)] = true
			}
		}
	}
	if lock {
		bc.mu.Unlock()
	}
	return
}

func (bc *BlockChain) ValidateTxn(txn *Transaction) bool {
	return bc._ValidateTxn(txn, true, nil)
}

// ValidateTxns validates a set of transactions and deal with conflicting transactions among them
func (bc *BlockChain) ValidateTxns(txns []*Transaction) (res []bool) {
	res = bc._ValidateTxns(txns, true, nil)
	return
}

// TxnStatus returns the number of blocks that confirm the given txn. -1 indicates txn not found
func (bc *BlockChain) TxnStatus(txid []byte) int {
	// get an iterator for the longest chain
	bc.mu.Lock()
	iter := bc.NewIterator(bc.LastHash)
	bc.mu.Unlock()
	res := -1
	for block, end := iter.Next(); !end; block, end = iter.Next() {
		for _, txn := range block.Txns {
			if bytes.Compare(txn.ID, txid) == 0 {
				res = iter.Index
				break
			}
		}
		if res != -1 {
			break
		}
	}

	return res
}

func (bc *BlockChain) VotingStatus() (votes []uint, txns []Transaction) {
	for i := 0; i < len(bc.Candidates); i++ {
		votes = append(votes, 0)
	}
	bc.mu.Lock()
	iter := bc.NewIterator(bc.LastHash)
	bc.mu.Unlock()
	skip := NumConfirmed // last NUM_CONFIRMED blocks do not count
	for block, end := iter.Next(); !end; block, end = iter.Next() {
		if skip > 0 {
			skip--
			continue
		}
		for _, txn := range block.Txns {
			txns = append(txns, *txn)
			for idx, cand := range bc.Candidates {
				if txn.Data.VoterCandidate == cand.CandidateData.CandidateName {
					votes[idx]++
					break
				}
			}
		}
	}
	return
}

// ----- ChainIterator APIs -----

func (iter *ChainIterator) Next() (block *Block, end bool) {
	block = iter.BlockChain.Get(iter.CurrentHash)
	iter.CurrentHash = block.PrevHash
	iter.Index++
	return block, block.BlockNum == 0
}

func (iter *ChainIterator) Reset() {
	iter.CurrentHash = iter.LastHash
	iter.Index = -1
}

// ----- Utility functions -----

// DBKeyForBlock returns the database key for a given block hash by concatenating prefix and hash.
func DBKeyForBlock(blockHash []byte) []byte {
	return bytes.Join([][]byte{[]byte(BlockKeyPrefix), blockHash}, []byte{})
}
