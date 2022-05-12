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
	OpModePush          = "Push"
	OpModePushPull      = "PushPull"
	OpModePull          = "Pull"
	BlockIDPrefix       = "block-"
	TransactionIDPrefix = "txn-"
	NodeIDPrefix        = "node-"
	TypeTracker         = "tracker"
	TypeMiner           = "miner"
	TriggerNewUpdate    = "update"
	TriggerInterval     = "interval"
	SubscribeTxn        = 1 << 0
	SubscribeBlock      = 1 << 1
	SubscribeNode       = 1 << 2
)

type Update struct {
	ID   string
	Data []byte
}

type Peer struct {
	Identifier   string
	GossipAddr   string
	APIAddr      string
	Active       bool
	Type         string // "tracker" or "miner"
	Subscription int
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
	printLog        bool
	running         bool
	mode            string // operating mode of gossip protocol. ["Push", "PushPull", "Pull"]
	Identity        Peer   // gossip client identifier.
	localListenAddr string

	queryChan  chan<- Update // for gossip client to query updates
	updateChan <-chan Update // for gossip client to put updates

	pendingPushQueue chan PendingPush // pending updates (from the client or peers) that need to be pushed

	rw        sync.RWMutex
	updateMap map[string]Update // stores every update
	updateLog []string          // update id history
	fanOut    uint8             // number of connections
	peerList  []Peer            // peer addresses

	exitSignal chan int
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
	printLog = logging

	qCh := make(chan Update, 500)
	uCh := make(chan Update, 500)

	queryChan = qCh
	updateChan = uCh
	pendingPushQueue = make(chan PendingPush, 100)
	updateMap = make(map[string]Update)
	updateLog = []string{}
	fanOut = fanOut
	exitSignal = make(chan int, 2)

	// unpack initial updates
	for _, update := range initialUpdates {
		updateMap[update.ID] = update
		updateLog = append(updateLog, update.ID)
	}
	// append node updates after
	for _, p := range peers {
		peer := p          // make a copy
		peer.Active = true // set to be active
		initialUpdates = append(initialUpdates, NewUpdate(NodeIDPrefix, []byte(peer.Identifier), peer.Encode()))
	}

	handler := new(RPCHandler)
	localListenAddr, err = util.NewRPCServerWithIp(handler, localIp)
	if err != nil {
		return nil, nil, "", err
	}
	verbose("listen to gossips at " + localListenAddr)
	Identity.GossipAddr = localListenAddr

	if Identity.Type == TypeMiner {
		// push its info to peers
		uCh <- NewUpdate(NodeIDPrefix, []byte(Identity.Identifier), Identity.Encode())
	} else if Identity.Type == TypeTracker {
		if operatingMode != OpModePushPull {
			err = errors.New("[Error] tracker node can only operate on PushPull")
			return
		}
	} else {
		err = errors.New("[Error] unexpected node type")
		return
	}
	setPeers(peers) // set peers should be called only after local address is assigned

	//TODO: default subscribes to SubscribeNode

	if operatingMode == OpModePush {
		go pushService(trigger)
	} else if operatingMode == OpModePushPull {
		go pushService(trigger)
	} else if operatingMode == OpModePull {
		if trigger != TriggerInterval {
			err = errors.New("[Error] Only interval trigger is allowed for Pull mode")
			return
		}
		go pullService()
	} else {
		return nil, nil, "", errors.New("[Error] unexpected gossip mode")
	}

	go digestLocalUpdateService(trigger)

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
		peerList = append(peers[:i], peers[i+1:]...)
	} else {
		peerList = peers
	}
}

// internal function for adding a peer to peer list.
// lock should be acquired by the caller function.
func addPeer(peer Peer) {
	if peer.GossipAddr != localListenAddr {
		for _, p := range peerList {
			if p.GossipAddr == peer.GossipAddr {
				return
			}
		}
		peerList = append(peerList, peer)
		verbose("discover a new peer <" + peer.Identifier + "> [" + peer.Type + "] at " + peer.GossipAddr)
	}
}

func GetPeers() []Peer {
	rw.RLock()
	defer rw.RUnlock()
	peer := peerList[:]
	return peer
}

func setActive(peer Peer) {
	rw.Lock()
	defer rw.Unlock()
	i := 0
	for ; i < len(peerList); i++ {
		if peerList[i].Identifier == peer.Identifier {
			if !peerList[i].Active {
				// update the entire peer info in case it has changed due to rejoin, etc.
				peer.Active = true
				peerList[i] = peer
				verbose("peer <" + peer.Identifier + "> is now active")
			}
			break
		}
	}
	// if the peer is new, add to peer list
	if i == len(peerList) {
		peer.Active = true
		peerList = append(peerList, peer)
		verbose("discover a new peer <" + peer.Identifier + "> [" + peer.Type + "] at " + peer.GossipAddr)
	}
}

func setInactive(peer Peer) {
	rw.Lock()
	defer rw.Unlock()
	for i, _ := range peerList {
		if peerList[i].GossipAddr == peer.GossipAddr {
			if peerList[i].Active {
				peerList[i].Active = false
				verbose("peer <" + peer.Identifier + "> at " + peer.GossipAddr + " is now inactive")
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

// checkSubscription checks if an update belongs to given subscriptions
func checkSubscription(sub int, id string) bool {
	if sub&SubscribeTxn != 0 && strings.Contains(id, TransactionIDPrefix) ||
		sub&SubscribeBlock != 0 && strings.Contains(id, BlockIDPrefix) ||
		sub&SubscribeNode != 0 && strings.Contains(id, NodeIDPrefix) {
		return true
	} else {
		return false
	}
}

func (handler *RPCHandler) Push(args PushArgs, reply *PushReply) error {
	// mark sender as active
	setActive(args.From)

	// check missing updates
	var missing []string
	rw.RLock()
	for _, id := range args.UpdateLog {
		if checkSubscription(Identity.Subscription, id) && len(updateMap[id].ID) == 0 && id != args.Update.ID {
			// never see this update, and update is not the latest one
			missing = append(missing, id)
		}
	}
	rw.RUnlock()

	// only accept the update if no earlier updates are missing
	if checkSubscription(Identity.Subscription, args.Update.ID) {
		if len(missing) == 0 {
			rw.Lock()
			if len(updateMap[args.Update.ID].ID) == 0 {
				updateMap[args.Update.ID] = args.Update
				updateLog = append(updateLog, args.Update.ID)
				verbose("update #" + args.Update.ID + " merged")
				if strings.HasPrefix(args.Update.ID, NodeIDPrefix) {
					addPeer(DecodeToPeer(args.Update.Data))
				} else {
					queryChan <- args.Update
				}
				// further, push the update to peers
				pendingPushQueue <- PendingPush{
					Update:    args.Update,
					UpdateLog: updateLog,
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
		if checkSubscription(Identity.Subscription, id) && len(updateMap[id].ID) == 0 && id != args.Update.ID {
			// never see this update, and update is not the latest one
			missing = append(missing, id)
		}
	}
	rw.RUnlock()

	// only accept the update if no earlier updates are missing
	if checkSubscription(Identity.Subscription, args.Update.ID) {
		if len(missing) == 0 {
			rw.Lock()
			if len(updateMap[args.Update.ID].ID) == 0 {
				updateMap[args.Update.ID] = args.Update
				updateLog = append(updateLog, args.Update.ID)
				verbose("update #" + args.Update.ID + " merged")
				if strings.HasPrefix(args.Update.ID, NodeIDPrefix) {
					addPeer(DecodeToPeer(args.Update.Data))
				} else {
					queryChan <- args.Update
				}
				// further, push the update to peers
				pendingPushQueue <- PendingPush{
					Update:    args.Update,
					UpdateLog: updateLog,
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
	localLog := updateLog[:]
	rw.RUnlock()
	peerMap := make(map[string]bool)
	for _, id := range args.UpdateLog {
		peerMap[id] = true
	}

	// request missing updates, and retransmit updates to peer
	for _, id := range localLog {
		if !peerMap[id] && checkSubscription(args.From.Subscription, id) {
			reply.Updates = append(reply.Updates, updateMap[id])
		}
	}
	return nil
}

func (handler *RPCHandler) Pull(args PullArgs, reply *PullReply) error {
	// mark sender as active
	setActive(args.From)

	// check what updates peer is missing
	rw.RLock()
	localLog := updateLog[:]
	rw.RUnlock()
	peerMap := make(map[string]bool)
	for _, id := range args.UpdateLog {
		peerMap[id] = true
	}

	// retransmit missing updates to peer
	*reply = PullReply{}
	for _, id := range localLog {
		if !peerMap[id] && checkSubscription(args.From.Subscription, id) {
			reply.Updates = append(reply.Updates, updateMap[id])
		}
	}
	return nil
}

// Retransmit should follow a Push or PushPull.
func (handler *RPCHandler) Retransmit(args RetransmitArgs, reply *RetransmitReply) error {
	rw.Lock()
	defer rw.Unlock()
	for _, update := range args.Updates {
		if len(updateMap[update.ID].ID) == 0 && checkSubscription(Identity.Subscription, update.ID) {
			updateMap[update.ID] = update
			updateLog = append(updateLog, update.ID)
			verbose("update #" + update.ID + " merged")
			if strings.HasPrefix(update.ID, NodeIDPrefix) {
				addPeer(DecodeToPeer(update.Data))
			} else {
				queryChan <- update
			}
		}
	}
	return nil
}

func digestLocalUpdateService(trigger string) {
	for {
		select {
		case <-exitSignal:
			return
		case update := <-updateChan:
			rw.Lock()
			if len(updateMap[update.ID].ID) == 0 {
				updateMap[update.ID] = update
				updateLog = append(updateLog, update.ID)
				verbose("new local update #" + update.ID)
				if mode != OpModePull && trigger == TriggerNewUpdate {
					// need to push the update to peers
					pendingPushQueue <- PendingPush{
						Update:    update,
						UpdateLog: updateLog,
					}
				}
			}
			rw.Unlock()
		}
	}
}

func pushService(trigger string) {
	if trigger == TriggerNewUpdate {
		for {
			select {
			case <-exitSignal:
				return
			case pendingPush := <-pendingPushQueue:
				pushCycle(pendingPush)
			}
		}
	} else if trigger == TriggerInterval {
		for {
			time.Sleep(5 * time.Second)
			rw.Lock()
			if len(updateLog) == 0 {
				rw.Unlock()
				continue
			}
			pendingPush := PendingPush{
				Update:    updateMap[updateLog[len(updateLog)-1]],
				UpdateLog: updateLog,
			}
			rw.Unlock()
			pushCycle(pendingPush)
		}
	}

}

func pushCycle(pendingPush PendingPush) {
	verbose("new push cycle (#" + pendingPush.Update.ID + ")")
	// randomly select peers (select tracker nodes only if it is a node update)
	targetSubscription := 0
	if strings.HasPrefix(pendingPush.Update.ID, NodeIDPrefix) {
		targetSubscription = SubscribeNode
	} else if strings.HasPrefix(pendingPush.Update.ID, BlockIDPrefix) {
		targetSubscription = SubscribeBlock
	} else if strings.HasPrefix(pendingPush.Update.ID, TransactionIDPrefix) {
		targetSubscription = SubscribeTxn
	}
	selectedPeers := selectPeers(targetSubscription)

	// push to peers
	for _, p := range selectedPeers {
		go func(peer Peer) {
			conn, err := rpc.Dial("tcp", peer.GossipAddr)
			if err != nil || conn == nil {
				// peer failed. mark inactive
				setInactive(peer)
				return
			}
			verbose("pushing... (#" + pendingPush.Update.ID + ", " + peer.Identifier + ")")
			if mode == OpModePush {
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
						args.Updates = append(args.Updates, updateMap[id])
					}
					rw.RUnlock()
					reply := RetransmitReply{}
					_ = conn.Call("RPCHandler.Retransmit", args, &reply)
				}
			} else if mode == OpModePushPull {
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
					if len(updateMap[update.ID].ID) == 0 && checkSubscription(Identity.Subscription, update.ID) {
						updateMap[update.ID] = update
						updateLog = append(updateLog, update.ID)
						verbose("update #" + update.ID + " merged")
						if strings.HasPrefix(update.ID, NodeIDPrefix) {
							addPeer(DecodeToPeer(update.Data))
						} else if Identity.Type == TypeMiner {
							queryChan <- update
						}
					}
				}
				rw.Unlock()
				// then retransmit if requested
				if len(reply.MissingUpdates) > 0 {
					args := RetransmitArgs{}
					rw.RLock()
					for _, id := range reply.MissingUpdates {
						args.Updates = append(args.Updates, updateMap[id])
					}
					rw.RUnlock()
					reply := RetransmitReply{}
					_ = conn.Call("RPCHandler.Retransmit", args, &reply)
				}
			}
		}(p)
	}
}

func pullService() {
	replyChan := make(chan []Update, fanOut)
	for {
		// timeout for next cycle
		time.Sleep(time.Duration(5) * time.Second)
		select {
		case <-exitSignal:
			return
		default:
			verbose("new pull cycle")
			// randomly select peers
			selectedPeers := selectPeers(Identity.Subscription)

			// pull from peers
			for _, p := range selectedPeers {
				go func(peer Peer) {
					conn, err := rpc.Dial("tcp", peer.GossipAddr)
					if err != nil || conn == nil {
						verbose("pull failed (" + peer.GossipAddr + ")")
						replyChan <- []Update{}
						setInactive(peer)
						return
					}
					verbose("pulling... (" + peer.Identifier + ")")
					rw.RLock()
					args := PullArgs{From: Identity, UpdateLog: updateLog[:]}
					rw.RUnlock()
					reply := PullReply{}
					err = conn.Call("RPCHandler.Pull", args, &reply)
					if err != nil {
						verbose("pull failed (" + peer.GossipAddr + ")")
						replyChan <- []Update{}
						setInactive(peer)
					} else {
						verbose("pull succeeded (" + peer.GossipAddr + ")")
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
					if len(updateMap[update.ID].ID) == 0 {
						updateMap[update.ID] = update
						updateLog = append(updateLog, update.ID)
						verbose("update #" + update.ID + " merged")
						if strings.HasPrefix(update.ID, NodeIDPrefix) {
							addPeer(DecodeToPeer(update.Data))
						} else {
							queryChan <- update
						}
					}
				}
				rw.Unlock()
			}
			verbose("pull cycle ended")
		}
	}
}

func selectPeers(targetSubs int) []Peer {
	rw.RLock()
	peers := peerList[:]
	rw.RUnlock()
	// subscription-based peer selection
	var activePeers []Peer
	for i, peer := range peers {
		if peer.Active && peer.Subscription&targetSubs != 0 {
			activePeers = append(activePeers, peers[i])
		}
	}
	var selectedPeers []Peer
	if len(activePeers) == 0 {
		verbose("no available peers")
	} else if len(activePeers) <= int(fanOut) {
		selectedPeers = activePeers
		verbose(fmt.Sprintf("%d selected, %d active, %d total", len(selectedPeers), len(activePeers), len(peers)))
	} else {
		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(activePeers), func(i, j int) {
			activePeers[i], activePeers[j] = activePeers[j], activePeers[i]
		})
		selectedPeers = activePeers[:fanOut]
		verbose(fmt.Sprintf("%d selected, %d active, %d total", len(selectedPeers), len(activePeers), len(peers)))
	}
	return selectedPeers
}

func verbose(str string) {
	if printLog {
		log.Println("[lib-gossip] " + str)
	}
}
