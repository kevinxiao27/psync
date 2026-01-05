package transport

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

// -----------------------------------------------------------------------------
// WebRTC Transport Implementation
// -----------------------------------------------------------------------------

// WebRTCTransport implements Transport using Pion WebRTC.
type WebRTCTransport struct {
	localID  PeerID
	groupID  string
	signalWS *websocket.Conn
	wsMu     sync.Mutex // Protects signalWS writes

	peers   map[PeerID]*PeerConnection
	peersMu sync.RWMutex

	onMessage          MessageHandler
	onPeerConnected    PeerHandler
	onPeerDisconnected PeerHandler

	config webrtc.Configuration
	ctx    context.Context
	cancel context.CancelFunc
}

// NewWebRTCTransport creates a new WebRTC transport.
func NewWebRTCTransport() *WebRTCTransport {
	return &WebRTCTransport{
		peers: make(map[PeerID]*PeerConnection),
		config: webrtc.Configuration{
			ICEServers: []webrtc.ICEServer{
				{URLs: []string{"stun:stun.cloudflare.com:3478"}},
				{URLs: []string{"stun:stun.l.google.com:19302"}},
			},
		},
	}
}

// Connect implements Transport.Connect.
func (t *WebRTCTransport) Connect(ctx context.Context, signalURL string, peerID PeerID, groupID string) error {
	t.ctx, t.cancel = context.WithCancel(ctx)
	t.localID = peerID
	t.groupID = groupID

	// Connect to signal server
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, signalURL, nil)
	if err != nil {
		return err
	}
	t.signalWS = conn

	// Register with signal server
	regPayload, _ := json.Marshal(RegisterPayload{
		PeerID:  peerID,
		GroupID: groupID,
	})

	t.wsMu.Lock()
	err = t.signalWS.WriteJSON(SignalMessage{
		Type:     SignalRegister,
		SourceID: peerID,
		Payload:  regPayload,
	})
	t.wsMu.Unlock()

	if err != nil {
		return err
	}

	// Start signal message handler
	go t.handleSignalMessages()

	return nil
}

// handleSignalMessages processes messages from the signal server.
func (t *WebRTCTransport) handleSignalMessages() {
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		var msg SignalMessage
		if err := t.signalWS.ReadJSON(&msg); err != nil {
			return // Connection closed
		}

		switch msg.Type {
		case SignalPeerList:
			var payload PeerListPayload
			json.Unmarshal(msg.Payload, &payload)
			// Initiate connections to existing peers
			for _, peer := range payload.Peers {
				go t.initiateConnection(peer.ID)
			}

		case SignalPeerJoined:
			var payload PeerJoinedPayload
			json.Unmarshal(msg.Payload, &payload)
			// New peer joined - they will initiate connection to us

		case SignalOffer:
			var payload SDPPayload
			json.Unmarshal(msg.Payload, &payload)
			go t.handleOffer(msg.SourceID, payload.SDP)

		case SignalAnswer:
			var payload SDPPayload
			json.Unmarshal(msg.Payload, &payload)
			t.handleAnswer(msg.SourceID, payload.SDP)

		case SignalCandidate:
			var payload ICECandidatePayload
			json.Unmarshal(msg.Payload, &payload)
			t.handleCandidate(msg.SourceID, payload)
		}
	}
}

// initiateConnection creates an offer to connect to a peer.
func (t *WebRTCTransport) initiateConnection(peerID PeerID) error {
	pc, err := webrtc.NewPeerConnection(t.config)
	if err != nil {
		return err
	}

	// Create data channel
	dc, err := pc.CreateDataChannel("sync", nil)
	if err != nil {
		return err
	}

	peerConn := &PeerConnection{
		ID:          peerID,
		Conn:        pc,
		DataChannel: dc,
	}

	t.peersMu.Lock()
	t.peers[peerID] = peerConn
	t.peersMu.Unlock()

	// Set up handlers
	t.setupDataChannelHandlers(peerID, dc)
	t.setupPeerConnectionHandlers(peerID, pc)

	// Create offer
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return err
	}

	// Send offer via signal server
	payload, _ := json.Marshal(SDPPayload{SDP: offer.SDP})

	t.wsMu.Lock()
	err = t.signalWS.WriteJSON(SignalMessage{
		Type:     SignalOffer,
		SourceID: t.localID,
		TargetID: peerID,
		Payload:  payload,
	})
	t.wsMu.Unlock()
	return err
}

// handleOffer processes an incoming offer.
func (t *WebRTCTransport) handleOffer(peerID PeerID, sdp string) error {
	pc, err := webrtc.NewPeerConnection(t.config)
	if err != nil {
		return err
	}

	peerConn := &PeerConnection{
		ID:   peerID,
		Conn: pc,
	}

	t.peersMu.Lock()
	t.peers[peerID] = peerConn
	t.peersMu.Unlock()

	// Handle incoming data channels
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		peerConn.DataChannel = dc
		t.setupDataChannelHandlers(peerID, dc)
	})

	t.setupPeerConnectionHandlers(peerID, pc)

	// Set remote description
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	}); err != nil {
		return err
	}

	// Create answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return err
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return err
	}

	// Send answer
	payload, _ := json.Marshal(SDPPayload{SDP: answer.SDP})

	t.wsMu.Lock()
	err = t.signalWS.WriteJSON(SignalMessage{
		Type:     SignalAnswer,
		SourceID: t.localID,
		TargetID: peerID,
		Payload:  payload,
	})
	t.wsMu.Unlock()
	return err
}

// handleAnswer processes an incoming answer.
func (t *WebRTCTransport) handleAnswer(peerID PeerID, sdp string) {
	t.peersMu.RLock()
	peerConn, ok := t.peers[peerID]
	t.peersMu.RUnlock()

	if !ok {
		return
	}

	peerConn.Conn.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	})
}

// handleCandidate adds an ICE candidate.
func (t *WebRTCTransport) handleCandidate(peerID PeerID, c ICECandidatePayload) {
	t.peersMu.RLock()
	peerConn, ok := t.peers[peerID]
	t.peersMu.RUnlock()

	if !ok {
		return
	}

	peerConn.Conn.AddICECandidate(webrtc.ICECandidateInit{
		Candidate:     c.Candidate,
		SDPMid:        &c.SDPMid,
		SDPMLineIndex: &c.SDPMLineIndex,
	})
}

// setupDataChannelHandlers configures handlers for a data channel.
func (t *WebRTCTransport) setupDataChannelHandlers(peerID PeerID, dc *webrtc.DataChannel) {
	dc.OnOpen(func() {
		t.peersMu.Lock()
		if pc, ok := t.peers[peerID]; ok {
			pc.Connected = true
		}
		t.peersMu.Unlock()

		if t.onPeerConnected != nil {
			t.onPeerConnected(peerID)
		}
	})

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if t.onMessage != nil {
			t.onMessage(peerID, msg.Data)
		}
	})

	dc.OnClose(func() {
		if t.onPeerDisconnected != nil {
			t.onPeerDisconnected(peerID)
		}
	})
}

// setupPeerConnectionHandlers configures ICE candidate handling.
func (t *WebRTCTransport) setupPeerConnectionHandlers(peerID PeerID, pc *webrtc.PeerConnection) {
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		payload, _ := json.Marshal(ICECandidatePayload{
			Candidate:     c.ToJSON().Candidate,
			SDPMid:        *c.ToJSON().SDPMid,
			SDPMLineIndex: *c.ToJSON().SDPMLineIndex,
		})
		t.wsMu.Lock()
		t.signalWS.WriteJSON(SignalMessage{
			Type:     SignalCandidate,
			SourceID: t.localID,
			TargetID: peerID,
			Payload:  payload,
		})
		t.wsMu.Unlock()
	})
}

// SendTo implements Transport.SendTo.
func (t *WebRTCTransport) SendTo(peerID PeerID, data []byte) error {
	t.peersMu.RLock()
	peerConn, ok := t.peers[peerID]
	t.peersMu.RUnlock()

	if !ok || !peerConn.Connected || peerConn.DataChannel == nil {
		return ErrPeerNotConnected
	}

	log.Printf("[Transport] Sending %d bytes to %s", len(data), peerID)
	return peerConn.DataChannel.Send(data)
}

// Broadcast implements Transport.Broadcast.
func (t *WebRTCTransport) Broadcast(data []byte) error {
	t.peersMu.RLock()
	defer t.peersMu.RUnlock()

	log.Printf("[Transport] Broadcasting %d bytes to %d peers", len(data), len(t.peers))
	for id, pc := range t.peers {
		if pc.Connected && pc.DataChannel != nil {
			// log.Printf("[Transport] Sending to %s", id)
			if err := pc.DataChannel.Send(data); err != nil {
				log.Printf("Failed to send to %s: %v", id, err)
			}
		} else {
			log.Printf("[Transport] Skipping %s (not connected/no DC)", id)
		}
	}
	return nil
}

// OnMessage implements Transport.OnMessage.
func (t *WebRTCTransport) OnMessage(handler MessageHandler) {
	t.onMessage = handler
}

// OnPeerConnected implements Transport.OnPeerConnected.
func (t *WebRTCTransport) OnPeerConnected(handler PeerHandler) {
	t.onPeerConnected = handler
}

// OnPeerDisconnected implements Transport.OnPeerDisconnected.
func (t *WebRTCTransport) OnPeerDisconnected(handler PeerHandler) {
	t.onPeerDisconnected = handler
}

// GetConnectedPeers implements Transport.GetConnectedPeers.
func (t *WebRTCTransport) GetConnectedPeers() []PeerID {
	t.peersMu.RLock()
	defer t.peersMu.RUnlock()

	var peers []PeerID
	for id, pc := range t.peers {
		if pc.Connected {
			peers = append(peers, id)
		}
	}
	return peers
}

// Close implements Transport.Close.
func (t *WebRTCTransport) Close() error {
	if t.cancel != nil {
		t.cancel()
	}

	t.peersMu.Lock()
	defer t.peersMu.Unlock()

	for _, pc := range t.peers {
		pc.Conn.Close()
	}
	t.peers = make(map[PeerID]*PeerConnection)

	if t.signalWS != nil {
		t.signalWS.Close()
	}

	return nil
}
