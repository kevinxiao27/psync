package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// MockSignalServer simulates the signal server for testing.
type MockSignalServer struct {
	upgrader  websocket.Upgrader
	clients   map[PeerID]*websocket.Conn
	clientsMu sync.RWMutex
	groups    map[string][]PeerID // groupID -> peerIDs
}

func NewMockSignalServer() *MockSignalServer {
	return &MockSignalServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		clients: make(map[PeerID]*websocket.Conn),
		groups:  make(map[string][]PeerID),
	}
}

func (s *MockSignalServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	return mux
}

func (s *MockSignalServer) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var peerID PeerID
	var groupID string

	for {
		var msg SignalMessage
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}

		switch msg.Type {
		case SignalRegister:
			var payload RegisterPayload
			json.Unmarshal(msg.Payload, &payload)
			peerID = payload.PeerID
			groupID = payload.GroupID

			s.clientsMu.Lock()
			s.clients[peerID] = conn

			// Get existing peers
			existingPeers := s.groups[groupID]
			s.groups[groupID] = append(s.groups[groupID], peerID)
			s.clientsMu.Unlock()

			// Send peer list to new peer
			var peerInfos []PeerInfo
			for _, p := range existingPeers {
				peerInfos = append(peerInfos, PeerInfo{ID: p, GroupID: groupID})
			}
			listPayload, _ := json.Marshal(PeerListPayload{Peers: peerInfos})
			conn.WriteJSON(SignalMessage{
				Type:    SignalPeerList,
				Payload: listPayload,
			})

			// Notify existing peers
			joinedPayload, _ := json.Marshal(PeerJoinedPayload{
				Peer: PeerInfo{ID: peerID, GroupID: groupID},
			})
			for _, existingPeer := range existingPeers {
				s.clientsMu.RLock()
				if c, ok := s.clients[existingPeer]; ok {
					c.WriteJSON(SignalMessage{
						Type:    SignalPeerJoined,
						Payload: joinedPayload,
					})
				}
				s.clientsMu.RUnlock()
			}

		case SignalOffer, SignalAnswer, SignalCandidate:
			// Route to target
			s.clientsMu.RLock()
			if targetConn, ok := s.clients[msg.TargetID]; ok {
				targetConn.WriteJSON(msg)
			}
			s.clientsMu.RUnlock()
		}
	}

	// Cleanup on disconnect
	s.clientsMu.Lock()
	delete(s.clients, peerID)
	// Remove from group
	newGroup := []PeerID{}
	for _, p := range s.groups[groupID] {
		if p != peerID {
			newGroup = append(newGroup, p)
		}
	}
	s.groups[groupID] = newGroup
	s.clientsMu.Unlock()
}

// TestTransportInterface verifies the interface is properly defined.
func TestTransportInterface(t *testing.T) {
	var _ Transport = (*WebRTCTransport)(nil)
}

func TestNewWebRTCTransport(t *testing.T) {
	transport := NewWebRTCTransport()
	if transport == nil {
		t.Fatal("NewWebRTCTransport returned nil")
	}
	if transport.peers == nil {
		t.Error("peers map not initialized")
	}
}

func TestSignalMessageMarshal(t *testing.T) {
	payload, _ := json.Marshal(RegisterPayload{
		PeerID:  "peer-a",
		GroupID: "test-group",
	})

	msg := SignalMessage{
		Type:     SignalRegister,
		SourceID: "peer-a",
		Payload:  payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded SignalMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Type != SignalRegister {
		t.Errorf("Expected type %s, got %s", SignalRegister, decoded.Type)
	}
}

func TestMockSignalServerRegistration(t *testing.T) {
	server := NewMockSignalServer()
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Register
	payload, _ := json.Marshal(RegisterPayload{
		PeerID:  "test-peer",
		GroupID: "test-group",
	})
	if err := conn.WriteJSON(SignalMessage{
		Type:     SignalRegister,
		SourceID: "test-peer",
		Payload:  payload,
	}); err != nil {
		t.Fatalf("Failed to send register: %v", err)
	}

	// Should receive empty peer list
	conn.SetReadDeadline(time.Now().Add(time.Second))
	var msg SignalMessage
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("Failed to read peer list: %v", err)
	}

	if msg.Type != SignalPeerList {
		t.Errorf("Expected peer_list, got %s", msg.Type)
	}
}

func TestMockSignalServerRouting(t *testing.T) {
	server := NewMockSignalServer()
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	// Peer A
	connA, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connA.Close()

	payloadA, _ := json.Marshal(RegisterPayload{PeerID: "peer-a", GroupID: "group"})
	connA.WriteJSON(SignalMessage{Type: SignalRegister, SourceID: "peer-a", Payload: payloadA})
	connA.ReadJSON(&SignalMessage{}) // Consume peer_list

	// Peer B
	connB, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connB.Close()

	payloadB, _ := json.Marshal(RegisterPayload{PeerID: "peer-b", GroupID: "group"})
	connB.WriteJSON(SignalMessage{Type: SignalRegister, SourceID: "peer-b", Payload: payloadB})
	connB.ReadJSON(&SignalMessage{}) // Consume peer_list (contains A)

	// Wait for A to receive peer_joined
	time.Sleep(50 * time.Millisecond)

	// A sends offer to B
	offerPayload, _ := json.Marshal(SDPPayload{SDP: "test-sdp"})
	connA.WriteJSON(SignalMessage{
		Type:     SignalOffer,
		SourceID: "peer-a",
		TargetID: "peer-b",
		Payload:  offerPayload,
	})

	// B should receive the offer
	connB.SetReadDeadline(time.Now().Add(time.Second))
	var msg SignalMessage
	if err := connB.ReadJSON(&msg); err != nil {
		t.Fatalf("B failed to receive offer: %v", err)
	}

	if msg.Type != SignalOffer {
		t.Errorf("Expected offer, got %s", msg.Type)
	}
	if msg.SourceID != "peer-a" {
		t.Errorf("Expected source peer-a, got %s", msg.SourceID)
	}
}

func TestTransportCallbacks(t *testing.T) {
	transport := NewWebRTCTransport()

	var messageCalled bool
	var connectCalled bool
	var disconnectCalled bool

	transport.OnMessage(func(peerID PeerID, data []byte) {
		messageCalled = true
	})
	transport.OnPeerConnected(func(peerID PeerID) {
		connectCalled = true
	})
	transport.OnPeerDisconnected(func(peerID PeerID) {
		disconnectCalled = true
	})

	// Verify callbacks are set
	if transport.onMessage == nil {
		t.Error("onMessage not set")
	}
	if transport.onPeerConnected == nil {
		t.Error("onPeerConnected not set")
	}
	if transport.onPeerDisconnected == nil {
		t.Error("onPeerDisconnected not set")
	}

	// Note: messageCalled, connectCalled, disconnectCalled would be tested
	// in integration tests with actual WebRTC connections
	_ = messageCalled
	_ = connectCalled
	_ = disconnectCalled
}

func TestGetConnectedPeersEmpty(t *testing.T) {
	transport := NewWebRTCTransport()
	peers := transport.GetConnectedPeers()
	if len(peers) != 0 {
		t.Errorf("Expected 0 peers, got %d", len(peers))
	}
}

func TestSendToNotConnected(t *testing.T) {
	transport := NewWebRTCTransport()
	err := transport.SendTo("nonexistent", []byte("test"))
	if err != ErrPeerNotConnected {
		t.Errorf("Expected ErrPeerNotConnected, got %v", err)
	}
}

func TestConnectToSignalServer(t *testing.T) {
	server := NewMockSignalServer()
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	transport := NewWebRTCTransport()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := transport.Connect(ctx, wsURL, "test-peer", "test-group")
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer transport.Close()

	// Verify we're registered with the server
	time.Sleep(100 * time.Millisecond)
	server.clientsMu.RLock()
	_, registered := server.clients["test-peer"]
	server.clientsMu.RUnlock()

	if !registered {
		t.Error("Transport not registered with signal server")
	}
}

func TestCloseTransport(t *testing.T) {
	transport := NewWebRTCTransport()

	// Should not panic on nil
	err := transport.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}
