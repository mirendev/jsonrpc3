package jsonrpc3

import (
	"encoding/binary"
	"fmt"
	"io"
)

// writeFramedMessage writes a length-prefixed message to the stream.
// Format: [4-byte big-endian length][message data]
// This is necessary because WebTransport streams don't have built-in message framing
// like WebSocket frames.
func writeFramedMessage(w io.Writer, data []byte) error {
	// Write 4-byte length prefix
	length := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return fmt.Errorf("failed to write message length: %w", err)
	}

	// Write message data
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write message data: %w", err)
	}

	return nil
}

// readFramedMessage reads a length-prefixed message from the stream.
// Returns the message data or an error.
func readFramedMessage(r io.Reader) ([]byte, error) {
	// Read 4-byte length prefix
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("failed to read message length: %w", err)
	}

	// Sanity check: prevent excessive memory allocation
	// Limit messages to 100MB (configurable if needed)
	const maxMessageSize = 100 * 1024 * 1024
	if length > maxMessageSize {
		return nil, fmt.Errorf("message too large: %d bytes (max %d)", length, maxMessageSize)
	}

	// Read message data
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("failed to read message data: %w", err)
	}

	return data, nil
}
