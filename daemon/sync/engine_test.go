package sync

import (
	"context"
	"testing"
	"time"

	"github.com/kevinxiao27/psync/daemon/merkle"
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
	tree, _ := merkle.Build(dir)
	mockTransport := &MockTransport{}
	mockWatcher, _ := watcher.NewWatcher(dir, 100*time.Millisecond)

	engine := NewEngine("peer-a", dir, mockTransport, mockWatcher, tree)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)

	// When peer-b connects with the same hash
	// (simulate receiving an init message with matching hash)
	// TODO: Implement after protocol.go exists

	// Then no file_list should be requested
	// (check mockTransport.sentMessages)
	t.Skip("Pending protocol.go implementation")
}

func TestHandleInit_HashDiffers(t *testing.T) {
	// Given two peers with different root hashes
	dir := t.TempDir()
	tree, _ := merkle.Build(dir)
	mockTransport := &MockTransport{}
	mockWatcher, _ := watcher.NewWatcher(dir, 100*time.Millisecond)

	engine := NewEngine("peer-a", dir, mockTransport, mockWatcher, tree)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)

	// When peer-b connects with a different hash
	// TODO: Implement after protocol.go exists

	// Then a file_list message should be requested
	t.Skip("Pending protocol.go implementation")
}

func TestHandleLocalEvent_BroadcastsPush(t *testing.T) {
	// Given an engine with connected peers
	dir := t.TempDir()
	tree, _ := merkle.Build(dir)
	mockTransport := &MockTransport{}
	mockWatcher, _ := watcher.NewWatcher(dir, 100*time.Millisecond)

	engine := NewEngine("peer-a", dir, mockTransport, mockWatcher, tree)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)

	// When a local file is modified
	// TODO: Trigger watcher event

	// Then a broadcast should be sent
	t.Skip("Pending engine implementation")
}

func TestHandlePeerConnected_SendsInit(t *testing.T) {
	// Given an engine
	dir := t.TempDir()
	tree, _ := merkle.Build(dir)
	mockTransport := &MockTransport{}
	mockWatcher, _ := watcher.NewWatcher(dir, 100*time.Millisecond)

	engine := NewEngine("peer-a", dir, mockTransport, mockWatcher, tree)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)

	// When a peer connects
	mockTransport.SimulatePeerConnect("peer-b")

	// Then an init message should be sent
	// TODO: Verify mockTransport.sentMessages contains init
	t.Skip("Pending protocol.go implementation")
}
