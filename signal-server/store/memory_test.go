package store

import (
	"testing"
	"time"

	"github.com/kevinxiao27/psync/signal-server/types"
)

func TestAddAndGetPeer(t *testing.T) {
	s := NewMemoryStore()

	peer := types.Peer{
		ID:       "peer-1",
		GroupID:  "group-1",
		LastSeen: time.Now(),
	}

	if err := s.AddPeer(peer); err != nil {
		t.Fatalf("AddPeer failed: %v", err)
	}

	got, ok := s.GetPeer("group-1", "peer-1")
	if !ok {
		t.Fatal("GetPeer returned not found")
	}
	if got.ID != peer.ID {
		t.Errorf("Expected ID %s, got %s", peer.ID, got.ID)
	}
}

func TestGetPeerNotFound(t *testing.T) {
	s := NewMemoryStore()

	_, ok := s.GetPeer("nonexistent", "peer-1")
	if ok {
		t.Error("Expected not found for nonexistent group")
	}
}

func TestRemovePeer(t *testing.T) {
	s := NewMemoryStore()

	peer := types.Peer{ID: "peer-1", GroupID: "group-1", LastSeen: time.Now()}
	s.AddPeer(peer)

	s.RemovePeer("group-1", "peer-1")

	_, ok := s.GetPeer("group-1", "peer-1")
	if ok {
		t.Error("Peer should have been removed")
	}
}

func TestRemovePeerCleansEmptyGroup(t *testing.T) {
	s := NewMemoryStore()

	peer := types.Peer{ID: "peer-1", GroupID: "group-1", LastSeen: time.Now()}
	s.AddPeer(peer)
	s.RemovePeer("group-1", "peer-1")

	// Verify group is cleaned up
	if len(s.groups) != 0 {
		t.Errorf("Expected empty groups map, got %d groups", len(s.groups))
	}
}

func TestGetPeersInGroup(t *testing.T) {
	s := NewMemoryStore()

	s.AddPeer(types.Peer{ID: "peer-1", GroupID: "group-1", LastSeen: time.Now()})
	s.AddPeer(types.Peer{ID: "peer-2", GroupID: "group-1", LastSeen: time.Now()})
	s.AddPeer(types.Peer{ID: "peer-3", GroupID: "group-2", LastSeen: time.Now()})

	peers := s.GetPeersInGroup("group-1")
	if len(peers) != 2 {
		t.Errorf("Expected 2 peers in group-1, got %d", len(peers))
	}
}

func TestUpdatePeerActivity(t *testing.T) {
	s := NewMemoryStore()

	oldTime := time.Now().Add(-time.Hour)
	peer := types.Peer{ID: "peer-1", GroupID: "group-1", LastSeen: oldTime}
	s.AddPeer(peer)

	err := s.UpdatePeerActivity(peer)
	if err != nil {
		t.Fatalf("UpdatePeerActivity failed: %v", err)
	}

	got, _ := s.GetPeer("group-1", "peer-1")
	if !got.LastSeen.After(oldTime) {
		t.Error("LastSeen should have been updated")
	}
}

func TestUpdatePeerActivityNotFound(t *testing.T) {
	s := NewMemoryStore()

	err := s.UpdatePeerActivity(types.Peer{ID: "nonexistent", GroupID: "group-1"})
	if err == nil {
		t.Error("Expected error for nonexistent peer")
	}
}

func TestPruneStale(t *testing.T) {
	s := NewMemoryStore()

	// Add a fresh peer
	s.AddPeer(types.Peer{ID: "fresh", GroupID: "group-1", LastSeen: time.Now()})

	// Add a stale peer
	s.AddPeer(types.Peer{ID: "stale", GroupID: "group-1", LastSeen: time.Now().Add(-time.Hour)})

	pruned := s.PruneStale(30 * time.Minute)
	if pruned != 1 {
		t.Errorf("Expected 1 pruned, got %d", pruned)
	}

	_, ok := s.GetPeer("group-1", "stale")
	if ok {
		t.Error("Stale peer should have been pruned")
	}

	_, ok = s.GetPeer("group-1", "fresh")
	if !ok {
		t.Error("Fresh peer should still exist")
	}
}

func TestGroupIsolation(t *testing.T) {
	s := NewMemoryStore()

	s.AddPeer(types.Peer{ID: "peer-1", GroupID: "group-1", LastSeen: time.Now()})
	s.AddPeer(types.Peer{ID: "peer-1", GroupID: "group-2", LastSeen: time.Now()})

	// Same peer ID in different groups should be separate
	peers1 := s.GetPeersInGroup("group-1")
	peers2 := s.GetPeersInGroup("group-2")

	if len(peers1) != 1 || len(peers2) != 1 {
		t.Error("Groups should be isolated")
	}
}
