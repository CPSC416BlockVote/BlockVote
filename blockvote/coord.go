package blockvote

import (
	"errors"
	"github.com/DistributedClocks/tracing"
)

type CoordConfig struct {
	ClientAPIListenAddr string
	MinerAPIListenAddr  string
	TracingServerAddr   string
	Secret              []byte
	TracingIdentity     string
}

type Coord struct {
	// Coord state may go here
}

func NewCoord() *Coord {
	return &Coord{}
}

func (c *Coord) Start(clientAPIListenAddr string, minerAPIListenAddr string, ctrace *tracing.Tracer) error {
	return errors.New("not implemented")
}
