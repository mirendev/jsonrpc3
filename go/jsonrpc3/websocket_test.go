package jsonrpc3

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebSocket_BasicConnection tests that WebSocket connection can be established
func TestWebSocket_BasicConnection(t *testing.T) {
	root := NewMethodMap()
	handler := NewWebSocketHandler(root)

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)
	defer client.Close()

	assert.NotNil(t, client.GetSession())
}

// TestWebSocket_ClientToServerCall tests client calling server method
func TestWebSocket_ClientToServerCall(t *testing.T) {
	root := NewMethodMap()
	root.Register("add", func(params Params) (any, error) {
		var nums []int
		if err := params.Decode(&nums); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		sum := 0
		for _, n := range nums {
			sum += n
		}
		return sum, nil
	})

	handler := NewWebSocketHandler(root)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)
	defer client.Close()

	val, err := client.Call("add", []int{1, 2, 3})
	require.NoError(t, err)
	var result int
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 6, result)
}

// TestWebSocket_ClientToServerNotification tests client sending notification to server
func TestWebSocket_ClientToServerNotification(t *testing.T) {
	var called bool
	var mu sync.Mutex

	root := NewMethodMap()
	root.Register("notify", func(params Params) (any, error) {
		mu.Lock()
		called = true
		mu.Unlock()
		return nil, nil
	})

	handler := NewWebSocketHandler(root)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)
	defer client.Close()

	err = client.Notify("notify", "test")
	require.NoError(t, err)

	// Wait a bit for notification to be processed
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.True(t, called)
	mu.Unlock()
}

// TestWebSocket_ServerToClientCall tests server calling client method
func TestWebSocket_ServerToClientCall(t *testing.T) {
	// Track the server connection
	var serverConn *WebSocketConn
	var connMu sync.Mutex

	root := NewMethodMap()
	root.Register("getConnection", func(params Params) (any, error) {
		// This is a hack to get the server connection for testing
		// In real code, connections would be tracked differently
		return "ok", nil
	})

	handler := NewWebSocketHandler(root)

	// Wrap handler to capture connection
	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Upgrade connection
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
		wsConn := newWebSocketConn(conn, root, contentType, handler.mimeTypes)

		connMu.Lock()
		serverConn = wsConn
		connMu.Unlock()

		wsConn.handle()
	})

	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Client with method
	clientRoot := NewMethodMap()
	clientRoot.Register("ping", func(params Params) (any, error) {
		return "pong", nil
	})

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, clientRoot)
	require.NoError(t, err)
	defer client.Close()

	// Wait for connection to be established
	time.Sleep(100 * time.Millisecond)

	// Server calls client
	connMu.Lock()
	sc := serverConn
	connMu.Unlock()
	require.NotNil(t, sc)

	val, err := sc.Call("", "ping", nil)
	require.NoError(t, err)
	var result string
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "pong", result)
}

// TestWebSocket_ConcurrentRequests tests multiple concurrent requests
func TestWebSocket_ConcurrentRequests(t *testing.T) {
	root := NewMethodMap()
	root.Register("echo", func(params Params) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		// Simulate some processing time
		time.Sleep(10 * time.Millisecond)
		return msg, nil
	})

	handler := NewWebSocketHandler(root)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)
	defer client.Close()

	// Send multiple concurrent requests
	const numRequests = 10
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func(n int) {
			defer wg.Done()
			msg := "test" + string(rune('0'+n))
			val, err := client.Call("echo", msg)
			assert.NoError(t, err)
			var result string
			err = val.Decode(&result)
			assert.NoError(t, err)
			assert.Equal(t, msg, result)
		}(i)
	}

	wg.Wait()
}

// TestWebSocket_ObjectRegistration tests registering and calling objects
func TestWebSocket_ObjectRegistration(t *testing.T) {
	// Server object
	type serverCounter struct {
		count int
		mu    sync.Mutex
	}

	counter := &serverCounter{}
	counterObj := NewMethodMap()
	counterObj.Register("increment", func(params Params) (any, error) {
		counter.mu.Lock()
		defer counter.mu.Unlock()
		counter.count++
		return counter.count, nil
	})
	counterObj.Register("getCount", func(params Params) (any, error) {
		counter.mu.Lock()
		defer counter.mu.Unlock()
		return counter.count, nil
	})

	root := NewMethodMap()
	root.Register("getCounter", func(params Params) (any, error) {
		return NewReference("counter-1"), nil
	})

	// Create handler that registers objects
	handler := NewWebSocketHandler(root)

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
		wsConn := newWebSocketConn(conn, root, contentType, handler.mimeTypes)
		wsConn.RegisterObject("counter-1", counterObj)

		wsConn.handle()
	})

	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)
	defer client.Close()

	// Get counter reference
	val1, err := client.Call("getCounter", nil)
	require.NoError(t, err)
	var ref Reference
	err = val1.Decode(&ref)
	require.NoError(t, err)
	assert.Equal(t, "counter-1", ref.Ref)

	// Call increment on ref
	val2, err := client.Call("increment", nil, ToRef(ref))
	require.NoError(t, err)
	var count int
	err = val2.Decode(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Call increment again
	val3, err := client.Call("increment", nil, ToRef(ref))
	require.NoError(t, err)
	err = val3.Decode(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Get count
	val4, err := client.Call("getCount", nil, ToRef(ref))
	require.NoError(t, err)
	err = val4.Decode(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// TestWebSocket_ErrorHandling tests error responses
func TestWebSocket_ErrorHandling(t *testing.T) {
	root := NewMethodMap()
	root.Register("fail", func(params Params) (any, error) {
		return nil, &Error{
			Code:    -32000,
			Message: "Intentional error",
		}
	})

	handler := NewWebSocketHandler(root)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.Call("fail", nil)
	require.Error(t, err)

	rpcErr, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, -32000, rpcErr.Code)
	assert.Contains(t, rpcErr.Message, "Intentional error")
}

// TestWebSocket_MethodNotFound tests calling non-existent method
func TestWebSocket_MethodNotFound(t *testing.T) {
	root := NewMethodMap()
	handler := NewWebSocketHandler(root)

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.Call("nonexistent", nil)
	require.Error(t, err)

	rpcErr, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, CodeMethodNotFound, rpcErr.Code)
}

// TestWebSocket_Close tests graceful connection closure
func TestWebSocket_Close(t *testing.T) {
	root := NewMethodMap()
	handler := NewWebSocketHandler(root)

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewWebSocketClient(wsURL, nil)
	require.NoError(t, err)

	// Close client
	err = client.Close()
	assert.NoError(t, err)

	// Subsequent calls should fail
	_, err = client.Call("test", nil)
	assert.Error(t, err)
}

// TestWebSocket_ProtocolNegotiation tests CBOR protocol negotiation
func TestWebSocket_ProtocolNegotiation(t *testing.T) {
	root := NewMethodMap()
	root.Register("test", func(params Params) (any, error) {
		return "ok", nil
	})

	handler := NewWebSocketHandler(root)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Test CBOR
	client, err := NewWebSocketClient(wsURL, nil, WithContentType("application/cbor"))
	require.NoError(t, err)
	defer client.Close()

	assert.Equal(t, "application/cbor", client.contentType)

	val, err := client.Call("test", nil)
	require.NoError(t, err)
	var result string
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "ok", result)
}

// TestWebSocket_SessionManagement tests that session persists across calls
func TestWebSocket_SessionManagement(t *testing.T) {
	root := NewMethodMap()
	root.Register("createCounter", func(params Params) (any, error) {
		return &testCounter{value: 0}, nil
	})

	handler := NewWebSocketHandler(root)

	var serverConn *WebSocketConn
	var connMu sync.Mutex

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
		wsConn := newWebSocketConn(conn, root, contentType, handler.mimeTypes)

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

	// Create counter (returns reference)
	val, err := client.Call("createCounter", nil)
	require.NoError(t, err)
	var ref Reference
	err = val.Decode(&ref)
	require.NoError(t, err)

	// Verify we got a reference
	assert.NotEmpty(t, ref.Ref)

	// Server session should have the object
	time.Sleep(100 * time.Millisecond)
	connMu.Lock()
	sc := serverConn
	connMu.Unlock()
	require.NotNil(t, sc)

	serverSession := sc.GetSession()
	assert.NotEmpty(t, serverSession.ListLocalRefs())
}
