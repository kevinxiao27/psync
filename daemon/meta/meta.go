package meta

// -----------------------------------------------------------------------------
// VectorClock Methods
// -----------------------------------------------------------------------------

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

// Compare returns the relationship between two vector clocks.
// Returns: -1 if vc < other, 0 if concurrent, 1 if vc > other
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
		return 0 // Concurrent
	}
	if less {
		return -1
	}
	if greater {
		return 1
	}
	return 0 // Equal
}

// Clone returns a deep copy of the vector clock.
func (vc VectorClock) Clone() VectorClock {
	clone := make(VectorClock, len(vc))
	for k, v := range vc {
		clone[k] = v
	}
	return clone
}
