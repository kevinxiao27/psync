package vclock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State represents the persisted sync state.
type State struct {
	RootHash   string               `json:"root_hash"`
	FileStates map[string]FileState `json:"file_states"` // Path -> State
}

// LoadState loads the sync state from the .psync/metadata.json file.
func LoadState(root string) (*State, error) {
	metaDir := filepath.Join(root, ".psync")
	metaPath := filepath.Join(metaDir, "metadata.json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				FileStates: make(map[string]FileState),
			}, nil
		}
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	if state.FileStates == nil {
		state.FileStates = make(map[string]FileState)
	}

	return &state, nil
}

// SaveState saves the sync state to the .psync/metadata.json file.
func SaveState(root string, state *State) error {
	metaDir := filepath.Join(root, ".psync")
	metaPath := filepath.Join(metaDir, "metadata.json")

	// Ensure .psync exists
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		return fmt.Errorf("failed to create .psync directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}

	// Write to temporary file first for atomicity
	tmpPath := metaPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary metadata: %w", err)
	}

	if err := os.Rename(tmpPath, metaPath); err != nil {
		return fmt.Errorf("failed to finalize metadata: %w", err)
	}

	return nil
}
