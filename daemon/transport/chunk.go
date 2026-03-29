package transport

import (
	"encoding/binary"
	"fmt"
)

const (
	ChunkSize       = 16 * 1024                   // ChunkSize is the max payload per chunk frame (16 KiB).
	ChunkHeaderSize = 12                          // Header Size is the size of metadata
	MaxFrameSize    = ChunkHeaderSize + ChunkSize // Sum above

	// SmallFileFrameLimit is the max number of frames before chunked transfer kicks in.
	// Files requiring more frames than this use the chunked path; below this the
	// existing single-message MsgTypeFileData path is used.
	SmallFileFrameLimit = 4

	// FileSizeThreshold is the byte size above which chunked transfer is used,
	FileSizeThreshold          = SmallFileFrameLimit * ChunkSize // ChunkSize is the max payload per chunk frame (16 KiB).
	MaxBufferedAmount          = ChunkSize * 16
	BufferedAmountLowThreshold = 128 * 1024 // ChunkSize is the max payload per chunk frame (16 KiB).
)

// EncodeChunk creates a binary chunk frame.
//
// Wire format (big-endian):
//
//	[0:4]  ChunkIndex  (uint32)
//	[4:8]  TotalChunks (uint32)
//	[8:12] DataLen     (uint32)
//	[12:N] Data        (raw bytes)
func EncodeChunk(index, total int, data []byte) []byte {
	frame := make([]byte, ChunkHeaderSize+len(data))
	binary.BigEndian.PutUint32(frame[0:4], uint32(index))
	binary.BigEndian.PutUint32(frame[4:8], uint32(total))
	binary.BigEndian.PutUint32(frame[8:12], uint32(len(data)))
	copy(frame[12:], data)
	return frame
}

// DecodeChunk parses a binary chunk frame into its components.
func DecodeChunk(frame []byte) (index, total int, data []byte, err error) {
	if len(frame) < ChunkHeaderSize {
		return 0, 0, nil, fmt.Errorf("chunk frame too short: %d bytes", len(frame))
	}

	index = int(binary.BigEndian.Uint32(frame[0:4]))
	total = int(binary.BigEndian.Uint32(frame[4:8]))
	dataLen := int(binary.BigEndian.Uint32(frame[8:12]))

	if ChunkHeaderSize+dataLen > len(frame) {
		return 0, 0, nil, fmt.Errorf("chunk data truncated: header says %d bytes, frame has %d", dataLen, len(frame)-ChunkHeaderSize)
	}

	data = frame[ChunkHeaderSize : ChunkHeaderSize+dataLen]
	return index, total, data, nil
}
