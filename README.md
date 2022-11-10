# BlockVote - A Blockchain-Based Digital Voting System

UBC CPSC 416 2021w2 Project

Group members: Zhongze Chen, Eric Lyu, Mingyang Ye, Xinyi Ye, Yitong Zhao

## Improvements

- [ ] Decentralize coord 
    - [x] Simplify coord's functionalities
    - [ ] Distribute coord
- [ ] Improve gossip protocol
    - [x] Allow cycle to be triggered by both intervals and new updates
    - [x] Allow nodes to subscribe to specific types of updates
    - [ ] Truncate log and local cache
- [x] Allow miner rejoin
- [x] Support multiple elections
- [x] Support dynamic difficulties

## Usage

### Coord

1. Start coord (clean start):

    `go run cmd/coord/main.go`

2. Restart coord:

    `go run cmd/coord/main.go -r true`

To interrupt coord, use `Ctrl + C`. A `txns.txt` file and a `votes.txt` file will be generated upon keyboard interrupt.

### Miner

1. Start a single miner using terminal:

    `go run cmd/miner/main.go`

2. Start and kill multiple miners using the Python script:

    `python scripts/miner.py -n [number of initial miners]`

    Follow instructions printed by the script to start more miners or kill existing miners.
    
    To see miner outputs, go to `logs` folder and look for `miner[x].txt`
    
### Client

1. Start a single client using terminal:

    `go run cmd/client/main.go`

    Three `.txt` files will be generated after the client sending all transactions to miners.

2. Start multiple clients using the Python script:

   `python scripts/client.py -n [number of clients]`

   To see client outputs, go to `logs` folder and look for `client[x].txt`

## Testing

### Criteria

1. All valid transactions are committed and appear exactly once
2. All invalid transactions will not be committed
3. Each group of conflicting transactions can only have exactly one committed
4. Vote counts are correct

### Checker script

To use the checker script, you will need `txns.txt` and `votes.txt` files generated by coord
and `client[x]valid.txt`, `client[x]invalid.txt`, `client[x]conflict.txt` files generated by each client.
Then run command:
    `python scripts/checker.py -n [number of clients]`

### Instructions

To test the system, you can start coord, miners, clients in any order.
Please make sure that at least 4 blocks with no transaction are received by coord
after the last block that has transaction. 
Then you can safely terminate everything and run the checker script.
