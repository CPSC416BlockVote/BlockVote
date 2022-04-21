package main

import (
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"flag"
	"log"
	"os"
	"strings"
)

func main() {
	var config blockvote.MinerConfig
	util.ReadJSONConfig("config/miner_config.json", &config)

	// parse args
	var thetis bool
	var anvil bool
	var remote bool
	flag.StringVar(&config.MinerId, "id", config.MinerId, "miner[num]")
	flag.StringVar(&config.MinerAddr, "addr", config.MinerAddr, "miner[num]")
	flag.BoolVar(&thetis, "thetis", false, "run miner on thetis server")
	flag.BoolVar(&anvil, "anvil", false, "run miner on anvil server")
	flag.BoolVar(&remote, "remote", false, "run miner on remote server")
	flag.Parse()

	var ip string
	if thetis {
		ip = "thetis.students.cs.ubc.ca"
	} else if anvil {
		ip = "anvil.students.cs.ubc.ca"
	} else if remote {
		ip = "remote.students.cs.ubc.ca"
	}
	if thetis || anvil || remote {
		config.CoordAddr = "thetis.students.cs.ubc.ca" + config.CoordAddr[strings.Index(config.CoordAddr, ":"):]
		config.MinerAddr = ip + config.MinerAddr[strings.Index(config.MinerAddr, ":"):]
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

	//mtracer := tracing.NewTracer(tracing.TracerConfig{
	//	ServerAddress:  config.TracingServerAddr,
	//	TracerIdentity: config.TracingIdentity,
	//	Secret:         config.Secret,
	//})
	server := blockvote.NewMiner()
	server.Start(config.MinerId, config.CoordAddr, config.MinerAddr, config.Difficulty, config.MaxTxn, nil)
}
