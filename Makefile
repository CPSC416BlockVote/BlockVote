.PHONY: client tracing clean all

all: miner coord client tracing

miner:
	go build -o bin/miner ./cmd/miner

coord:
	go build -o bin/coord ./cmd/coord

client:
	go build -o bin/client ./cmd/client

tracing:
	go build -o bin/tracing ./cmd/tracing-server

clean:
	rm -f bin/*
