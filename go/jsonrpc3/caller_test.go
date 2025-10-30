package jsonrpc3

import "testing"

// TestCallerInterface verifies that all expected types implement the Caller interface.
func TestCallerInterface(t *testing.T) {
	// These assignments will fail at compile time if the types don't implement Caller
	var _ Caller = (*Peer)(nil)
	var _ Caller = (*WebSocketClient)(nil)
	var _ Caller = (*WebTransportClient)(nil)
	var _ Caller = (*WebTransportConn)(nil)
}
