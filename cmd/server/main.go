package main

import (
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/evlib"
	"cs.ubc.ca/cpsc416/BlockVote/util"
)

func main() {
	var config blockvote.ServerConfig
	err := util.ReadJSONConfig("config/server_config.json", &config)
	util.CheckErr(err, "Error reading server config: %v\n", err)

	r := evlib.SetupHTTPServer("config/client_config.json")
	err = r.Run(config.ServerHTTPIpPort)
	util.CheckErr(err, "Error setting up HTTP server: %v\n", err)
}
