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
	"github.com/kevinxiao27/psync/daemon/transport"
	"github.com/kevinxiao27/psync/daemon/vclock"
	"github.com/kevinxiao27/psync/daemon/watcher"
)

// Engine orchestrates the synchronization process.
type Engine struct {
	localID   vclock.PeerID
	rootPath  string
	transport transport.Transport
	watcher   *watcher.Watcher
	tree      *merkle.Tree
	state     *vclock.State
	resolver  *Resolver
}

// NewEngine creates a new Sync Engine.
func NewEngine(
	id vclock.PeerID,
	root string,
	t transport.Transport,
	w *watcher.Watcher,
	tree *merkle.Tree,
) *Engine {
	// Initialize state
	state, err := vclock.LoadState(root)
	if err != nil {
		log.Printf("Warning: failed to load state, starting fresh: %v", err)
		state = &vclock.State{
			RootHash:   tree.Root.Hash,
			FileStates: make(map[string]vclock.FileState),
		}
	} else {
		// Update root hash
		state.RootHash = tree.Root.Hash
	}

	// Ensure state tracks all files in the tree (handles case where state is lost but files exist)
	populateStateFromTree(tree.Root, state, id)

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

// populateStateFromTree recursively adds files from the merkle tree to the state
// if they are missing, ensuring we don't treat existing files as deleted.
func populateStateFromTree(node *merkle.Node, state *vclock.State, localID vclock.PeerID) {
	if node == nil {
		return
	}

	// Skip root
	if node.Path != "" && node.Path != "." {
		if _, exists := state.FileStates[node.Path]; !exists {
			state.FileStates[node.Path] = vclock.FileState{
				Info: vclock.FileInfo{
					Path:  node.Path,
					Hash:  node.Hash,
					Size:  node.Size,
					IsDir: node.IsDir,
				},
				Version: vclock.VectorClock{
					localID: 1, // Treat as new local version
				},
				Tombstone: false,
			}
		}
	}

	for _, child := range node.Children {
		populateStateFromTree(child, state, localID)
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
		fileState = vclock.FileState{
			Version: make(vclock.VectorClock),
		}
	}

	// Capture old state properties for echo detection
	wasTombstone := false
	oldHash := ""
	if exists {
		wasTombstone = fileState.Tombstone
		oldHash = fileState.Info.Hash
	}

	// Update metadata
	var newHash string
	if node != nil {
		// File exists (Created or Modified)
		fileState.Info = vclock.FileInfo{
			Path: event.Path,
			Hash: node.Hash,
			// Size and ModTime would come from node or os.Stat
		}
		fileState.Tombstone = false
		newHash = node.Hash
	} else {
		// Node not in Merkle tree, check if it's an empty directory
		fullPath := engine.resolvePath(event.Path)
		info, err := os.Stat(fullPath)
		if err == nil && info.IsDir() {
			// It's an existing empty directory
			fileState.Info = vclock.FileInfo{
				Path:  event.Path,
				Hash:  "", // Empty hash for directories
				IsDir: true,
				// Size and ModTime would come from info
			}
			fileState.Tombstone = false
			newHash = "" // No content hash for directories
		} else {
			// File deleted (or non-existent path that wasn't a directory)
			fileState.Tombstone = true
			fileState.Info.Path = event.Path
			// Hash irrelevant for tombstone, keeping old one or empty
			newHash = ""
		}
	}

	// Check if this is an echo event (hash hasn't changed)
	// This prevents incrementing version for files we just received from remote
	// BUT, if it was a tombstone, we MUST process it (resurrection)
	if exists && !wasTombstone && oldHash == newHash {
		log.Printf("Skipping echo event for %s (hash unchanged: %s)", event.Path, newHash)
		return
	}

	// Increment our version
	fileState.Version.Increment(engine.localID)

	// Save back to state
	engine.state.FileStates[event.Path] = fileState

	// Persist state
	if err := vclock.SaveState(engine.rootPath, engine.state); err != nil {
		log.Printf("Failed to save state: %v", err)
	}

	// 3. Broadcast new Root Hash
	engine.broadcastInit()
}

// broadcastInit sends an Init message to all connected peers.
func (engine *Engine) broadcastInit() {
	payload := vclock.InitPayload{
		PeerID:   engine.localID,
		RootHash: engine.tree.Root.Hash,
	}

	log.Printf("Broadcasting Init with Hash %s", payload.RootHash)

	data, err := MarshalSyncMessage(vclock.MsgTypeInit, engine.localID, payload)
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
	case vclock.MsgTypeInit:
		engine.handleInit(msg)
	case vclock.MsgTypeFileList:
		engine.handleFileList(msg)
	case vclock.MsgTypeFileRequest:
		engine.handleFileRequest(msg)
	case vclock.MsgTypeGetFileList:
		engine.handleGetFileList(msg)
	case vclock.MsgTypeFileData:
		engine.handleFileData(msg)
	default:
		log.Printf("Unknown message type from %s: %s", peerID, msg.Type)
	}
}

// handlePeerConnected handles new peer connections.
func (engine *Engine) handlePeerConnected(peerID transport.PeerID) {
	log.Printf("Peer connected: %s", peerID)

	// Send Init message to the new peer
	payload := vclock.InitPayload{
		PeerID:   engine.localID,
		RootHash: engine.tree.Root.Hash,
	}

	data, err := MarshalSyncMessage(vclock.MsgTypeInit, engine.localID, payload)
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
func (engine *Engine) handleInit(msg *vclock.SyncMessage) {
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
func (engine *Engine) handleGetFileList(msg *vclock.SyncMessage) {
	log.Printf("Received GetFileList request from %s", msg.SourceID)

	// Collect all local file states
	var files []vclock.FileState
	if engine.state != nil {
		for _, fs := range engine.state.FileStates {
			files = append(files, fs)
		}
	}

	payload := vclock.FileListPayload{
		Files: files,
	}

	data, err := MarshalSyncMessage(vclock.MsgTypeFileList, engine.localID, payload)
	if err != nil {
		log.Printf("Failed to marshal file list: %v", err)
		return
	}

	if err := engine.transport.SendTo(transport.PeerID(msg.SourceID), data); err != nil {
		log.Printf("Failed to send file list to %s: %v", msg.SourceID, err)
	}
}

func (engine *Engine) deleteLocalPath(filepath string) error {
	fullPath := engine.resolvePath(filepath)
	if err := os.RemoveAll(fullPath); err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to delete local file %s: %v", fullPath, err)
		return err
	}

	return nil
}

// handleFileList processes a received list of files.
func (engine *Engine) handleFileList(msg *vclock.SyncMessage) {
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
		// First, handle local files that are missing from remote's list (potential remote deletion)
		// Or remote files that are new to us (potential new file)
		if !exists {
			if !remoteFile.Tombstone {
				// Remote has a file/dir we don't have and it's not a tombstone
				if remoteFile.Info.IsDir {
					log.Printf("New remote directory detected: %s", remoteFile.Info.Path)
					// Create directory immediately
					fullPath := engine.resolvePath(remoteFile.Info.Path)
					if err := os.MkdirAll(fullPath, 0755); err != nil {
						log.Printf("Failed to create directory %s: %v", fullPath, err)
					}
					// Update state
					engine.state.FileStates[remoteFile.Info.Path] = vclock.FileState{
						Info:      remoteFile.Info,
						Version:   remoteFile.Version.Clone(),
						Tombstone: false,
					}
					needsRebuild = true
				} else {
					log.Printf("New remote file detected: %s", remoteFile.Info.Path)
					filesToRequest = append(filesToRequest, remoteFile.Info.Path)
				}
			} else {
				// Remote has a tombstone for a file we don't know about.
				// This means we might have created it recently or simply never seen it.
				// If we have the file locally, we need to resolve. If not, we just record the tombstone.
				log.Printf("Received remote tombstone for unknown file: %s", remoteFile.Info.Path)
				fullPath := engine.resolvePath(remoteFile.Info.Path)
				if _, err := os.Stat(fullPath); os.IsNotExist(err) {
					// File doesn't exist locally, so we simply adopt the remote tombstone.
					engine.state.FileStates[remoteFile.Info.Path] = vclock.FileState{
						Info:      remoteFile.Info,
						Version:   remoteFile.Version.Clone(),
						Tombstone: true,
					}
					needsRebuild = true
					log.Printf("Adopted remote tombstone for %s (local not exist).", remoteFile.Info.Path)
				} else if err == nil {
					// File exists locally, but remote says it's deleted. This is a conflict.
					// We treat our local file as a new concurrent version.
					log.Printf("Conflict: remote deleted %s, but local exists. Keeping local as new version.", remoteFile.Info.Path)

					// We don't have previous state, so we initialize it as a new version.
					// We need to get details from disk/tree?
					// For now, simpler to just initialize it.
					// Note: calling handleLocalEvent-like logic or just setting state.
					// Since we are not deleting, we effectively "win".
					// We should validly populate the state so next sync we dominate (or conflict properly).
					// NOTE: We rely on the tree walker or next local event to fix the Hash if it's missing.
					// But we should try to get it right.
					// Assuming populateStateFromTree ran on startup, we shouldn't be here unless watcher lagged.
					// If watcher lagged, we just add it to state.

					info, _ := os.Stat(fullPath)
					isDir := info.IsDir()
					if !isDir {
						// We don't have the hash handy. We rely on the watcher to update it later.
						// The state will hold empty hash for now, which is fine as valid versioning will trigger updates.
					}

					engine.state.FileStates[remoteFile.Info.Path] = vclock.FileState{
						Info: vclock.FileInfo{
							Path:  remoteFile.Info.Path,
							IsDir: isDir,
							// Hash unknown unless we calculate it.
						},
						Version: vclock.VectorClock{
							engine.localID: 1, // Fresh version
						},
						Tombstone: false,
					}
					// We do NOT set needsRebuild=true because we didn't change disk, only state.
					// But we DO want to persist state.
					if err := vclock.SaveState(engine.rootPath, engine.state); err != nil {
						log.Printf("Failed to save state: %v", err)
					}
					// And we should probably broadcast our existance?
					// Yes, broadcast Init so remote asks for our list.
					// But handleFileList calls broadcastInit at end if needsRebuild is true.
					needsRebuild = true // Trigger broadcast
				} else {
					log.Printf("Error stating local file %s: %v", remoteFile.Info.Path, err)
				}
			}
			continue
		}
		// At this point, localFile exists in our state. Resolve conflict/update.
		resolution := engine.resolver.Resolve(localFile, remoteFile, msg.SourceID)
		switch resolution.Type {
		case ResolutionRemoteDominates:
			if !remoteFile.Tombstone {
				if remoteFile.Info.IsDir {
					log.Printf("Remote directory newer: %s", remoteFile.Info.Path)
					fullPath := engine.resolvePath(remoteFile.Info.Path)
					if err := os.MkdirAll(fullPath, 0755); err != nil {
						log.Printf("Failed to create directory %s: %v", fullPath, err)
					} else {
						engine.state.FileStates[remoteFile.Info.Path] = vclock.FileState{
							Info:      remoteFile.Info,
							Version:   remoteFile.Version.Clone(),
							Tombstone: false,
						}
						needsRebuild = true
					}
				} else {
					log.Printf("Remote file newer: %s", remoteFile.Info.Path)
					filesToRequest = append(filesToRequest, remoteFile.Info.Path)
				}
			} else {
				// Remote deletion dominates local. Apply the deletion.
				log.Printf("Remote deletion dominates for %s. Applying.", remoteFile.Info.Path)
				if err := engine.deleteLocalPath(remoteFile.Info.Path); err != nil {
					log.Printf("Failed to delete local path %s: %v", remoteFile.Info.Path, err)
				} else {
					// Update local state with remote's tombstone and version
					engine.state.FileStates[remoteFile.Info.Path] = vclock.FileState{
						Info:      remoteFile.Info,
						Version:   remoteFile.Version.Clone(),
						Tombstone: true,
					}
					needsRebuild = true
				}
			}
		case ResolutionConflict:
			log.Printf("Conflict detected for %s", remoteFile.Info.Path)
			if resolution.LocalWins {
				log.Printf("Local version wins. Keeping local.")
				// Local wins, but if remote was a tombstone, and local is not, we need to ensure the remote knows local exists.
				// This might mean broadcasting local state to remote, but for now, no action.
			} else {
				// Remote wins - check if it's a deletion
				if !remoteFile.Tombstone {
					if remoteFile.Info.IsDir {
						log.Printf("Remote directory wins conflict: %s", remoteFile.Info.Path)
						fullPath := engine.resolvePath(remoteFile.Info.Path)
						if err := os.MkdirAll(fullPath, 0755); err != nil {
							log.Printf("Failed to create directory %s: %v", fullPath, err)
						} else {
							engine.state.FileStates[remoteFile.Info.Path] = vclock.FileState{
								Info:      remoteFile.Info,
								Version:   remoteFile.Version.Clone(),
								Tombstone: false,
							}
							needsRebuild = true
						}
					} else {
						log.Printf("Remote version wins. Requesting remote.")
						filesToRequest = append(filesToRequest, remoteFile.Info.Path)
					}
				} else {
					// Remote deletion wins conflict. Apply the deletion.
					log.Printf("Remote deletion wins conflict for %s. Applying.", remoteFile.Info.Path)
					if err := engine.deleteLocalPath(remoteFile.Info.Path); err != nil {
						log.Printf("Failed to delete local path %s: %v", remoteFile.Info.Path, err)
					} else {
						// Update local state with remote's tombstone and version
						engine.state.FileStates[remoteFile.Info.Path] = vclock.FileState{
							Info:      remoteFile.Info,
							Version:   remoteFile.Version.Clone(),
							Tombstone: true,
						}
						needsRebuild = true
					}
				}
			}
		case ResolutionEqual, ResolutionLocalDominates:
			// No action needed
			log.Printf("No action needed for %s (resolution: %v)", remoteFile.Info.Path, resolution.Type)
		}
	}

	// If any changes (deletions or new tombstones) were applied, persist state and broadcast new root hash
	if needsRebuild {
		// Rebuild the Merkle tree to reflect the local deletions (if any) and newly received files
		newTree, err := merkle.Build(engine.rootPath, []string{".psync"})
		if err != nil {
			log.Printf("Failed to rebuild merkle tree after file list sync: %v", err)
			return
		}
		engine.tree = newTree
		engine.state.RootHash = engine.tree.Root.Hash
		if err := vclock.SaveState(engine.rootPath, engine.state); err != nil {
			log.Printf("Failed to save state after file list sync: %v", err)
		}
		// Broadcast new root hash after applying changes
		engine.broadcastInit()
	}
	// Request any needed files
	if len(filesToRequest) > 0 {
		engine.requestFiles(transport.PeerID(msg.SourceID), filesToRequest)
	}

	// Apply deletions incrementally and save state
	if needsRebuild {
		// For deletions, each handleDeletion call already updated the tree
		// so we just need to ensure the root hash is current and broadcast
		engine.state.RootHash = engine.tree.Root.Hash

		if err := vclock.SaveState(engine.rootPath, engine.state); err != nil {
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

	payload := vclock.FileRequestPayload{
		Paths: paths,
	}

	data, err := MarshalSyncMessage(vclock.MsgTypeFileRequest, engine.localID, payload)
	if err != nil {
		log.Printf("Failed to marshal file request: %v", err)
		return
	}

	if err := engine.transport.SendTo(peerID, data); err != nil {
		log.Printf("Failed to send file request to %s: %v", peerID, err)
	}
}

// handleFileRequest processes a request for specific files.
func (engine *Engine) handleFileRequest(msg *vclock.SyncMessage) {
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
		dataPayload := vclock.FileDataPayload{
			Path:    path,
			Content: content,
			Version: fileState.Version,
		}

		data, err := MarshalSyncMessage(vclock.MsgTypeFileData, engine.localID, dataPayload)
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
func (engine *Engine) handleFileData(msg *vclock.SyncMessage) {
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
	fileState := vclock.FileState{
		Info: vclock.FileInfo{
			Path: payload.Path,
			Hash: contentHash,
			Size: int64(len(payload.Content)),
		},
		Version:   payload.Version, // Use remote version
		Tombstone: false,
	}
	engine.state.FileStates[payload.Path] = fileState

	// Persist state
	if err := vclock.SaveState(engine.rootPath, engine.state); err != nil {
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
	data, err := MarshalSyncMessage(vclock.MsgTypeGetFileList, engine.localID, nil)
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
	payload := vclock.HeartbeatPayload{
		Timestamp: time.Now().Unix(),
	}

	// Send heartbeat to signal server via WebSocket
	if err := engine.transport.SendSignalMessage(transport.SignalHeartbeat, payload); err != nil {
		log.Printf("Failed to send heartbeat: %v", err)
	}
}
