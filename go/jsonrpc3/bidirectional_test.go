package jsonrpc3

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BidirectionalPair connects two Handler instances for bidirectional testing
// Simulates a scenario where both client and server can make calls to each other
type BidirectionalPair struct {
	clientSession *Session
	serverSession *Session
	clientHandler *Handler
	serverHandler *Handler
	nextID        int
}

// NewBidirectionalPair creates a pair of handlers that can call each other
func NewBidirectionalPair(clientRoot, serverRoot Object) *BidirectionalPair {
	clientSession := NewSession()
	serverSession := NewSession()

	clientHandler := NewHandler(clientSession, clientRoot, nil)
	serverHandler := NewHandler(serverSession, serverRoot, nil)

	return &BidirectionalPair{
		clientSession: clientSession,
		serverSession: serverSession,
		clientHandler: clientHandler,
		serverHandler: serverHandler,
		nextID:        1,
	}
}

// ClientCall makes a call from client to server
func (p *BidirectionalPair) ClientCall(method string, params any, result any) error {
	id := p.nextID
	p.nextID++

	req, err := NewRequest(method, params, id)
	if err != nil {
		return err
	}

	// Process request on server side
	resp := p.serverHandler.HandleRequest(req)

	// Track any local references from server as remote refs on client
	if resp.Result != nil {
		p.trackReferences(resp.Result, p.clientSession, p.serverSession)
	}

	if resp.Error != nil {
		return resp.Error
	}

	if result != nil {
		// Decode result
		params := NewParams(resp.Result)
		return params.Decode(result)
	}

	return nil
}

// ServerCallClient makes a call from server to client (callback)
// This is used when server invokes a method on a client-provided reference
func (p *BidirectionalPair) ServerCallClient(ref, method string, params any, result any) error {
	id := p.nextID
	p.nextID++

	req, err := NewRequestWithRef(ref, method, params, id)
	if err != nil {
		return err
	}

	// Process request on client side
	resp := p.clientHandler.HandleRequest(req)

	// Track any local references from client as remote refs on server
	if resp.Result != nil {
		p.trackReferences(resp.Result, p.serverSession, p.clientSession)
	}

	if resp.Error != nil {
		return resp.Error
	}

	if result != nil {
		// Decode result
		params := NewParams(resp.Result)
		return params.Decode(result)
	}

	return nil
}

// trackReferences scans the result for LocalReferences and tracks them as RemoteRefs
func (p *BidirectionalPair) trackReferences(data RawMessage, receiverSession, senderSession *Session) {
	// Parse as generic structure
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return
	}

	p.trackReferencesInValue(generic, receiverSession, senderSession)
}

// trackReferencesInValue recursively finds LocalReferences and tracks them
func (p *BidirectionalPair) trackReferencesInValue(value any, receiverSession, senderSession *Session) {
	switch v := value.(type) {
	case map[string]any:
		// Check if this is a LocalReference
		if refStr, ok := v["$ref"].(string); ok && len(v) == 1 {
			// This is a LocalReference - track it as RemoteRef in receiver session
			if obj := senderSession.GetLocalRef(refStr); obj != nil {
				receiverSession.AddRemoteRef(refStr, nil)
			}
		} else {
			// Recurse into map values
			for _, val := range v {
				p.trackReferencesInValue(val, receiverSession, senderSession)
			}
		}
	case []any:
		// Recurse into array elements
		for _, val := range v {
			p.trackReferencesInValue(val, receiverSession, senderSession)
		}
	}
}

// TestBidirectionalCallback demonstrates server calling back to client
func TestBidirectionalCallback(t *testing.T) {
	// Client-side callback object
	callbackCalled := false
	var callbackMessage string

	clientRoot := NewMethodMap()
	clientRoot.Register("onNotify", func(params Params) (any, error) {
		callbackCalled = true
		err := params.Decode(&callbackMessage)
		if err != nil {
			return nil, err
		}
		return "callback received: " + callbackMessage, nil
	})

	// Server-side method that accepts a callback
	serverRoot := NewMethodMap()
	var capturedCallbackRef string

	serverRoot.Register("subscribe", func(params Params) (any, error) {
		var p struct {
			Topic    string         `json:"topic"`
			Callback LocalReference `json:"callback"`
		}
		if err := params.Decode(&p); err != nil {
			return nil, err
		}

		capturedCallbackRef = p.Callback.Ref
		return map[string]any{
			"status": "subscribed",
			"topic":  p.Topic,
		}, nil
	})

	// Create bidirectional pair
	pair := NewBidirectionalPair(clientRoot, serverRoot)

	// Step 1: Client registers a local callback object
	callbackRef := "client-callback-1"
	callbackObj := clientRoot // Use the method map itself as the callback object
	pair.clientSession.AddLocalRef(callbackRef, callbackObj)

	// Step 2: Client calls server's subscribe method, passing callback reference
	var subscribeResult struct {
		Status string `json:"status"`
		Topic  string `json:"topic"`
	}
	err := pair.ClientCall("subscribe", map[string]any{
		"topic":    "updates",
		"callback": map[string]string{"$ref": callbackRef},
	}, &subscribeResult)

	require.NoError(t, err)
	assert.Equal(t, "subscribed", subscribeResult.Status)
	assert.Equal(t, "updates", subscribeResult.Topic)
	assert.Equal(t, callbackRef, capturedCallbackRef, "Server should have captured callback ref")

	// Step 3: Server tracks client's callback as a remote reference
	pair.serverSession.AddRemoteRef(capturedCallbackRef, nil)

	// Step 4: Server invokes callback on client
	var callbackResult string
	err = pair.ServerCallClient(capturedCallbackRef, "onNotify", "Hello from server!", &callbackResult)

	require.NoError(t, err)
	assert.True(t, callbackCalled, "Client callback should have been invoked")
	assert.Equal(t, "Hello from server!", callbackMessage)
	assert.Equal(t, "callback received: Hello from server!", callbackResult)
}

// TestBidirectionalCallbackWithObjectReturn tests bidirectional callbacks with object returns
func TestBidirectionalCallbackWithObjectReturn(t *testing.T) {
	// Client callback that returns an object
	type ClientCounter struct {
		Value int
	}

	clientCounter := &ClientCounter{Value: 0}

	clientRoot := NewMethodMap()
	clientRoot.Register("getCounter", func(params Params) (any, error) {
		return clientCounter, nil
	})

	// Server method that calls client callback and uses returned object
	serverRoot := NewMethodMap()
	var capturedCallbackRef string

	serverRoot.Register("registerCallback", func(params Params) (any, error) {
		var callback LocalReference
		if err := params.Decode(&callback); err != nil {
			return nil, err
		}
		capturedCallbackRef = callback.Ref
		return "registered", nil
	})

	// Create bidirectional pair
	pair := NewBidirectionalPair(clientRoot, serverRoot)

	// Client registers callback
	callbackRef := "client-callback-2"
	pair.clientSession.AddLocalRef(callbackRef, clientRoot)

	// Client calls server
	var registerResult string
	err := pair.ClientCall("registerCallback", LocalReference{Ref: callbackRef}, &registerResult)
	require.NoError(t, err)
	assert.Equal(t, "registered", registerResult)

	// Server tracks callback
	pair.serverSession.AddRemoteRef(capturedCallbackRef, nil)

	// Server calls client callback to get counter object
	var counterResult map[string]any
	err = pair.ServerCallClient(capturedCallbackRef, "getCounter", nil, &counterResult)
	require.NoError(t, err)

	// Counter should have been returned
	assert.Contains(t, counterResult, "Value")
	assert.Equal(t, float64(0), counterResult["Value"]) // JSON numbers are float64
}

// TestBidirectionalCallbackMultiple tests multiple callbacks
func TestBidirectionalCallbackMultiple(t *testing.T) {
	// Client with multiple callback methods
	callCounts := make(map[string]int)

	clientRoot := NewMethodMap()
	clientRoot.Register("onEvent", func(params Params) (any, error) {
		var event string
		params.Decode(&event)
		callCounts["onEvent"]++
		return "event: " + event, nil
	})
	clientRoot.Register("onError", func(params Params) (any, error) {
		var errMsg string
		params.Decode(&errMsg)
		callCounts["onError"]++
		return "error: " + errMsg, nil
	})

	serverRoot := NewMethodMap()
	var callbackRef string

	serverRoot.Register("registerListener", func(params Params) (any, error) {
		var ref LocalReference
		params.Decode(&ref)
		callbackRef = ref.Ref
		return "ok", nil
	})

	pair := NewBidirectionalPair(clientRoot, serverRoot)

	// Register callback
	ref := "listener-1"
	pair.clientSession.AddLocalRef(ref, clientRoot)

	var result string
	err := pair.ClientCall("registerListener", LocalReference{Ref: ref}, &result)
	require.NoError(t, err)

	pair.serverSession.AddRemoteRef(callbackRef, nil)

	// Server calls multiple methods on client callback
	var eventResult, errorResult string

	err = pair.ServerCallClient(callbackRef, "onEvent", "test-event", &eventResult)
	require.NoError(t, err)
	assert.Equal(t, "event: test-event", eventResult)

	err = pair.ServerCallClient(callbackRef, "onError", "test-error", &errorResult)
	require.NoError(t, err)
	assert.Equal(t, "error: test-error", errorResult)

	// Verify both callbacks were invoked
	assert.Equal(t, 1, callCounts["onEvent"])
	assert.Equal(t, 1, callCounts["onError"])
}

// TestBidirectionalCallbackError tests error handling in callbacks
func TestBidirectionalCallbackError(t *testing.T) {
	clientRoot := NewMethodMap()
	clientRoot.Register("failingCallback", func(params Params) (any, error) {
		return nil, &Error{
			Code:    CodeInternalError,
			Message: "callback failed",
		}
	})

	serverRoot := NewMethodMap()
	var callbackRef string

	serverRoot.Register("register", func(params Params) (any, error) {
		var ref LocalReference
		params.Decode(&ref)
		callbackRef = ref.Ref
		return "ok", nil
	})

	pair := NewBidirectionalPair(clientRoot, serverRoot)

	ref := "callback-err"
	pair.clientSession.AddLocalRef(ref, clientRoot)

	var result string
	err := pair.ClientCall("register", LocalReference{Ref: ref}, &result)
	require.NoError(t, err)

	pair.serverSession.AddRemoteRef(callbackRef, nil)

	// Server calls client callback that returns an error
	err = pair.ServerCallClient(callbackRef, "failingCallback", nil, &result)
	require.Error(t, err)

	// Verify it's an RPC error
	rpcErr, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, CodeInternalError, rpcErr.Code)
	assert.Contains(t, rpcErr.Message, "callback failed")
}
