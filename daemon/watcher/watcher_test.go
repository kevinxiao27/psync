package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWatcher_Events(t *testing.T) {
	root := t.TempDir()
	// Use a short debounce for testing
	debounce := 10 * time.Millisecond
	w, err := NewWatcher(root, debounce)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}

	// Helper to collect events
	var events []Event
	var mu sync.Mutex
	collectDone := make(chan struct{})

	go func() {
		defer close(collectDone)
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-w.Events():
				mu.Lock()
				events = append(events, e)
				mu.Unlock()
			}
		}
	}()

	// 1. Create a file
	testFile := filepath.Join(root, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Wait for debounce + buffer
	time.Sleep(100 * time.Millisecond)

	// Verify Create/Modify
	mu.Lock()
	foundCreate := false
	for _, e := range events {
		if e.Path == "test.txt" && (e.Type == EventCreate || e.Type == EventModify) {
			foundCreate = true
			break
		}
	}
	mu.Unlock()

	if !foundCreate {
		t.Error("Expected Create/Modify event for test.txt")
	}

	// 2. Modify the file
	mu.Lock() // Clear previous events to avoid confusion if needed, though we can just check appending
	events = nil
	mu.Unlock()

	if err := os.WriteFile(testFile, []byte("world"), 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	foundModify := false
	for _, e := range events {
		if e.Path == "test.txt" && e.Type == EventModify {
			foundModify = true
			break
		}
	}
	mu.Unlock()

	if !foundModify {
		t.Error("Expected Modify event for test.txt")
	}

	// 3. Rename file
	mu.Lock()
	events = nil
	mu.Unlock()

	newName := filepath.Join(root, "moved.txt")
	if err := os.Rename(testFile, newName); err != nil {
		t.Fatalf("Failed to rename file: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	foundRename := false
	for _, e := range events {
		if e.Path == "moved.txt" && (e.Type == EventRename || e.Type == EventCreate) {
			foundRename = true
		}
	}
	mu.Unlock()

	if !foundRename {
		t.Error("Expected Rename/Create event for moved.txt")
	}

	// 4. Delete file
	mu.Lock()
	events = nil
	mu.Unlock()

	if err := os.Remove(newName); err != nil {
		t.Fatalf("Failed to remove file: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	foundDelete := false
	for _, e := range events {
		// fsnotify behavior can vary, sometimes rename/delete might look different
		if e.Path == "moved.txt" && (e.Type == EventDelete || e.Type == EventRename) {
			foundDelete = true
		}
	}
	mu.Unlock()

	if !foundDelete {
		t.Error("Expected Delete event for moved.txt")
	}
}

func TestWatcher_Recursive(t *testing.T) {
	root := t.TempDir()
	w, err := NewWatcher(root, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	// Create subdirectory
	subDir := filepath.Join(root, "subdir")
	os.Mkdir(subDir, 0755)
	time.Sleep(100 * time.Millisecond)

	// Create file in subdir
	subFile := filepath.Join(subDir, "sub.txt")
	os.WriteFile(subFile, []byte("content"), 0644)
	time.Sleep(100 * time.Millisecond)

	// Check events
	select {
	case e := <-w.Events():
		// Could get directory create event first
		if e.Path == "subdir" {
			// Consume next
			select {
			case e2 := <-w.Events():
				if e2.Path == filepath.Join("subdir", "sub.txt") {
					return
				}
				t.Errorf("Unexpected second event: %v", e2)
			case <-time.After(1 * time.Second):
				t.Fatal("Timeout waiting for subdir file event")
			}
		} else if e.Path == filepath.Join("subdir", "sub.txt") {
			return
		} else {
			// consume potential other events?
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for recursive event")
	}
}

func TestWatcher_Ignore(t *testing.T) {
	root := t.TempDir()
	w, err := NewWatcher(root, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	// Create ignore dir
	ignoreDir := filepath.Join(root, ".psync")
	os.Mkdir(ignoreDir, 0755)

	// Modify file in ignore dir
	ignoredFile := filepath.Join(ignoreDir, "metadata.json")
	os.WriteFile(ignoredFile, []byte("{}"), 0644)
	time.Sleep(100 * time.Millisecond)

	select {
	case e := <-w.Events():
		t.Fatalf("Should ignore .psync events, got: %v", e)
	default:
		// success
	}
}
