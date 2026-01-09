package transport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	signalApi "github.com/kevinxiao27/psync/signal-server/api"
	signalTypes "github.com/kevinxiao27/psync/signal-server/types"
)

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

func TestConnectToSignalServer(t *testing.T) {
	// Use REAL signal server API
	server := signalApi.NewServer(":0")
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	transport := NewWebRTCTransport()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := transport.Connect(ctx, wsURL, "test-peer", "test-group")
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer transport.Close()

	// Verify we're registered with the server
	// We wait slightly for the registration to complete asynchronously
	deadline := time.Now().Add(2 * time.Second)
	var registered bool
	for time.Now().Before(deadline) {
		if _, ok := server.GetPeer(signalTypes.PeerID("test-peer")); ok {
			registered = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !registered {
		t.Error("Transport not registered with signal server")
	}
}

func TestP2PDataExchange(t *testing.T) {
	// This test performs a full WebRTC P2P exchange between two transport instances
	// mediated by a real signal server instance.

	server := signalApi.NewServer(":0")
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	groupID := "p2p-test-group"

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Initialize Peer A
	tA := NewWebRTCTransport()
	defer tA.Close()

	var aReceivedCount int32
	tA.OnMessage(func(peerID PeerID, data []byte) {
		atomic.AddInt32(&aReceivedCount, 1)
		t.Logf("Peer A received: %s", string(data))
	})

	// Initialize Peer B
	tB := NewWebRTCTransport()
	defer tB.Close()

	var bReceivedData string
	var bReceivedCount int32
	bSignal := make(chan struct{})
	tB.OnMessage(func(peerID PeerID, data []byte) {
		atomic.AddInt32(&bReceivedCount, 1)
		bReceivedData = string(data)
		t.Logf("Peer B received: %s", bReceivedData)
		close(bSignal)
	})

	// Connect Peer A
	t.Log("Connecting peer A to signal server...")
	if err := tA.Connect(ctx, wsURL, "peer-a", groupID); err != nil {
		t.Fatalf("A connect failed: %v", err)
	}
	t.Log("Peer A connected, waiting for registration...")

	// Give A a moment to register
	time.Sleep(100 * time.Millisecond)

	// Connect Peer B
	// When B joins, it receives A in PeerList and initiates connection
	t.Log("Connecting peer B to signal server...")
	if err := tB.Connect(ctx, wsURL, "peer-b", groupID); err != nil {
		t.Fatalf("B connect failed: %v", err)
	}
	t.Log("Peer B connected, initiating P2P connection...")

	// Wait for DataChannel to open
	t.Log("Waiting for P2P connection to establish...")

	// Wait for peer discovery and connection
	connectionDeadline := time.Now().Add(10 * time.Second)
	connected := false
	for time.Now().Before(connectionDeadline) {
		if len(tA.GetConnectedPeers()) > 0 && len(tB.GetConnectedPeers()) > 0 {
			connected = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !connected {
		t.Fatalf("Peers failed to connect. A peers: %v, B peers: %v",
			tA.GetConnectedPeers(), tB.GetConnectedPeers())
	}

	t.Log("P2P connection established, sending data...")

	// Send data from A to B
	testData := "hello from a"
	if err := tA.SendTo("peer-b", []byte(testData)); err != nil {
		t.Fatalf("A send failed: %v", err)
	}

	select {
	case <-bSignal:
		if bReceivedData != testData {
			t.Errorf("B received %q, want %q", bReceivedData, testData)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timed out waiting for B to receive data")
	}

	// Send data from B to A to verify bidirectional channel
	testDataB := "reply from b"
	if err := tB.SendTo("peer-a", []byte(testDataB)); err != nil {
		t.Fatalf("B send failed: %v", err)
	}

	// Poll for A's reception
	deadlineA := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadlineA) {
		if atomic.LoadInt32(&aReceivedCount) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if atomic.LoadInt32(&aReceivedCount) == 0 {
		t.Error("A failed to receive data from B")
	}
}

func TestPeeringWithSelf(t *testing.T) {
	// "Peering with yourself" in the sense of two transports in the same group
	// but with different PeerIDs. Connecting a single PeerConnection to itself
	// usually fails due to ICE/SDP role conflicts unless specially configured.

	server := signalApi.NewServer(":0")
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t1 := NewWebRTCTransport()
	defer t1.Close()
	t2 := NewWebRTCTransport()
	defer t2.Close()

	if err := t1.Connect(ctx, wsURL, "self-1", "self-group"); err != nil {
		t.Fatalf("T1 connect failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := t2.Connect(ctx, wsURL, "self-2", "self-group"); err != nil {
		t.Fatalf("T2 connect failed: %v", err)
	}

	// Wait for connection
	connected := false
	for range 20 {
		if len(t1.GetConnectedPeers()) > 0 {
			connected = true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	if !connected {
		t.Fatal("Self-peered instances failed to connect")
	}

	t.Log("Successfully peered two instances in the same process")
}

func TestCloseTransport(t *testing.T) {
	transport := NewWebRTCTransport()

	// Should not panic on nil
	err := transport.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}
