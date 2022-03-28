package evlib

import (
	"errors"
	"github.com/DistributedClocks/tracing"
)

type EV struct {
	// Add EV instance state here.
}

func NewEV() *EV {
	return &EV{}
}

// Start Starts the instance of EV to use for connecting to the system with the given coord's IP:port.
func (d *EV) Start(localTracer *tracing.Tracer, clientId string, coordIPPort string) error {
	return errors.New("not implemented")
}

// Stop Stops the EV instance.
// This call always succeeds.
func (d *EV) Stop() {
	return
}
