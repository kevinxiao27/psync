package sync

import (
	"fmt"
	"time"

	"github.com/kevinxiao27/psync/daemon/meta"
)

// ResolutionType defines the outcome of a state comparison.
type ResolutionType int

const (
	ResolutionEqual ResolutionType = iota
	ResolutionLocalDominates
	ResolutionRemoteDominates
	ResolutionConflict
)

// Resolution describes how a state difference should be handled.
type Resolution struct {
	Type      ResolutionType
	LocalWins bool // Only meaningful when Type == ResolutionConflict
}

// Resolver handles comparing local and remote file states.
type Resolver struct {
	localID meta.PeerID
}

// NewResolver creates a new Resolver.
func NewResolver(localID meta.PeerID) *Resolver {
	return &Resolver{localID: localID}
}

// Resolve compares two file states and determines the relationship.
func (r *Resolver) Resolve(local, remote meta.FileState, remoteID meta.PeerID) Resolution {
	// Compare version clocks
	cmp := local.Version.Compare(remote.Version)

	switch cmp {
	case meta.VCGreater:
		// Local dominates
		return Resolution{Type: ResolutionLocalDominates}
	case meta.VCLess:
		// Remote dominates
		return Resolution{Type: ResolutionRemoteDominates}
	case meta.VCEqual:
		// Clocks are identical - files should be the same
		return Resolution{Type: ResolutionEqual}
	case meta.VCConcurrent:
		// Clocks are concurrent - conflict!
		// If hashes match, they converged independently (no action needed)
		if local.Info.Hash == remote.Info.Hash && local.Tombstone == remote.Tombstone {
			return Resolution{Type: ResolutionEqual}
		}
		// Otherwise, determine winner by peer ID (alphabetically lower wins)
		localWins := r.localID < remoteID
		return Resolution{
			Type:      ResolutionConflict,
			LocalWins: localWins,
		}
	default:
		// Should never happen
		return Resolution{Type: ResolutionEqual}
	}
}

// GenerateConflictPath creates a new path for a conflicting file version.
// Format: "dir/filename (conflict-PEERID-YYYYMMDD-HHMMSS).ext"
func GenerateConflictPath(originalPath string, peerID meta.PeerID) string {
	ext := ""
	base := originalPath

	// Find extension
	for i := len(originalPath) - 1; i >= 0 && originalPath[i] != '/' && originalPath[i] != '\\'; i-- {
		if originalPath[i] == '.' {
			ext = originalPath[i:]
			base = originalPath[:i]
			break
		}
	}

	ts := time.Now().Format("20060102-150405")
	return fmt.Sprintf("%s (conflict-%s-%s)%s", base, peerID, ts, ext)
}
