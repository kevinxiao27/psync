package sync

import (
	"context"
	"log"

	"github.com/kevinxiao27/psync/daemon/merkle"
	"github.com/kevinxiao27/psync/daemon/meta"
	"github.com/kevinxiao27/psync/daemon/transport"
	"github.com/kevinxiao27/psync/daemon/watcher"
)

// Engine orchestrates the synchronization process.
type Engine struct {
	localID   meta.PeerID
	rootPath  string
	transport transport.Transport
	watcher   *watcher.Watcher
	tree      *merkle.Tree
}

// NewEngine creates a new Sync Engine.
func NewEngine(
	id meta.PeerID,
	root string,
	t transport.Transport,
	w *watcher.Watcher,
	tree *merkle.Tree,
) *Engine {
	return &Engine{
		localID:   id,
		rootPath:  root,
		transport: t,
		watcher:   w,
		tree:      tree,
	}
}

// Start begins the engine's processing loop.
func (e *Engine) Start(ctx context.Context) error {
	log.Printf("Sync Engine starting for peer %s at %s", e.localID, e.rootPath)

	// Set up transport handlers
	e.transport.OnMessage(e.handleIncomingMessage)
	e.transport.OnPeerConnected(e.handlePeerConnected)
	e.transport.OnPeerDisconnected(e.handlePeerDisconnected)

	// Start processing events
	go e.processEvents(ctx)

	return nil
}

// processEvents handles both local file events and remote messages.
func (e *Engine) processEvents(ctx context.Context) {
	watcherEvents := e.watcher.Events()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcherEvents:
			if !ok {
				return
			}
			e.handleLocalEvent(event)
		}
	}
}

// handleLocalEvent processes changes from the local watcher.
func (e *Engine) handleLocalEvent(event watcher.Event) {
	log.Printf("Local event: %s %s", event.Type, event.Path)
	// TODO: Update Merkle tree, increment vector clock, and broadcast changes
}

// handleIncomingMessage processes messages from WebRTC peers.
func (e *Engine) handleIncomingMessage(peerID transport.PeerID, data []byte) {
	log.Printf("Incoming message from %s: %d bytes", peerID, len(data))
	// TODO: Unmarshal SyncMessage and handle based on type (Init, FileList, etc.)
}

// handlePeerConnected handles new peer connections.
func (e *Engine) handlePeerConnected(peerID transport.PeerID) {
	log.Printf("Peer connected: %s", peerID)
	// TODO: Send Init message with current root hash
}

// handlePeerDisconnected handles peer disconnections.
func (e *Engine) handlePeerDisconnected(peerID transport.PeerID) {
	log.Printf("Peer disconnected: %s", peerID)
}
