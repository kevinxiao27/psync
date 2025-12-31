# Signal Server

WebSocket-based signaling server for PSync peer discovery and WebRTC connection establishment.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/ws` | WebSocket | Main signaling connection |
| `/health` | GET | Health check (returns `OK`) |

## Message Protocol

All messages use JSON over WebSocket with this envelope:

```json
{
  "type": "message_type",
  "source_id": "sender_peer_id",
  "target_id": "recipient_peer_id",  // optional
  "payload": {}                       // type-specific data
}
```

### Message Types

#### `register` - Join a group
```json
{
  "type": "register",
  "source_id": "my-peer-id",
  "payload": {
    "peer_id": "my-peer-id",
    "group_id": "my-group"
  }
}
```
**Response**: Server sends `peer_list` with existing peers.

#### `peer_list` - Existing peers (server → client)
```json
{
  "type": "peer_list",
  "payload": {
    "peers": [
      {"id": "peer-a", "group_id": "my-group", "last_seen": "..."}
    ]
  }
}
```

#### `peer_joined` - New peer notification (server → client)
```json
{
  "type": "peer_joined",
  "payload": {
    "peer": {"id": "new-peer", "group_id": "my-group", "last_seen": "..."}
  }
}
```

#### `offer` / `answer` / `candidate` - WebRTC signaling
Routed directly to `target_id`. Payload contains SDP/ICE data (opaque to server).

```json
{
  "type": "offer",
  "source_id": "peer-a",
  "target_id": "peer-b",
  "payload": {"sdp": "..."}
}
```

#### `heartbeat` - Keep connection alive
```json
{
  "type": "heartbeat",
  "source_id": "my-peer-id",
  "payload": {"timestamp": 1703987654}
}
```

## Flow

```
Client A                    Server                    Client B
    |                          |                          |
    |-- register (group-1) --->|                          |
    |<-- peer_list [] ---------|                          |
    |                          |                          |
    |                          |<-- register (group-1) ---|
    |                          |--- peer_list [A] ------->|
    |<-- peer_joined (B) ------|                          |
    |                          |                          |
    |-- offer (to B) --------->|--- offer --------------->|
    |<-- answer ---------------|<-- answer (to A) --------|
    |-- candidate (to B) ----->|--- candidate ----------->|
    |<-- candidate ------------|<-- candidate (to A) -----|
    |                          |                          |
    |<========= Direct P2P Connection (WebRTC) ==========>|
```

## Running

```bash
cd signal-server
go run ./cmd/server -port [PORT_NUMBER]
```

## Testing

```bash
go test -v ./...
```
