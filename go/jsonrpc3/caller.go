package jsonrpc3

// Caller is the interface for making JSON-RPC method calls and sending notifications.
// It is implemented by Peer, WebSocketClient, WebTransportClient, and WebTransportConn.
type Caller interface {
	// Call invokes a method on the remote peer and waits for the response.
	Call(method string, params any, result any) error

	// CallRef invokes a method on a remote reference and waits for the response.
	CallRef(ref string, method string, params any, result any) error

	// Notify sends a notification (no response expected).
	Notify(method string, params any) error

	// NotifyRef sends a notification to a remote reference.
	NotifyRef(ref string, method string, params any) error

	// RegisterObject registers a local object that the remote peer can call.
	RegisterObject(ref string, obj Object)

	// UnregisterObject removes a registered local object.
	UnregisterObject(ref string)

	// GetSession returns the session for managing remote references.
	GetSession() *Session

	// Close closes the connection gracefully.
	Close() error
}
