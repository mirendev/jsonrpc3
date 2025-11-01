package jsonrpc3

import (
	"crypto/tls"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestWebTransportClient creates a client that accepts self-signed certificates
func createTestWebTransportClient(url string, rootObject Object, contentType string) (*WebTransportClient, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // Accept self-signed certs in tests
	}
	return NewWebTransportClient(url, rootObject,
		WithContentType(contentType),
		WithTLSConfig(tlsConfig),
	)
}

// TestWebTransportIntegration_BasicCall tests basic client-server communication
func TestWebTransportIntegration_BasicCall(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping WebTransport integration test in short mode")
	}

	// Server methods
	serverRoot := NewMethodMap()
	serverRoot.Register("echo", func(params Params) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return msg, nil
	})

	serverRoot.Register("add", func(params Params) (any, error) {
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

	// Start server
	server, err := NewWebTransportServer("localhost:14433", serverRoot, nil)
	require.NoError(t, err)

	go server.ListenAndServe()
	defer server.Close()

	// Give server time to start
	time.Sleep(500 * time.Millisecond)

	// Create client
	client, err := createTestWebTransportClient("https://localhost:14433/", nil, "application/json")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Give client and server time to fully establish connection
	time.Sleep(300 * time.Millisecond)

	// Test echo
	val1, err := client.Call("echo", "hello")
	require.NoError(t, err)
	var echoResult string
	err = val1.Decode(&echoResult)
	require.NoError(t, err)
	assert.Equal(t, "hello", echoResult)

	// Test add
	val2, err := client.Call("add", []int{1, 2, 3, 4})
	require.NoError(t, err)
	var addResult int
	err = val2.Decode(&addResult)
	require.NoError(t, err)
	assert.Equal(t, 10, addResult)
}

// TestWebTransportIntegration_BidirectionalCallbacks tests server calling client methods
func TestWebTransportIntegration_BidirectionalCallbacks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping WebTransport integration test in short mode")
	}

	var serverConn *WebTransportConn
	var connMu sync.Mutex

	// Server methods
	serverRoot := NewMethodMap()
	serverRoot.Register("subscribe", func(params Params) (any, error) {
		var p struct {
			Topic    string         `json:"topic"`
			Callback Reference `json:"callback"`
		}
		if err := params.Decode(&p); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}

		// Server calls client callback
		go func() {
			time.Sleep(50 * time.Millisecond)
			connMu.Lock()
			sc := serverConn
			connMu.Unlock()

			if sc != nil {
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

	// Create server with connection callback
	server, err := NewWebTransportServer("localhost:14434", serverRoot, nil)
	require.NoError(t, err)

	// Set callback to capture connection
	server.onNewConnection = func(conn *WebTransportConn) {
		connMu.Lock()
		serverConn = conn
		connMu.Unlock()
	}

	go server.ListenAndServe()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	// Client with callback
	callbackObj := NewMethodMap()
	var updates []string
	var updateMu sync.Mutex

	callbackObj.Register("onUpdate", func(params Params) (any, error) {
		var data map[string]string
		params.Decode(&data)
		updateMu.Lock()
		updates = append(updates, data["message"])
		updateMu.Unlock()
		return nil, nil
	})

	client, err := createTestWebTransportClient("https://localhost:14434/", callbackObj, "application/json")
	require.NoError(t, err)
	defer client.Close()

	// Give client and server time to fully establish connection
	time.Sleep(300 * time.Millisecond)

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
	time.Sleep(300 * time.Millisecond)

	// Verify notifications were received
	updateMu.Lock()
	assert.Len(t, updates, 2)
	if len(updates) >= 2 {
		assert.Equal(t, "Update 1", updates[0])
		assert.Equal(t, "Update 2", updates[1])
	}
	updateMu.Unlock()
}

// TestWebTransportIntegration_ConcurrentCalls tests concurrent client requests
func TestWebTransportIntegration_ConcurrentCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping WebTransport integration test in short mode")
	}

	serverRoot := NewMethodMap()
	var callCount atomic.Int32

	serverRoot.Register("increment", func(params Params) (any, error) {
		count := callCount.Add(1)
		time.Sleep(10 * time.Millisecond) // Simulate work
		return count, nil
	})

	server, err := NewWebTransportServer("localhost:14435", serverRoot, nil)
	require.NoError(t, err)

	go server.ListenAndServe()
	defer server.Close()

	time.Sleep(200 * time.Millisecond)

	client, err := createTestWebTransportClient("https://localhost:14435/", nil, "application/json")
	require.NoError(t, err)
	defer client.Close()

	// Make concurrent calls
	const numCalls = 10
	var wg sync.WaitGroup
	results := make([]int, numCalls)
	var resultsMu sync.Mutex

	wg.Add(numCalls)
	for i := 0; i < numCalls; i++ {
		go func(idx int) {
			defer wg.Done()
			val, err := client.Call("increment", nil)
			assert.NoError(t, err)
			var result int
			err = val.Decode(&result)
			assert.NoError(t, err)
			resultsMu.Lock()
			results[idx] = result
			resultsMu.Unlock()
		}(i)
	}

	wg.Wait()

	// Verify all calls completed
	resultsMu.Lock()
	assert.Equal(t, int32(numCalls), callCount.Load())
	assert.Len(t, results, numCalls)
	resultsMu.Unlock()
}

// TestWebTransportIntegration_ErrorHandling tests error propagation
func TestWebTransportIntegration_ErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping WebTransport integration test in short mode")
	}

	serverRoot := NewMethodMap()
	serverRoot.Register("divide", func(params Params) (any, error) {
		var nums struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&nums); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		if nums.B == 0 {
			return nil, &Error{
				Code:    -32000,
				Message: "Division by zero",
			}
		}
		return nums.A / nums.B, nil
	})

	server, err := NewWebTransportServer("localhost:14436", serverRoot, nil)
	require.NoError(t, err)

	go server.ListenAndServe()
	defer server.Close()

	time.Sleep(200 * time.Millisecond)

	client, err := createTestWebTransportClient("https://localhost:14436/", nil, "application/json")
	require.NoError(t, err)
	defer client.Close()

	// Test valid division
	val, err := client.Call("divide", map[string]float64{"a": 10, "b": 2})
	require.NoError(t, err)
	var result float64
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 5.0, result)

	// Test division by zero
	_, err = client.Call("divide", map[string]float64{"a": 10, "b": 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Division by zero")

	// Test method not found
	_, err = client.Call("nonexistent", nil)
	require.Error(t, err)
}

// TestWebTransportIntegration_Notifications tests one-way notifications
func TestWebTransportIntegration_Notifications(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping WebTransport integration test in short mode")
	}

	var notifCount atomic.Int32
	serverRoot := NewMethodMap()
	serverRoot.Register("log", func(params Params) (any, error) {
		notifCount.Add(1)
		return nil, nil
	})

	server, err := NewWebTransportServer("localhost:14437", serverRoot, nil)
	require.NoError(t, err)

	go server.ListenAndServe()
	defer server.Close()

	time.Sleep(200 * time.Millisecond)

	client, err := createTestWebTransportClient("https://localhost:14437/", nil, "application/json")
	require.NoError(t, err)
	defer client.Close()

	// Send notifications
	err = client.Notify("log", "message 1")
	require.NoError(t, err)

	err = client.Notify("log", "message 2")
	require.NoError(t, err)

	err = client.Notify("log", "message 3")
	require.NoError(t, err)

	// Wait for notifications to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify notifications were received
	assert.Equal(t, int32(3), notifCount.Load())
}

// TestWebTransportIntegration_CBOR tests CBOR encoding
func TestWebTransportIntegration_CBOR(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping WebTransport integration test in short mode")
	}

	serverRoot := NewMethodMap()
	serverRoot.Register("echo", func(params Params) (any, error) {
		var data map[string]any
		params.Decode(&data)
		return data, nil
	})

	server, err := NewWebTransportServer("localhost:14438", serverRoot, nil)
	require.NoError(t, err)

	go server.ListenAndServe()
	defer server.Close()

	time.Sleep(200 * time.Millisecond)

	client, err := createTestWebTransportClient("https://localhost:14438/", nil, "application/cbor")
	require.NoError(t, err)
	defer client.Close()

	// Verify CBOR encoding is used
	assert.Equal(t, "application/cbor", client.contentType)

	// Test with CBOR
	input := map[string]any{
		"name":  "test",
		"value": 42,
	}
	val, err := client.Call("echo", input)
	require.NoError(t, err)
	var result map[string]any
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "test", result["name"])
	// CBOR may decode integers as uint64 or int64
	switch v := result["value"].(type) {
	case uint64:
		assert.Equal(t, uint64(42), v)
	case int64:
		assert.Equal(t, int64(42), v)
	case float64:
		assert.Equal(t, float64(42), v)
	default:
		t.Fatalf("unexpected type for value: %T", v)
	}
}

// TestWebTransportIntegration_CertificateGeneration tests certificate generation
func TestWebTransportIntegration_CertificateGeneration(t *testing.T) {
	cert, err := generateSelfSignedCert()
	assert.NoError(t, err)
	assert.NotNil(t, cert.Certificate)
	assert.NotEmpty(t, cert.Certificate)
	assert.NotNil(t, cert.PrivateKey)
}

// TestWebTransportIntegration_ServerCreation tests server creation and lifecycle
func TestWebTransportIntegration_ServerCreation(t *testing.T) {
	root := NewMethodMap()
	server, err := NewWebTransportServer("localhost:0", root, nil)
	assert.NoError(t, err)
	assert.NotNil(t, server)
	assert.Equal(t, "localhost:0", server.addr)
	assert.Equal(t, root, server.rootObject)
	assert.NotNil(t, server.server)

	// Start and immediately close
	go server.ListenAndServe()
	time.Sleep(50 * time.Millisecond)
	err = server.Close()
	assert.NoError(t, err)
}

// TestWebTransportIntegration_ServerWithCustomTLS tests server with custom TLS config
func TestWebTransportIntegration_ServerWithCustomTLS(t *testing.T) {
	root := NewMethodMap()

	// Generate custom certificate
	cert, err := generateSelfSignedCert()
	require.NoError(t, err)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	server, err := NewWebTransportServer("localhost:0", root, tlsConfig)
	assert.NoError(t, err)
	assert.NotNil(t, server)

	go server.ListenAndServe()
	time.Sleep(50 * time.Millisecond)
	err = server.Close()
	assert.NoError(t, err)
}

// TestWebTransportIntegration_MultipleServers tests multiple servers can coexist
func TestWebTransportIntegration_MultipleServers(t *testing.T) {
	root1 := NewMethodMap()
	server1, err := NewWebTransportServer("localhost:0", root1, nil)
	require.NoError(t, err)

	root2 := NewMethodMap()
	server2, err := NewWebTransportServer("localhost:0", root2, nil)
	require.NoError(t, err)

	// Start both servers
	go server1.ListenAndServe()
	go server2.ListenAndServe()

	time.Sleep(100 * time.Millisecond)

	// Close both
	server1.Close()
	server2.Close()
}

// TestWebTransportIntegration_Documentation provides usage documentation
func TestWebTransportIntegration_Documentation(t *testing.T) {
	// Example of how to create a WebTransport server
	_ = func() error {
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

		server, err := NewWebTransportServer("localhost:4433", root, nil)
		if err != nil {
			return err
		}

		// Start server
		go server.ListenAndServe()
		defer server.Close()

		return nil
	}

	// Example of how to create a WebTransport client with options
	_ = func() error {
		// Simple client with defaults (CBOR encoding)
		client, err := NewWebTransportClient("https://localhost:4433/", nil)
		if err != nil {
			return err
		}
		defer client.Close()

		// Client with custom options
		tlsConfig := &tls.Config{InsecureSkipVerify: true}
		client2, err := NewWebTransportClient("https://localhost:4433/", nil,
			WithJSON(),
			WithTLSConfig(tlsConfig),
		)
		if err != nil {
			return err
		}
		defer client2.Close()

		val, err := client2.Call("add", []int{1, 2, 3})
		if err != nil {
			return err
		}
		var result int
		err = val.Decode(&result)
		if err != nil {
			return err
		}

		fmt.Printf("Result: %d\n", result)
		return nil
	}

	t.Log("WebTransport API documentation test passed")
}
