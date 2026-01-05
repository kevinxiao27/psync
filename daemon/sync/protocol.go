package sync

import (
	"encoding/json"
	"fmt"

	"github.com/kevinxiao27/psync/daemon/meta"
)

// MarshalSyncMessage serializes a SyncMessage to bytes.
func MarshalSyncMessage(msgType meta.SyncMessageType, sourceID meta.PeerID, payload interface{}) ([]byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	msg := meta.SyncMessage{
		Type:     msgType,
		SourceID: sourceID,
		Payload:  payloadBytes,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal sync message: %w", err)
	}

	return data, nil
}

// UnmarshalSyncMessage deserializes bytes to a SyncMessage.
func UnmarshalSyncMessage(data []byte) (*meta.SyncMessage, error) {
	var msg meta.SyncMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sync message: %w", err)
	}
	return &msg, nil
}

// ExtractInitPayload extracts InitPayload from a SyncMessage.
func ExtractInitPayload(msg *meta.SyncMessage) (*meta.InitPayload, error) {
	if msg.Type != meta.MsgTypeInit {
		return nil, fmt.Errorf("expected Init message, got %s", msg.Type)
	}
	var payload meta.InitPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal InitPayload: %w", err)
	}
	return &payload, nil
}

// ExtractFileListPayload extracts FileListPayload from a SyncMessage.
func ExtractFileListPayload(msg *meta.SyncMessage) (*meta.FileListPayload, error) {
	if msg.Type != meta.MsgTypeFileList {
		return nil, fmt.Errorf("expected FileList message, got %s", msg.Type)
	}
	var payload meta.FileListPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal FileListPayload: %w", err)
	}
	return &payload, nil
}

// ExtractFileRequestPayload extracts FileRequestPayload from a SyncMessage.
func ExtractFileRequestPayload(msg *meta.SyncMessage) (*meta.FileRequestPayload, error) {
	if msg.Type != meta.MsgTypeFileRequest {
		return nil, fmt.Errorf("expected FileRequest message, got %s", msg.Type)
	}
	var payload meta.FileRequestPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal FileRequestPayload: %w", err)
	}
	return &payload, nil
}

// ExtractFileDataPayload extracts FileDataPayload from a SyncMessage.
func ExtractFileDataPayload(msg *meta.SyncMessage) (*meta.FileDataPayload, error) {
	if msg.Type != meta.MsgTypeFileData {
		return nil, fmt.Errorf("expected FileData message, got %s", msg.Type)
	}
	var payload meta.FileDataPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal FileDataPayload: %w", err)
	}
	return &payload, nil
}
