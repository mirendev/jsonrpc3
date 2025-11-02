package jsonrpc3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebSocketIntegration_BidirectionalCallbacks tests full bidirectional callback scenario
func TestWebSocketIntegration_BidirectionalCallbacks(t *testing.T) {
	// Server state
	var serverConn *WebSocketConn
	var connMu sync.Mutex

	// Server methods
	serverRoot := NewMethodMap()
	serverRoot.Register("subscribe", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var p struct {
			Topic    string    `json:"topic"`
			Callback Reference `json:"callback"`
		}
		if err := params.Decode(&p); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}

		// Simulate server sending notifications to client callback
		go func() {
			time.Sleep(50 * time.Millisecond)
			connMu.Lock()
			sc := serverConn
			connMu.Unlock()

			if sc != nil {
				// Send notification to client callback
				sc.Notify("onUpdate", map[string]any{
					"topic":   p.Topic,
					"message": "Update 1",
				}, ToRef(p.Callback))

				time.Sleep(50 * time.Millisecond)
				sc.Notify("onUpdate", map[string]any{
					"topic":   p.Topic,
					"message": "Update 2",
				}, ToRef(p.Callback))
			}
		}()

		return map[string]any{
			"status": "subscribed",
			"topic":  p.Topic,
		}, nil
	})

	handler := NewWebSocketHandler(serverRoot)

	// Custom handler to capture connection
	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := handler.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		// Determine content type from accepted subprotocol
		contentType := "application/json"
		switch conn.Subprotocol() {
		case "jsonrpc3.cbor":
			contentType = "application/cbor"
		case "jsonrpc3.cbor-compact":
			contentType = "application/cbor; format=compact"
		}
		wsConn := newWebSocketConn(conn, serverRoot, contentType, handler.mimeTypes)

		connMu.Lock()
		serverConn = wsConn
		connMu.Unlock()

		wsConn.handle()
	})

	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Client with callback object
	var updates []string
	var updateMu sync.Mutex

	callbackObj := NewMethodMap()
	callbackObj.Register("onUpdate", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var data map[string]string
		if err := params.Decode(&data); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}

		updateMu.Lock()
		updates = append(updates, data["message"])
		updateMu.Unlock()

		return nil, nil
	})

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)
	defer client.Close()

	client.RegisterObject("my-callback", callbackObj)

	// Subscribe with callback
	val, err := client.Call("subscribe", map[string]any{
		"topic":    "news",
		"callback": NewReference("my-callback"),
	})
	require.NoError(t, err)
	var result map[string]any
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "subscribed", result["status"])

	// Wait for notifications
	time.Sleep(200 * time.Millisecond)

	// Verify notifications were received
	updateMu.Lock()
	assert.Len(t, updates, 2)
	assert.Equal(t, "Update 1", updates[0])
	assert.Equal(t, "Update 2", updates[1])
	updateMu.Unlock()
}

// TestWebSocketIntegration_ConcurrentBidirectional tests concurrent calls in both directions
func TestWebSocketIntegration_ConcurrentBidirectional(t *testing.T) {
	var serverConn *WebSocketConn
	var connMu sync.Mutex

	// Server methods
	serverRoot := NewMethodMap()
	serverRoot.Register("serverEcho", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		time.Sleep(10 * time.Millisecond) // Simulate work
		return "server: " + msg, nil
	})

	handler := NewWebSocketHandler(serverRoot)

	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := handler.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		// Determine content type from accepted subprotocol
		contentType := "application/json"
		switch conn.Subprotocol() {
		case "jsonrpc3.cbor":
			contentType = "application/cbor"
		case "jsonrpc3.cbor-compact":
			contentType = "application/cbor; format=compact"
		}
		wsConn := newWebSocketConn(conn, serverRoot, contentType, handler.mimeTypes)

		connMu.Lock()
		serverConn = wsConn
		connMu.Unlock()

		wsConn.handle()
	})

	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Client methods
	clientRoot := NewMethodMap()
	clientRoot.Register("clientEcho", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		time.Sleep(10 * time.Millisecond) // Simulate work
		return "client: " + msg, nil
	})

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, clientRoot)
	require.NoError(t, err)
	defer client.Close()

	// Wait for connection
	time.Sleep(100 * time.Millisecond)

	// Concurrent client→server and server→client calls
	const numCalls = 5
	var wg sync.WaitGroup
	wg.Add(numCalls * 2)

	// Client calls server
	for i := 0; i < numCalls; i++ {
		go func(n int) {
			defer wg.Done()
			val, err := client.Call("serverEcho", "test")
			assert.NoError(t, err)
			var result string
			err = val.Decode(&result)
			assert.NoError(t, err)
			assert.Equal(t, "server: test", result)
		}(i)
	}

	// Server calls client
	for i := 0; i < numCalls; i++ {
		go func(n int) {
			defer wg.Done()
			connMu.Lock()
			sc := serverConn
			connMu.Unlock()

			if sc != nil {
				val, err := sc.Call("clientEcho", "test")
				assert.NoError(t, err)
				var result string
				err = val.Decode(&result)
				assert.NoError(t, err)
				assert.Equal(t, "client: test", result)
			}
		}(i)
	}

	wg.Wait()
}

// TestWebSocketIntegration_ObjectLifecycle tests object creation, use, and disposal
func TestWebSocketIntegration_ObjectLifecycle(t *testing.T) {
	var serverConn *WebSocketConn
	var connMu sync.Mutex

	// Object counter
	var objectCount atomic.Int32

	// Server root methods
	serverRoot := NewMethodMap()
	serverRoot.Register("createObject", func(ctx context.Context, params Params, caller Caller) (any, error) {
		// Create a new object
		obj := NewMethodMap()
		obj.Register("getValue", func(ctx context.Context, params Params, caller Caller) (any, error) {
			return "object value", nil
		})

		objectCount.Add(1)
		return obj, nil // Handler will auto-register
	})

	handler := NewWebSocketHandler(serverRoot)

	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := handler.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		// Determine content type from accepted subprotocol
		contentType := "application/json"
		switch conn.Subprotocol() {
		case "jsonrpc3.cbor":
			contentType = "application/cbor"
		case "jsonrpc3.cbor-compact":
			contentType = "application/cbor; format=compact"
		}
		wsConn := newWebSocketConn(conn, serverRoot, contentType, handler.mimeTypes)

		connMu.Lock()
		serverConn = wsConn
		connMu.Unlock()

		wsConn.handle()
	})

	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)
	defer client.Close()

	// Create object
	val1, err := client.Call("createObject", nil)
	require.NoError(t, err)
	var ref Reference
	err = val1.Decode(&ref)
	require.NoError(t, err)
	assert.NotEmpty(t, ref.Ref)
	assert.Equal(t, int32(1), objectCount.Load())

	// Use object
	val2, err := client.Call("getValue", nil, ToRef(ref))
	require.NoError(t, err)
	var value string
	err = val2.Decode(&value)
	require.NoError(t, err)
	assert.Equal(t, "object value", value)

	// Verify object is in session
	time.Sleep(100 * time.Millisecond)
	connMu.Lock()
	sc := serverConn
	connMu.Unlock()
	require.NotNil(t, sc)

	session := sc.GetSession()
	refs := session.ListLocalRefs()
	assert.Contains(t, refs, ref.Ref)
}

// TestWebSocketIntegration_ProtocolMethods tests $rpc protocol methods over WebSocket
func TestWebSocketIntegration_ProtocolMethods(t *testing.T) {
	serverRoot := NewMethodMap()
	serverRoot.Register("createObject", func(ctx context.Context, params Params, caller Caller) (any, error) {
		obj := NewMethodMap()
		obj.Register("test", func(ctx context.Context, params Params, caller Caller) (any, error) {
			return "ok", nil
		})
		return obj, nil
	})

	handler := NewWebSocketHandler(serverRoot)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)
	defer client.Close()

	// Create object
	val1, err := client.Call("createObject", nil)
	require.NoError(t, err)
	var ref Reference
	err = val1.Decode(&ref)
	require.NoError(t, err)

	// Get session ID via protocol method
	val2, err := client.Call("session_id", nil, ToRef(Protocol))
	require.NoError(t, err)
	var sessionResult SessionIDResult
	err = val2.Decode(&sessionResult)
	require.NoError(t, err)
	assert.NotEmpty(t, sessionResult.SessionID)

	// List refs via protocol method
	val3, err := client.Call("list_refs", nil, ToRef(Protocol))
	require.NoError(t, err)
	var refs []RefInfoResult
	err = val3.Decode(&refs)
	require.NoError(t, err)
	// Client shouldn't see its own remote refs in list_refs
	// list_refs returns local refs, which for client are objects it exposes

	// Get mimetypes
	val4, err := client.Call("mimetypes", nil, ToRef(Protocol))
	require.NoError(t, err)
	var mimeResult MimeTypesResult
	err = val4.Decode(&mimeResult)
	require.NoError(t, err)
	assert.Contains(t, mimeResult.MimeTypes, "application/json")
}

// TestWebSocketIntegration_ReconnectionBehavior tests that sessions don't persist across reconnections
func TestWebSocketIntegration_ReconnectionBehavior(t *testing.T) {
	serverRoot := NewMethodMap()
	serverRoot.Register("test", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return "ok", nil
	})

	handler := NewWebSocketHandler(serverRoot)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// First connection
	client1, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)

	session1ID := client1.GetSession().ID()

	_, err = client1.Call("test", nil)
	require.NoError(t, err)

	// Close first connection
	client1.Close()

	// Second connection should have new session
	client2, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)
	defer client2.Close()

	session2ID := client2.GetSession().ID()

	// Sessions should be different
	assert.NotEqual(t, session1ID, session2ID)

	_, err = client2.Call("test", nil)
	require.NoError(t, err)
}

// TestWebSocketIntegration_MultipleClients tests multiple clients connecting to same server
func TestWebSocketIntegration_MultipleClients(t *testing.T) {
	serverRoot := NewMethodMap()
	serverRoot.Register("echo", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return msg, nil
	})

	handler := NewWebSocketHandler(serverRoot)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Create multiple clients
	const numClients = 5
	clients := make([]*WebSocketClient, numClients)

	for i := 0; i < numClients; i++ {
		client, err := NewWebSocketClient(wsURL, nil)
		require.NoError(t, err)
		clients[i] = client
		defer client.Close()
	}

	// All clients make requests concurrently
	var wg sync.WaitGroup
	wg.Add(numClients)

	for i := 0; i < numClients; i++ {
		go func(n int, c *WebSocketClient) {
			defer wg.Done()
			val, err := c.Call("echo", "test")
			assert.NoError(t, err)
			var result string
			err = val.Decode(&result)
			assert.NoError(t, err)
			assert.Equal(t, "test", result)
		}(i, clients[i])
	}

	wg.Wait()
}
