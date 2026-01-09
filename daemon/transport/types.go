package transport

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pion/webrtc/v4"
)

// PeerID uniquely identifies a peer.
type PeerID string

// -----------------------------------------------------------------------------
// Signal Server Message Types (matching signal-server protocol)
// -----------------------------------------------------------------------------

// SignalMessageType matches the signal server's message types.
type SignalMessageType string

const (
	SignalRegister   SignalMessageType = "register"
	SignalPeerList   SignalMessageType = "peer_list"
	SignalPeerJoined SignalMessageType = "peer_joined"
	SignalOffer      SignalMessageType = "offer"
	SignalAnswer     SignalMessageType = "answer"
	SignalCandidate  SignalMessageType = "candidate"
	SignalHeartbeat  SignalMessageType = "heartbeat"
)

// SignalMessage is the envelope for signal server communication.
type SignalMessage struct {
	Type     SignalMessageType `json:"type"`
	SourceID PeerID            `json:"source_id"`
	TargetID PeerID            `json:"target_id,omitempty"`
	Payload  json.RawMessage   `json:"payload,omitempty"`
}

// RegisterPayload for joining a group.
type RegisterPayload struct {
	PeerID  PeerID `json:"peer_id"`
	GroupID string `json:"group_id"`
}

// PeerInfo from signal server.
type PeerInfo struct {
	ID       PeerID    `json:"id"`
	GroupID  string    `json:"group_id"`
	LastSeen time.Time `json:"last_seen"`
}

// PeerListPayload contains existing peers.
type PeerListPayload struct {
	Peers []PeerInfo `json:"peers"`
}

// PeerJoinedPayload for new peer notifications.
type PeerJoinedPayload struct {
	Peer PeerInfo `json:"peer"`
}

// SDPPayload for offer/answer messages.
type SDPPayload struct {
	SDP string `json:"sdp"`
}

// ICECandidatePayload for ICE candidates.
type ICECandidatePayload struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdp_mid,omitempty"`
	SDPMLineIndex uint16 `json:"sdp_mline_index,omitempty"`
}

// -----------------------------------------------------------------------------
// Transport Interface
// -----------------------------------------------------------------------------

// MessageHandler is called when data is received from a peer.
type MessageHandler func(peerID PeerID, data []byte)

// PeerHandler is called when a peer connects/disconnects.
type PeerHandler func(peerID PeerID)

// Transport manages WebRTC connections to peers via the signal server.
type Transport interface {
	// Connect establishes connection to the signal server and joins the group.
	Connect(ctx context.Context, signalURL string, peerID PeerID, groupID string) error

	// SendTo sends data to a specific peer via WebRTC data channel.
	SendTo(peerID PeerID, data []byte) error

	// Broadcast sends data to all connected peers.
	Broadcast(data []byte) error

	// OnMessage sets the handler for incoming data.
	OnMessage(handler MessageHandler)

	// OnPeerConnected sets the handler for peer connections.
	OnPeerConnected(handler PeerHandler)

	// OnPeerDisconnected sets the handler for peer disconnections.
	OnPeerDisconnected(handler PeerHandler)

	// GetConnectedPeers returns list of currently connected peer IDs.
	GetConnectedPeers() []PeerID

	// SendSignalMessage sends a message to signal server.
	SendSignalMessage(messageType SignalMessageType, payload interface{}) error

	// Close shuts down all connections.
	Close() error
}

// -----------------------------------------------------------------------------
// Peer Connection
// -----------------------------------------------------------------------------

// PeerConnection wraps a WebRTC peer connection and data channel.
type PeerConnection struct {
	ID          PeerID
	Conn        *webrtc.PeerConnection
	DataChannel *webrtc.DataChannel
	Connected   bool
}

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

type transportError string

func (e transportError) Error() string { return string(e) }

const (
	ErrPeerNotConnected transportError = "peer not connected"
	ErrNotConnected     transportError = "transport not connected"
)
