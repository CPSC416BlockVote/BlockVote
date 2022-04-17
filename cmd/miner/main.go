package main

import (
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"flag"
	"github.com/DistributedClocks/tracing"
)

func main() {
	var config blockvote.MinerConfig
	util.ReadJSONConfig("config/miner_config.json", &config)
	flag.StringVar(&config.MinerId, "id", config.MinerId, "miner[num]")
	flag.StringVar(&config.MinerAddr, "addr", config.MinerAddr, "miner[num]")
	flag.Parse()
	mtracer := tracing.NewTracer(tracing.TracerConfig{
		ServerAddress:  config.TracingServerAddr,
		TracerIdentity: config.TracingIdentity,
		Secret:         config.Secret,
	})
	server := blockvote.NewMiner()
	server.Start(config.MinerId, config.CoordAddr, config.MinerAddr, config.Difficulty, config.MaxTxn, mtracer)
}
