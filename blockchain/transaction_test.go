package blockchain

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"fmt"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
)

type TxnValidationTestSuite struct {
	suite.Suite
	storage    *util.Database
	blockchain *BlockChain
	admins     [2]*Identity.Signer
	cands      [3]*Identity.Signer
	voters     [5]*Identity.Signer
}

func TestTxnValidationTestSuite(t *testing.T) {
	suite.Run(t, new(TxnValidationTestSuite))
}

func (s *TxnValidationTestSuite) SetupTest() {
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

func (s *TxnValidationTestSuite) TearDownTest() {
	s.storage.Close()
}

func (s *TxnValidationTestSuite) TestValidateSignature() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"ShouldReturnFalseIfInvalidSignature", func() {
			txn := NewTransaction(&Payload{}, 0, s.voters[0].PublicKey)
			txn.Sign(s.admins[1].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), nil, 0)
			valid, code := txn.Validate(queryHandler)

			s.False(valid)
			s.Equal(TxnStatusInvalidSignature, code)
		}},
		{"ShouldReturnTrueIfValidSignature", func() {
			txn := NewTransaction(&Payload{}, 0, s.voters[0].PublicKey)
			txn.Sign(s.voters[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), nil, 0)
			valid, _ := txn.Validate(queryHandler)

			s.True(valid)
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *TxnValidationTestSuite) TestValidateLaunch() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestAdminAndDurationNotSet", func() {
			txn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestAdminAndDurationNotSet",
				Extra: Rules{
					Admins:       nil,
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), nil, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusAdminNotSet, code)
		}},
		{"TestAdminNotSetButDurationSet", func() {
			txn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestAdminNotSetButDurationSet",
				Extra: Rules{
					Admins:       nil,
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     1,
				},
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), nil, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusSuccess, code)
		}},
		{"ShouldFailWithZeroVotesPerUser", func() {
			txn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "ShouldFailWithZeroVotesPerUser",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 0,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     1,
				},
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), nil, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusInvalidVotesPerUser, code)
		}},
		{"ShouldFailWithNotEnoughOptions", func() {
			txn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "ShouldFailWithNotEnoughOptions",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name},
					BannedUsers:  nil,
					Duration:     1,
				},
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), nil, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusNotEnoughOptions, code)
		}},
		{"ShouldFailIfPollExist", func() {
			txn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "ShouldFailIfPollExist",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			txnCopy := &*txn

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), []*Transaction{txn}, 0)
			valid, code := txnCopy.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusPollIDExist, code)
		}},
		{"TestDurationNotSet", func() {
			txn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestDurationNotSet",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), nil, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusSuccess, code)
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *TxnValidationTestSuite) TestValidateVote() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"ShouldFailWithInvalidPollID", func() {
			txn := NewTransaction(&Payload{
				Method:   PayloadMethodVote,
				UserName: s.voters[0].Name,
				UserID:   s.voters[0].ID,
				PollID:   "ShouldFailWithInvalidPollID",
				Extra:    []string{s.cands[0].Name},
			}, 0, s.voters[0].PublicKey)
			txn.Sign(s.voters[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), nil, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusInvalidPollID, code)
		}},
		{"ShouldFailIfExpired", func() {
			launchTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "ShouldFailIfExpired",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     1,
				},
			}, 0, s.admins[0].PublicKey)
			launchTxn.Sign(s.admins[0].PrivateKey)

			s.mine([]*Transaction{launchTxn})
			s.mine(nil)

			txn := NewTransaction(&Payload{
				Method:   PayloadMethodVote,
				UserName: s.voters[0].Name,
				UserID:   s.voters[0].ID,
				PollID:   "ShouldFailIfExpired",
				Extra:    []string{s.cands[0].Name},
			}, 0, s.voters[0].PublicKey)
			txn.Sign(s.voters[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), nil, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusPollExpired, code)
		}},
		{"ShouldFailIfTerminated", func() {
			launchTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "ShouldFailIfTerminated",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			launchTxn.Sign(s.admins[0].PrivateKey)
			terminateTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "ShouldFailIfTerminated",
				Extra:    nil,
			}, 0, s.admins[0].PublicKey)
			terminateTxn.Sign(s.admins[0].PrivateKey)

			txn := NewTransaction(&Payload{
				Method:   PayloadMethodVote,
				UserName: s.voters[0].Name,
				UserID:   s.voters[0].ID,
				PollID:   "ShouldFailIfTerminated",
				Extra:    []string{s.cands[0].Name},
			}, 0, s.voters[0].PublicKey)
			txn.Sign(s.voters[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), []*Transaction{launchTxn, terminateTxn}, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusPollExpired, code)
		}},
		{"ShouldFailIfBanned", func() {
			launchTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "ShouldFailIfBanned",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  [][]byte{s.cands[0].PublicKey, s.cands[1].PublicKey},
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			launchTxn.Sign(s.admins[0].PrivateKey)

			txn := NewTransaction(&Payload{
				Method:   PayloadMethodVote,
				UserName: s.cands[0].Name,
				UserID:   s.cands[0].ID,
				PollID:   "ShouldFailIfBanned",
				Extra:    []string{s.cands[0].Name},
			}, 0, s.cands[0].PublicKey)
			txn.Sign(s.cands[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), []*Transaction{launchTxn}, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusBannedUser, code)
		}},
		{"ShouldFailWithInvalidOptions", func() {
			launchTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "ShouldFailWithInvalidOptions",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			launchTxn.Sign(s.admins[0].PrivateKey)

			txn := NewTransaction(&Payload{
				Method:   PayloadMethodVote,
				UserName: s.voters[0].Name,
				UserID:   s.voters[0].ID,
				PollID:   "ShouldFailWithInvalidOptions",
				Extra:    []string{s.cands[2].Name},
			}, 0, s.voters[0].PublicKey)
			txn.Sign(s.voters[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), []*Transaction{launchTxn}, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusInvalidOptions, code)
		}},
		{"ShouldFailWithNoVotes", func() {
			launchTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "ShouldFailWithNoVotes",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			launchTxn.Sign(s.admins[0].PrivateKey)

			voteTxn := NewTransaction(&Payload{
				Method:   PayloadMethodVote,
				UserName: s.voters[0].Name,
				UserID:   s.voters[0].ID,
				PollID:   "ShouldFailWithNoVotes",
				Extra:    []string{s.cands[0].Name},
			}, 0, s.voters[0].PublicKey)
			voteTxn.Sign(s.voters[0].PrivateKey)

			txn := NewTransaction(&Payload{
				Method:   PayloadMethodVote,
				UserName: s.voters[0].Name,
				UserID:   s.voters[0].ID,
				PollID:   "ShouldFailWithNoVotes",
				Extra:    []string{s.cands[1].Name},
			}, 0, s.voters[0].PublicKey)
			txn.Sign(s.voters[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), []*Transaction{launchTxn, voteTxn}, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusNoVotes, code)
		}},
		{"ShouldSucceedIfOK", func() {
			launchTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "ShouldSucceedIfOK",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			launchTxn.Sign(s.admins[0].PrivateKey)

			txn := NewTransaction(&Payload{
				Method:   PayloadMethodVote,
				UserName: s.voters[0].Name,
				UserID:   s.voters[0].ID,
				PollID:   "ShouldSucceedIfOK",
				Extra:    []string{s.cands[1].Name},
			}, 0, s.voters[0].PublicKey)
			txn.Sign(s.voters[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), []*Transaction{launchTxn}, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusSuccess, code)
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *TxnValidationTestSuite) TestValidateTerminate() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestInvalidPollID", func() {
			launchTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestInvalidPollID",
				Extra: Rules{
					Admins:       nil,
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			launchTxn.Sign(s.admins[0].PrivateKey)

			txn := NewTransaction(&Payload{
				Method:   PayloadMethodTerminate,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "",
				Extra:    nil,
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), []*Transaction{launchTxn}, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusInvalidPollID, code)
		}},
		{"TestTerminatedPoll", func() {
			launchTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestTerminatedPoll",
				Extra: Rules{
					Admins:       nil,
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			launchTxn.Sign(s.admins[0].PrivateKey)

			terminateTxn := NewTransaction(&Payload{
				Method:   PayloadMethodTerminate,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestTerminatedPoll",
				Extra:    nil,
			}, 0, s.admins[0].PublicKey)
			terminateTxn.Sign(s.admins[0].PrivateKey)

			txn := NewTransaction(&Payload{
				Method:   PayloadMethodTerminate,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestTerminatedPoll",
				Extra:    nil,
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), []*Transaction{launchTxn, terminateTxn}, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusPollAlreadyEnded, code)
		}},
		{"TestEndedPoll", func() {
			launchTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestEndedPoll",
				Extra: Rules{
					Admins:       nil,
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name, s.cands[1].Name},
					BannedUsers:  nil,
					Duration:     1,
				},
			}, 0, s.admins[0].PublicKey)
			launchTxn.Sign(s.admins[0].PrivateKey)

			s.mine([]*Transaction{launchTxn})
			s.mine(nil)

			txn := NewTransaction(&Payload{
				Method:   PayloadMethodTerminate,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestEndedPoll",
				Extra:    nil,
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), nil, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusPollAlreadyEnded, code)
		}},
		{"TestUnauthorizedUser", func() {
			launchTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestUnauthorizedUser",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			launchTxn.Sign(s.admins[0].PrivateKey)

			txn := NewTransaction(&Payload{
				Method:   PayloadMethodTerminate,
				UserName: s.voters[0].Name,
				UserID:   s.voters[0].ID,
				PollID:   "TestUnauthorizedUser",
				Extra:    nil,
			}, 0, s.voters[0].PublicKey)
			txn.Sign(s.voters[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), []*Transaction{launchTxn}, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusUnauthorizedUser, code)
		}},
		{"TestOK", func() {
			launchTxn := NewTransaction(&Payload{
				Method:   PayLoadMethodLaunch,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestOK",
				Extra: Rules{
					Admins:       [][]byte{s.admins[0].PublicKey},
					VotesPerUser: 1,
					Options:      []string{s.cands[0].Name},
					BannedUsers:  nil,
					Duration:     0,
				},
			}, 0, s.admins[0].PublicKey)
			launchTxn.Sign(s.admins[0].PrivateKey)

			txn := NewTransaction(&Payload{
				Method:   PayloadMethodTerminate,
				UserName: s.admins[0].Name,
				UserID:   s.admins[0].ID,
				PollID:   "TestOK",
				Extra:    nil,
			}, 0, s.admins[0].PublicKey)
			txn.Sign(s.admins[0].PrivateKey)

			queryHandler := NewQueryHandler(s.blockchain.NewIterator(s.blockchain.GetLastHash()), []*Transaction{launchTxn}, 0)
			valid, code := txn.Validate(queryHandler)

			s.True(valid)
			s.Equal(TxnStatusSuccess, code)
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *TxnValidationTestSuite) getSigners(n uint) (signers []*Identity.Signer) {
	for i := uint(0); i < n; i++ {
		signers = append(signers, Identity.CreateSigner("user"+fmt.Sprintf("%04d", i), fmt.Sprintf("%04d", i)))
	}
	return
}

func (s *TxnValidationTestSuite) mine(txns []*Transaction) {
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
		Hash:       []byte{},
	}
	pow := *NewProof(&block)
	pow.Run()
	success, _, _ := s.blockchain.Put(block, true)
	s.Require().True(success)
}
