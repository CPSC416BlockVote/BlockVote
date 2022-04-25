package main

import (
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"flag"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var config blockvote.CoordConfig
	util.ReadJSONConfig("config/coord_config.json", &config)
	var restart bool
	flag.BoolVar(&restart, "r", false, "whether to restart coord")
	flag.Parse()
	if !restart {
		if _, err := os.Stat("./storage/coord"); err == nil {
			os.RemoveAll("./storage/coord")
		}
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	coord := blockvote.NewCoord()
	go func() {
		<-sigs
		coord.PrintChain()
		os.Exit(0)
	}()
	coord.Start(config.ClientAPIListenAddr, config.MinerAPIListenAddr, config.NCandidates, nil)
}
