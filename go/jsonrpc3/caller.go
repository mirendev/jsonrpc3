package jsonrpc3

// CallOption is a functional option for Call and Notify operations.
type CallOption interface {
	apply(*callOptions)
}

// callOptions holds the options for Call and Notify operations.
type callOptions struct {
	ref *Reference
}

// ToRef specifies that the operation should target a remote object reference.
// The ref parameter should be a Reference value obtained from a previous call.
// This option works with both Call and Notify operations.
func ToRef(ref Reference) CallOption {
	return toRefOption{ref: ref}
}

type toRefOption struct {
	ref Reference
}

func (t toRefOption) apply(o *callOptions) {
	o.ref = &t.ref
}

// Caller is the interface for making JSON-RPC method calls and sending notifications.
// It is implemented by Peer, WebSocketClient, WebTransportClient, and WebTransportConn.
type Caller interface {
	// Call invokes a method on the remote peer and waits for the response.
	// Returns a Value that can be decoded or inspected.
	// Use the ToRef option to call a method on a remote object reference.
	Call(method string, params any, opts ...CallOption) (Value, error)

	// Notify sends a notification (no response expected).
	// Use the ToRef option to send a notification to a remote object reference.
	Notify(method string, params any, opts ...CallOption) error

	// CallBatch sends a batch of requests and returns the results.
	CallBatch(requests []BatchRequest) (*BatchResults, error)

	// RegisterObject registers a local object that the remote peer can call.
	// If ref is empty, a random reference ID is generated.
	// Returns a Reference instance that can be used to identify the object.
	RegisterObject(ref string, obj Object) Reference

	// UnregisterObject removes a registered local object.
	UnregisterObject(ref Reference)

	// GetSession returns the session for managing remote references.
	GetSession() *Session

	// Close closes the connection gracefully.
	Close() error
}
