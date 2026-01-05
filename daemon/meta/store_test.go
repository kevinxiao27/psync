package meta

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadState_EmptyDir(t *testing.T) {
	// Given a directory with no .psync folder
	dir := t.TempDir()

	// When we load state
	state, err := LoadState(dir)

	// Then we should get an empty state with no error
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if state == nil {
		t.Fatal("Expected non-nil state")
	}
	if len(state.FileStates) != 0 {
		t.Errorf("Expected empty FileStates, got %d entries", len(state.FileStates))
	}
}

func TestLoadState_ExistingFile(t *testing.T) {
	// Given a directory with existing metadata
	dir := t.TempDir()
	psyncDir := filepath.Join(dir, ".psync")
	os.MkdirAll(psyncDir, 0755)

	metadata := `{
		"root_hash": "abc123",
		"file_states": {
			"test.txt": {
				"info": {"path": "test.txt", "hash": "def456", "size": 100},
				"version": {"peer-a": 1}
			}
		}
	}`
	os.WriteFile(filepath.Join(psyncDir, "metadata.json"), []byte(metadata), 0644)

	// When we load state
	state, err := LoadState(dir)

	// Then we should get the persisted state
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if state.RootHash != "abc123" {
		t.Errorf("Expected RootHash 'abc123', got '%s'", state.RootHash)
	}
	if len(state.FileStates) != 1 {
		t.Errorf("Expected 1 FileState, got %d", len(state.FileStates))
	}
}

func TestSaveState_CreatesDir(t *testing.T) {
	// Given a directory with no .psync folder
	dir := t.TempDir()

	state := &State{
		RootHash:   "test-hash",
		FileStates: make(map[string]FileState),
	}

	// When we save state
	err := SaveState(dir, state)

	// Then .psync should be created and metadata saved
	if err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	metaPath := filepath.Join(dir, ".psync", "metadata.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Error("metadata.json was not created")
	}
}

func TestSaveState_Atomic(t *testing.T) {
	// Given an existing metadata file
	dir := t.TempDir()
	psyncDir := filepath.Join(dir, ".psync")
	os.MkdirAll(psyncDir, 0755)
	os.WriteFile(filepath.Join(psyncDir, "metadata.json"), []byte("old"), 0644)

	state := &State{
		RootHash:   "new-hash",
		FileStates: make(map[string]FileState),
	}

	// When we save state
	err := SaveState(dir, state)

	// Then there should be no .tmp file left over (atomic rename)
	if err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	tmpPath := filepath.Join(psyncDir, "metadata.json.tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("Temporary file was not cleaned up")
	}
}
