package sync

import (
	"strings"
	"testing"

	"github.com/kevinxiao27/psync/daemon/meta"
)

func TestResolve_LocalDominates(t *testing.T) {
	resolver := NewResolver("peer-a")

	local := meta.FileState{
		Info:    meta.FileInfo{Path: "test.txt", Hash: "hash1"},
		Version: meta.VectorClock{"peer-a": 2, "peer-b": 1},
	}
	remote := meta.FileState{
		Info:    meta.FileInfo{Path: "test.txt", Hash: "hash2"},
		Version: meta.VectorClock{"peer-a": 1, "peer-b": 1},
	}

	result := resolver.Resolve(local, remote, "peer-b")

	if result.Type != ResolutionLocalDominates {
		t.Errorf("Expected LocalDominates, got %v", result.Type)
	}
}

func TestResolve_RemoteDominates(t *testing.T) {
	resolver := NewResolver("peer-a")

	local := meta.FileState{
		Info:    meta.FileInfo{Path: "test.txt", Hash: "hash1"},
		Version: meta.VectorClock{"peer-a": 1, "peer-b": 1},
	}
	remote := meta.FileState{
		Info:    meta.FileInfo{Path: "test.txt", Hash: "hash2"},
		Version: meta.VectorClock{"peer-a": 2, "peer-b": 2},
	}

	result := resolver.Resolve(local, remote, "peer-b")

	if result.Type != ResolutionRemoteDominates {
		t.Errorf("Expected RemoteDominates, got %v", result.Type)
	}
}

func TestResolve_Concurrent_LocalWins(t *testing.T) {
	// peer-a < peer-b alphabetically, so local (peer-a) wins
	resolver := NewResolver("peer-a")

	local := meta.FileState{
		Info:    meta.FileInfo{Path: "test.txt", Hash: "hash1"},
		Version: meta.VectorClock{"peer-a": 2},
	}
	remote := meta.FileState{
		Info:    meta.FileInfo{Path: "test.txt", Hash: "hash2"},
		Version: meta.VectorClock{"peer-b": 2},
	}

	result := resolver.Resolve(local, remote, "peer-b")

	if result.Type != ResolutionConflict {
		t.Errorf("Expected Conflict, got %v", result.Type)
	}
	if result.LocalWins != true {
		t.Error("Expected LocalWins to be true (peer-a < peer-b)")
	}
}

func TestResolve_Concurrent_RemoteWins(t *testing.T) {
	// peer-b > peer-a alphabetically, so remote (peer-a) wins
	resolver := NewResolver("peer-b")

	local := meta.FileState{
		Info:    meta.FileInfo{Path: "test.txt", Hash: "hash1"},
		Version: meta.VectorClock{"peer-b": 2},
	}
	remote := meta.FileState{
		Info:    meta.FileInfo{Path: "test.txt", Hash: "hash2"},
		Version: meta.VectorClock{"peer-a": 2},
	}

	result := resolver.Resolve(local, remote, "peer-a")

	if result.Type != ResolutionConflict {
		t.Errorf("Expected Conflict, got %v", result.Type)
	}
	if result.LocalWins != false {
		t.Error("Expected LocalWins to be false (peer-a < peer-b)")
	}
}

func TestResolve_EqualHashes(t *testing.T) {
	resolver := NewResolver("peer-a")

	// Clocks are concurrent but hashes match - no action needed
	local := meta.FileState{
		Info:    meta.FileInfo{Path: "test.txt", Hash: "same-hash"},
		Version: meta.VectorClock{"peer-a": 2},
	}
	remote := meta.FileState{
		Info:    meta.FileInfo{Path: "test.txt", Hash: "same-hash"},
		Version: meta.VectorClock{"peer-b": 2},
	}

	result := resolver.Resolve(local, remote, "peer-b")

	if result.Type != ResolutionEqual {
		t.Errorf("Expected Equal, got %v", result.Type)
	}
}

func TestGenerateConflictPath(t *testing.T) {
	path := GenerateConflictPath("notes/todo.md", "peer-b")

	// Should contain "conflict" and peer ID
	if !strings.Contains(path, "conflict") {
		t.Errorf("Path should contain 'conflict': %s", path)
	}
	if !strings.Contains(path, "peer-b") {
		t.Errorf("Path should contain peer ID: %s", path)
	}
	if !strings.HasSuffix(path, ".md") {
		t.Errorf("Path should preserve extension: %s", path)
	}
}
