package main

import (
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"github.com/DistributedClocks/tracing"
)

func main() {
	var config blockvote.MinerConfig
	util.ReadJSONConfig("config/miner_config.json", &config)
	mtracer := tracing.NewTracer(tracing.TracerConfig{
		ServerAddress:  config.TracingServerAddr,
		TracerIdentity: config.TracingIdentity,
		Secret:         config.Secret,
	})
	server := blockvote.NewMiner()
	server.Start(config.MinerId, config.CoordAddr, config.MinerAddr, config.Difficulty, config.MaxTxn, mtracer)
}
