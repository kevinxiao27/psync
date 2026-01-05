package api

import (
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/kevinxiao27/psync/signal-server/types"
)

// ConnectionManager handles active WebSocket connections for routing.
type ConnectionManager struct {
	conns   map[types.PeerID]*websocket.Conn
	connsMu sync.RWMutex
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		conns: make(map[types.PeerID]*websocket.Conn),
	}
}

// Add registers a connection for a peer.
func (cm *ConnectionManager) Add(peerID types.PeerID, conn *websocket.Conn) {
	cm.connsMu.Lock()
	defer cm.connsMu.Unlock()
	cm.conns[peerID] = conn
}

// Remove unregisters a connection for a peer.
func (cm *ConnectionManager) Remove(peerID types.PeerID) {
	cm.connsMu.Lock()
	defer cm.connsMu.Unlock()
	delete(cm.conns, peerID)
}

// SendTo sends a message to a specific peer.
func (cm *ConnectionManager) SendTo(peerID types.PeerID, msg types.Message) {
	cm.connsMu.RLock()
	conn, ok := cm.conns[peerID]
	cm.connsMu.RUnlock()

	if !ok {
		return
	}

	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("Failed to send to peer %s: %v", peerID, err)
	}
}
