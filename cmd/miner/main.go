package main

import (
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"flag"
	"log"
	"os"
)

func main() {
	var config blockvote.MinerConfig
	util.ReadJSONConfig("config/miner_config.json", &config)

	// parse args
	var restart bool
	flag.BoolVar(&restart, "r", false, "whether to restart miner")
	flag.StringVar(&config.MinerId, "id", config.MinerId, "miner ID")
	flag.StringVar(&config.MinerAddr, "addr", config.MinerAddr, "miner IP:Port")
	flag.Parse()

	// delete storage for clean start
	if !restart {
		if _, err := os.Stat("./storage/" + config.MinerId); err == nil {
			os.RemoveAll("./storage/" + config.MinerId)
		}
	}

	// redirect output to file
	if len(os.Args) > 1 {
		f, err := os.Create("./logs/" + config.MinerId + ".txt")
		if err != nil {
			log.Fatalf("error opening file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	miner := blockvote.NewMiner()
	miner.Start(config.MinerId, config.CoordAddr, config.MinerAddr, config.MaxTxn)
}
