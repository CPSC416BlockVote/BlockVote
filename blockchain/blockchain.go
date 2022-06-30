package blockchain

import (
	"bytes"
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
	mu       sync.Mutex
	LastHash []byte // should not be accessed without locking (unsafe). should not be accessed directly from outside
	DB       *util.Database
}

type ChainIterator struct {
	LastHash    []byte
	CurrentHash []byte
	Index       int
	BlockChain  *BlockChain
}

type QueryHandler struct {
	iter        *ChainIterator
	pendingTxns []*Transaction // pending transactions that need to be considered for the query
	numSkip     int            // number of blocks to skip
}

type QueryCondition func(*Transaction) bool

type QueryGroupBy func(*Transaction) interface{}

// ----- BlockChain APIs -----

func NewBlockChain(DB *util.Database) *BlockChain {
	return &BlockChain{DB: DB}
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
func (bc *BlockChain) GetLastHash() (lastHash []byte) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	lastHash = append(lastHash, bc.LastHash...) // make a copy of slice as source will change
	return lastHash
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
			"the block (%x) from [%s] will not be added to the chain.\n", block.PrevHash[:5], block.Hash[:5], block.MinerID)
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
			log.Println("[WARN] Block has invalid pow and is rejected")
			success = false
			return
		}
		// validate txns (use the chain that the block is on, not necessarily the longest)
		for _, valid := range bc._ValidateTxns(block.Txns, false, block.PrevHash, true) {
			if !valid {
				log.Println("[WARN] Block has invalid txns and is rejected")
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
func (bc *BlockChain) _ValidateTxn(txn *Transaction, lock bool, fork []byte, pendingTxns []*Transaction, checkCode bool) bool {
	// when fork is nil, default to validate on the longest chain

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
	queryHandler := NewQueryHandler(iter, pendingTxns, 0)

	valid, code := txn.Validate(queryHandler)
	if checkCode && code != txn.Receipt.Code {
		valid = false
		code = TxnStatusInvalidStatus
	}
	txn.Receipt.Code = code

	if !valid {
		logInvalidTxn(txn)
		return false
	}

	return true
}

// INTERNAL USE ONLY
func (bc *BlockChain) _ValidateTxns(txns []*Transaction, lock bool, fork []byte, checkStatus bool) (res []bool) {
	// check conflicting txns (first received wins)
	// NOTE: txns should be sorted by when they were received. earlier txns should appear in front
	// when fork is nil, default to validate on the longest chain
	// checkStatus: whether to check the status code of a transaction as a validation process, or to set them after validation
	if lock {
		bc.mu.Lock()
	}
	var validPendingTxns []*Transaction
	for _, txn := range txns {
		res = append(res, bc._ValidateTxn(txn, false, fork, validPendingTxns, checkStatus))
		if res[len(res)-1] {
			validPendingTxns = append(validPendingTxns, txn)
		}
	}
	if lock {
		bc.mu.Unlock()
	}
	return
}

//func (bc *BlockChain) ValidateTxn(txn *Transaction) bool {
//	return bc._ValidateTxn(txn, true, nil)
//}

// ValidatePendingTxns validates a set of pending transactions and set the status code for them
func (bc *BlockChain) ValidatePendingTxns(txns []*Transaction) (res []bool) {
	res = bc._ValidateTxns(txns, true, nil, false)
	return
}

// TxnStatus returns the number of blocks that confirm the given txn. -1 indicates txn not found
func (bc *BlockChain) TxnStatus(txId []byte) TransactionStatus {
	// get an iterator for the longest chain
	queryHandler := NewQueryHandler(bc.NewIterator(bc.GetLastHash()), nil, 0)
	return queryHandler.Status(txId)
}

func (bc *BlockChain) VotingStatus(pollID string) (meta PollMeta) {
	queryHandler := NewQueryHandler(bc.NewIterator(bc.GetLastHash()), nil, NumConfirmed) // last NUM_CONFIRMED blocks do not

	// find launch event and terminate event
	events, bkNums := queryHandler.FetchWithBlockNum(func(txn *Transaction) bool {
		return txn.Data.Method != PayloadMethodVote && txn.Data.PollID == pollID
	})

	// count votes
	votesMap := queryHandler.CountByGroup(func(txn *Transaction) bool {
		return txn.Data.Method == PayloadMethodVote && txn.Data.PollID == pollID
	}, func(txn *Transaction) interface{} {
		return txn.Data.Extra.(string)
	})

	// construct metadata
	if len(events) == 2 {
		// poll is ended
		if events[0].Data.Method == PayloadMethodTerminate {
			events[0], events[1] = events[1], events[0] // launch event first, then terminate event
			bkNums[0], bkNums[1] = bkNums[1], bkNums[0]
		}
		meta.EndBlock = bkNums[1]
	} else if len(events) == 1 {
		// poll may be ongoing
		duration := events[0].Data.Extra.(Rules).Duration
		if duration == 0 {
			meta.EndBlock = 0
		} else {
			meta.EndBlock = bkNums[0] + duration
		}
	} else {
		// poll not found
		return
	}
	meta.PollID = pollID
	rules := events[0].Data.Extra.(Rules)
	var voteCounts []uint
	totalVotes := uint(0)
	for _, op := range rules.Options {
		voteCounts = append(voteCounts, votesMap[op])
		totalVotes += votesMap[op]
	}
	meta.InitiatorName = events[0].Data.UserName
	meta.InitiatorID = events[0].Data.UserID
	meta.InitiatorPubKey = events[0].PublicKey
	meta.StartBlock = bkNums[0]
	meta.Rules = rules
	meta.VoteCounts = voteCounts
	meta.TotalVotes = totalVotes

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

// ----- QueryHandler APIs -----

func NewQueryHandler(iter *ChainIterator, pendingTxns []*Transaction, skip int) *QueryHandler {
	return &QueryHandler{
		iter:        iter,
		pendingTxns: pendingTxns,
		numSkip:     skip,
	}
}

func (handler *QueryHandler) skip() (end bool) {
	handler.iter.Reset()
	if handler.numSkip == 0 {
		return
	}
	skips := 1
	for _, end = handler.iter.Next(); !end && skips != handler.numSkip; _, end = handler.iter.Next() {
		skips++
	}
	return
}

// Count API returns the number of transactions that satisfy given condition
func (handler *QueryHandler) Count(cond QueryCondition) (count uint) {
	end := handler.skip() // necessary as there may be multiple query requests for a handler
	for _, pdTxn := range handler.pendingTxns {
		if cond(pdTxn) && pdTxn.Success() {
			count++
		}
	}
	if end {
		return
	}
	for block, end := handler.iter.Next(); !end; block, end = handler.iter.Next() {
		for _, pastTxn := range block.Txns {
			if cond(pastTxn) && pastTxn.Success() {
				count++
			}
		}
	}
	return
}

// CountByGroup API returns the number of transactions that satisfy given condition by group
func (handler *QueryHandler) CountByGroup(cond QueryCondition, groupBy QueryGroupBy) (counts map[interface{}]uint) {
	end := handler.skip() // necessary as there may be multiple query requests for a handler
	for _, pdTxn := range handler.pendingTxns {
		if cond(pdTxn) && pdTxn.Success() {
			counts[groupBy(pdTxn)]++
		}
	}
	if end {
		return
	}
	for block, end := handler.iter.Next(); !end; block, end = handler.iter.Next() {
		for _, pastTxn := range block.Txns {
			if cond(pastTxn) && pastTxn.Success() {
				counts[groupBy(pastTxn)]++
			}
		}
	}
	return
}

// Fetch API returns all transactions that satisfy given condition
func (handler *QueryHandler) Fetch(cond QueryCondition) (txns []*Transaction) {
	end := handler.skip()
	for _, pdTxn := range handler.pendingTxns {
		if cond(pdTxn) && pdTxn.Success() {
			txns = append(txns, pdTxn)
		}
	}
	if end {
		return
	}
	for block, end := handler.iter.Next(); !end; block, end = handler.iter.Next() {
		for _, pastTxn := range block.Txns {
			if cond(pastTxn) && pastTxn.Success() {
				txns = append(txns, pastTxn)
			}
		}
	}
	return
}

// FetchWithBlockNum API returns all transactions that satisfy given condition, along with corresponding block number
func (handler *QueryHandler) FetchWithBlockNum(cond QueryCondition) (txns []*Transaction, bkNums []uint) {
	nextBkNum := handler.NextBlockNum()
	end := handler.skip()
	for _, pdTxn := range handler.pendingTxns {
		if cond(pdTxn) && pdTxn.Success() {
			txns = append(txns, pdTxn)
			bkNums = append(bkNums, nextBkNum)
		}
	}
	if end {
		return
	}
	for block, end := handler.iter.Next(); !end; block, end = handler.iter.Next() {
		for _, pastTxn := range block.Txns {
			if cond(pastTxn) && pastTxn.Success() {
				txns = append(txns, pastTxn)
				bkNums = append(bkNums, block.BlockNum)
			}
		}
	}
	return
}

func (handler *QueryHandler) CurrentBlockNum() uint {
	handler.iter.Reset() // do not skip, only reset.
	block, _ := handler.iter.Next()
	return block.BlockNum
}

func (handler *QueryHandler) NextBlockNum() uint {
	handler.iter.Reset() // do not skip, only reset.
	block, _ := handler.iter.Next()
	return block.BlockNum + 1
}

func (handler *QueryHandler) Status(txId []byte) (status TransactionStatus) {
	end := handler.skip()
	if end {
		return TransactionStatus{-1, TransactionReceipt{}}
	}
	for block, end := handler.iter.Next(); !end; block, end = handler.iter.Next() {
		for _, pastTxn := range block.Txns {
			if bytes.Compare(pastTxn.ID, txId) == 0 {
				return TransactionStatus{
					Confirmed: handler.iter.Index,
					Receipt:   pastTxn.Receipt,
				}
			}
		}
	}

	return TransactionStatus{-1, TransactionReceipt{}}
}

// ----- Utility functions -----

// DBKeyForBlock returns the database key for a given block hash by concatenating prefix and hash.
func DBKeyForBlock(blockHash []byte) []byte {
	return bytes.Join([][]byte{[]byte(BlockKeyPrefix), blockHash}, []byte{})
}

func logInvalidTxn(txn *Transaction) {
	var msg string
	switch txn.Receipt.Code {
	case TxnStatusInvalidSignature:
		msg = "invalid signature"
		break
	case TxnStatusInvalidPayloadMethod:
		msg = "invalid payload method"
		break
	case TxnStatusInvalidPollID:
		msg = "invalid poll ID"
		break
	case TxnStatusPollExpired:
		msg = "poll is expired"
		break
	case TxnStatusAdminNotSet:
		msg = "admins should be set if expired time is not set"
		break
	case TxnStatusInvalidVotesPerUser:
		msg = "votes per user should be at least one"
		break
	case TxnStatusNotEnoughOptions:
		msg = "not enough options"
		break
	case TxnStatusPollIDExist:
		msg = "cannot launch a poll with an existing poll id."
		break
	case TxnStatusBannedUser:
		msg = "user is not allowed to vote"
		break
	case TxnStatusInvalidOptions:
		msg = "invalid options"
		break
	case TxnStatusNoVotes:
		msg = "no votes remaining"
		break
	case TxnStatusPollAlreadyEnded:
		msg = "poll is already ended"
		break
	case TxnStatusUnauthorizedUser:
		msg = "unauthorized user"
		break
	}
	log.Println(fmt.Sprintf("Invalid transaction %x: %s\n%v", txn.ID[:5], msg, txn.Data))
}
