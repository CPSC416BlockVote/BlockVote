package blockchain

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"fmt"
	"github.com/stretchr/testify/suite"
	"testing"
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
	s.storage = new(util.Database)
	err := s.storage.New("", true)
	s.Require().Nil(err)
	s.blockchain = NewBlockChain(s.storage)
	err = s.blockchain.Init()
	s.Require().Nil(err)
	copy(s.admins[:], s.getSigners(2))
	copy(s.voters[:], s.getSigners(5))
	copy(s.cands[:], s.getSigners(3))
}

func (s *BlockchainTestSuite) TearDownTest() {
	s.storage.Close()
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

			s.mine([]*Transaction{txn})
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

			s.mine([]*Transaction{txn})
			for i := 0; i < NumConfirmed; i++ {
				s.mine(nil)
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

			s.mine([]*Transaction{txn})
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

			s.mine([]*Transaction{txn})
			startBlock := s.blockchain.Get(s.blockchain.GetLastHash()).BlockNum
			for i := 0; i < NumConfirmed; i++ {
				s.mine(nil)
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

			s.mine([]*Transaction{txn, voteTxn})
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
				s.mine([]*Transaction{voteTxn})
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

			s.mine([]*Transaction{txn, voteTxn})
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
				s.mine([]*Transaction{voteTxn})
			}
			for i := 0; i < NumConfirmed; i++ {
				s.mine(nil)
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

			s.mine([]*Transaction{txn})
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
			s.mine([]*Transaction{voteTxn, terminateTxn})
			for i := 0; i < 4; i++ {
				voteTxn = NewTransaction(&Payload{
					Method:   PayloadMethodVote,
					UserName: s.voters[(i+1)%len(s.voters)].Name,
					UserID:   s.voters[(i+1)%len(s.voters)].ID,
					PollID:   "TestTerminatedPollWithVotes",
					Extra:    []string{s.cands[i%2].Name},
				}, 0, s.voters[(i+1)%len(s.voters)].PublicKey)
				voteTxn.Sign(s.voters[(i+1)%len(s.voters)].PrivateKey)
				s.mine([]*Transaction{voteTxn})
			}
			for i := 0; i < NumConfirmed; i++ {
				s.mine(nil)
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

func (s *BlockchainTestSuite) mine(txns []*Transaction) {
	valids := s.blockchain.ValidatePendingTxns(txns)
	for _, valid := range valids {
		s.Require().True(valid)
	}
	block := Block{
		PrevHash: s.blockchain.GetLastHash(),
		BlockNum: s.blockchain.Get(s.blockchain.GetLastHash()).BlockNum + 1,
		Nonce:    0,
		Txns:     txns,
		MinerID:  "test",
		Hash:     []byte{},
	}
	pow := *NewProof(&block)
	pow.Run()
	success, _, _ := s.blockchain.Put(block, true)
	s.Require().True(success)
}
