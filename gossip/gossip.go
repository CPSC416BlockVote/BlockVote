package gossip

import (
	"cs.ubc.ca/cpsc416/BlockVote/util"
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
	printLog bool
	running  bool
	mode     string // operating mode of gossip protocol. ["Push", "PushPull", "Pull"]
	self     Peer

	querySendChan  chan<- Update // for gossip client to query updates
	updateRecvChan <-chan Update // for gossip client to put updates

	pendingPushQueue chan PendingPush // pending updates (from the client or peers) that need to be pushed

	rw          sync.RWMutex
	updateMap   map[string]Update // stores every update
	updateLog   []string          // update id history
	fanOut      uint8             // number of connections
	peerManager PeerManager       // manages peers

	exitSignal chan int
)

type RPCHandler struct {
}

func Start(fan uint8, // number of connections
	operatingMode string, // operating mode of gossip protocol. ["Push", "PushPull", "Pull"]
	trigger string, // trigger for new cycle. ["interval", "update"]. For Pull, only interval trigger is allowed
	localIp string,
	peers []Peer, // initial peer addresses
	initialUpdates []Update, // all updates client has to date
	clientIdentity *Peer, // gossip client identifier
	logging bool, // whether to print
) (queryChan <-chan Update, updateChan chan<- Update, localAddr string, err error) {
	if running {
		return nil, nil, "", errors.New("[ERROR] gossip service already running")
	}
	running = true
	mode = operatingMode
	printLog = logging

	self = *clientIdentity
	peerManager = PeerManager{Self: &self}
	self.Active = true
	self.Subscription |= SubscribeNode // default subscribes to SubscribeNode

	qCh := make(chan Update, 500)
	uCh := make(chan Update, 500)

	querySendChan = qCh
	updateRecvChan = uCh
	pendingPushQueue = make(chan PendingPush, 100)
	updateMap = make(map[string]Update)
	updateLog = []string{}
	fanOut = fan
	exitSignal = make(chan int, 2)

	// unpack initial updates
	for _, p := range peers { // append node updates after
		if p.Type == TypeMiner {
			peer := p          // make a copy
			peer.Active = true // set to be active
			initialUpdates = append(initialUpdates, NewUpdate(NodeIDPrefix+peer.Identifier+fmt.Sprintf("-%x", peer.LastAnnounced.Unix()), []byte{}, peer.Encode()))
		}
	}
	for _, update := range initialUpdates {
		updateMap[update.ID] = update
		updateLog = append(updateLog, update.ID)
	}

	// start API handler
	handler := new(RPCHandler)
	localListenAddr, err := util.NewRPCServerWithIp(handler, localIp)
	if err != nil {
		return nil, nil, "", err
	}
	verbose("listen to gossips at " + localListenAddr)
	self.GossipAddr = localListenAddr

	if self.Type == TypeMiner {
		// announce its info to peers. for node that restarts, re-announce its latest info.
		self.LastAnnounced = time.Now()
		uCh <- NewUpdate(NodeIDPrefix+self.Identifier+fmt.Sprintf("-%x", self.LastAnnounced.Unix()), []byte{}, self.Encode())
	} else if self.Type == TypeTracker {
		if operatingMode != OpModePushPull {
			err = errors.New("[Error] tracker node can only operate on PushPull")
			return
		}
	} else {
		err = errors.New("[Error] unexpected node type")
		return
	}
	peerManager.SetPeers(peers) // set peers should be called only after local address is assigned

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

func GetPeers(excludeSelf bool, excludeInactive bool) (peers []Peer) {
	if excludeSelf {
		for _, peer := range peerManager.GetPeers() {
			if !excludeInactive || excludeInactive && peer.Active {
				peers = append(peers, peer)
			}
		}
	} else {
		for _, peer := range append(peerManager.GetPeers(), self) {
			if !excludeInactive || excludeInactive && peer.Active {
				peers = append(peers, peer)
			}
		}
	}
	return
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
	peerManager.SetActive(&args.From)

	// check missing updates
	var missing []string
	rw.RLock()
	for _, id := range args.UpdateLog {
		if checkSubscription(self.Subscription, id) && len(updateMap[id].ID) == 0 && id != args.Update.ID {
			// never see this update, and update is not the latest one
			missing = append(missing, id)
		}
	}
	rw.RUnlock()

	// only accept the update if no earlier updates are missing
	if len(missing) == 0 {
		rw.Lock()
		filterIncomingUpdate(args.Update, "update #"+args.Update.ID+" merged", true)
		rw.Unlock()
	} else {
		missing = append(missing, args.Update.ID)
	}

	// return missing update ids to request for retransmit
	*reply = PushReply{MissingUpdates: missing}

	return nil
}

func (handler *RPCHandler) PushPull(args PushPullArgs, reply *PushPullReply) error {
	// mark sender as active
	peerManager.SetActive(&args.From)

	// 1. Push
	// check missing updates
	var missing []string
	rw.RLock()
	for _, id := range args.UpdateLog {
		if checkSubscription(self.Subscription, id) && len(updateMap[id].ID) == 0 && id != args.Update.ID {
			// never see this update, and update is not the latest one
			missing = append(missing, id)
		}
	}
	rw.RUnlock()

	// only accept the update if no earlier updates are missing
	if len(missing) == 0 {
		rw.Lock()
		filterIncomingUpdate(args.Update, "update #"+args.Update.ID+" merged", true)
		rw.Unlock()
	} else {
		missing = append(missing, args.Update.ID)
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
	peerManager.SetActive(&args.From)

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
func (handler *RPCHandler) Retransmit(args RetransmitArgs, _ *RetransmitReply) error {
	rw.Lock()
	defer rw.Unlock()
	for _, update := range args.Updates {
		filterIncomingUpdate(update, "update #"+update.ID+" merged", false)
	}
	return nil
}

func digestLocalUpdateService(trigger string) {
	for {
		select {
		case <-exitSignal:
			return
		case update := <-updateRecvChan:
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
	selectedPeers := peerManager.SelectPeers(targetSubscription)

	// push to peers
	for _, p := range selectedPeers {
		go func(peer Peer) {
			conn, err := rpc.Dial("tcp", peer.GossipAddr)
			if err != nil || conn == nil {
				// peer failed. mark inactive
				peerManager.SetInactive(&peer)
				return
			}
			verbose("pushing... (#" + pendingPush.Update.ID + ", " + peer.Identifier + ")")
			if mode == OpModePush {
				args := PushArgs{
					From:      self,
					Update:    pendingPush.Update,
					UpdateLog: pendingPush.UpdateLog,
				}
				reply := PushReply{}
				err = conn.Call("RPCHandler.Push", args, &reply)
				if err != nil {
					// peer failed. mark inactive
					peerManager.SetInactive(&peer)
					return
				} else {
					peerManager.SetActive(&peer)
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
					From:      self,
					Update:    pendingPush.Update,
					UpdateLog: pendingPush.UpdateLog,
				}
				reply := PushPullReply{}
				err = conn.Call("RPCHandler.PushPull", args, &reply)
				if err != nil {
					// peer failed. mark inactive
					peerManager.SetInactive(&peer)
					return
				} else {
					peerManager.SetActive(&peer)
				}
				// add pulled updates first
				rw.Lock()
				for _, update := range reply.Updates {
					filterIncomingUpdate(update, "update #"+update.ID+" merged", false)
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
			selectedPeers := peerManager.SelectPeers(self.Subscription)

			// pull from peers
			for _, p := range selectedPeers {
				go func(peer Peer) {
					conn, err := rpc.Dial("tcp", peer.GossipAddr)
					if err != nil || conn == nil {
						verbose("pull failed (" + peer.GossipAddr + ")")
						replyChan <- []Update{}
						peerManager.SetInactive(&peer)
						return
					}
					verbose("pulling... (" + peer.Identifier + ")")
					rw.RLock()
					args := PullArgs{From: self, UpdateLog: updateLog[:]}
					rw.RUnlock()
					reply := PullReply{}
					err = conn.Call("RPCHandler.Pull", args, &reply)
					if err != nil {
						verbose("pull failed (" + peer.GossipAddr + ")")
						replyChan <- []Update{}
						peerManager.SetInactive(&peer)
					} else {
						verbose("pull succeeded (" + peer.GossipAddr + ")")
						replyChan <- reply.Updates
						peerManager.SetActive(&peer)
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
					filterIncomingUpdate(update, "update #"+update.ID+" merged", false)
				}
				rw.Unlock()
			}
			verbose("pull cycle ended")
		}
	}
}

// filterIncomingUpdate filters incoming updates and process them accordingly.
// Note that lock should be acquired by the caller function
func filterIncomingUpdate(update Update, logMsg string, toPush bool) {
	// check existence and subscription
	if len(updateMap[update.ID].ID) == 0 && checkSubscription(self.Subscription, update.ID) {
		updateMap[update.ID] = update
		updateLog = append(updateLog, update.ID)
		if logMsg != "" {
			verbose(logMsg)
		}
		if strings.HasPrefix(update.ID, NodeIDPrefix) {
			// if it is a node update, process it internally
			node := DecodeToPeer(update.Data)
			peerManager.AddOrUpdatePeer(&node)
		} else {
			// otherwise, push it to client
			querySendChan <- update
		}
		if toPush {
			// further, push the update to peers
			pendingPushQueue <- PendingPush{
				Update:    update,
				UpdateLog: updateLog,
			}
		}
	}
}

func verbose(str string) {
	if printLog {
		log.Println("[lib-gossip] " + str)
	}
}
