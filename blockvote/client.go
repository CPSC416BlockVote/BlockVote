package blockvote

type ClientConfig struct {
	ClientID          uint
	CoordIPPort       string
	TracingServerAddr string
	N_Receives        int
	Secret            []byte
	TracingIdentity   string
}
