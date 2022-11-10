package blockchain

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"fmt"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
)

type BlockchainTestSuite struct {
	suite.Suite
	storage    *util.Database
	blockchain *BlockChain
	admins     [2]*Identity.Signer
	cands      [3]*Identity.Signer
	voters     [5]*Identity.Signer
}

func TestBlockchainTestSuite(t *testing.T) {
	suite.Run(t, new(BlockchainTestSuite))
}

func (s *BlockchainTestSuite) SetupTest() {
	s.resetBlockchain()
	copy(s.admins[:], s.getSigners(2))
	copy(s.voters[:], s.getSigners(5))
	copy(s.cands[:], s.getSigners(3))
}

func (s *BlockchainTestSuite) TearDownTest() {
	s.storage.Close()
}

func (s *BlockchainTestSuite) TestAdjustDifficulty() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"Adjust difficulty at expected interval", func() {
			s.resetBlockchain()
			initialDifficulty := s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty
			s.mockAddBlock(&Block{
				BlockNum:   1,
				Difficulty: initialDifficulty,
				Hash:       []byte{byte(1)},
				MinerID:    "test",
				Nonce:      0,
				PrevHash:   s.blockchain.GetLastHash(),
				Timestamp:  time.Now().Unix(),
				Txns:       nil,
			})

			for i := 0; i < DifficultyAdjustInterval; i++ {
				height := 2 + i
				timestamp := s.blockchain.Get(s.blockchain.GetLastHash()).Timestamp + int64(ExpectedMiningSpeedSeconds*2) // double the time
				difficulty := s.blockchain.AdjustDifficulty(s.blockchain.GetLastHash(),
					s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty, timestamp, uint(height))
				s.mockAddBlock(&Block{
					BlockNum:   uint(height),
					Difficulty: difficulty,
					Hash:       []byte{byte(height)},
					MinerID:    "test",
					Nonce:      0,
					PrevHash:   s.blockchain.GetLastHash(),
					Timestamp:  timestamp,
					Txns:       nil,
				})
				if i == DifficultyAdjustInterval-1 {
					s.NotEqual(initialDifficulty, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				} else {
					s.Equal(initialDifficulty, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				}
			}

			adjustedDifficulty := s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty
			for i := 0; i < DifficultyAdjustInterval; i++ {
				height := 2 + DifficultyAdjustInterval + i
				timestamp := s.blockchain.Get(s.blockchain.GetLastHash()).Timestamp + int64(ExpectedMiningSpeedSeconds/2) // half the time
				difficulty := s.blockchain.AdjustDifficulty(s.blockchain.GetLastHash(),
					s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty, timestamp, uint(height))
				s.mockAddBlock(&Block{
					BlockNum:   uint(height),
					Difficulty: difficulty,
					Hash:       []byte{byte(height)},
					MinerID:    "test",
					Nonce:      0,
					PrevHash:   s.blockchain.GetLastHash(),
					Timestamp:  timestamp,
					Txns:       nil,
				})
				if i == DifficultyAdjustInterval-1 {
					s.NotEqual(adjustedDifficulty, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				} else {
					s.Equal(adjustedDifficulty, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				}
			}
		}},
		{"Adjust difficulty correctly", func() {
			s.resetBlockchain()
			initialDifficulty := s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty
			s.mockAddBlock(&Block{
				BlockNum:   1,
				Difficulty: initialDifficulty,
				Hash:       []byte{byte(1)},
				MinerID:    "test",
				Nonce:      0,
				PrevHash:   s.blockchain.GetLastHash(),
				Timestamp:  time.Now().Unix(),
				Txns:       nil,
			})

			for i := 0; i < DifficultyAdjustInterval; i++ {
				height := 2 + i
				timestamp := s.blockchain.Get(s.blockchain.GetLastHash()).Timestamp + int64(ExpectedMiningSpeedSeconds*2) // double the time
				difficulty := s.blockchain.AdjustDifficulty(s.blockchain.GetLastHash(),
					s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty, timestamp, uint(height))
				s.mockAddBlock(&Block{
					BlockNum:   uint(height),
					Difficulty: difficulty,
					Hash:       []byte{byte(height)},
					MinerID:    "test",
					Nonce:      0,
					PrevHash:   s.blockchain.GetLastHash(),
					Timestamp:  timestamp,
					Txns:       nil,
				})
				if i == DifficultyAdjustInterval-1 {
					s.Equal(initialDifficulty/2, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				} else {
					s.Equal(initialDifficulty, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				}
			}

			for i := 0; i < DifficultyAdjustInterval; i++ {
				height := 2 + DifficultyAdjustInterval + i
				timestamp := s.blockchain.Get(s.blockchain.GetLastHash()).Timestamp + int64(ExpectedMiningSpeedSeconds/2) // half the time
				difficulty := s.blockchain.AdjustDifficulty(s.blockchain.GetLastHash(),
					s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty, timestamp, uint(height))
				s.mockAddBlock(&Block{
					BlockNum:   uint(height),
					Difficulty: difficulty,
					Hash:       []byte{byte(height)},
					MinerID:    "test",
					Nonce:      0,
					PrevHash:   s.blockchain.GetLastHash(),
					Timestamp:  timestamp,
					Txns:       nil,
				})
				if i == DifficultyAdjustInterval-1 {
					s.Equal(initialDifficulty, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				} else {
					s.Equal(initialDifficulty/2, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				}
			}
		}},
		{"Adjust difficulty bounded max adjust factor", func() {
			s.resetBlockchain()
			initialDifficulty := s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty
			s.mockAddBlock(&Block{
				BlockNum:   1,
				Difficulty: initialDifficulty,
				Hash:       []byte{byte(1)},
				MinerID:    "test",
				Nonce:      0,
				PrevHash:   s.blockchain.GetLastHash(),
				Timestamp:  time.Now().Unix(),
				Txns:       nil,
			})

			for i := 0; i < DifficultyAdjustInterval; i++ {
				height := 2 + i
				timestamp := s.blockchain.Get(s.blockchain.GetLastHash()).Timestamp + int64(ExpectedMiningSpeedSeconds*(MaxDifficultyAdjustFactor+1))
				difficulty := s.blockchain.AdjustDifficulty(s.blockchain.GetLastHash(),
					s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty, timestamp, uint(height))
				s.mockAddBlock(&Block{
					BlockNum:   uint(height),
					Difficulty: difficulty,
					Hash:       []byte{byte(height)},
					MinerID:    "test",
					Nonce:      0,
					PrevHash:   s.blockchain.GetLastHash(),
					Timestamp:  timestamp,
					Txns:       nil,
				})
				if i == DifficultyAdjustInterval-1 {
					s.Equal(initialDifficulty/MaxDifficultyAdjustFactor, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				} else {
					s.Equal(initialDifficulty, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				}
			}

			for i := 0; i < DifficultyAdjustInterval; i++ {
				height := 2 + DifficultyAdjustInterval + i
				timestamp := s.blockchain.Get(s.blockchain.GetLastHash()).Timestamp + int64(1)
				difficulty := s.blockchain.AdjustDifficulty(s.blockchain.GetLastHash(),
					s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty, timestamp, uint(height))
				s.mockAddBlock(&Block{
					BlockNum:   uint(height),
					Difficulty: difficulty,
					Hash:       []byte{byte(height)},
					MinerID:    "test",
					Nonce:      0,
					PrevHash:   s.blockchain.GetLastHash(),
					Timestamp:  timestamp,
					Txns:       nil,
				})
				if i == DifficultyAdjustInterval-1 {
					s.Equal(initialDifficulty, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				} else {
					s.Equal(initialDifficulty/MaxDifficultyAdjustFactor, s.blockchain.Get(s.blockchain.GetLastHash()).Difficulty)
				}
			}
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *BlockchainTestSuite) TestTxnStatus() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestInvalidTxID", func() {
			txn := NewTransaction(&Payload{}, 0, s.voters[0].PublicKey)

			TxnStatus := s.blockchain.TxnStatus(txn.ID)

			s.Equal(TransactionStatus{
				Confirmed: -1,
				Receipt:   TransactionReceipt{},
			}, TxnStatus)
		}},
		{"TestUncommittedTxID", func() {
			txn := NewTransaction(&Payload{}, 0, s.voters[0].PublicKey)
			txn.Sign(s.voters[0].PrivateKey)

			s.mockMine([]*Transaction{txn})
			TxnStatus := s.blockchain.TxnStatus(txn.ID)

			s.Equal(TransactionStatus{
				Confirmed: 0,
				Receipt: TransactionReceipt{
					Code: TxnStatusInvalidPayloadMethod,
					// TODO: check the following if they are implemented
					//BlockNum: 1,
					//MinerID:  "test",
				},
			}, TxnStatus)
		}},
		{"TestCommittedTxID", func() {
			txn := NewTransaction(&Payload{}, 0, s.voters[0].PublicKey)
			txn.Sign(s.voters[0].PrivateKey)

			s.mockMine([]*Transaction{txn})
			for i := 0; i < NumConfirmed; i++ {
				s.mockMine(nil)
			}
			TxnStatus := s.blockchain.TxnStatus(txn.ID)

			s.Equal(TransactionStatus{
				Confirmed: 4,
				Receipt: TransactionReceipt{
					Code: TxnStatusInvalidPayloadMethod,
					// TODO: check the following if they are implemented
					//BlockNum: 1,
					//MinerID:  "test",
				},
			}, TxnStatus)
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *BlockchainTestSuite) TestPollStatus() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestInvalidPoll", func() {
			meta := s.blockchain.VotingStatus("TestInvalidPoll")
			s.Equal(PollMeta{}, meta)
		}},
		{"TestUncommittedPoll", func() {
			txn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestUncommittedPoll",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			s.mockMine([]*Transaction{txn})
			meta := s.blockchain.VotingStatus("TestUncommittedPoll")
			s.Equal(PollMeta{}, meta)
		}},
		{"TestCommittedPoll", func() {
			txn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestCommittedPoll",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			s.mockMine([]*Transaction{txn})
			startBlock := s.blockchain.Get(s.blockchain.GetLastHash()).BlockNum
			for i := 0; i < NumConfirmed; i++ {
				s.mockMine(nil)
			}
			meta := s.blockchain.VotingStatus("TestCommittedPoll")

			s.Equal(PollMeta{
				PollID:          "TestCommittedPoll",
				InitiatorName:   s.admins[0].Name,
				InitiatorID:     s.admins[0].ID,
				InitiatorPubKey: s.admins[0].PublicKey,
				StartBlock:      startBlock,
				EndBlock:        0,
				Rules: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
				VoteCounts: []uint{0, 0},
				TotalVotes: 0,
			}, meta)
		}},
		{"TestPollWithVotes", func() {
			txn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestPollWithVotes",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			voteTxn := NewTransaction(&Payload{
				Method:   PayloadMethodVote,
				UserName: s.voters[0].Name,
				UserID:   s.voters[0].ID,
				PollID:   "TestPollWithVotes",
				Extra:    []string{s.cands[0].Name},
			}, 0, s.voters[0].PublicKey)
			voteTxn.Sign(s.voters[0].PrivateKey)

			s.mockMine([]*Transaction{txn, voteTxn})
			startBlock := s.blockchain.Get(s.blockchain.GetLastHash()).BlockNum
			for i := 0; i < NumConfirmed; i++ {
				voteTxn = NewTransaction(&Payload{
					Method:   PayloadMethodVote,
					UserName: s.voters[(i+1)%len(s.voters)].Name,
					UserID:   s.voters[(i+1)%len(s.voters)].ID,
					PollID:   "TestPollWithVotes",
					Extra:    []string{s.cands[i%2].Name},
				}, 0, s.voters[(i+1)%len(s.voters)].PublicKey)
				voteTxn.Sign(s.voters[(i+1)%len(s.voters)].PrivateKey)
				s.mockMine([]*Transaction{voteTxn})
			}
			meta := s.blockchain.VotingStatus("TestPollWithVotes")

			s.Equal(PollMeta{
				PollID:          "TestPollWithVotes",
				InitiatorName:   s.admins[0].Name,
				InitiatorID:     s.admins[0].ID,
				InitiatorPubKey: s.admins[0].PublicKey,
				StartBlock:      startBlock,
				EndBlock:        0,
				Rules: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
				VoteCounts: []uint{1, 0},
				TotalVotes: 1,
			}, meta)
		}},
		{"TestTimedPollWithVotes", func() {
			txn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestTimedPollWithVotes",
				Extra: Rules{
					Admins:       nil,
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     2,
				},
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			voteTxn := NewTransaction(&Payload{
				Method:   PayloadMethodVote,
				UserName: s.voters[0].Name,
				UserID:   s.voters[0].ID,
				PollID:   "TestTimedPollWithVotes",
				Extra:    []string{s.cands[0].Name},
			}, 0, s.voters[0].PublicKey)
			voteTxn.Sign(s.voters[0].PrivateKey)

			s.mockMine([]*Transaction{txn, voteTxn})
			startBlock := s.blockchain.Get(s.blockchain.GetLastHash()).BlockNum
			for i := 0; i < 4; i++ {
				voteTxn = NewTransaction(&Payload{
					Method:   PayloadMethodVote,
					UserName: s.voters[(i+1)%len(s.voters)].Name,
					UserID:   s.voters[(i+1)%len(s.voters)].ID,
					PollID:   "TestTimedPollWithVotes",
					Extra:    []string{s.cands[i%2].Name},
				}, 0, s.voters[(i+1)%len(s.voters)].PublicKey)
				voteTxn.Sign(s.voters[(i+1)%len(s.voters)].PrivateKey)
				s.mockMine([]*Transaction{voteTxn})
			}
			for i := 0; i < NumConfirmed; i++ {
				s.mockMine(nil)
			}
			meta := s.blockchain.VotingStatus("TestTimedPollWithVotes")

			s.Equal(PollMeta{
				PollID:          "TestTimedPollWithVotes",
				InitiatorName:   s.admins[0].Name,
				InitiatorID:     s.admins[0].ID,
				InitiatorPubKey: s.admins[0].PublicKey,
				StartBlock:      startBlock,
				EndBlock:        startBlock + 2,
				Rules: Rules{
					Admins:       nil,
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     2,
				},
				VoteCounts: []uint{2, 1},
				TotalVotes: 3,
			}, meta)
		}},
		{"TestTerminatedPollWithVotes", func() {
			txn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestTerminatedPollWithVotes",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			s.mockMine([]*Transaction{txn})
			startBlock := s.blockchain.Get(s.blockchain.GetLastHash()).BlockNum

			voteTxn := NewTransaction(&Payload{
				Method:   PayloadMethodVote,
				UserName: s.voters[0].Name,
				UserID:   s.voters[0].ID,
				PollID:   "TestTerminatedPollWithVotes",
				Extra:    []string{s.cands[0].Name},
			}, 0, s.voters[0].PublicKey)
			voteTxn.Sign(s.voters[0].PrivateKey)

			terminateTxn := NewTransaction(&Payload{
				Method:   PayloadMethodTerminate,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestTerminatedPollWithVotes",
				Extra:    nil,
			}, 0, s.admins[0].PublicKey)
			terminateTxn.Sign(s.admins[0].PrivateKey)
			s.mockMine([]*Transaction{voteTxn, terminateTxn})
			for i := 0; i < 4; i++ {
				voteTxn = NewTransaction(&Payload{
					Method:   PayloadMethodVote,
					UserName: s.voters[(i+1)%len(s.voters)].Name,
					UserID:   s.voters[(i+1)%len(s.voters)].ID,
					PollID:   "TestTerminatedPollWithVotes",
					Extra:    []string{s.cands[i%2].Name},
				}, 0, s.voters[(i+1)%len(s.voters)].PublicKey)
				voteTxn.Sign(s.voters[(i+1)%len(s.voters)].PrivateKey)
				s.mockMine([]*Transaction{voteTxn})
			}
			for i := 0; i < NumConfirmed; i++ {
				s.mockMine(nil)
			}
			meta := s.blockchain.VotingStatus("TestTerminatedPollWithVotes")

			s.Equal(PollMeta{
				PollID:          "TestTerminatedPollWithVotes",
				InitiatorName:   s.admins[0].Name,
				InitiatorID:     s.admins[0].ID,
				InitiatorPubKey: s.admins[0].PublicKey,
				StartBlock:      startBlock,
				EndBlock:        startBlock + 1,
				Rules: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
				VoteCounts: []uint{1, 0},
				TotalVotes: 1,
			}, meta)
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *BlockchainTestSuite) getSigners(n uint) (signers []*Identity.Signer) {
	for i := uint(0); i < n; i++ {
		signers = append(signers, Identity.CreateSigner("user"+fmt.Sprintf("%04d", i), fmt.Sprintf("%04d", i)))
	}
	return
}

func (s *BlockchainTestSuite) resetBlockchain() {
	s.storage = new(util.Database)
	err := s.storage.New("", true)
	s.Require().Nil(err)
	s.blockchain = NewBlockChain(s.storage)
	err = s.blockchain.Init()
	s.Require().Nil(err)
}

// mockAddBlock mocks adding the block to the blockchain, without validating transactions or pow.
// Sanity check will still in place.
func (s *BlockchainTestSuite) mockAddBlock(block *Block) {
	success, _, _ := s.blockchain.Put(*block, true)
	s.Require().True(success)
}

// mockMine mocks the mining process without actually running the proof of work algorithm.
// Sets the block hash to block number.
func (s *BlockchainTestSuite) mockMine(txns []*Transaction) {
	valids := s.blockchain.ValidatePendingTxns(txns)
	for _, valid := range valids {
		s.Require().True(valid)
	}

	prevHash := s.blockchain.GetLastHash()
	height := s.blockchain.Get(prevHash).BlockNum + 1
	timestamp := time.Now().Unix()
	difficulty := s.blockchain.AdjustDifficulty(prevHash, s.blockchain.Get(prevHash).Difficulty, timestamp, height)
	block := Block{
		PrevHash:   prevHash,
		BlockNum:   height,
		Timestamp:  timestamp,
		Difficulty: difficulty,
		Nonce:      0,
		Txns:       txns,
		MinerID:    "test",
		Hash:       []byte{byte(height)},
	}
	success, _, _ := s.blockchain.Put(block, true)
	s.Require().True(success)
}
