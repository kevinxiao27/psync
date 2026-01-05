package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/kevinxiao27/psync/signal-server/types"
)

// PeerStore defines the interface for managing peer state.
// (Interface useful for mocking or swapping backends later)
type PeerStore interface {
	AddPeer(p types.Peer) error
	GetPeer(groupID string, peerID types.PeerID) (types.Peer, bool)
	UpdatePeerActivity(p types.Peer) error
	RemovePeer(groupID string, peerID types.PeerID)
	GetPeersInGroup(groupID string) []types.Peer
	GetAllPeers() []types.Peer           // For testing and monitoring
	PruneStale(maxAge time.Duration) int // Returns count of pruned peers
}

// MemoryStore is an in-memory implementation of PeerStore.
type MemoryStore struct {
	groups map[string]map[types.PeerID]types.Peer
	mu     sync.RWMutex
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		groups: make(map[string]map[types.PeerID]types.Peer),
	}
}

func (s *MemoryStore) AddPeer(p types.Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.groups[p.GroupID] == nil {
		s.groups[p.GroupID] = make(map[types.PeerID]types.Peer)
	}
	s.groups[p.GroupID][p.ID] = p
	return nil
}

func (s *MemoryStore) GetPeer(groupID string, peerID types.PeerID) (types.Peer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	group, ok := s.groups[groupID]
	if !ok {
		return types.Peer{}, false
	}
	p, ok := group[peerID]
	return p, ok
}

func (s *MemoryStore) UpdatePeerActivity(p types.Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	group, ok := s.groups[p.GroupID]
	if !ok {
		return fmt.Errorf("group not found")
	}
	peer, ok := group[p.ID]
	if !ok {
		return fmt.Errorf("peer not found")
	}

	peer.LastSeen = time.Now()
	group[p.ID] = peer

	return nil
}

func (s *MemoryStore) RemovePeer(groupID string, peerID types.PeerID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if group, ok := s.groups[groupID]; ok {
		delete(group, peerID)
		if len(group) == 0 {
			delete(s.groups, groupID)
		}
	}
}

func (s *MemoryStore) GetPeersInGroup(groupID string) []types.Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var peers []types.Peer
	if group, ok := s.groups[groupID]; ok {
		for _, p := range group {
			peers = append(peers, p)
		}
	}
	return peers
}

func (s *MemoryStore) GetAllPeers() []types.Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var peers []types.Peer
	for _, group := range s.groups {
		for _, p := range group {
			peers = append(peers, p)
		}
	}
	return peers
}

func (s *MemoryStore) PruneStale(maxAge time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	pruned := 0

	for groupID, group := range s.groups {
		for peerID, peer := range group {
			if peer.LastSeen.Before(cutoff) {
				delete(group, peerID)
				pruned++
			}
		}
		if len(group) == 0 {
			delete(s.groups, groupID)
		}
	}
	return pruned
}
