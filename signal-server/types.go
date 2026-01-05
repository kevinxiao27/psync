package signal_client

import (
	"encoding/json"
	"time"
)

type PeerID string

type Peer struct {
	ID       PeerID    `json:"id"`
	GroupID  string    `json:"group_id"`
	LastSeen time.Time `json:"last_seen"`
}

// MessageType defines the type of signaling message.
type MessageType string

const (
	MessageTypeHeartbeat MessageType = "heartbeat"
	MessageTypeRegister  MessageType = "register"
	MessageTypeOffer     MessageType = "offer"
	MessageTypeAnswer    MessageType = "answer"
	MessageTypeCandidate MessageType = "candidate"
)

// Message is the generic envelope for all signaling messages.
type Message struct {
	Type     MessageType     `json:"type"`
	SourceID PeerID          `json:"source_id"`
	TargetID PeerID          `json:"target_id,omitempty"` // Optional for some messages (e.g. initial register)
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// RegisterPayload represents the data sent when registering.
type RegisterPayload struct {
	PeerID  PeerID `json:"peer_id"`
	GroupID string `json:"group_id"`
}

// HeartbeatPayload represents the data sent in a heartbeat.
type HeartbeatPayload struct {
	Timestamp int64 `json:"timestamp"` // Unix timestamp
}

// PeerListPayload represents the list of peers sent to a client upon joining.
type PeerListPayload struct {
	Peers []Peer `json:"peers"`
}

// PeerJoinedPayload represents the event when a new peer joins the group.
type PeerJoinedPayload struct {
	Peer Peer `json:"peer"`
}
