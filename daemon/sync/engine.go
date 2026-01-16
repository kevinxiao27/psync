package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"time"

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
	state     *meta.State
	resolver  *Resolver
}

// NewEngine creates a new Sync Engine.
func NewEngine(
	id meta.PeerID,
	root string,
	t transport.Transport,
	w *watcher.Watcher,
	tree *merkle.Tree,
) *Engine {
	// Initialize state
	state, err := meta.LoadState(root)
	if err != nil {
		log.Printf("Warning: failed to load state, starting fresh: %v", err)
		state = &meta.State{
			RootHash:   tree.Root.Hash,
			FileStates: make(map[string]meta.FileState),
		}
	} else {
		// Update root hash
		state.RootHash = tree.Root.Hash
	}

	return &Engine{
		localID:   id,
		rootPath:  root,
		transport: t,
		watcher:   w,
		tree:      tree,
		state:     state,
		resolver:  NewResolver(id),
	}
}

// Start begins the engine's processing loop.
func (engine *Engine) Start(ctx context.Context) error {
	log.Printf("Sync Engine starting for peer %s at %s", engine.localID, engine.rootPath)

	// Set up transport handlers
	engine.transport.OnMessage(engine.handleIncomingMessage)
	engine.transport.OnPeerConnected(engine.handlePeerConnected)
	engine.transport.OnPeerDisconnected(engine.handlePeerDisconnected)

	// Start processing events
	go engine.processEvents(ctx)

	return nil
}

// processEvents handles both local file events and remote messages.
func (engine *Engine) processEvents(ctx context.Context) {
	watcherEvents := engine.watcher.Events()
	ticker := time.NewTicker(30 * time.Second) // Heartbeat ticker
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcherEvents:
			if !ok {
				return
			}
			engine.handleLocalEvent(event)
		case <-ticker.C:
			engine.sendHeartbeat()
		}
	}
}

// handleLocalEvent processes changes from the local watcher.
func (engine *Engine) handleLocalEvent(event watcher.Event) {
	log.Printf("Local event: %s %s", event.Type, event.Path)

	// 1. Incrementally update Merkle tree
	_, err := engine.tree.UpdateNode(engine.rootPath, event.Path)
	if err != nil {
		log.Printf("Failed to update merkle tree incrementally: %v", err)
		newTree, err := merkle.Build(engine.rootPath, []string{".psync"})
		if err != nil {
			log.Printf("Failed to rebuild merkle tree: %v", err)
			return
		}
		engine.tree = newTree
	}

	// Update root hash
	engine.state.RootHash = engine.tree.Root.Hash

	// 2. Update FileState for the specific file
	// We need to find the node in the new tree
	node := engine.tree.GetNode(event.Path)

	// Increment Vector Clock
	fileState, exists := engine.state.FileStates[event.Path]
	if !exists {
		fileState = meta.FileState{
			Version: make(meta.VectorClock),
		}
	}

	// Update metadata
	var newHash string
	if node != nil {
		// File exists (Created or Modified)
		fileState.Info = meta.FileInfo{
			Path: event.Path,
			Hash: node.Hash,
			// Size and ModTime would come from node or os.Stat
		}
		fileState.Tombstone = false
		newHash = node.Hash
	} else {
		// File deleted
		fileState.Tombstone = true
		fileState.Info.Path = event.Path
		// Hash irrelevant for tombstone, keeping old one or empty
		newHash = ""
	}

	// Check if this is an echo event (hash hasn't changed)
	// This prevents incrementing version for files we just received from remote
	if exists && fileState.Info.Hash == newHash {
		log.Printf("Skipping echo event for %s (hash unchanged)", event.Path)
		return
	}

	// Increment our version
	fileState.Version.Increment(engine.localID)

	// Save back to state
	engine.state.FileStates[event.Path] = fileState

	// Persist state
	if err := meta.SaveState(engine.rootPath, engine.state); err != nil {
		log.Printf("Failed to save state: %v", err)
	}

	// 3. Broadcast new Root Hash
	engine.broadcastInit()
}

// broadcastInit sends an Init message to all connected peers.
func (engine *Engine) broadcastInit() {
	payload := meta.InitPayload{
		PeerID:   engine.localID,
		RootHash: engine.tree.Root.Hash,
	}

	log.Printf("Broadcasting Init with Hash %s", payload.RootHash)

	data, err := MarshalSyncMessage(meta.MsgTypeInit, engine.localID, payload)
	if err != nil {
		log.Printf("Failed to marshal init message: %v", err)
		return
	}

	if err := engine.transport.Broadcast(data); err != nil {
		log.Printf("Failed to broadcast init: %v", err)
	}
}

// handleIncomingMessage processes messages from WebRTC peers.
func (engine *Engine) handleIncomingMessage(peerID transport.PeerID, data []byte) {
	msg, err := UnmarshalSyncMessage(data)
	if err != nil {
		log.Printf("Failed to unmarshal message from %s: %v", peerID, err)
		return
	}

	switch msg.Type {
	case meta.MsgTypeInit:
		engine.handleInit(msg)
	case meta.MsgTypeFileList:
		engine.handleFileList(msg)
	case meta.MsgTypeFileRequest:
		engine.handleFileRequest(msg)
	case meta.MsgTypeGetFileList:
		engine.handleGetFileList(msg)
	case meta.MsgTypeFileData:
		engine.handleFileData(msg)
	default:
		log.Printf("Unknown message type from %s: %s", peerID, msg.Type)
	}
}

// handlePeerConnected handles new peer connections.
func (engine *Engine) handlePeerConnected(peerID transport.PeerID) {
	log.Printf("Peer connected: %s", peerID)

	// Send Init message to the new peer
	payload := meta.InitPayload{
		PeerID:   engine.localID,
		RootHash: engine.tree.Root.Hash,
	}

	data, err := MarshalSyncMessage(meta.MsgTypeInit, engine.localID, payload)
	if err != nil {
		log.Printf("Failed to marshal init message: %v", err)
		return
	}

	if err := engine.transport.SendTo(peerID, data); err != nil {
		log.Printf("Failed to send init message to %s: %v", peerID, err)
	}
}

// handlePeerDisconnected handles peer disconnections.
func (engine *Engine) handlePeerDisconnected(peerID transport.PeerID) {
	log.Printf("Peer disconnected: %s", peerID)
}

// handleInit processes an initialization message from a peer.
func (engine *Engine) handleInit(msg *meta.SyncMessage) {
	payload, err := ExtractInitPayload(msg)
	if err != nil {
		log.Printf("Invalid init payload: %v", err)
		return
	}

	log.Printf("Received Init from %s (Hash: %s)", payload.PeerID, payload.RootHash)

	// Compare root hashes
	if payload.RootHash != engine.tree.Root.Hash {
		log.Printf("Root hash mismatch with %s. Requesting file list.", payload.PeerID)
		engine.requestFileList(transport.PeerID(payload.PeerID))
	} else {
		log.Printf("Root hash matches with %s. In sync.", payload.PeerID)
	}
}

// handleGetFileList handles a request for our file list.
func (engine *Engine) handleGetFileList(msg *meta.SyncMessage) {
	log.Printf("Received GetFileList request from %s", msg.SourceID)

	// Collect all local file states
	var files []meta.FileState
	if engine.state != nil {
		for _, fs := range engine.state.FileStates {
			files = append(files, fs)
		}
	}

	payload := meta.FileListPayload{
		Files: files,
	}

	data, err := MarshalSyncMessage(meta.MsgTypeFileList, engine.localID, payload)
	if err != nil {
		log.Printf("Failed to marshal file list: %v", err)
		return
	}

	if err := engine.transport.SendTo(transport.PeerID(msg.SourceID), data); err != nil {
		log.Printf("Failed to send file list to %s: %v", msg.SourceID, err)
	}
}

func handleDeletion(engine *Engine, fileState meta.FileState) bool {
	fullPath := engine.resolvePath(fileState.Info.Path)
	if err := os.RemoveAll(fullPath); err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to delete local file %s: %v", fullPath, err)
		return false
	}

	localFileState := fileState
	localFileState.Version.Increment(engine.localID)
	engine.state.FileStates[fileState.Info.Path] = localFileState
	log.Printf("Applied remote deletion: %s", fileState.Info.Path)
	return true
}

// handleFileList processes a received list of files.
func (engine *Engine) handleFileList(msg *meta.SyncMessage) {
	payload, err := ExtractFileListPayload(msg)
	if err != nil {
		log.Printf("Invalid file list payload: %v", err)
		return
	}

	log.Printf("Received FileList from %s with %d files", msg.SourceID, len(payload.Files))

	var filesToRequest []string
	var needsRebuild bool

	for _, remoteFile := range payload.Files {
		localFile, exists := engine.state.FileStates[remoteFile.Info.Path]

		if !exists {
			// New file from remote
			if !remoteFile.Tombstone {
				log.Printf("New remote file detected: %s", remoteFile.Info.Path)
				filesToRequest = append(filesToRequest, remoteFile.Info.Path)
			} else {
				// Record deletion for unknown file
				needsRebuild = handleDeletion(engine, remoteFile)
			}
			continue
		}

		// Resolve conflict/update
		resolution := engine.resolver.Resolve(localFile, remoteFile, msg.SourceID)

		switch resolution.Type {
		case ResolutionRemoteDominates:
			if !remoteFile.Tombstone {
				log.Printf("Remote file newer: %s", remoteFile.Info.Path)
				filesToRequest = append(filesToRequest, remoteFile.Info.Path)
			} else {
				// Remote deletion dominates local
				handleDeletion(engine, remoteFile)
			}
		case ResolutionConflict:
			log.Printf("Conflict detected for %s", remoteFile.Info.Path)
			if resolution.LocalWins {
				log.Printf("Local version wins. Keeping local.")
			} else {
				// Remote wins - check if it's a deletion
				if !remoteFile.Tombstone {
					log.Printf("Remote version wins. Requesting remote.")
					filesToRequest = append(filesToRequest, remoteFile.Info.Path)
				} else {
					// Remote deletion wins conflict
					handleDeletion(engine, remoteFile)
				}
			}
		case ResolutionEqual, ResolutionLocalDominates:
			// No action needed
		}
	}

	// Apply deletions incrementally and save state
	if needsRebuild {
		// For deletions, each handleDeletion call already updated the tree
		// so we just need to ensure the root hash is current and broadcast
		engine.state.RootHash = engine.tree.Root.Hash

		if err := meta.SaveState(engine.rootPath, engine.state); err != nil {
			log.Printf("Failed to save state after deletions: %v", err)
		}

		// Broadcast new root hash after applying deletions
		engine.broadcastInit()
	}

	// Request any needed files
	if len(filesToRequest) > 0 {
		engine.requestFiles(transport.PeerID(msg.SourceID), filesToRequest)
	}
}

// requestFiles sends a FileRequest message for specific paths.
func (engine *Engine) requestFiles(peerID transport.PeerID, paths []string) {
	log.Printf("Requesting %d files from %s", len(paths), peerID)

	payload := meta.FileRequestPayload{
		Paths: paths,
	}

	data, err := MarshalSyncMessage(meta.MsgTypeFileRequest, engine.localID, payload)
	if err != nil {
		log.Printf("Failed to marshal file request: %v", err)
		return
	}

	if err := engine.transport.SendTo(peerID, data); err != nil {
		log.Printf("Failed to send file request to %s: %v", peerID, err)
	}
}

// handleFileRequest processes a request for specific files.
func (engine *Engine) handleFileRequest(msg *meta.SyncMessage) {
	payload, err := ExtractFileRequestPayload(msg)
	if err != nil {
		log.Printf("Invalid file request payload: %v", err)
		return
	}

	for _, path := range payload.Paths {
		// Verify we have the file
		fileState, exists := engine.state.FileStates[path]
		if !exists || fileState.Tombstone {
			log.Printf("Requested file not found or deleted: %s", path)
			continue
		}

		// Read file content
		fullPath := engine.resolvePath(path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			log.Printf("Failed to read requested file %s: %v", path, err)
			continue
		}

		// Send file data
		dataPayload := meta.FileDataPayload{
			Path:    path,
			Content: content,
			Version: fileState.Version,
		}

		data, err := MarshalSyncMessage(meta.MsgTypeFileData, engine.localID, dataPayload)
		if err != nil {
			log.Printf("Failed to marshal file data for %s: %v", path, err)
			continue
		}

		if err := engine.transport.SendTo(transport.PeerID(msg.SourceID), data); err != nil {
			log.Printf("Failed to send file data to %s: %v", msg.SourceID, err)
		}
		log.Printf("Sent file %s to %s (%d bytes)", path, msg.SourceID, len(content))
	}
}

// handleFileData processes received file content.
func (engine *Engine) handleFileData(msg *meta.SyncMessage) {
	payload, err := ExtractFileDataPayload(msg)
	if err != nil {
		log.Printf("Invalid file data payload: %v", err)
		return
	}

	log.Printf("Received file %s from %s (%d bytes)", payload.Path, msg.SourceID, len(payload.Content))

	// Write file to disk
	fullPath := engine.resolvePath(payload.Path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		log.Printf("Failed to create directories for %s: %v", payload.Path, err)
		return
	}

	// Atomic write
	tmpPath := fullPath + ".tmp"
	if err := os.WriteFile(tmpPath, payload.Content, 0644); err != nil {
		log.Printf("Failed to write temp file for %s: %v", payload.Path, err)
		return
	}
	if err := os.Rename(tmpPath, fullPath); err != nil {
		log.Printf("Failed to rename file for %s: %v", payload.Path, err)
		return
	}

	// Update local state immediately to prevent echo conflicts
	// Calculate hash from received content
	hasher := sha256.New()
	hasher.Write(payload.Content)
	contentHash := hex.EncodeToString(hasher.Sum(nil))

	// Incrementally update tree for the received file
	_, err = engine.tree.UpdateNode(engine.rootPath, payload.Path)
	if err != nil {
		log.Printf("Failed to update merkle tree after receiving file: %v", err)
		// Fall back to full rebuild
		newTree, err := merkle.Build(engine.rootPath, []string{".psync"})
		if err != nil {
			log.Printf("Failed to rebuild merkle tree after receiving file: %v", err)
			return
		}
		engine.tree = newTree
	}
	engine.state.RootHash = engine.tree.Root.Hash

	// Update FileState with remote version and new hash
	fileState := meta.FileState{
		Info: meta.FileInfo{
			Path: payload.Path,
			Hash: contentHash,
			Size: int64(len(payload.Content)),
		},
		Version:   payload.Version, // Use remote version
		Tombstone: false,
	}
	engine.state.FileStates[payload.Path] = fileState

	// Persist state
	if err := meta.SaveState(engine.rootPath, engine.state); err != nil {
		log.Printf("Failed to save state after receiving file: %v", err)
	}
}

// resolvePath returns the absolute path for a relative sync path.
func (engine *Engine) resolvePath(relPath string) string {
	return filepath.Join(engine.rootPath, relPath)
}

// requestFileList sends a request for the full file list to a peer.
func (engine *Engine) requestFileList(peerID transport.PeerID) {
	log.Printf("Requesting file list from %s", peerID)

	// Send GetFileList message asking peer to send their list
	data, err := MarshalSyncMessage(meta.MsgTypeGetFileList, engine.localID, nil)
	if err != nil {
		log.Printf("Failed to marshal get_file_list: %v", err)
		return
	}

	if err := engine.transport.SendTo(peerID, data); err != nil {
		log.Printf("Failed to send get_file_list request to %s: %v", peerID, err)
	}
}

// sendHeartbeat sends a heartbeat message to the signal server.
func (engine *Engine) sendHeartbeat() {
	payload := meta.HeartbeatPayload{
		Timestamp: time.Now().Unix(),
	}

	// Send heartbeat to signal server via WebSocket
	if err := engine.transport.SendSignalMessage(transport.SignalHeartbeat, payload); err != nil {
		log.Printf("Failed to send heartbeat: %v", err)
	}
}
