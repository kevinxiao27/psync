package meta

import (
	"encoding/json"
	"testing"
)

func TestVectorClockIncrement(t *testing.T) {
	vc := make(VectorClock)

	vc.Increment("peer-a")
	if vc["peer-a"] != 1 {
		t.Errorf("Expected 1, got %d", vc["peer-a"])
	}

	vc.Increment("peer-a")
	if vc["peer-a"] != 2 {
		t.Errorf("Expected 2, got %d", vc["peer-a"])
	}

	vc.Increment("peer-b")
	if vc["peer-b"] != 1 {
		t.Errorf("Expected 1, got %d", vc["peer-b"])
	}
}

func TestVectorClockMerge(t *testing.T) {
	vc1 := VectorClock{"peer-a": 3, "peer-b": 1}
	vc2 := VectorClock{"peer-a": 1, "peer-b": 5, "peer-c": 2}

	vc1.Merge(vc2)

	expected := VectorClock{"peer-a": 3, "peer-b": 5, "peer-c": 2}
	for peer, count := range expected {
		if vc1[peer] != count {
			t.Errorf("peer %s: expected %d, got %d", peer, count, vc1[peer])
		}
	}
}

func TestVectorClockCompare(t *testing.T) {
	tests := []struct {
		name     string
		vc1      VectorClock
		vc2      VectorClock
		expected int
	}{
		{
			name:     "equal clocks",
			vc1:      VectorClock{"a": 1, "b": 2},
			vc2:      VectorClock{"a": 1, "b": 2},
			expected: 0,
		},
		{
			name:     "vc1 dominates",
			vc1:      VectorClock{"a": 2, "b": 3},
			vc2:      VectorClock{"a": 1, "b": 2},
			expected: 1,
		},
		{
			name:     "vc2 dominates",
			vc1:      VectorClock{"a": 1, "b": 2},
			vc2:      VectorClock{"a": 2, "b": 3},
			expected: -1,
		},
		{
			name:     "concurrent - neither dominates",
			vc1:      VectorClock{"a": 2, "b": 1},
			vc2:      VectorClock{"a": 1, "b": 2},
			expected: 0,
		},
		{
			name:     "missing peer in vc1",
			vc1:      VectorClock{"a": 1},
			vc2:      VectorClock{"a": 1, "b": 1},
			expected: -1,
		},
		{
			name:     "missing peer in vc2",
			vc1:      VectorClock{"a": 1, "b": 1},
			vc2:      VectorClock{"a": 1},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.vc1.Compare(tt.vc2)
			if result != tt.expected {
				t.Errorf("Compare() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestVectorClockClone(t *testing.T) {
	original := VectorClock{"a": 1, "b": 2}
	clone := original.Clone()

	// Modify original
	original.Increment("a")

	// Clone should be unchanged
	if clone["a"] != 1 {
		t.Errorf("Clone was modified: expected 1, got %d", clone["a"])
	}
}

func TestSyncMessageMarshal(t *testing.T) {
	msg := SyncMessage{
		Type:     MsgTypeInit,
		SourceID: "peer-a",
		Payload:  json.RawMessage(`{"peer_id":"peer-a","root_hash":"abc123"}`),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded SyncMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Type != MsgTypeInit {
		t.Errorf("Type mismatch: expected %s, got %s", MsgTypeInit, decoded.Type)
	}
	if decoded.SourceID != "peer-a" {
		t.Errorf("SourceID mismatch: expected peer-a, got %s", decoded.SourceID)
	}
}

func TestFileStateTombstone(t *testing.T) {
	state := FileState{
		Info:      FileInfo{Path: "deleted.txt"},
		Version:   VectorClock{"peer-a": 5},
		Tombstone: true,
	}

	if !state.Tombstone {
		t.Error("Expected Tombstone to be true")
	}
}
