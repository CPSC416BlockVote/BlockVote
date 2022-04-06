package gossip

import (
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"errors"
	"log"
	"math/rand"
	"net/rpc"
	"sync"
	"time"
)

type Update struct {
	ID   string
	Data []byte
}

// messages

type (
	PushArgs struct { // Note: Update needs to be the last of UpdateLog
		Identity  string
		Update    Update
		UpdateLog []string
	}
	PushReply struct {
		MissingUpdates []string
	}
	PushPullArgs struct { // Note: Update needs to be the last of UpdateLog
		Identity  string
		Update    Update
		UpdateLog []string
	}
	PushPullReply struct {
		Updates        []Update
		MissingUpdates []string
	}
	PullArgs struct {
		Identity  string
		UpdateLog []string
	}
	PullReply struct {
		Updates []Update
	}
	RetransmitArgs struct {
		Identity string
		Updates  []Update
	}
	RetransmitReply struct {
	}
)

type PendingPush struct { // this struct ensures update to be the last of update log
	Update    Update
	UpdateLog []string
}

var (
	verbose         bool
	running         bool
	mode            string // operating mode of gossip protocol. ["Push", "PushPull", "Pull"]
	identity        string // gossip client identifier.
	localListenAddr string

	QueryChan  chan<- Update // for gossip client to query updates
	UpdateChan <-chan Update // for gossip client to put updates

	PendingPushQueue chan PendingPush // pending updates (from the client or peers) that need to be pushed

	mu        sync.Mutex
	UpdateMap map[string]Update // stores every update
	UpdateLog []string          // update id history
	FanOut    uint8             // number of connections
	PeerList  []string          // peer addresses

	ExitSignal chan int
)

type RPCHandler struct {
}

func Start(fanOut uint8, // number of connections
	operatingMode string, // operating mode of gossip protocol. ["Push", "PushPull", "Pull"]
	localIp string,
	//peers []string, // peer addresses
	initialUpdates []Update, // all updates client has to date
	clientIdentity string, // gossip client identifier
	logging bool, // whether to print
) (queryChan <-chan Update, updateChan chan<- Update, localAddr string, err error) {
	if running {
		return nil, nil, "", errors.New("[ERROR] gossip service already running")
	}
	running = true
	mode = operatingMode
	identity = clientIdentity
	verbose = logging

	qCh := make(chan Update, 50)
	uCh := make(chan Update, 50)

	QueryChan = qCh
	UpdateChan = uCh
	PendingPushQueue = make(chan PendingPush, 100)
	UpdateMap = make(map[string]Update)
	UpdateLog = []string{}
	FanOut = fanOut
	ExitSignal = make(chan int, 2)

	// unpack initial updates
	for _, update := range initialUpdates {
		UpdateMap[update.ID] = update
		UpdateLog = append(UpdateLog, update.ID)
	}

	handler := new(RPCHandler)
	localListenAddr, err = util.NewRPCServerWithIp(handler, localIp)
	if err != nil {
		return nil, nil, "", err
	}
	Verbose("listen to gossips at " + localListenAddr)
	//SetPeers(peers) // set peers should be called only after local address is assigned

	go DigestLocalUpdateService()

	if operatingMode == "Push" {
		go PushService()
	} else if operatingMode == "PushPull" {
		go PushService()
	} else if operatingMode == "Pull" {
		go PullService()
	} else {
		return nil, nil, "", errors.New("[Error] unexpected gossip mode")
	}

	return qCh, uCh, localListenAddr, nil
}

func SetPeers(peers []string) {
	// find self
	i := 0
	for ; i < len(peers); i++ {
		if peers[i] == localListenAddr {
			break
		}
	}
	// exclude self
	if i < len(peers) {
		PeerList = append(peers[:i], peers[i+1:]...)
	}
}

func NewUpdate(prefix string, hash []byte, data []byte) Update {
	return Update{
		ID:   prefix + string(hash),
		Data: data,
	}
}

func (handler *RPCHandler) Push(args PushArgs, reply *PushReply) error {
	// check missing updates
	var missing []string
	for _, id := range args.UpdateLog {
		if len(UpdateMap[id].ID) == 0 && id != args.Update.ID {
			// never see this update, and update is not the latest one
			missing = append(missing, id)
		}
	}

	// only accept the update if no earlier updates are missing
	if len(missing) == 0 {
		mu.Lock()
		if len(UpdateMap[args.Update.ID].ID) == 0 {
			UpdateMap[args.Update.ID] = args.Update
			UpdateLog = append(UpdateLog, args.Update.ID)
			Verbose("update #" + args.Update.ID + " merged")
			QueryChan <- args.Update
			// further, push the update to peers
			PendingPushQueue <- PendingPush{
				Update:    args.Update,
				UpdateLog: UpdateLog,
			}
		}
		mu.Unlock()
	} else {
		missing = append(missing, args.Update.ID)
	}

	// return missing update ids to request for retransmit
	*reply = PushReply{MissingUpdates: missing}

	return nil
}

func (handler *RPCHandler) PushPull(args PushPullArgs, reply *PushPullReply) error {
	// 1. Push
	// check missing updates
	var missing []string
	for _, id := range args.UpdateLog {
		if len(UpdateMap[id].ID) == 0 && id != args.Update.ID {
			// never see this update, and update is not the latest one
			missing = append(missing, id)
		}
	}

	// only accept the update if no earlier updates are missing
	if len(missing) == 0 {
		mu.Lock()
		if len(UpdateMap[args.Update.ID].ID) == 0 {
			UpdateMap[args.Update.ID] = args.Update
			UpdateLog = append(UpdateLog, args.Update.ID)
			Verbose("update #" + args.Update.ID + " merged")
			QueryChan <- args.Update
			// further, push the update to peers
			PendingPushQueue <- PendingPush{
				Update:    args.Update,
				UpdateLog: UpdateLog,
			}
		}
		mu.Unlock()
	} else {
		missing = append(missing, args.Update.ID)
	}

	// 2. Pull
	// check what updates peer is missing
	localLog := UpdateLog[:]
	peerMap := make(map[string]bool)
	for _, id := range args.UpdateLog {
		peerMap[id] = true
	}

	// request missing updates, and retransmit updates to peer
	*reply = PushPullReply{MissingUpdates: missing}
	for _, id := range localLog {
		if !peerMap[id] {
			reply.Updates = append(reply.Updates, UpdateMap[id])
		}
	}
	return nil
}

func (handler *RPCHandler) Pull(args PullArgs, reply *PullReply) error {
	// check what updates peer is missing
	localLog := UpdateLog[:]
	peerMap := make(map[string]bool)
	for _, id := range args.UpdateLog {
		peerMap[id] = true
	}

	// retransmit missing updates to peer
	*reply = PullReply{}
	for _, id := range localLog {
		if !peerMap[id] {
			reply.Updates = append(reply.Updates, UpdateMap[id])
		}
	}
	return nil
}

// Retransmit should follow a Push or PushPull.
func (handler *RPCHandler) Retransmit(args RetransmitArgs, reply *RetransmitReply) error {
	mu.Lock()
	defer mu.Unlock()
	for _, update := range args.Updates {
		if len(UpdateMap[update.ID].ID) == 0 {
			UpdateMap[update.ID] = update
			UpdateLog = append(UpdateLog, update.ID)
			Verbose("update #" + update.ID + " merged")
			QueryChan <- update
		}
	}
	return nil
}

func DigestLocalUpdateService() {
	for {
		select {
		case <-ExitSignal:
			return
		case update := <-UpdateChan:
			mu.Lock()
			if len(UpdateMap[update.ID].ID) == 0 {
				UpdateMap[update.ID] = update
				UpdateLog = append(UpdateLog, update.ID)
				Verbose("update #" + update.ID + " added")
				if mode != "Pull" {
					// need to push the update to peers
					PendingPushQueue <- PendingPush{
						Update:    update,
						UpdateLog: UpdateLog,
					}
				}
			}
			mu.Unlock()
		}
	}
}

func PushService() {
	for {
		select {
		case <-ExitSignal:
			return
		case pendingPush := <-PendingPushQueue:
			Verbose("new push cycle (#" + pendingPush.Update.ID + ")")
			// randomly select peers
			selectedPeers := SelectPeers()

			// push to peers
			for _, peer := range selectedPeers {
				go func(peerAddr string) {
					conn, err := rpc.Dial("tcp", peerAddr)
					if err != nil {
						return
					}
					Verbose("pushing... (#" + pendingPush.Update.ID + ", " + peerAddr + ")")
					if mode == "Push" {
						args := PushArgs{
							Identity:  identity,
							Update:    pendingPush.Update,
							UpdateLog: pendingPush.UpdateLog,
						}
						reply := PushReply{}
						err = conn.Call("RPCHandler.Push", args, &reply)
						if err != nil {
							return
						}
						// check if peer request retransmit
						if len(reply.MissingUpdates) > 0 {
							args := RetransmitArgs{Identity: identity}
							for _, id := range reply.MissingUpdates {
								args.Updates = append(args.Updates, UpdateMap[id])
							}
							reply := RetransmitReply{}
							_ = conn.Call("RPCHandler.Retransmit", args, &reply)
						}
					} else if mode == "PushPull" {
						args := PushPullArgs{
							Identity:  identity,
							Update:    pendingPush.Update,
							UpdateLog: pendingPush.UpdateLog,
						}
						reply := PushPullReply{}
						err = conn.Call("RPCHandler.PushPull", args, &reply)
						if err != nil {
							return
						}
						// add pulled updates first
						mu.Lock()
						for _, update := range reply.Updates {
							if len(UpdateMap[update.ID].ID) == 0 {
								UpdateMap[update.ID] = update
								UpdateLog = append(UpdateLog, update.ID)
								Verbose("update #" + update.ID + " merged")
								QueryChan <- update
							}
						}
						mu.Unlock()
						// then retransmit if requested
						if len(reply.MissingUpdates) > 0 {
							args := RetransmitArgs{Identity: identity}
							for _, id := range reply.MissingUpdates {
								args.Updates = append(args.Updates, UpdateMap[id])
							}
							reply := RetransmitReply{}
							_ = conn.Call("RPCHandler.Retransmit", args, &reply)
						}
					}
				}(peer)
			}
		}
	}
}

func PullService() {
	replyChan := make(chan []Update, FanOut)
	for {
		select {
		case <-ExitSignal:
			return
		default:
			Verbose("new pull cycle")
			// randomly select peers
			selectedPeers := SelectPeers()

			// pull from peers
			for _, peer := range selectedPeers {
				go func(peerAddr string) {
					conn, err := rpc.Dial("tcp", peerAddr)
					if err != nil {
						replyChan <- []Update{}
						return
					}
					Verbose("pulling... (" + peerAddr + ")")
					args := PullArgs{Identity: identity, UpdateLog: UpdateLog}
					reply := PullReply{}
					err = conn.Call("RPCHandler.Pull", args, &reply)
					if err != nil {
						Verbose("pull failed (" + peerAddr + ")")
						replyChan <- []Update{}
					} else {
						Verbose("pull succeeded (" + peerAddr + ")")
						replyChan <- reply.Updates
					}
				}(peer)
			}

			// process replies
			replyCount := 0
			for replyCount < len(selectedPeers) {
				updates := <-replyChan
				mu.Lock()
				for _, update := range updates {
					if len(UpdateMap[update.ID].ID) == 0 {
						UpdateMap[update.ID] = update
						UpdateLog = append(UpdateLog, update.ID)
						Verbose("update #" + update.ID + " merged")
						QueryChan <- update
					}
				}
				mu.Unlock()
			}
			Verbose("pull cycle ended")
			// timeout for next cycle
			time.Sleep(time.Duration(5) * time.Second)
		}
	}
}

func SelectPeers() []string {
	peers := PeerList[:]
	var selectedPeers []string
	if len(peers) == 0 {
		Verbose("no available peers")
	} else if len(peers) <= int(FanOut) {
		selectedPeers = peers
	} else {
		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(peers), func(i, j int) {
			peers[i], peers[j] = peers[j], peers[i]
		})
		selectedPeers = peers[:FanOut]
	}
	return selectedPeers
}

func Verbose(str string) {
	if verbose {
		log.Println("[INFO] gossip: " + str)
	}
}
