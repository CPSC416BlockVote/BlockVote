package blockchain

import (
	"sync"
)

type MemoryPool struct {
	rwMu         sync.RWMutex
	transactions []Transaction
	capacity     uint
}

func NewMemoryPool(capacity uint) *MemoryPool {
	return &MemoryPool{
		capacity: capacity,
	}
}

func (p *MemoryPool) Size() int {
	p.rwMu.RLock()
	defer p.rwMu.RUnlock()
	return len(p.transactions)
}

//Get returns a copy of given number of the oldest transactions
func (p *MemoryPool) Get(size uint) []Transaction {
	p.rwMu.RLock()
	defer p.rwMu.RUnlock()

	if len(p.transactions) == 0 {
		return nil
	}

	if size > uint(len(p.transactions)) {
		size = uint(len(p.transactions))
	}

	txns := make([]Transaction, size)
	copy(txns[:], p.transactions[:size])
	return txns
}

//All returns a copy of all transactions
func (p *MemoryPool) All() []Transaction {
	p.rwMu.RLock()
	defer p.rwMu.RUnlock()

	txns := make([]Transaction, len(p.transactions))
	copy(txns[:], p.transactions)
	return txns
}

//AddTxns pushes new transactions into the back the memory pool
func (p *MemoryPool) AddTxns(incoming []Transaction) {
	p.rwMu.Lock()
	defer p.rwMu.Unlock()

	// check capacity and remove the oldest transactions if necessary
	if uint(len(incoming)+len(p.transactions)) > p.capacity {
		if uint(len(incoming)) < p.capacity {
			p.transactions = p.transactions[uint(len(incoming)+len(p.transactions))-p.capacity:]
		} else {
			p.transactions = nil
			incoming = incoming[uint(len(incoming))-p.capacity:]
		}
	}

	p.transactions = append(p.transactions, incoming...)
}

//AddTxn pushes a new transaction into the back the memory pool
func (p *MemoryPool) AddTxn(incoming Transaction) {
	p.rwMu.Lock()
	defer p.rwMu.Unlock()

	if uint(len(p.transactions))+1 > p.capacity {
		p.transactions = p.transactions[1:]
	}

	p.transactions = append(p.transactions, incoming)
}

//RemoveTxns removes given transactions from the memory pool
func (p *MemoryPool) RemoveTxns(txns []*Transaction) {
	pendingRemove := make(map[string]bool)
	for _, txn := range txns {
		pendingRemove[string(txn.ID)] = true
	}

	p.rwMu.Lock()
	defer p.rwMu.Unlock()
	p.remove(pendingRemove)
}

//RemoveTxnsByTxID removes given transactions from the memory pool
func (p *MemoryPool) RemoveTxnsByTxID(txIDs [][]byte) {
	pendingRemove := make(map[string]bool)
	for _, TxID := range txIDs {
		pendingRemove[string(TxID)] = true
	}

	p.rwMu.Lock()
	defer p.rwMu.Unlock()
	p.remove(pendingRemove)
}

func (p *MemoryPool) remove(pendingRemove map[string]bool) {
	for i := 0; i < len(p.transactions); {
		if pendingRemove[string(p.transactions[i].ID)] {
			p.transactions = append(p.transactions[:i], p.transactions[i+1:]...)
		} else {
			i++
		}
	}
}
