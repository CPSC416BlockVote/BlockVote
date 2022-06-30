package blockchain

import (
	"bytes"
	"fmt"
	"log"
)

const (
	TxnStatusInvalidPayloadMethod = 1
	TxnStatusInvalidPollID        = 2
	TxnStatusPollExpired          = 3
	TxnStatusAdminNotSet          = 10
	TxnStatusInvalidVotesPerUser  = 11
	TxnStatusNotEnoughOptions     = 12
	TxnStatusPollIDExist          = 13
	TxnStatusBannedUser           = 20
	TxnStatusInvalidOptions       = 21
	TxnStatusNoVotes              = 22
	TxnStatusPollAlreadyEnded     = 30
	TxnStatusUnauthorizedUser     = 31
)

const (
	PayloadMethodVote      = "Vote"
	PayLoadMethodLaunch    = "Launch"
	PayloadMethodTerminate = "Terminate"
)

type Payload struct {
	Method   string
	UserName string
	UserID   string
	PollID   string      // uniquely identify poll. launching a poll with an existing poll id will be rejected.
	Extra    interface{} // for method "Vote", extra is option; for method "Launch", extra is rules
}

type PollMeta struct {
	PollID          string
	InitiatorName   string
	InitiatorID     string
	InitiatorPubKey []byte
	StartBlock      uint
	EndBlock        uint
	Rules           Rules
	VoteCounts      []uint
	TotalVotes      uint
}

type Rules struct {
	Admins       [][]byte // admins of the poll (public keys). admins can adjust rules or end poll.
	VotesPerUser uint     // number of votes allowed for each user
	Options      []string // options available for the polls
	BannedUsers  [][]byte // users banned for this poll (public keys).
	Duration     uint     // block num starting which the poll will become expired. set to 0 to disable it.
}

func (r *Rules) Expired(numBlocks uint) bool {
	return r.Duration > 0 && numBlocks > r.Duration
}

func (r *Rules) CanVote(voter []byte) bool {
	for _, pubKey := range r.BannedUsers {
		if bytes.Compare(pubKey, voter) == 0 {
			return false
		}
	}
	return true
}

func (r *Rules) IsAdmin(user []byte) bool {
	for _, pubKey := range r.Admins {
		if bytes.Compare(pubKey, user) == 0 {
			return true
		}
	}
	return false
}

func (r *Rules) ValidOptions(option []string) bool {
	// check length
	if len(option) == 0 || uint(len(option)) > r.VotesPerUser {
		return false
	}

	// check duplicates
	selected := make(map[string]bool)
	for _, op := range option {
		if selected[op] {
			return false
		}
		selected[op] = true
	}

	// check validity
	for _, op := range r.Options {
		if selected[op] {
			selected[op] = false
		}
	}
	for _, v := range selected {
		if v {
			return false
		}
	}
	return true
}

func (p *Payload) Validate(parent *Transaction, handler *QueryHandler) (status int) {
	if p.Method == PayLoadMethodLaunch {
		return p.ValidateLaunch(parent, handler)
	} else if p.Method == PayloadMethodVote {
		return p.ValidateVote(parent, handler)
	} else if p.Method == PayloadMethodTerminate {
		return p.ValidateTerminate(parent, handler)
	} else {
		return TxnStatusInvalidPayloadMethod
	}
}

func (p *Payload) ValidateLaunch(parent *Transaction, handler *QueryHandler) (status int) {
	// 1. sanity check
	rules := p.Extra.(Rules)
	// 1.1 admins cannot be empty
	if len(rules.Admins) == 0 && rules.Duration <= 0 {
		return TxnStatusAdminNotSet
	}
	// 1.2 votes per user should be positive
	if rules.VotesPerUser <= 0 {
		return TxnStatusInvalidVotesPerUser
	}
	// 1.3 number of options should be at least two
	if len(rules.Options) < 2 {
		return TxnStatusNotEnoughOptions
	}

	// 2. check PollID availability
	// just need to check if any of the transaction has the same poll id
	if handler.Count(func(txn *Transaction) bool {
		return txn.Data.PollID == p.PollID
	}) > 0 {
		return TxnStatusPollIDExist
	}

	return TxnStatusSuccess
}

func (p *Payload) ValidateVote(parent *Transaction, handler *QueryHandler) (status int) {
	// 1. check poll existence and available
	txns, bkNums := handler.FetchWithBlockNum(func(txn *Transaction) bool {
		return txn.Data.PollID == p.PollID &&
			(txn.Data.Method == PayLoadMethodLaunch || txn.Data.Method == PayloadMethodTerminate)
	})
	if len(txns) == 0 {
		// launch txn not found
		return TxnStatusInvalidPollID
	} else if len(txns) != 1 {
		// terminate txn found
		return TxnStatusPollExpired
	}

	// 2. check if voted
	if handler.Count(func(txn *Transaction) bool {
		return bytes.Compare(txn.PublicKey, parent.PublicKey) == 0
	}) > 0 {
		return TxnStatusNoVotes
	}

	// 2. check rule
	rules := txns[0].Data.Extra.(Rules)
	if rules.Expired(handler.NextBlockNum() - bkNums[0]) {
		return TxnStatusPollExpired
	}
	if !rules.CanVote(parent.PublicKey) {
		return TxnStatusBannedUser
	}
	if !rules.ValidOptions(p.Extra.([]string)) {
		return TxnStatusInvalidOptions
	}

	return TxnStatusSuccess
}

func (p *Payload) ValidateTerminate(parent *Transaction, handler *QueryHandler) (status int) {
	// 1. check poll existence and validity
	txns, bkNums := handler.FetchWithBlockNum(func(txn *Transaction) bool {
		return txn.Data.PollID == p.PollID &&
			(txn.Data.Method == PayLoadMethodLaunch || txn.Data.Method == PayloadMethodTerminate)
	})
	if len(txns) == 0 {
		// launch txn not found
		return TxnStatusInvalidPollID
	} else if len(txns) != 1 {
		// terminate txn found
		return TxnStatusPollAlreadyEnded
	}

	// 2. check rule
	rules := txns[0].Data.Extra.(Rules)
	if rules.Expired(handler.NextBlockNum() - bkNums[0]) {
		return TxnStatusPollAlreadyEnded
	}
	if !rules.IsAdmin(parent.PublicKey) {
		return TxnStatusUnauthorizedUser
	}

	return TxnStatusSuccess
}

func (p *Payload) ToString() string {
	str := fmt.Sprintf("[%s:%s]\t%s(%s)\t -> ", p.Method, p.PollID, p.UserName, p.UserID)
	if p.Method == PayloadMethodVote {
		str += fmt.Sprintf("%v", p.Extra)
	} else if p.Method == PayLoadMethodLaunch {
		rules := p.Extra.(Rules)
		expiryStr := "expired time not set"
		if rules.Duration > 0 {
			expiryStr = fmt.Sprintf("expired at bk %d", rules.Duration)
		}
		str += fmt.Sprintf("{%d votes per user, %d options, %s}", rules.VotesPerUser, len(rules.Options), expiryStr)
	} else if p.Method == PayloadMethodTerminate {
		str += "|"
	}
	return str
}

func PrintPayload(payload *Payload) {
	log.Printf("[%s] From %s(%s) to poll %s with %v\n", payload.Method, payload.UserName, payload.UserID, payload.PollID, payload.Extra)
}
