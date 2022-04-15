package main

import (
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/evlib"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"github.com/DistributedClocks/tracing"
)

func main() {
	var config blockvote.ClientConfig
	err := util.ReadJSONConfig("config/client_config.json", &config)
	util.CheckErr(err, "Error reading client config: %v\n", err)

	tracer := tracing.NewTracer(tracing.TracerConfig{
		ServerAddress:  config.TracingServerAddr,
		TracerIdentity: config.TracingIdentity,
		Secret:         config.Secret,
	})

	client := evlib.NewEV()
	err = client.Start(tracer, config.ClientID, config.CoordIPPort, config.LocalCoordIPPort, config.LocalMinerIPPort, config.N_Receives)
	util.CheckErr(err, "Error reading client config: %v\n", err)

	// Add client operations here

	//client.Stop()
}
