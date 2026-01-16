package meta

// -----------------------------------------------------------------------------
// VectorClock Methods
// -----------------------------------------------------------------------------

import "maps"

// Increment increases the counter for the given peer.
func (vc VectorClock) Increment(peer PeerID) {
	vc[peer]++
}

// Merge combines two vector clocks, taking the max of each entry.
func (vc VectorClock) Merge(other VectorClock) {
	for peer, count := range other {
		if count > vc[peer] {
			vc[peer] = count
		}
	}
}

// CompareResult constants for VectorClock.Compare
const (
	VCLess       = -1 // vc < other (remote dominates)
	VCEqual      = 0  // vc == other (identical)
	VCGreater    = 1  // vc > other (local dominates)
	VCConcurrent = 2  // Neither dominates (conflict)
)

// Compare returns the relationship between two vector clocks.
// Returns: VCLess (-1), VCEqual (0), VCGreater (1), or VCConcurrent (2)
func (vc VectorClock) Compare(other VectorClock) int {
	less, greater := false, false

	// Check all keys from both clocks
	allPeers := make(map[PeerID]struct{})
	for p := range vc {
		allPeers[p] = struct{}{}
	}
	for p := range other {
		allPeers[p] = struct{}{}
	}

	for peer := range allPeers {
		v1, v2 := vc[peer], other[peer]
		if v1 < v2 {
			less = true
		}
		if v1 > v2 {
			greater = true
		}
	}

	if less && greater {
		return VCConcurrent // Concurrent - neither dominates
	}
	if less {
		return VCLess // vc < other
	}
	if greater {
		return VCGreater // vc > other
	}
	return VCEqual // Identical clocks
}

// Clone returns a deep copy of the vector clock.
func (vc VectorClock) Clone() VectorClock {
	clone := make(VectorClock, len(vc))
	maps.Copy(clone, vc)
	return clone
}
