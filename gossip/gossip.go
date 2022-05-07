package gossip

import (
	"bytes"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/rpc"
	"strings"
	"sync"
	"time"
)

const (
	BlockIDPrefix       = "block-"
	TransactionIDPrefix = "txn-"
	NodeIDPrefix        = "node-"
	TypeTracker         = "tracker"
	TypeMiner           = "miner"
	TriggerNewUpdate    = "update"
	TriggerInterval     = "interval"
)

type Update struct {
	ID   string
	Data []byte
}

type Peer struct {
	Identifier string
	GossipAddr string
	APIAddr    string
	Active     bool
	Type       string // "tracker" or "miner"
}

func (p *Peer) Encode() []byte {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(p)
	if err != nil {
		log.Println("[WARN] peer encode error")
	}
	return buf.Bytes()
}

func DecodeToPeer(data []byte) Peer {
	peer := Peer{}
	err := gob.NewDecoder(bytes.NewReader(data)).Decode(&peer)
	if err != nil {
		log.Println("[ERROR] block decode error")
		log.Fatal(err)
	}
	return peer
}

// messages

type (
	PushArgs struct { // Note: Update needs to be the last of UpdateLog
		From      Peer
		Update    Update
		UpdateLog []string
	}
	PushReply struct {
		MissingUpdates []string
	}
	PushPullArgs struct { // Note: Update needs to be the last of UpdateLog
		From      Peer
		Update    Update
		UpdateLog []string
	}
	PushPullReply struct {
		Updates        []Update
		MissingUpdates []string
	}
	PullArgs struct {
		From      Peer
		UpdateLog []string
	}
	PullReply struct {
		Updates []Update
	}
	RetransmitArgs struct {
		Updates []Update
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
	Identity        Peer   // gossip client identifier.
	localListenAddr string

	QueryChan  chan<- Update // for gossip client to query updates
	UpdateChan <-chan Update // for gossip client to put updates

	PendingPushQueue chan PendingPush // pending updates (from the client or peers) that need to be pushed

	rw        sync.RWMutex
	UpdateMap map[string]Update // stores every update
	UpdateLog []string          // update id history
	FanOut    uint8             // number of connections
	PeerList  []Peer            // peer addresses

	ExitSignal chan int
)

type RPCHandler struct {
}

func Start(fanOut uint8, // number of connections
	operatingMode string, // operating mode of gossip protocol. ["Push", "PushPull", "Pull"]
	trigger string, // trigger for new cycle. ["interval", "update"]. For Pull, only interval trigger is allowed
	localIp string,
	peers []Peer, // initial peer addresses
	initialUpdates []Update, // all updates client has to date
	clientIdentity Peer, // gossip client identifier
	logging bool, // whether to print
) (queryChan <-chan Update, updateChan chan<- Update, localAddr string, err error) {
	if running {
		return nil, nil, "", errors.New("[ERROR] gossip service already running")
	}
	running = true
	mode = operatingMode
	Identity = clientIdentity
	verbose = logging

	qCh := make(chan Update, 500)
	uCh := make(chan Update, 500)

	QueryChan = qCh
	UpdateChan = uCh
	PendingPushQueue = make(chan PendingPush, 100)
	UpdateMap = make(map[string]Update)
	UpdateLog = []string{}
	FanOut = fanOut
	ExitSignal = make(chan int, 2)

	// unpack initial updates
	for _, p := range peers {
		initialUpdates = append(initialUpdates, NewUpdate(NodeIDPrefix, []byte(p.Identifier), p.Encode()))
	}
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
	Identity.GossipAddr = localListenAddr

	if Identity.Type == TypeMiner {
		// push its info to peers
		uCh <- NewUpdate(NodeIDPrefix, []byte(Identity.Identifier), Identity.Encode())
	} else if Identity.Type == TypeTracker {
		if operatingMode != "PushPull" {
			err = errors.New("[Error] tracker node can only operate on PushPull")
			return
		}
	} else {
		err = errors.New("[Error] unexpected node type")
		return
	}
	setPeers(peers) // set peers should be called only after local address is assigned

	if operatingMode == "Push" {
		go PushService(trigger)
	} else if operatingMode == "PushPull" {
		go PushService(trigger)
	} else if operatingMode == "Pull" {
		if trigger != TriggerInterval {
			err = errors.New("[Error] Only interval trigger is allowed for Pull mode")
			return
		}
		go PullService()
	} else {
		return nil, nil, "", errors.New("[Error] unexpected gossip mode")
	}

	go DigestLocalUpdateService(trigger)

	return qCh, uCh, localListenAddr, nil
}

func setPeers(peers []Peer) {
	// find self
	i := 0
	for ; i < len(peers); i++ {
		if peers[i].GossipAddr == localListenAddr {
			break
		}
	}

	rw.Lock()
	defer rw.Unlock()
	// exclude self
	if i < len(peers) {
		PeerList = append(peers[:i], peers[i+1:]...)
	} else {
		PeerList = peers
	}
}

// internal function for adding a peer to peer list.
// lock should be acquired by the caller function.
func addPeer(peer Peer) {
	if peer.GossipAddr != localListenAddr {
		for _, p := range PeerList {
			if p.GossipAddr == peer.GossipAddr {
				return
			}
		}
		PeerList = append(PeerList, peer)
	}
}

func GetPeers() []Peer {
	rw.RLock()
	defer rw.RUnlock()
	peer := PeerList[:]
	return peer
}

func setActive(peer Peer) {
	rw.Lock()
	defer rw.Unlock()
	i := 0
	for ; i < len(PeerList); i++ {
		if PeerList[i].GossipAddr == peer.GossipAddr {
			if !PeerList[i].Active {
				PeerList[i].Active = true
				Verbose("peer <" + peer.Identifier + "> at " + peer.GossipAddr + " is now active")
			}
			break
		}
	}
	// if the peer is new, add to peer list
	if i == len(PeerList) {
		peer.Active = true
		PeerList = append(PeerList, peer)
		Verbose("discover a new peer <" + peer.Identifier + "> [" + peer.Type + "] at " + peer.GossipAddr)
	}
}

func setInactive(peer Peer) {
	rw.Lock()
	defer rw.Unlock()
	for i, _ := range PeerList {
		if PeerList[i].GossipAddr == peer.GossipAddr {
			if PeerList[i].Active {
				PeerList[i].Active = false
				Verbose("peer <" + peer.Identifier + "> at " + peer.GossipAddr + " is now inactive")
			}
			break
		}
	}
}

func NewUpdate(prefix string, hash []byte, data []byte) Update {
	return Update{
		ID:   prefix + fmt.Sprintf("%x", hash),
		Data: data,
	}
}

func (handler *RPCHandler) Push(args PushArgs, reply *PushReply) error {
	// mark sender as active
	setActive(args.From)

	// check missing updates
	var missing []string
	rw.RLock()
	for _, id := range args.UpdateLog {
		if len(UpdateMap[id].ID) == 0 && id != args.Update.ID {
			if Identity.Type == TypeTracker && !strings.Contains(id, NodeIDPrefix) {
				continue
			}
			// never see this update, and update is not the latest one
			missing = append(missing, id)
		}
	}
	rw.RUnlock()

	// only accept the update if no earlier updates are missing
	if Identity.Type == TypeMiner || Identity.Type == TypeTracker && strings.Contains(args.Update.ID, NodeIDPrefix) {
		if len(missing) == 0 {
			rw.Lock()
			if len(UpdateMap[args.Update.ID].ID) == 0 {
				UpdateMap[args.Update.ID] = args.Update
				UpdateLog = append(UpdateLog, args.Update.ID)
				Verbose("update #" + args.Update.ID + " merged")
				if strings.HasPrefix(args.Update.ID, NodeIDPrefix) {
					addPeer(DecodeToPeer(args.Update.Data))
				} else {
					QueryChan <- args.Update
				}
				// further, push the update to peers
				PendingPushQueue <- PendingPush{
					Update:    args.Update,
					UpdateLog: UpdateLog,
				}
			}
			rw.Unlock()
		} else {
			missing = append(missing, args.Update.ID)
		}
	}

	// return missing update ids to request for retransmit
	*reply = PushReply{MissingUpdates: missing}

	return nil
}

func (handler *RPCHandler) PushPull(args PushPullArgs, reply *PushPullReply) error {
	// mark sender as active
	setActive(args.From)

	// 1. Push
	// check missing updates
	var missing []string
	rw.RLock()
	for _, id := range args.UpdateLog {
		if len(UpdateMap[id].ID) == 0 && id != args.Update.ID {
			if Identity.Type == TypeTracker && !strings.Contains(id, NodeIDPrefix) {
				continue
			}
			// never see this update, and update is not the latest one
			missing = append(missing, id)
		}
	}
	rw.RUnlock()

	// only accept the update if no earlier updates are missing
	if Identity.Type == TypeMiner || Identity.Type == TypeTracker && strings.Contains(args.Update.ID, NodeIDPrefix) {
		if len(missing) == 0 {
			rw.Lock()
			if len(UpdateMap[args.Update.ID].ID) == 0 {
				UpdateMap[args.Update.ID] = args.Update
				UpdateLog = append(UpdateLog, args.Update.ID)
				Verbose("update #" + args.Update.ID + " merged")
				if strings.HasPrefix(args.Update.ID, NodeIDPrefix) {
					addPeer(DecodeToPeer(args.Update.Data))
				} else {
					QueryChan <- args.Update
				}
				// further, push the update to peers
				PendingPushQueue <- PendingPush{
					Update:    args.Update,
					UpdateLog: UpdateLog,
				}
			}
			rw.Unlock()
		} else {
			missing = append(missing, args.Update.ID)
		}
	}

	*reply = PushPullReply{MissingUpdates: missing}
	// 2. Pull
	// check what updates peer is missing
	rw.RLock()
	localLog := UpdateLog[:]
	rw.RUnlock()
	peerMap := make(map[string]bool)
	for _, id := range args.UpdateLog {
		peerMap[id] = true
	}

	// request missing updates, and retransmit updates to peer
	for _, id := range localLog {
		if !peerMap[id] &&
			(Identity.Type == TypeMiner ||
				Identity.Type == TypeTracker && strings.HasPrefix(id, NodeIDPrefix)) {
			reply.Updates = append(reply.Updates, UpdateMap[id])
		}
	}
	return nil
}

func (handler *RPCHandler) Pull(args PullArgs, reply *PullReply) error {
	// mark sender as active
	setActive(args.From)

	// check what updates peer is missing
	rw.RLock()
	localLog := UpdateLog[:]
	rw.RUnlock()
	peerMap := make(map[string]bool)
	for _, id := range args.UpdateLog {
		peerMap[id] = true
	}

	// retransmit missing updates to peer
	*reply = PullReply{}
	for _, id := range localLog {
		if !peerMap[id] && (args.From.Type == TypeMiner || args.From.Type == TypeTracker && strings.Contains(id, NodeIDPrefix)) {
			reply.Updates = append(reply.Updates, UpdateMap[id])
		}
	}
	return nil
}

// Retransmit should follow a Push or PushPull.
func (handler *RPCHandler) Retransmit(args RetransmitArgs, reply *RetransmitReply) error {
	rw.Lock()
	defer rw.Unlock()
	for _, update := range args.Updates {
		if len(UpdateMap[update.ID].ID) == 0 &&
			(Identity.Type == TypeMiner ||
				Identity.Type == TypeTracker && strings.HasPrefix(update.ID, NodeIDPrefix)) {
			UpdateMap[update.ID] = update
			UpdateLog = append(UpdateLog, update.ID)
			Verbose("update #" + update.ID + " merged")
			if strings.HasPrefix(update.ID, NodeIDPrefix) {
				addPeer(DecodeToPeer(update.Data))
			} else {
				QueryChan <- update
			}
		}
	}
	return nil
}

func DigestLocalUpdateService(trigger string) {
	for {
		select {
		case <-ExitSignal:
			return
		case update := <-UpdateChan:
			rw.Lock()
			if len(UpdateMap[update.ID].ID) == 0 {
				UpdateMap[update.ID] = update
				UpdateLog = append(UpdateLog, update.ID)
				Verbose("update #" + update.ID + " added")
				if mode != "Pull" && trigger == TriggerNewUpdate {
					// need to push the update to peers
					PendingPushQueue <- PendingPush{
						Update:    update,
						UpdateLog: UpdateLog,
					}
				}
			}
			rw.Unlock()
		}
	}
}

func PushService(trigger string) {
	if trigger == TriggerNewUpdate {
		for {
			select {
			case <-ExitSignal:
				return
			case pendingPush := <-PendingPushQueue:
				PushCycle(pendingPush)
			}
		}
	} else if trigger == TriggerInterval {
		for {
			time.Sleep(2 * time.Second)
			rw.Lock()
			if len(UpdateLog) == 0 {
				rw.Unlock()
				continue
			}
			pendingPush := PendingPush{
				Update:    UpdateMap[UpdateLog[len(UpdateLog)-1]],
				UpdateLog: UpdateLog,
			}
			rw.Unlock()
			PushCycle(pendingPush)
		}
	}

}

func PushCycle(pendingPush PendingPush) {
	Verbose("new push cycle (#" + pendingPush.Update.ID + ")")
	// randomly select peers (select tracker nodes only if it is a node update)
	selectedPeers := SelectPeers(strings.Contains(pendingPush.Update.ID, NodeIDPrefix))

	// push to peers
	for _, p := range selectedPeers {
		go func(peer Peer) {
			conn, err := rpc.Dial("tcp", peer.GossipAddr)
			if err != nil || conn == nil {
				// peer failed. mark inactive
				setInactive(peer)
				return
			}
			Verbose("pushing... (#" + pendingPush.Update.ID + ", " + peer.GossipAddr + ")")
			if mode == "Push" {
				args := PushArgs{
					From:      Identity,
					Update:    pendingPush.Update,
					UpdateLog: pendingPush.UpdateLog,
				}
				reply := PushReply{}
				err = conn.Call("RPCHandler.Push", args, &reply)
				if err != nil {
					// peer failed. mark inactive
					setInactive(peer)
					return
				} else {
					setActive(peer)
				}
				// check if peer request retransmit
				if len(reply.MissingUpdates) > 0 {
					args := RetransmitArgs{}
					rw.RLock()
					for _, id := range reply.MissingUpdates {
						args.Updates = append(args.Updates, UpdateMap[id])
					}
					rw.RUnlock()
					reply := RetransmitReply{}
					_ = conn.Call("RPCHandler.Retransmit", args, &reply)
				}
			} else if mode == "PushPull" {
				time.Sleep(time.Duration(rand.New(rand.NewSource(time.Now().UnixNano())).Intn(2500)) * time.Millisecond)
				args := PushPullArgs{
					From:      Identity,
					Update:    pendingPush.Update,
					UpdateLog: pendingPush.UpdateLog,
				}
				reply := PushPullReply{}
				err = conn.Call("RPCHandler.PushPull", args, &reply)
				if err != nil {
					// peer failed. mark inactive
					setInactive(peer)
					return
				} else {
					setActive(peer)
				}
				// add pulled updates first
				rw.Lock()
				for _, update := range reply.Updates {
					if len(UpdateMap[update.ID].ID) == 0 &&
						(Identity.Type == TypeMiner || Identity.Type == TypeTracker && strings.HasPrefix(update.ID, NodeIDPrefix)) {
						UpdateMap[update.ID] = update
						UpdateLog = append(UpdateLog, update.ID)
						Verbose("update #" + update.ID + " merged")
						if strings.HasPrefix(update.ID, NodeIDPrefix) {
							addPeer(DecodeToPeer(update.Data))
						} else if Identity.Type == TypeMiner {
							QueryChan <- update
						}
					}
				}
				rw.Unlock()
				// then retransmit if requested
				if len(reply.MissingUpdates) > 0 {
					args := RetransmitArgs{}
					rw.RLock()
					for _, id := range reply.MissingUpdates {
						args.Updates = append(args.Updates, UpdateMap[id])
					}
					rw.RUnlock()
					reply := RetransmitReply{}
					_ = conn.Call("RPCHandler.Retransmit", args, &reply)
				}
			}
		}(p)
	}
	Verbose("push cycle ended")
}

func PullService() {
	replyChan := make(chan []Update, FanOut)
	for {
		// timeout for next cycle
		time.Sleep(time.Duration(5) * time.Second)
		select {
		case <-ExitSignal:
			return
		default:
			Verbose("new pull cycle")
			// randomly select peers
			selectedPeers := SelectPeers(true)

			// pull from peers
			for _, p := range selectedPeers {
				go func(peer Peer) {
					conn, err := rpc.Dial("tcp", peer.GossipAddr)
					if err != nil || conn == nil {
						Verbose("pull failed (" + peer.GossipAddr + ")")
						replyChan <- []Update{}
						setInactive(peer)
						return
					}
					Verbose("pulling... (" + peer.GossipAddr + ")")
					rw.RLock()
					args := PullArgs{From: Identity, UpdateLog: UpdateLog[:]}
					rw.RUnlock()
					reply := PullReply{}
					err = conn.Call("RPCHandler.Pull", args, &reply)
					if err != nil {
						Verbose("pull failed (" + peer.GossipAddr + ")")
						replyChan <- []Update{}
						setInactive(peer)
					} else {
						Verbose("pull succeeded (" + peer.GossipAddr + ")")
						replyChan <- reply.Updates
						setActive(peer)
					}
				}(p)
			}

			// process replies
			for replyCount := 0; replyCount < len(selectedPeers); replyCount++ {
				updates := <-replyChan
				if len(updates) == 0 {
					continue
				}
				rw.Lock()
				for _, update := range updates {
					if len(UpdateMap[update.ID].ID) == 0 {
						UpdateMap[update.ID] = update
						UpdateLog = append(UpdateLog, update.ID)
						Verbose("update #" + update.ID + " merged")
						if strings.HasPrefix(update.ID, NodeIDPrefix) {
							addPeer(DecodeToPeer(update.Data))
						} else {
							QueryChan <- update
						}
					}
				}
				rw.Unlock()
			}
			Verbose("pull cycle ended")
		}
	}
}

func SelectPeers(includeTracker bool) []Peer {
	rw.RLock()
	peers := PeerList[:]
	rw.RUnlock()
	var activePeers []Peer
	for i, peer := range peers {
		if peer.Active && (includeTracker || !includeTracker && peer.Type == TypeMiner) {
			activePeers = append(activePeers, peers[i])
		}
	}
	var selectedPeers []Peer
	if len(activePeers) == 0 {
		Verbose("no available peers")
	} else if len(activePeers) <= int(FanOut) {
		selectedPeers = activePeers
	} else {
		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(activePeers), func(i, j int) {
			activePeers[i], activePeers[j] = activePeers[j], activePeers[i]
		})
		selectedPeers = activePeers[:FanOut]
	}
	return selectedPeers
}

func Verbose(str string) {
	if verbose {
		log.Println("[INFO] gossip: " + str)
	}
}
