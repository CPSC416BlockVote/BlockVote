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
	flag.StringVar(&config.MinerId, "id", config.MinerId, "miner ID")
	flag.StringVar(&config.MinerAddr, "addr", config.MinerAddr, "miner IP:Port")
	flag.Parse()

	// redirect output to file
	if len(os.Args) > 1 {
		f, err := os.Create("./logs/" + config.MinerId + ".txt")
		if err != nil {
			log.Fatalf("error opening file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}
	server := blockvote.NewMiner()
	server.Start(config.MinerId, config.CoordAddr, config.MinerAddr, config.Difficulty, config.MaxTxn, nil)
}
