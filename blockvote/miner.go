package blockvote

import (
	"errors"
	"github.com/DistributedClocks/tracing"
)

type MinerConfig struct {
	MinerId           uint8
	CoordAddr         string
	MinerAddr         string
	TracingServerAddr string
	Difficulty        uint8
	Secret            []byte
	TracingIdentity   string
}

type Miner struct {
	// Miner state may go here

}

func NewMiner() *Miner {
	return &Miner{}
}

func (m *Miner) Start(minerId uint8, coordAddr string, minerAddr string, difficulty uint8, mtrace *tracing.Tracer) error {
	return errors.New("not implemented")
}
