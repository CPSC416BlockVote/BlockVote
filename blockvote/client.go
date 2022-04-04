package blockvote

type ClientConfig struct {
	ClientID          string
	CoordIPPort       string
	LocalCoordIPPort  string
	LocalMinerIPPort  string
	TracingServerAddr string
	N_Receives        int
	Secret            []byte
	TracingIdentity   string
}
