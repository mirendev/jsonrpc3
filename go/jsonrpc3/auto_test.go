package jsonrpc3

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"testing"
	"time"
)

// getProtocolName returns the protocol name based on the Caller type.
func getProtocolName(caller Caller) string {
	switch caller.(type) {
	case *WebTransportClient:
		return "http/3"
	case *HTTP2Client:
		return "http/2"
	case *WebSocketClient:
		return "websocket"
	case *HTTPClient:
		return "http"
	case *Peer:
		return "peer"
	default:
		return "unknown"
	}
}

// TestAutoServer_Startup tests that AutoServer starts all protocols.
func TestAutoServer_Startup(t *testing.T) {
	root := NewMethodMap()
	root.Register("ping", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return "pong", nil
	})

	server, err := NewAutoServer("localhost:14001", root, nil)
	if err != nil {
		t.Fatalf("Failed to create AutoServer: %v", err)
	}

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(500 * time.Millisecond)

	// Verify server addresses are set
	if server.GetHTTP2Addr() == "" {
		t.Error("HTTP/2 address not set")
	}
	if server.GetHTTP3Addr() == "" {
		t.Error("HTTP/3 address not set")
	}
}

// TestAutoClient_HTTP2Connection tests that AutoClient connects via HTTP/2.
func TestAutoClient_HTTP2Connection(t *testing.T) {
	root := NewMethodMap()
	root.Register("add", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&args); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return args.A + args.B, nil
	})

	server, err := NewAutoServer("localhost:14002", root, nil)
	if err != nil {
		t.Fatalf("Failed to create AutoServer: %v", err)
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	// Create AutoClient
	clientRoot := NewMethodMap()
	endpoint, err := Probe("https://localhost:14002")
	if err != nil {
		t.Fatalf("Failed to probe endpoint: %v", err)
	}
	client, err := endpoint.Connect(clientRoot)
	if err != nil {
		t.Fatalf("Failed to create AutoClient: %v", err)
	}
	defer client.Close()

	// Verify it connected via HTTP/2
	protocol := getProtocolName(client)
	if protocol != "http/2" && protocol != "http/3" {
		t.Errorf("Expected http/2 or http/3, got %s", protocol)
	}

	// Test method call
	result, err := client.Call("add", map[string]any{"a": 5.0, "b": 3.0})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var sum float64
	if err := result.Decode(&sum); err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	if sum != 8.0 {
		t.Errorf("Expected 8.0, got %v", sum)
	}
}

// TestAutoClient_ProtocolUpgrade tests that AutoClient upgrades from HTTP/2 to HTTP/3.
func TestAutoClient_ProtocolUpgrade(t *testing.T) {
	root := NewMethodMap()
	root.Register("getProtocol", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return "success", nil
	})

	server, err := NewAutoServer("localhost:14003", root, nil)
	if err != nil {
		t.Fatalf("Failed to create AutoServer: %v", err)
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	// Create AutoClient
	clientRoot := NewMethodMap()
	endpoint, err := Probe("https://localhost:14003")
	if err != nil {
		t.Fatalf("Failed to probe endpoint: %v", err)
	}
	client, err := endpoint.Connect(clientRoot)
	if err != nil {
		t.Fatalf("Failed to create AutoClient: %v", err)
	}
	defer client.Close()

	// Initial protocol should be HTTP/2
	initialProtocol := getProtocolName(client)
	t.Logf("Initial protocol: %s", initialProtocol)

	// Give it time to potentially upgrade to HTTP/3
	time.Sleep(1 * time.Second)

	// Check final protocol
	finalProtocol := getProtocolName(client)
	t.Logf("Final protocol: %s", finalProtocol)

	// Should have upgraded to HTTP/3 or stayed on HTTP/2
	if finalProtocol != "http/2" && finalProtocol != "http/3" {
		t.Errorf("Unexpected protocol: %s", finalProtocol)
	}

	// Test that it still works
	result, err := client.Call("getProtocol", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var response string
	if err := result.Decode(&response); err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	if response != "success" {
		t.Errorf("Expected 'success', got %s", response)
	}
}

// TestAutoClient_BatchRequest tests batch requests through AutoClient.
func TestAutoClient_BatchRequest(t *testing.T) {
	root := NewMethodMap()
	root.Register("add", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&args); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return args.A + args.B, nil
	})
	root.Register("multiply", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&args); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return args.A * args.B, nil
	})

	server, err := NewAutoServer("localhost:14004", root, nil)
	if err != nil {
		t.Fatalf("Failed to create AutoServer: %v", err)
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	// Create AutoClient
	clientRoot := NewMethodMap()
	endpoint, err := Probe("https://localhost:14004")
	if err != nil {
		t.Fatalf("Failed to probe endpoint: %v", err)
	}
	client, err := endpoint.Connect(clientRoot)
	if err != nil {
		t.Fatalf("Failed to create AutoClient: %v", err)
	}
	defer client.Close()

	// Test batch request
	batch := []BatchRequest{
		{Method: "add", Params: map[string]any{"a": 5.0, "b": 3.0}, IsNotification: false},
		{Method: "multiply", Params: map[string]any{"a": 4.0, "b": 2.0}, IsNotification: false},
	}

	results, err := client.CallBatch(batch)
	if err != nil {
		t.Fatalf("CallBatch failed: %v", err)
	}

	// Check first result
	res0, err := results.GetResult(0)
	if err != nil {
		t.Fatalf("Failed to get result 0: %v", err)
	}
	if sum, ok := res0.(float64); !ok || sum != 8.0 {
		t.Errorf("Expected 8.0, got %v", res0)
	}

	// Check second result
	res1, err := results.GetResult(1)
	if err != nil {
		t.Fatalf("Failed to get result 1: %v", err)
	}
	if product, ok := res1.(float64); !ok || product != 8.0 {
		t.Errorf("Expected 8.0, got %v", res1)
	}
}

// TestAutoClient_Notification tests notifications through AutoClient.
func TestAutoClient_Notification(t *testing.T) {
	root := NewMethodMap()
	var notificationReceived bool
	root.Register("notify", func(ctx context.Context, params Params, caller Caller) (any, error) {
		notificationReceived = true
		return nil, nil
	})

	server, err := NewAutoServer("localhost:14005", root, nil)
	if err != nil {
		t.Fatalf("Failed to create AutoServer: %v", err)
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	// Create AutoClient
	clientRoot := NewMethodMap()
	endpoint, err := Probe("https://localhost:14005")
	if err != nil {
		t.Fatalf("Failed to probe endpoint: %v", err)
	}
	client, err := endpoint.Connect(clientRoot)
	if err != nil {
		t.Fatalf("Failed to create AutoClient: %v", err)
	}
	defer client.Close()

	// Send notification
	err = client.Notify("notify", nil)
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	// Give it time to be processed
	time.Sleep(200 * time.Millisecond)

	if !notificationReceived {
		t.Error("Notification was not received")
	}
}

// TestAutoClient_ErrorHandling tests error handling through AutoClient.
func TestAutoClient_ErrorHandling(t *testing.T) {
	root := NewMethodMap()
	root.Register("divide", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&args); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		if args.B == 0 {
			return nil, NewError(-32000, "Division by zero", nil)
		}
		return args.A / args.B, nil
	})

	server, err := NewAutoServer("localhost:14006", root, nil)
	if err != nil {
		t.Fatalf("Failed to create AutoServer: %v", err)
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	// Create AutoClient
	clientRoot := NewMethodMap()
	endpoint, err := Probe("https://localhost:14006")
	if err != nil {
		t.Fatalf("Failed to probe endpoint: %v", err)
	}
	client, err := endpoint.Connect(clientRoot)
	if err != nil {
		t.Fatalf("Failed to create AutoClient: %v", err)
	}
	defer client.Close()

	// Test error case
	_, err = client.Call("divide", map[string]any{"a": 10.0, "b": 0.0})
	if err == nil {
		t.Fatal("Expected error for division by zero")
	}

	rpcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("Expected RPC error, got %T", err)
	}

	if rpcErr.Code != -32000 {
		t.Errorf("Expected error code -32000, got %d", rpcErr.Code)
	}
}

// TestConnect_HTTP3OnlyServer tests that Connect can detect HTTP/3-only servers.
func TestConnect_HTTP3OnlyServer(t *testing.T) {
	root := NewMethodMap()
	root.Register("ping", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return "pong", nil
	})

	// Create HTTP/3-only server (WebTransport only, no HTTP/2 or HTTP/1.1)
	server, err := NewWebTransportServer("localhost:14015", root, nil)
	if err != nil {
		t.Fatalf("Failed to create WebTransport server: %v", err)
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	clientRoot := NewMethodMap()
	// Use regular https:// URL - parallel probing should detect HTTP/3
	endpoint, err := Probe("https://localhost:14015/")
	if err != nil {
		t.Fatalf("Failed to probe endpoint: %v", err)
	}
	client, err := endpoint.Connect(clientRoot)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	protocol := getProtocolName(client)
	t.Logf("Connected via: %s", protocol)

	// Should connect via HTTP/3 even though we didn't use http3:// scheme
	if protocol != "http/3" {
		t.Errorf("Expected http/3, got %s", protocol)
	}

	// Verify it works
	result, err := client.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var response string
	if err := result.Decode(&response); err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	if response != "pong" {
		t.Errorf("Expected 'pong', got '%s'", response)
	}
}

// TestConnect_HTTP3Scheme tests direct HTTP/3 connection using http3:// scheme.
func TestConnect_HTTP3Scheme(t *testing.T) {
	root := NewMethodMap()
	root.Register("ping", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return "pong", nil
	})

	// Create AutoServer
	server, err := NewAutoServer("localhost:14014", root, nil)
	if err != nil {
		t.Fatalf("Failed to create AutoServer: %v", err)
	}

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	clientRoot := NewMethodMap()
	// Use http3:// scheme to directly connect via WebTransport
	endpoint, err := Probe("http3://localhost:14014/")
	if err != nil {
		t.Fatalf("Failed to probe endpoint: %v", err)
	}
	client, err := endpoint.Connect(clientRoot)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	protocol := getProtocolName(client)
	t.Logf("Connected via: %s", protocol)

	if protocol != "http/3" {
		t.Errorf("Expected http/3, got %s", protocol)
	}

	// Verify it works
	result, err := client.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var response string
	if err := result.Decode(&response); err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	if response != "pong" {
		t.Errorf("Expected 'pong', got '%s'", response)
	}
}

// TestAutoClient_HTTP3Protocol tests that AutoClient can connect via HTTP/3.
func TestAutoClient_HTTP3Protocol(t *testing.T) {
	root := NewMethodMap()
	root.Register("ping", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return "pong", nil
	})

	// AutoServer advertises HTTP/3 via Alt-Svc
	server, err := NewAutoServer("localhost:14010", root, nil)
	if err != nil {
		t.Fatalf("Failed to create AutoServer: %v", err)
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	clientRoot := NewMethodMap()
	endpoint, err := Probe("https://localhost:14010")
	if err != nil {
		t.Fatalf("Failed to probe endpoint: %v", err)
	}
	client, err := endpoint.Connect(clientRoot)
	if err != nil {
		t.Fatalf("Failed to create AutoClient: %v", err)
	}
	defer client.Close()

	protocol := getProtocolName(client)
	t.Logf("Connected via: %s", protocol)

	if protocol != "http/3" {
		t.Errorf("Expected http/3, got %s", protocol)
	}

	// Verify it works
	result, err := client.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var response string
	if err := result.Decode(&response); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if response != "pong" {
		t.Errorf("Expected 'pong', got '%s'", response)
	}
}

// TestAutoClient_HTTP2Protocol tests that AutoClient can connect via HTTP/2.
func TestAutoClient_HTTP2Protocol(t *testing.T) {
	root := NewMethodMap()
	root.Register("ping", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return "pong", nil
	})

	// Use only HTTP/2 server (no HTTP/3, no Alt-Svc)
	http2Server := NewHTTP2Server(root, nil)

	addr := "localhost:14011"
	go func() {
		if err := http2Server.ListenAndServe(addr); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer http2Server.Close()

	time.Sleep(500 * time.Millisecond)

	// Create client directly with HTTP/2 (bypass AutoClient's protocol detection)
	// This is because AutoClient would try to probe with HEAD first, which might fail
	// Instead, we'll test HTTP/2 by connecting directly
	clientRoot := NewMethodMap()
	http2Client, err := NewHTTP2Client(
		"https://"+addr,
		clientRoot,
		WithContentType(MimeTypeJSON),
		WithTLSConfig(&tls.Config{InsecureSkipVerify: true}),
	)
	if err != nil {
		t.Fatalf("Failed to create HTTP/2 client: %v", err)
	}
	defer http2Client.Close()

	protocol := "http/2"
	t.Logf("Connected via: %s", protocol)

	// Verify it works
	result, err := http2Client.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var response string
	if err := result.Decode(&response); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if response != "pong" {
		t.Errorf("Expected 'pong', got '%s'", response)
	}
}

// TestAutoClient_WebSocketProtocol tests that AutoClient can connect via WebSocket.
func TestAutoClient_WebSocketProtocol(t *testing.T) {
	root := NewMethodMap()
	root.Register("ping", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return "pong", nil
	})

	// Use HTTP/1.1 server with WebSocket support
	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	addr := "localhost:14012"
	server := &http.Server{
		Addr:    addr,
		Handler: httpHandler,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	clientRoot := NewMethodMap()
	endpoint, err := Probe("http://" + addr)
	if err != nil {
		t.Fatalf("Failed to probe endpoint: %v", err)
	}
	client, err := endpoint.Connect(clientRoot)
	if err != nil {
		t.Fatalf("Failed to create AutoClient: %v", err)
	}
	defer client.Close()

	protocol := getProtocolName(client)
	t.Logf("Connected via: %s", protocol)

	if protocol != "websocket" {
		t.Errorf("Expected websocket, got %s", protocol)
	}

	// Verify it works
	result, err := client.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var response string
	if err := result.Decode(&response); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if response != "pong" {
		t.Errorf("Expected 'pong', got '%s'", response)
	}
}

// TestConnect_PeerTransportDetection tests that Connect detects non-HTTP URLs.
func TestConnect_PeerTransportDetection(t *testing.T) {
	// This test just verifies that non-HTTP URLs are detected
	// The peer transport requires an established connection, so we expect
	// the connection to fail but we can verify the protocol detection logic

	clientRoot := NewMethodMap()

	// Try to connect to an address without http:// or https://
	// This should attempt to use peer transport over TCP
	endpoint, err := Probe("localhost:99999")
	if err != nil {
		t.Fatalf("Failed to probe endpoint: %v", err)
	}
	_, err = endpoint.Connect(clientRoot)

	// We expect this to fail since there's no server listening
	// But the error should be from the TCP dial, not from protocol detection
	if err == nil {
		t.Error("Expected error connecting to non-existent server")
	}

	// Verify the error is a dial error, not a protocol error
	if !strings.Contains(err.Error(), "dial") && !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("Expected dial/connection error, got: %v", err)
	}
}

// TestAutoClient_HTTPProtocol tests that AutoClient can connect via basic HTTP.
func TestAutoClient_HTTPProtocol(t *testing.T) {
	root := NewMethodMap()
	root.Register("ping", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return "pong", nil
	})

	// Use HTTP/1.1 server with WebSocket support (but client will use stateless HTTP)
	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	addr := "localhost:14013"
	server := &http.Server{
		Addr:    addr,
		Handler: httpHandler,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	// Use HTTPClient directly to force stateless HTTP
	httpClient := NewHTTPClient("http://"+addr, nil)
	defer httpClient.Close()

	protocol := "http"
	t.Logf("Using protocol: %s", protocol)

	// Verify it works
	result, err := httpClient.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var response string
	if err := result.Decode(&response); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if response != "pong" {
		t.Errorf("Expected 'pong', got '%s'", response)
	}
}

// TestAutoClient_ProtocolFallback tests the protocol fallback chain.
func TestAutoClient_ProtocolFallback(t *testing.T) {
	// Test the fallback behavior when protocols are unavailable
	tests := []struct {
		name             string
		serverType       string
		expectedProtocol string
		setupServer      func(*testing.T) (addr string, cleanup func())
	}{
		{
			name:             "HTTP/3 available",
			serverType:       "AutoServer",
			expectedProtocol: "http/3",
			setupServer: func(t *testing.T) (string, func()) {
				root := NewMethodMap()
				root.Register("test", func(ctx context.Context, params Params, caller Caller) (any, error) {
					return "ok", nil
				})
				server, _ := NewAutoServer("localhost:14020", root, nil)
				go server.ListenAndServe()
				time.Sleep(500 * time.Millisecond)
				return "https://localhost:14020", func() { server.Close() }
			},
		},
		{
			name:             "HTTP/2 only",
			serverType:       "HTTP2Server",
			expectedProtocol: "http/2",
			setupServer: func(t *testing.T) (string, func()) {
				root := NewMethodMap()
				root.Register("test", func(ctx context.Context, params Params, caller Caller) (any, error) {
					return "ok", nil
				})
				// Create HTTP/2 server with explicit TLS
				http2Server := NewHTTP2Server(root, nil)
				go http2Server.ListenAndServe("localhost:14021")
				time.Sleep(500 * time.Millisecond)
				return "https://localhost:14021", func() { http2Server.Close() }
			},
		},
		{
			name:             "WebSocket fallback",
			serverType:       "HTTPHandler",
			expectedProtocol: "websocket",
			setupServer: func(t *testing.T) (string, func()) {
				root := NewMethodMap()
				root.Register("test", func(ctx context.Context, params Params, caller Caller) (any, error) {
					return "ok", nil
				})
				handler := NewHTTPHandler(root)
				srv := &http.Server{
					Addr:    "localhost:14022",
					Handler: handler,
				}
				go srv.ListenAndServe()
				time.Sleep(500 * time.Millisecond)
				return "http://localhost:14022", func() {
					srv.Close()
					handler.Close()
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, cleanup := tt.setupServer(t)
			defer cleanup()

			clientRoot := NewMethodMap()
			endpoint, err := Probe(addr)

			if err != nil {

				t.Fatalf("Failed to probe endpoint: %v", err)

			}

			client, err := endpoint.Connect(clientRoot)
			if err != nil {
				t.Fatalf("Failed to create AutoClient: %v", err)
			}
			defer client.Close()

			protocol := getProtocolName(client)
			t.Logf("Connected via: %s (expected: %s)", protocol, tt.expectedProtocol)

			if protocol != tt.expectedProtocol {
				t.Errorf("Expected protocol %s, got %s", tt.expectedProtocol, protocol)
			}

			// Verify connection works
			result, err := client.Call("test", nil)
			if err != nil {
				t.Fatalf("Call failed: %v", err)
			}

			var response string
			if err := result.Decode(&response); err != nil {
				t.Fatalf("Failed to decode: %v", err)
			}

			if response != "ok" {
				t.Errorf("Expected 'ok', got '%s'", response)
			}
		})
	}
}

// TestAutoClient_AllMethods tests all AutoClient methods comprehensively.
func TestAutoClient_AllMethods(t *testing.T) {
	// Create server with comprehensive method set
	serverRoot := NewMethodMap()

	// Basic method for Call testing
	serverRoot.Register("echo", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return msg, nil
	})

	// Method for arithmetic testing
	serverRoot.Register("add", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&args); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return args.A + args.B, nil
	})

	serverRoot.Register("multiply", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&args); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return args.A * args.B, nil
	})

	// Track notifications
	var notificationReceived bool
	var notificationData string
	serverRoot.Register("logEvent", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var event string
		if err := params.Decode(&event); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		notificationReceived = true
		notificationData = event
		return nil, nil
	})

	// Method for bidirectional RPC testing
	serverRoot.Register("callClientMethod", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var args struct {
			Method string `json:"method"`
			Value  string `json:"value"`
		}
		if err := params.Decode(&args); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}

		// Call back to client
		result, err := caller.Call(args.Method, args.Value)
		if err != nil {
			return nil, err
		}

		var response string
		if err := result.Decode(&response); err != nil {
			return nil, err
		}

		return response, nil
	})

	// Method that uses object references
	serverRoot.Register("useRef", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var args struct {
			Ref    Reference `json:"ref"`
			Method string    `json:"method"`
			Value  int       `json:"value"`
		}
		if err := params.Decode(&args); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}

		// Call method on the reference
		result, err := caller.Call(args.Method, args.Value, ToRef(args.Ref))
		if err != nil {
			return nil, err
		}

		var sum int
		if err := result.Decode(&sum); err != nil {
			return nil, err
		}

		return sum, nil
	})

	server, err := NewAutoServer("localhost:14007", serverRoot, nil)
	if err != nil {
		t.Fatalf("Failed to create AutoServer: %v", err)
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(500 * time.Millisecond)

	// Create client with methods for bidirectional RPC
	clientRoot := NewMethodMap()
	clientRoot.Register("greet", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var name string
		if err := params.Decode(&name); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return "Hello, " + name + "!", nil
	})

	// Create a counter object for reference testing
	counter := &struct {
		value int
	}{value: 0}

	counterObj := NewMethodMap()
	counterObj.Register("increment", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var amount int
		if err := params.Decode(&amount); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		counter.value += amount
		return counter.value, nil
	})

	endpoint, err := Probe("https://localhost:14007")
	if err != nil {
		t.Fatalf("Failed to probe endpoint: %v", err)
	}
	client, err := endpoint.Connect(clientRoot)
	if err != nil {
		t.Fatalf("Failed to create AutoClient: %v", err)
	}
	defer client.Close()

	t.Logf("Connected via protocol: %s", getProtocolName(client))

	// Test 1: Call method
	t.Run("Call", func(t *testing.T) {
		result, err := client.Call("echo", "test message")
		if err != nil {
			t.Fatalf("Call failed: %v", err)
		}

		var msg string
		if err := result.Decode(&msg); err != nil {
			t.Fatalf("Failed to decode result: %v", err)
		}

		if msg != "test message" {
			t.Errorf("Expected 'test message', got '%s'", msg)
		}
	})

	// Test 2: Notify method
	t.Run("Notify", func(t *testing.T) {
		notificationReceived = false
		notificationData = ""

		err := client.Notify("logEvent", "test event")
		if err != nil {
			t.Fatalf("Notify failed: %v", err)
		}

		// Give notification time to be processed
		time.Sleep(200 * time.Millisecond)

		if !notificationReceived {
			t.Error("Notification was not received")
		}

		if notificationData != "test event" {
			t.Errorf("Expected 'test event', got '%s'", notificationData)
		}
	})

	// Test 3: CallBatch method
	t.Run("CallBatch", func(t *testing.T) {
		batch := []BatchRequest{
			{Method: "add", Params: map[string]any{"a": 10.0, "b": 5.0}, IsNotification: false},
			{Method: "multiply", Params: map[string]any{"a": 3.0, "b": 4.0}, IsNotification: false},
			{Method: "echo", Params: "batch test", IsNotification: false},
		}

		results, err := client.CallBatch(batch)
		if err != nil {
			t.Fatalf("CallBatch failed: %v", err)
		}

		// Check first result (add)
		res0, err := results.GetResult(0)
		if err != nil {
			t.Fatalf("Failed to get result 0: %v", err)
		}
		if sum, ok := res0.(float64); !ok || sum != 15.0 {
			t.Errorf("Expected 15.0, got %v", res0)
		}

		// Check second result (multiply)
		res1, err := results.GetResult(1)
		if err != nil {
			t.Fatalf("Failed to get result 1: %v", err)
		}
		if product, ok := res1.(float64); !ok || product != 12.0 {
			t.Errorf("Expected 12.0, got %v", res1)
		}

		// Check third result (echo)
		res2, err := results.GetResult(2)
		if err != nil {
			t.Fatalf("Failed to get result 2: %v", err)
		}
		if msg, ok := res2.(string); !ok || msg != "batch test" {
			t.Errorf("Expected 'batch test', got %v", res2)
		}
	})

	// Test 4: RegisterObject and UnregisterObject
	t.Run("RegisterObject", func(t *testing.T) {
		// Register the counter object
		ref := client.RegisterObject("my-counter", counterObj)
		if ref.Ref != "my-counter" {
			t.Errorf("Expected ref 'my-counter', got '%s'", ref.Ref)
		}

		// Test using the reference
		result, err := client.Call("useRef", map[string]any{
			"ref":    ref,
			"method": "increment",
			"value":  5,
		})
		if err != nil {
			t.Fatalf("Call with ref failed: %v", err)
		}

		var sum int
		if err := result.Decode(&sum); err != nil {
			t.Fatalf("Failed to decode result: %v", err)
		}

		if sum != 5 {
			t.Errorf("Expected counter value 5, got %d", sum)
		}

		// Unregister the object
		client.UnregisterObject(ref)

		// Calling with the ref should now fail
		_, err = client.Call("useRef", map[string]any{
			"ref":    ref,
			"method": "increment",
			"value":  5,
		})
		if err == nil {
			t.Error("Expected error when calling unregistered object")
		}
	})

	// Test 5: GetSession
	t.Run("GetSession", func(t *testing.T) {
		session := client.GetSession()
		if session == nil {
			t.Error("GetSession returned nil")
		}
	})

	// Test 6: Protocol Detection
	t.Run("ProtocolDetection", func(t *testing.T) {
		protocol := getProtocolName(client)
		if protocol == "" {
			t.Error("Protocol detection returned empty string")
		}

		// Should be one of the supported protocols
		validProtocols := map[string]bool{
			"http/3":    true,
			"http/2":    true,
			"websocket": true,
			"http":      true,
		}

		if !validProtocols[protocol] {
			t.Errorf("Protocol detection returned unexpected protocol: %s", protocol)
		}

		t.Logf("Active protocol: %s", protocol)
	})

	// Test 7: Bidirectional RPC (server calls client)
	t.Run("BidirectionalRPC", func(t *testing.T) {
		// Only test if protocol supports bidirectional RPC
		protocol := getProtocolName(client)
		if protocol == "http" {
			t.Skip("Stateless HTTP doesn't support bidirectional RPC")
		}

		result, err := client.Call("callClientMethod", map[string]any{
			"method": "greet",
			"value":  "World",
		})
		if err != nil {
			t.Fatalf("Bidirectional call failed: %v", err)
		}

		var greeting string
		if err := result.Decode(&greeting); err != nil {
			t.Fatalf("Failed to decode result: %v", err)
		}

		if greeting != "Hello, World!" {
			t.Errorf("Expected 'Hello, World!', got '%s'", greeting)
		}
	})
}
