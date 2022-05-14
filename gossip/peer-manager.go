package gossip

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

type Peer struct {
	Identifier   string
	GossipAddr   string
	APIAddr      string
	Type         string // "tracker" or "miner"
	Subscription int

	Active        bool
	LastAnnounced time.Time // peer's local time of its last announcement
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

// -------------------------------------------------------//

type PeerManager struct {
	Self *Peer

	mu       sync.Mutex
	PeerList []Peer
}

func (pm *PeerManager) SetPeers(peers []Peer) {
	// find self
	i := 0
	for ; i < len(peers); i++ {
		if peers[i].Identifier == pm.Self.Identifier {
			break
		}
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()
	// exclude self
	if i < len(peers) {
		pm.PeerList = append(peers[:i], peers[i+1:]...)
	} else {
		pm.PeerList = peers
	}
}

// AddOrUpdatePeer adds a peer to the list or update its info if the peer exists
func (pm *PeerManager) AddOrUpdatePeer(peer *Peer) {
	if peer.Identifier != pm.Self.Identifier {
		pm.mu.Lock()
		defer pm.mu.Unlock()
		for idx, p := range pm.PeerList {
			if p.Identifier == peer.Identifier {
				// existing peer, update its info if it has been re-announced
				if peer.LastAnnounced.After(p.LastAnnounced) {
					pm.PeerList[idx] = *peer
					verbose("update a peer <" + peer.Identifier + "> [" + peer.Type + "] at " + peer.GossipAddr)
				}
				return
			}
		}
		// peer does not exist, add it to list
		pm.PeerList = append(pm.PeerList, *peer)
		verbose("discover a new peer <" + peer.Identifier + "> [" + peer.Type + "] at " + peer.GossipAddr)
	}
}

func (pm *PeerManager) GetPeers() []Peer {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	peer := pm.PeerList[:]
	return peer
}

// SetActive sets a peer to be active. If the peer is not known to the manager, then it will be ignored.
func (pm *PeerManager) SetActive(peer *Peer) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i := 0; i < len(pm.PeerList); i++ {
		if pm.PeerList[i].Identifier == peer.Identifier {
			if !pm.PeerList[i].Active {
				// update the entire peer info in case it has changed due to rejoin, etc.
				peer.Active = true
				pm.PeerList[i] = *peer
				verbose("peer <" + peer.Identifier + "> is now active")
			}
			break
		}
	}
}

func (pm *PeerManager) SetInactive(peer *Peer) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i := range pm.PeerList {
		if pm.PeerList[i].Identifier == peer.Identifier {
			if pm.PeerList[i].Active {
				pm.PeerList[i].Active = false
				verbose("peer <" + peer.Identifier + "> at " + peer.GossipAddr + " is now inactive")
			}
			break
		}
	}
}

func (pm *PeerManager) SelectPeers(targetSubs int) []Peer {
	pm.mu.Lock()
	peers := pm.PeerList[:]
	pm.mu.Unlock()
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
