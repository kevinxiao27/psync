package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kevinxiao27/psync/daemon/merkle"
	"github.com/kevinxiao27/psync/daemon/meta"
	"github.com/kevinxiao27/psync/daemon/transport"
	"github.com/kevinxiao27/psync/daemon/watcher"
)

// MockTransport implements transport.Transport for testing.
type MockTransport struct {
	sentMessages [][]byte
	onMessage    transport.MessageHandler
	onConnected  transport.PeerHandler
}

func (m *MockTransport) Connect(ctx context.Context, signalURL string, peerID transport.PeerID, groupID string) error {
	return nil
}

func (m *MockTransport) SendTo(peerID transport.PeerID, data []byte) error {
	m.sentMessages = append(m.sentMessages, data)
	return nil
}

func (m *MockTransport) Broadcast(data []byte) error {
	m.sentMessages = append(m.sentMessages, data)
	return nil
}

func (m *MockTransport) OnMessage(handler transport.MessageHandler) {
	m.onMessage = handler
}

func (m *MockTransport) OnPeerConnected(handler transport.PeerHandler) {
	m.onConnected = handler
}

func (m *MockTransport) OnPeerDisconnected(handler transport.PeerHandler) {}

func (m *MockTransport) GetConnectedPeers() []transport.PeerID {
	return nil
}

func (m *MockTransport) Close() error {
	return nil
}

func (m *MockTransport) SendSignalMessage(messageType transport.SignalMessageType, payload interface{}) error {
	// For tests, just serialize as regular message to sentMessages
	data, err := MarshalSyncMessage(meta.SyncMessageType(messageType), "", payload)
	if err != nil {
		return err
	}
	m.sentMessages = append(m.sentMessages, data)
	return nil
}

// SimulatePeerConnect triggers the connected handler.
func (m *MockTransport) SimulatePeerConnect(peerID transport.PeerID) {
	if m.onConnected != nil {
		m.onConnected(peerID)
	}
}

// SimulateMessage triggers the message handler.
func (m *MockTransport) SimulateMessage(peerID transport.PeerID, data []byte) {
	if m.onMessage != nil {
		m.onMessage(peerID, data)
	}
}

func TestHandleInit_HashMatch(t *testing.T) {
	// Given two peers with the same root hash
	dir := t.TempDir()
	tree, _ := merkle.Build(dir, nil)
	mockTransport := &MockTransport{}
	mockWatcher, _ := watcher.NewWatcher(dir, 100*time.Millisecond)

	engine := NewEngine("peer-a", dir, mockTransport, mockWatcher, tree)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)

	// When peer-b connects with the same hash
	// (simulate receiving an init message with matching hash)
	payload := meta.InitPayload{
		PeerID:   "peer-b",
		RootHash: tree.Root.Hash,
	}
	data, _ := SyncMarshalSyncMessage(meta.MsgTypeInit, "peer-b", payload)
	mockTransport.SimulateMessage("peer-b", data)

	// Then no file_list should be requested
	// (check mockTransport.sentMessages)
	// We expect NO response if hashes match
	time.Sleep(50 * time.Millisecond)
	if len(mockTransport.sentMessages) > 0 {
		t.Errorf("Expected 0 messages, got %d", len(mockTransport.sentMessages))
	}
}

func TestHandleInit_HashDiffers(t *testing.T) {
	// Given two peers with different root hashes
	dir := t.TempDir()
	tree, _ := merkle.Build(dir, nil)
	mockTransport := &MockTransport{}
	mockWatcher, _ := watcher.NewWatcher(dir, 100*time.Millisecond)

	engine := NewEngine("peer-a", dir, mockTransport, mockWatcher, tree)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)

	// When peer-b connects with a different hash
	payload := meta.InitPayload{
		PeerID:   "peer-b",
		RootHash: "different-hash",
	}
	data, _ := MarshalSyncMessage(meta.MsgTypeInit, "peer-b", payload)
	mockTransport.SimulateMessage("peer-b", data)

	// Then a get_file_list message should be sent
	time.Sleep(50 * time.Millisecond)
	if len(mockTransport.sentMessages) == 0 {
		t.Fatal("Expected GetFileList message, got nothing")
	}

	// Verify last message is GetFileList request
	lastMsgBytes := mockTransport.sentMessages[len(mockTransport.sentMessages)-1]
	lastMsg, _ := UnmarshalSyncMessage(lastMsgBytes)
	if lastMsg.Type != meta.MsgTypeGetFileList {
		t.Errorf("Expected MsgTypeGetFileList, got %s", lastMsg.Type)
	}
}

func TestHandleLocalEvent_BroadcastsPush(t *testing.T) {
	// Given an engine with connected peers
	dir := t.TempDir()
	tree, _ := merkle.Build(dir, nil)
	mockTransport := &MockTransport{}
	mockWatcher, _ := watcher.NewWatcher(dir, 100*time.Millisecond)

	engine := NewEngine("peer-a", dir, mockTransport, mockWatcher, tree)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)

	// Simulate peer connection so we have someone to broadcast to
	mockTransport.Connect(ctx, "", "peer-b", "group")

	// When a local file is modified (simulated)
	// We call handleLocalEvent directly to avoid watcher timing issues in tests
	event := watcher.Event{Path: "test.txt", Type: watcher.EventCreate}
	// We need actual file on disk for handleLocalEvent to work (merkle build)
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("content"), 0644)

	engine.handleLocalEvent(event)

	// Then a broadcast should be sent (Init message with new hash)
	if len(mockTransport.sentMessages) == 0 {
		t.Error("Expected broadcast message")
	}
}

func TestHandlePeerConnected_SendsInit(t *testing.T) {
	// Given an engine
	dir := t.TempDir()
	tree, _ := merkle.Build(dir, nil)
	mockTransport := &MockTransport{}
	mockWatcher, _ := watcher.NewWatcher(dir, 100*time.Millisecond)

	engine := NewEngine("peer-a", dir, mockTransport, mockWatcher, tree)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)

	// When a peer connects
	mockTransport.SimulatePeerConnect("peer-b")

	// Then an init message should be sent
	if len(mockTransport.sentMessages) == 0 {
		t.Fatal("Expected Init message")
	}

	msg, _ := UnmarshalSyncMessage(mockTransport.sentMessages[0])
	if msg.Type != meta.MsgTypeInit {
		t.Errorf("Expected Init message, got %s", msg.Type)
	}
}

// Helper to access package-private marshal function if needed,
// but since we are in package sync (test), we can access MarshalSyncMessage
var SyncMarshalSyncMessage = MarshalSyncMessage
