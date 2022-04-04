package main

import (
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"github.com/DistributedClocks/tracing"
)

func main() {
	var config blockvote.CoordConfig
	util.ReadJSONConfig("config/coord_config.json", &config)
	ctracer := tracing.NewTracer(tracing.TracerConfig{
		ServerAddress:  config.TracingServerAddr,
		TracerIdentity: config.TracingIdentity,
		Secret:         config.Secret,
	})
	coord := blockvote.NewCoord()
	coord.Start(config.ClientAPIListenAddr, config.MinerAPIListenAddr, config.NCandidates, ctracer)
}
