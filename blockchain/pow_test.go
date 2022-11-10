package blockchain

import (
	"github.com/stretchr/testify/suite"
	"math"
	"math/big"
	"testing"
	"time"
)

type PoWTestSuite struct {
	suite.Suite
	block       Block
	difficulty1 float64
	difficulty2 float64
}

func TestPoWTestSuite(t *testing.T) {
	suite.Run(t, new(PoWTestSuite))
}

func (s *PoWTestSuite) SetupTest() {
	s.block = Block{
		BlockNum:   1,
		Difficulty: 0,
		Hash:       nil,
		MinerID:    "test",
		Nonce:      0,
		PrevHash:   nil,
		Timestamp:  time.Now().Unix(),
		Txns:       nil,
	}
	s.difficulty1 = math.Pow(2, 8)
	s.difficulty2 = math.Pow(2, 16)
}

func (s *PoWTestSuite) TestNewProof() {
	var pow *ProofOfWork
	var expectedTarget *big.Int

	s.block.Difficulty = s.difficulty1
	pow = NewProof(&s.block)
	expectedTarget = big.NewInt(1)
	expectedTarget.Lsh(expectedTarget, uint(256-8))
	s.Equal(expectedTarget, pow.Target)

	s.block.Difficulty = s.difficulty2
	pow = NewProof(&s.block)
	expectedTarget = big.NewInt(1)
	expectedTarget.Lsh(expectedTarget, uint(256-16))
	s.Equal(expectedTarget, pow.Target)
}

func (s *PoWTestSuite) TestRun() {
	var pow *ProofOfWork
	var expectedTarget *big.Int
	var actual big.Int

	s.block.Difficulty = s.difficulty1
	pow = NewProof(&s.block)
	pow.Run()

	expectedTarget = big.NewInt(1)
	expectedTarget.Lsh(expectedTarget, uint(256-8))
	actual.SetBytes(s.block.Hash[:])
	s.Equal(-1, actual.Cmp(expectedTarget))

	s.block.Difficulty = s.difficulty2
	pow = NewProof(&s.block)
	pow.Run()

	expectedTarget = big.NewInt(1)
	expectedTarget.Lsh(expectedTarget, uint(256-16))
	actual.SetBytes(s.block.Hash[:])
	s.Equal(-1, actual.Cmp(expectedTarget))
}

func (s *PoWTestSuite) TestValidate() {
	var pow *ProofOfWork

	s.block.Difficulty = s.difficulty1
	pow = NewProof(&s.block)
	pow.Run()

	s.True(pow.Validate())

	s.block.Difficulty /= 2
	pow = NewProof(&s.block)
	s.True(pow.Validate())

	s.block.Difficulty = math.Pow(2, 255)
	pow = NewProof(&s.block)
	s.False(pow.Validate())
}
