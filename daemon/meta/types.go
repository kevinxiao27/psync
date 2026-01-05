package meta

import (
	"encoding/json"
	"time"
)

// PeerID uniquely identifies a peer in the network.
type PeerID string

// -----------------------------------------------------------------------------
// File Metadata
// -----------------------------------------------------------------------------

// FileInfo represents metadata about a single file or directory.
type FileInfo struct {
	Path    string    `json:"path"`             // Relative path from sync root
	Hash    string    `json:"hash"`             // SHA-256 of content (empty for dirs)
	Size    int64     `json:"size"`             // File size in bytes
	ModTime time.Time `json:"mod_time"`         // Last modification time
	IsDir   bool      `json:"is_dir,omitempty"` // True for directories
}

// -----------------------------------------------------------------------------
// Vector Clocks
// -----------------------------------------------------------------------------

// VectorClock tracks logical time across peers for conflict resolution.
// Keys are PeerIDs, values are monotonically increasing counters.
type VectorClock map[PeerID]uint64

// -----------------------------------------------------------------------------
// File State
// -----------------------------------------------------------------------------

// FileState represents the sync state of a single file.
type FileState struct {
	Info      FileInfo    `json:"info"`
	Version   VectorClock `json:"version"`
	Tombstone bool        `json:"tombstone,omitempty"` // True if deleted
}

// -----------------------------------------------------------------------------
// Sync Messages
// -----------------------------------------------------------------------------

// SyncMessageType identifies the type of sync protocol message.
type SyncMessageType string

const (
	// Initialization messages
	MsgTypeInit        SyncMessageType = "init"          // Initial handshake
	MsgTypeFileList    SyncMessageType = "file_list"     // List of files with versions
	MsgTypeGetFileList SyncMessageType = "get_file_list" // Request full file list

	// File transfer messages
	MsgTypeFileRequest SyncMessageType = "file_request" // Request specific files
	MsgTypeFileData    SyncMessageType = "file_data"    // File content transfer
	MsgTypeFileChunk   SyncMessageType = "file_chunk"   // Chunk for large files

	// Control messages
	MsgTypeHeartbeat SyncMessageType = "heartbeat" // Keep-alive
	MsgTypeAck       SyncMessageType = "ack"       // Acknowledgement
)

// SyncMessage is the envelope for all daemon-to-daemon messages.
type SyncMessage struct {
	Type     SyncMessageType `json:"type"`
	SourceID PeerID          `json:"source_id"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// -----------------------------------------------------------------------------
// Message Payloads
// -----------------------------------------------------------------------------

// InitPayload is sent when a peer joins or reconnects.
type InitPayload struct {
	PeerID   PeerID `json:"peer_id"`
	RootHash string `json:"root_hash"` // Merkle root hash for quick comparison
}

// FileListPayload contains file states for synchronization.
type FileListPayload struct {
	Files []FileState `json:"files"`
}

// FileRequestPayload requests specific files from a peer.
type FileRequestPayload struct {
	Paths []string `json:"paths"` // Relative paths to request
}

// FileDataPayload contains the actual file content.
type FileDataPayload struct {
	Path    string      `json:"path"`
	Content []byte      `json:"content"`
	Version VectorClock `json:"version"`
}

// FileChunkPayload is for streaming large files.
type FileChunkPayload struct {
	Path        string `json:"path"`
	ChunkIndex  int    `json:"chunk_index"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
}

// HeartbeatPayload contains peer health info.
type HeartbeatPayload struct {
	Timestamp int64  `json:"timestamp"` // Unix timestamp
	RootHash  string `json:"root_hash"` // Quick consistency check
}

// AckPayload acknowledges message receipt.
type AckPayload struct {
	MessageID string `json:"message_id,omitempty"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}
