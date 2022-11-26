package blockchain

import (
	"github.com/stretchr/testify/suite"
	"testing"
)

type MemPoolTestSuite struct {
	suite.Suite
	mockTxns [200]Transaction
	pool     *MemoryPool
	capacity uint
}

func TestMemPoolTestSuite(t *testing.T) {
	suite.Run(t, new(MemPoolTestSuite))
}

func (s *MemPoolTestSuite) SetupSuite() {
	s.capacity = 50
	for i := 0; i < len(s.mockTxns); i++ {
		s.mockTxns[i] = Transaction{
			ID: []byte(string(rune(i))),
		}
	}
}

func (s *MemPoolTestSuite) SetupTest() {
	s.pool = NewMemoryPool(s.capacity)
}

func (s *MemPoolTestSuite) TestNewMemoryPool() {
	var capacity uint = 100
	pool := NewMemoryPool(capacity)
	s.Equal(capacity, pool.capacity)
	s.Empty(pool.transactions)
}

func (s *MemPoolTestSuite) TestSize() {
	s.Zero(s.pool.Size())
	s.pool.AddTxns(s.mockTxns[:5])
	s.Equal(5, s.pool.Size())

	for i := 0; i < 3; i++ {
		s.pool.RemoveTxns([]*Transaction{&s.mockTxns[i]})
	}
	s.Equal(2, s.pool.Size())
}

func (s *MemPoolTestSuite) TestGet() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"should return nil if the pool is empty", func() {
			s.Nil(s.pool.Get(1))
		}},
		{"should return all transactions if the size requested exceeds pool size", func() {
			s.pool.AddTxns(s.mockTxns[:s.capacity])
			getTxns := s.pool.Get(uint(s.pool.Size() + 1))
			s.Equal(s.pool.Size(), len(getTxns))
			for i := 0; i < s.pool.Size(); i++ {
				s.Equal(s.mockTxns[i], getTxns[i])
			}
		}},
		{"should return a copy of oldest transactions of given size", func() {
			s.pool.AddTxns(s.mockTxns[:s.capacity])
			getTxns := s.pool.Get(uint(s.pool.Size() - 1))
			s.Equal(s.pool.Size()-1, len(getTxns))
			for i := 0; i < len(getTxns); i++ {
				s.Equal(s.mockTxns[i], getTxns[i])
			}
			s.NotSame(&getTxns[0], &(s.pool.Get(uint(s.pool.Size() - 1))[0])) // check it is a copy
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *MemPoolTestSuite) TestAll() {
	s.Empty(s.pool.All())

	s.pool.AddTxns(s.mockTxns[:s.capacity])
	allTxns := s.pool.All()
	s.Equal(s.pool.Size(), len(allTxns))
	for i := 0; i < len(allTxns); i++ {
		s.Equal(s.mockTxns[i].ID, allTxns[i].ID)
	}
	s.NotSame(&allTxns[0], &(s.pool.All()[0]))
}

func (s *MemPoolTestSuite) TestAddTxns() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"should append new transactions to the end of pool", func() {
			s.pool.AddTxns(s.mockTxns[:5])
			s.Equal(5, s.pool.Size())
			s.pool.AddTxns(s.mockTxns[5:10])
			s.Equal(10, s.pool.Size())

			txns := s.pool.All()
			for i := 0; i < 10; i++ {
				s.Equal(s.mockTxns[i], txns[i])
			}
		}},
		{"should remove oldest transactions if adding new transactions exceeds capacity", func() {
			s.pool.AddTxns(s.mockTxns[:s.capacity])
			s.pool.AddTxns(s.mockTxns[s.capacity : s.capacity+s.capacity/2])
			s.Equal(int(s.capacity), s.pool.Size())
			txns := s.pool.All()
			for i := 0; i < int(s.capacity/2); i++ {
				s.Equal(s.mockTxns[int(s.capacity)/2+i], txns[i])
				s.Equal(s.mockTxns[int(s.capacity)+i], txns[int(s.capacity)/2+i])
			}

			s.pool.AddTxns(s.mockTxns[s.capacity+s.capacity/2 : s.capacity*4])
			s.Equal(int(s.capacity), s.pool.Size())
			txns = s.pool.All()
			for i := 0; i < int(s.capacity); i++ {
				s.Equal(s.mockTxns[int(s.capacity)*3+i], txns[i])
			}
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *MemPoolTestSuite) TestAddTxn() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"should append new transaction to the end of pool", func() {
			s.pool.AddTxn(s.mockTxns[0])
			s.Equal(1, s.pool.Size())
			s.pool.AddTxn(s.mockTxns[1])
			s.Equal(2, s.pool.Size())

			txns := s.pool.All()
			for i := 0; i < 2; i++ {
				s.Equal(s.mockTxns[i], txns[i])
			}
		}},
		{"should remove oldest transaction if adding new transaction exceeds capacity", func() {
			s.pool.AddTxns(s.mockTxns[:s.capacity])
			s.pool.AddTxn(s.mockTxns[s.capacity])
			s.Equal(int(s.capacity), s.pool.Size())
			txns := s.pool.All()
			for i := 0; i < int(s.capacity)-1; i++ {
				s.Equal(s.mockTxns[1+i], txns[i])
			}
			s.Equal(s.mockTxns[s.capacity], txns[s.capacity-1])
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *MemPoolTestSuite) TestRemoveTxns() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"should remove given transactions", func() {
			s.pool.AddTxns(s.mockTxns[:5])
			toBeRemoved := []*Transaction{&s.mockTxns[0], &s.mockTxns[2], &s.mockTxns[4]}
			s.pool.RemoveTxns(toBeRemoved)
			s.Equal(2, s.pool.Size())
			txns := s.pool.All()
			s.Equal(s.mockTxns[1], txns[0])
			s.Equal(s.mockTxns[3], txns[1])
		}},
		{"should ignore non-existent transactions", func() {
			toBeRemoved := []*Transaction{&s.mockTxns[1], &s.mockTxns[2], &s.mockTxns[4], &s.mockTxns[6]}
			s.pool.RemoveTxns(toBeRemoved)
			s.Equal(1, s.pool.Size())
			txns := s.pool.All()
			s.Equal(s.mockTxns[3], txns[0])
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *MemPoolTestSuite) RemoveTxnsByTxID() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"should remove transactions with given ID", func() {
			s.pool.AddTxns(s.mockTxns[:5])
			toBeRemoved := [][]byte{s.mockTxns[0].ID, s.mockTxns[2].ID, s.mockTxns[4].ID}
			s.pool.RemoveTxnsByTxID(toBeRemoved)
			s.Equal(2, s.pool.Size())
			txns := s.pool.All()
			s.Equal(s.mockTxns[1], txns[0])
			s.Equal(s.mockTxns[3], txns[1])
		}},
		{"should ignore non-existent transactions", func() {
			toBeRemoved := [][]byte{s.mockTxns[1].ID, s.mockTxns[2].ID, s.mockTxns[4].ID, s.mockTxns[6].ID}
			s.pool.RemoveTxnsByTxID(toBeRemoved)
			s.Equal(1, s.pool.Size())
			txns := s.pool.All()
			s.Equal(s.mockTxns[3], txns[0])
		}},
	} {
		s.Run(t.Name, t.F)
	}
}
