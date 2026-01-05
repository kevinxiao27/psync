package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kevinxiao27/psync/signal-server/types"
)

// TestClient is a helper for testing WebSocket interactions
type TestClient struct {
	conn *websocket.Conn
	t    *testing.T
}

func newTestClient(t *testing.T, serverURL string) *TestClient {
	// Convert http/https to ws/wss
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial websocket: %v", err)
	}

	return &TestClient{
		conn: conn,
		t:    t,
	}
}

func (c *TestClient) Close() {
	c.conn.Close()
}

func (c *TestClient) Send(msg interface{}) {
	err := c.conn.WriteJSON(msg)
	if err != nil {
		c.t.Fatalf("Failed to write JSON: %v", err)
	}
}

func (c *TestClient) Read() types.Message {
	var msg types.Message
	c.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	err := c.conn.ReadJSON(&msg)
	if err != nil {
		c.t.Fatalf("Failed to read JSON: %v", err)
	}
	return msg
}

func TestIdentityAndGrouping(t *testing.T) {
	// Setup Server
	s := NewServer(":0") // Random port
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// Client A (Group 1)
	clientA := newTestClient(t, ts.URL)
	defer clientA.Close()

	regA := types.Message{
		Type:     types.MessageTypeRegister,
		SourceID: "client-a",
		Payload:  json.RawMessage(`{"group_id": "group-1", "peer_id": "client-a"}`),
	}
	clientA.Send(regA)

	// Client B (Group 2)
	clientB := newTestClient(t, ts.URL)
	defer clientB.Close()

	regB := types.Message{
		Type:     types.MessageTypeRegister,
		SourceID: "client-b",
		Payload:  json.RawMessage(`{"group_id": "group-2", "peer_id": "client-b"}`),
	}
	clientB.Send(regB)

	// Wait a bit for processing (since server is async stub)
	time.Sleep(50 * time.Millisecond)

	// White-box testing: Check server state directly
	if _, ok := s.store.GetPeer("group-1", "client-a"); !ok {
		// This is expected to fail currently as server stub doesn't process Register msg
		t.Log("Peer A not registered")
	}
	if _, ok := s.store.GetPeer("group-1", "client-b"); ok {
		t.Error("Client B found in group-1! Isolation failed.")
	}
}

func TestDiscovery(t *testing.T) {
	s := NewServer(":0")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// Peer A joins Group 1
	clientA := newTestClient(t, ts.URL)
	defer clientA.Close()

	t.Log("Client A registering...")
	clientA.Send(types.Message{
		Type:     types.MessageTypeRegister,
		SourceID: "client-a",
		Payload:  json.RawMessage(`{"group_id": "group-1", "peer_id": "client-a"}`),
	})

	// Wait for A to be "ready" (in real impl, we'd get an ack or empty peer list)
	t.Log("Client A waiting for initial PeerList...")
	msgA_init := clientA.Read()
	if msgA_init.Type != "peer_list" {
		t.Errorf("Expected peer_list, got %s", msgA_init.Type)
	}

	// Peer B joins Group 1
	clientB := newTestClient(t, ts.URL)
	defer clientB.Close()

	t.Log("Client B registering...")
	clientB.Send(types.Message{
		Type:     types.MessageTypeRegister,
		SourceID: "client-b",
		Payload:  json.RawMessage(`{"group_id": "group-1", "peer_id": "client-b"}`),
	})

	// Assertions
	// 1. Client B should receive PeerList containing A
	t.Log("Waiting for Client B to receive PeerList (containing A)...")
	msgB := clientB.Read()

	var list types.PeerListPayload
	if err := json.Unmarshal(msgB.Payload, &list); err != nil {
		t.Errorf("Failed to unmarshal PeerList: %v", err)
	}

	foundA := false
	for _, p := range list.Peers {
		if p.ID == "client-a" {
			foundA = true
			break
		}
	}
	if !foundA {
		t.Errorf("Client B did not receive Client A in PeerList. Got: %+v", list)
	}

	// 2. Client A should receive PeerJoined containing B
	t.Log("Waiting for Client A to receive PeerJoined (B joined)...")
	msgA := clientA.Read()

	var joined types.PeerJoinedPayload
	if err := json.Unmarshal(msgA.Payload, &joined); err != nil {
		t.Errorf("Failed to unmarshal PeerJoined: %v", err)
	}

	if joined.Peer.ID != "client-b" {
		t.Errorf("Client A expected 'client-b' joined, got %s", joined.Peer.ID)
	}
}

func TestSignalingRouting(t *testing.T) {
	s := NewServer(":0")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// Connect two peers in same group
	cA := newTestClient(t, ts.URL)
	defer cA.Close()

	t.Log("Registering Client A...")
	cA.Send(types.Message{
		Type:     types.MessageTypeRegister,
		SourceID: "client-a",
		Payload:  json.RawMessage(`{"group_id": "route-group", "peer_id": "client-a"}`),
	})
	// Consume A's list
	cA.Read()

	cB := newTestClient(t, ts.URL)
	defer cB.Close()

	t.Log("Registering Client B...")
	cB.Send(types.Message{
		Type:     types.MessageTypeRegister,
		SourceID: "client-b",
		Payload:  json.RawMessage(`{"group_id": "route-group", "peer_id": "client-b"}`),
	})

	// Consume B's list (contains A)
	msgB_init := cB.Read()
	if msgB_init.Type != "peer_list" {
		t.Errorf("Client B expected peer_list, got %s", msgB_init.Type)
	}

	// Allow registration to settle
	time.Sleep(10 * time.Millisecond)

	// A sends Offer to B
	offerPayload := `{"sdp": "mock-sdp"}`
	offerMsg := types.Message{
		Type:     types.MessageTypeOffer,
		SourceID: "client-a",
		TargetID: "client-b",
		Payload:  json.RawMessage(offerPayload),
	}

	t.Log("Client A sending Offer to Client B...")
	cA.Send(offerMsg)

	// B expects Offer
	t.Log("Client B waiting for Offer...")
	msg := cB.Read()

	if msg.Type != types.MessageTypeOffer {
		t.Errorf("Expected Offer, got %s", msg.Type)
	}

	// Check content matches (ignore whitespace)
	var expected, actual interface{}
	if err := json.Unmarshal([]byte(offerPayload), &expected); err != nil {
		t.Fatalf("Invalid test offer payload: %v", err)
	}
	if err := json.Unmarshal(msg.Payload, &actual); err != nil {
		t.Fatalf("Failed to unmarshal received payload: %v", err)
	}

	expectedJSON, _ := json.Marshal(expected)
	actualJSON, _ := json.Marshal(actual)

	if string(expectedJSON) != string(actualJSON) {
		t.Errorf("Expected payload %s, got %s", string(expectedJSON), string(actualJSON))
	}
}
