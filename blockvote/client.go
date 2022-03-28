package blockvote

type ClientConfig struct {
	ClientID          string
	CoordIPPort       string
	TracingServerAddr string
	Secret            []byte
	TracingIdentity   string
}
