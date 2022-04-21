package main

import (
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	var config blockvote.CoordConfig
	util.ReadJSONConfig("config/coord_config.json", &config)
	//ctracer := tracing.NewTracer(tracing.TracerConfig{
	//	ServerAddress:  config.TracingServerAddr,
	//	TracerIdentity: config.TracingIdentity,
	//	Secret:         config.Secret,
	//})
	var restart bool
	var thetis bool
	flag.BoolVar(&restart, "r", false, "whether to restart coord")
	flag.BoolVar(&thetis, "thetis", false, "run coord on thetis server")
	flag.Parse()
	if !restart {
		if _, err := os.Stat("./storage/coord"); err == nil {
			os.RemoveAll("./storage/coord")
		}
	}
	if thetis {
		config.MinerAPIListenAddr = "thetis.students.cs.ubc.ca" +
			config.MinerAPIListenAddr[strings.Index(config.MinerAPIListenAddr, ":"):]
		config.ClientAPIListenAddr = "thetis.students.cs.ubc.ca" +
			config.ClientAPIListenAddr[strings.Index(config.ClientAPIListenAddr, ":"):]
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
