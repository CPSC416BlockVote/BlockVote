.PHONY: client tracing clean all

all: miner miner2 coord client tracing

miner:
	go build -o bin/miner ./cmd/miner

miner2:
	go build -o bin/miner2 ./cmd/miner2

coord:
	go build -o bin/coord ./cmd/coord

client:
	go build -o bin/client ./cmd/client

tracing:
	go build -o bin/tracing ./cmd/tracing-server

clean:
	rm -f bin/*
