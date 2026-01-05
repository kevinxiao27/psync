package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/kevinxiao27/psync/daemon/merkle"
	"github.com/kevinxiao27/psync/daemon/meta"
	engine "github.com/kevinxiao27/psync/daemon/sync"
	"github.com/kevinxiao27/psync/daemon/transport"
	"github.com/kevinxiao27/psync/daemon/watcher"
)

func main() {
	// 1. Flags
	rootPath := flag.String("root", ".", "Path to the directory to sync")
	signalURL := flag.String("signal", "ws://localhost:8080/ws", "Signal server URL")
	peerIDStr := flag.String("id", "", "Unique Peer ID (default: hostname)")
	groupID := flag.String("group", "default-group", "Sync group ID")
	debounce := flag.Duration("debounce", 500*time.Millisecond, "Debounce duration for file changes")
	flag.Parse()

	if *peerIDStr == "" {
		hostname, _ := os.Hostname()
		*peerIDStr = hostname
	}
	peerID := meta.PeerID(*peerIDStr)

	log.Printf("Starting PSync Daemon for peer %s", peerID)

	// 2. Context and WaitGroup for startup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	var tree *merkle.Tree
	var wt transport.Transport

	go func() {
		defer wg.Done()
		log.Println("Building Merkle tree...")
		// Explicitly ignore .psync directory when building the tree
		t, err := merkle.Build(*rootPath, []string{".psync"})
		if err != nil {
			log.Fatalf("Failed to build Merkle tree: %v", err)
		}
		tree = t
		log.Printf("Merkle tree ready. Root hash: %s", tree.Root.Hash)
	}()

	wt = transport.NewWebRTCTransport()
	go func() {
		defer wg.Done()
		log.Println("Connecting to signal server...")
		err := wt.Connect(ctx, *signalURL, transport.PeerID(peerID), *groupID)
		if err != nil {
			log.Fatalf("Failed to connect to signal server: %v", err)
		}
		log.Println("Connected to signal server and registered.")
	}()

	wg.Wait()
	log.Println("Startup complete. Active phase started.")

	fs_watch, err := watcher.NewWatcher(*rootPath, *debounce)
	if err != nil {
		log.Fatalf("Failed to initialize watcher: %v", err)
	}

	engine := engine.NewEngine(peerID, *rootPath, wt, fs_watch, tree)

	if err := fs_watch.Start(ctx); err != nil {
		log.Fatalf("Failed to start watcher: %v", err)
	}

	if err := engine.Start(ctx); err != nil {
		log.Fatalf("Failed to start sync engine: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down...", sig)
	case <-ctx.Done():
	}

	cancel()
	wt.Close()
	log.Println("Daemon stopped.")
}
