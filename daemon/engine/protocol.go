package sync

import (
	"encoding/json"
	"fmt"

	"github.com/kevinxiao27/psync/daemon/vclock"
)

// MarshalSyncMessage serializes a SyncMessage to bytes.
func MarshalSyncMessage(msgType vclock.SyncMessageType, sourceID vclock.PeerID, payload any) ([]byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	msg := vclock.SyncMessage{
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
func UnmarshalSyncMessage(data []byte) (*vclock.SyncMessage, error) {
	var msg vclock.SyncMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sync message: %w", err)
	}
	return &msg, nil
}

// ExtractInitPayload extracts InitPayload from a SyncMessage.
func ExtractInitPayload(msg *vclock.SyncMessage) (*vclock.InitPayload, error) {
	if msg.Type != vclock.MsgTypeInit {
		return nil, fmt.Errorf("expected Init message, got %s", msg.Type)
	}
	var payload vclock.InitPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal InitPayload: %w", err)
	}
	return &payload, nil
}

// ExtractFileListPayload extracts FileListPayload from a SyncMessage.
func ExtractFileListPayload(msg *vclock.SyncMessage) (*vclock.FileListPayload, error) {
	if msg.Type != vclock.MsgTypeFileList {
		return nil, fmt.Errorf("expected FileList message, got %s", msg.Type)
	}
	var payload vclock.FileListPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal FileListPayload: %w", err)
	}
	return &payload, nil
}

// ExtractFileRequestPayload extracts FileRequestPayload from a SyncMessage.
func ExtractFileRequestPayload(msg *vclock.SyncMessage) (*vclock.FileRequestPayload, error) {
	if msg.Type != vclock.MsgTypeFileRequest {
		return nil, fmt.Errorf("expected FileRequest message, got %s", msg.Type)
	}
	var payload vclock.FileRequestPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal FileRequestPayload: %w", err)
	}
	return &payload, nil
}

// ExtractFileDataPayload extracts FileDataPayload from a SyncMessage.
func ExtractFileDataPayload(msg *vclock.SyncMessage) (*vclock.FileDataPayload, error) {
	if msg.Type != vclock.MsgTypeFileData {
		return nil, fmt.Errorf("expected FileData message, got %s", msg.Type)
	}
	var payload vclock.FileDataPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal FileDataPayload: %w", err)
	}
	return &payload, nil
}
