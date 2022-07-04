package evlib

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"fmt"
	"github.com/stretchr/testify/suite"
	"testing"
)

type EVTestSuite struct {
	suite.Suite
	evLib   *EV
	n       int
	admins1 [5]*Identity.Signer
	admins2 [5]*Identity.Signer
	voters  [15]*Identity.Signer
	cands1  [5]*Identity.Signer
	cands2  [5]*Identity.Signer
}

// The SetupSuite method will be run by testify once, at the very
// start of the testing suite, before any tests are run.
func (s *EVTestSuite) SetupSuite() {
	// start evLib
	s.evLib = NewEV()
	err := s.evLib.Start(nil, "127.0.0.1:22745")
	r := s.Require()
	r.Nil(err, "evLib failed to start")
	// setup signers
	s.n = 0
}

func (s *EVTestSuite) BeforeTest(_, _ string) {
	copy(s.admins1[:], s.getSigners(5))
	copy(s.admins2[:], s.getSigners(5))
	copy(s.voters[:], s.getSigners(15))
	copy(s.cands1[:], s.getSigners(5))
	copy(s.cands2[:], s.getSigners(5))
}

func TestEVTestSuite(t *testing.T) {
	suite.Run(t, new(EVTestSuite))
}

func (s *EVTestSuite) TestTransactionInitiation() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestInitLaunchTxn", func() {
			txID, err := s.evLib.LaunchPoll(s.admins1[0], "TestTransactionInitiation", blockchain.Rules{
				Admins:       [][]byte{s.admins1[0].PublicKey},
				VotesPerUser: 1,
				Options:      []string{s.cands1[0].Name, s.cands1[1].Name},
				BannedUsers:  nil,
				Duration:     0,
			})
			s.Nil(err)
			s.NotEmpty(txID)
		}},
		{"TestInitVoteTxn", func() {
			txID, err := s.evLib.CastVote(s.voters[0], "TestTransactionInitiation", []string{s.cands1[0].Name})
			s.Nil(err)
			s.NotEmpty(txID)
		}},
		{"TestInitTerminateTxn", func() {
			txID, err := s.evLib.TerminatePoll(s.admins1[0], "TestTransactionInitiation")
			s.Nil(err)
			s.NotEmpty(txID)
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *EVTestSuite) TestNormalOperation() {
	launchTxID, launchErr := s.evLib.LaunchPoll(s.admins1[0], "TestNormalOperation", blockchain.Rules{
		Admins:       [][]byte{s.admins1[0].PublicKey},
		VotesPerUser: 1,
		Options:      []string{s.cands1[0].Name, s.cands1[1].Name},
		BannedUsers:  nil,
		Duration:     0,
	})

	voteTxID, voteErr := s.evLib.CastVote(s.voters[0], "TestNormalOperation", []string{s.cands1[0].Name})

	terminateTxID, terminateErr := s.evLib.TerminatePoll(s.admins1[0], "TestNormalOperation")

	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestLaunchPoll", func() {
			s.Require().Nil(launchErr)

			status, err := s.evLib.CheckTxnStatus(launchTxID)
			s.Nil(err)
			s.Greater(status.Confirmed, -1)
			s.Equal(blockchain.TxnStatusSuccess, status.Receipt.Code)
		}},
		{"TestCastVote", func() {
			s.Require().Nil(launchErr)
			s.Require().Nil(voteErr)

			status, err := s.evLib.CheckTxnStatus(voteTxID)
			s.Nil(err)
			s.Greater(status.Confirmed, -1)
			s.Equal(blockchain.TxnStatusSuccess, status.Receipt.Code)
		}},
		{"TestTerminatePoll", func() {
			s.Require().Nil(launchErr)
			s.Require().Nil(terminateErr)

			status, err := s.evLib.CheckTxnStatus(terminateTxID)
			s.Nil(err)
			s.Greater(status.Confirmed, -1)
			s.Equal(blockchain.TxnStatusSuccess, status.Receipt.Code)
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *EVTestSuite) TestTransactionStatus() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestTransactionStatus", func() {
			launchTxID, launchErr := s.evLib.LaunchPoll(s.admins1[0], "TestTransactionStatus", blockchain.Rules{
				Admins:       [][]byte{s.admins1[0].PublicKey},
				VotesPerUser: 1,
				Options:      []string{s.cands1[0].Name, s.cands1[1].Name},
				BannedUsers:  nil,
				Duration:     0,
			})
			s.Require().Nil(launchErr)

			status, err := s.evLib.CheckTxnStatus(launchTxID)
			s.Nil(err)
			s.Greater(status.Confirmed, -1)
			s.Equal(blockchain.TxnStatusSuccess, status.Receipt.Code)
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *EVTestSuite) TestPollStatus() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestPollStatus", func() {
			launchTxID, launchErr := s.evLib.LaunchPoll(s.admins1[0], "TestPollStatus", blockchain.Rules{
				Admins:       [][]byte{s.admins1[0].PublicKey},
				VotesPerUser: 1,
				Options:      []string{s.cands1[0].Name, s.cands1[1].Name},
				BannedUsers:  nil,
				Duration:     0,
			})
			s.Require().Nil(launchErr)

			status, err := s.evLib.CheckTxnStatus(launchTxID)
			s.Require().Nil(err)
			s.Require().Equal(blockchain.TxnStatusSuccess, status.Receipt.Code)
			_, err = s.evLib.CheckPollStatus("TestPollStatus")
			s.Nil(err)
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

// getSigners does not begin with "Test", so it will not be run by
// testify as a test in the suite.  This is useful for creating helper
// methods for your tests.
func (s *EVTestSuite) getSigners(n uint) (signers []*Identity.Signer) {
	for i := uint(0); i < n; i++ {
		signers = append(signers, Identity.CreateSigner("user"+fmt.Sprintf("%04d", s.n), fmt.Sprintf("%04d", s.n)))
		s.n++
	}
	return
}
