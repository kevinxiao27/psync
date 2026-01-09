package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kevinxiao27/psync/signal-server/store"
	"github.com/kevinxiao27/psync/signal-server/types"
)

// Server represents the signalling server.
type Server struct {
	addr     string
	store    store.PeerStore
	upgrader websocket.Upgrader
	connMgr  *ConnectionManager
}

// NewServer creates a new instance of the signalling server.
func NewServer(addr string) *Server {
	return &Server{
		addr:    addr,
		store:   store.NewMemoryStore(),
		connMgr: NewConnectionManager(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// Start begins the server listening loop.
func (s *Server) Start() error {
	// Background cleanup goroutine
	go s.runCleanupLoop()

	fmt.Printf("Signal-Server starting on address: %s\n", s.addr)
	return http.ListenAndServe(s.addr, s.Handler())
}

// GetPeer retrieves a peer by ID from ANY group (convenience for testing).
func (s *Server) GetPeer(id types.PeerID) (types.Peer, bool) {
	for _, p := range s.store.GetAllPeers() {
		if p.ID == id {
			return p, true
		}
	}
	return types.Peer{}, false
}

const (
	cleanupInterval = 10 * time.Second
	peerMaxAge      = 1 * time.Minute
)

func (s *Server) runCleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		pruned := s.store.PruneStale(peerMaxAge)
		if pruned > 0 {
			log.Printf("Pruned %d stale peer(s)", pruned)
		}
	}
}

// Handler returns the HTTP handler for the server (ServeMux).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)
	return mux
}

// handleHealth returns a simple 200 OK.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK\n"))
}

// handleWebSocket handles the upgrade and connection lifecycle.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Printf("WebSocket upgrade failed: %v\n", err)
		return
	}
	s.handleConnection(conn)
}

// handleConnection manages the WebSocket lifecycle.
func (s *Server) handleConnection(conn *websocket.Conn) {
	defer conn.Close()
	fmt.Printf("Handling new connection from client: %s\n", conn.RemoteAddr())

	var currentPeerID types.PeerID
	var currentGroupID string

	defer func() {
		if currentPeerID != "" {
			s.connMgr.Remove(currentPeerID)
		}
	}()

	handleMessageLoop(conn, currentPeerID, currentGroupID, s)
}

func handleMessageLoop(conn *websocket.Conn, currentPeerID types.PeerID, currentGroupID string, s *Server) {
	for {
		var msg types.Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Read error: %v", err)
			}
			break
		}

		switch msg.Type {
		case types.MessageTypeRegister:
			// Parse payload
			var payload types.RegisterPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Printf("Invalid register payload: %v", err)
				continue
			}

			currentPeerID = payload.PeerID
			currentGroupID = payload.GroupID

			peer := types.Peer{
				ID:       payload.PeerID,
				GroupID:  payload.GroupID,
				LastSeen: time.Now(),
			}

			// 1. Storage
			if err := s.store.AddPeer(peer); err != nil {
				log.Printf("Failed to register peer: %v", err)
				continue
			}

			// 2. Track Connection
			s.connMgr.Add(peer.ID, conn)

			// 3. Send PeerList (existing peers)
			peers := s.store.GetPeersInGroup(payload.GroupID)
			var otherPeers []types.Peer
			for _, p := range peers {
				if p.ID != peer.ID {
					otherPeers = append(otherPeers, p)
				}
			}

			listPayload, _ := json.Marshal(types.PeerListPayload{Peers: otherPeers})
			// Use "peer_list" string as expected by tests
			conn.WriteJSON(types.Message{
				Type:    "peer_list",
				Payload: listPayload,
			})

			// 4. Broadcast PeerJoined to others
			joinedPayload, _ := json.Marshal(types.PeerJoinedPayload{Peer: peer})
			broadcastMsg := types.Message{
				Type:    "peer_joined",
				Payload: joinedPayload,
			}

			for _, p := range otherPeers {
				s.connMgr.SendTo(p.ID, broadcastMsg)
			}

		case types.MessageTypeOffer, types.MessageTypeAnswer, types.MessageTypeCandidate:
			// Routing
			if msg.TargetID == "" {
				continue
			}
			s.connMgr.SendTo(msg.TargetID, msg)

		case types.MessageTypeHeartbeat:
			if currentPeerID != "" {
				s.store.UpdatePeerActivity(types.Peer{
					ID:      currentPeerID,
					GroupID: currentGroupID,
				})
			}
		}
	}
}
