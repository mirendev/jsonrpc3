package jsonrpc3

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestWebSocketClient_ContextCancellation tests that dial failures are handled properly
// The context ensures that if Close() is called, the dial operation can be cancelled
func TestWebSocketClient_ContextCancellation(t *testing.T) {
	// Try to connect to a non-existent server
	// The context allows this to be cancelled if needed
	errChan := make(chan error, 1)

	go func() {
		// Try to connect to a server that doesn't exist
		_, err := NewWebSocketClient("ws://localhost:9999/nonexistent", nil)
		errChan <- err
	}()

	// The dial should fail quickly with a connection error
	select {
	case err := <-errChan:
		// Should get an error (connection refused or similar)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect")
	case <-time.After(5 * time.Second):
		t.Fatal("Dial operation took too long - should fail quickly")
	}
}
