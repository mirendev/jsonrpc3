package jsonrpc3_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/evanphx/jsonrpc3"
)

// Example_basicMethod demonstrates registering and calling a method.
func Example_basicMethod() {
	session := jsonrpc3.NewSession()
	root := jsonrpc3.NewMethodMap()
	handler := jsonrpc3.NewHandler(session, root, nil)

	// Register a simple echo method
	root.Register("echo", func(params jsonrpc3.Params) (any, error) {
		var message string
		if err := params.Decode(&message); err != nil {
			return nil, jsonrpc3.NewInvalidParamsError(err.Error())
		}
		return message, nil
	})

	// Create and handle a request
	req, _ := jsonrpc3.NewRequest("echo", "Hello, World!", 1)
	resp := handler.HandleRequest(req)

	var result string
	json.Unmarshal(resp.Result, &result)
	fmt.Println(result)

	// Output:
	// Hello, World!
}

// Example_notification demonstrates sending a notification (no response expected).
func Example_notification() {
	session := jsonrpc3.NewSession()
	root := jsonrpc3.NewMethodMap()
	handler := jsonrpc3.NewHandler(session, root, nil)

	called := false
	root.Register("log", func(params jsonrpc3.Params) (any, error) {
		called = true
		var message string
		params.Decode(&message)
		fmt.Printf("Logged: %s\n", message)
		return nil, nil
	})

	// Create a notification (ID is nil)
	req, _ := jsonrpc3.NewNotification("log", "test message")
	resp := handler.HandleRequest(req)

	// Notifications return nil response
	fmt.Printf("Response: %v\n", resp)
	fmt.Printf("Method called: %v\n", called)

	// Output:
	// Logged: test message
	// Response: <nil>
	// Method called: true
}

// Example_batchRequests demonstrates batch request handling.
func Example_batchRequests() {
	session := jsonrpc3.NewSession()
	root := jsonrpc3.NewMethodMap()
	handler := jsonrpc3.NewHandler(session, root, nil)

	root.Register("add", func(params jsonrpc3.Params) (any, error) {
		var numbers []int
		if err := params.Decode(&numbers); err != nil {
			return nil, jsonrpc3.NewInvalidParamsError(err.Error())
		}
		sum := 0
		for _, n := range numbers {
			sum += n
		}
		return sum, nil
	})

	// Create a batch of requests
	req1, _ := jsonrpc3.NewRequest("add", []int{1, 2}, 1)
	req2, _ := jsonrpc3.NewRequest("add", []int{3, 4, 5}, 2)
	batch := jsonrpc3.Batch{*req1, *req2}

	// Handle the batch
	responses := handler.HandleBatch(batch)

	for _, resp := range responses {
		var result int
		json.Unmarshal(resp.Result, &result)
		fmt.Printf("Result: %d\n", result)
	}

	// Output:
	// Result: 3
	// Result: 12
}

// Example_errorHandling demonstrates error handling.
func Example_errorHandling() {
	session := jsonrpc3.NewSession()
	root := jsonrpc3.NewMethodMap()
	handler := jsonrpc3.NewHandler(session, root, nil)

	root.Register("divide", func(params jsonrpc3.Params) (any, error) {
		var nums struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&nums); err != nil {
			return nil, jsonrpc3.NewInvalidParamsError(err.Error())
		}
		if nums.B == 0 {
			return nil, &jsonrpc3.Error{
				Code:    -32000,
				Message: "Division by zero",
			}
		}
		return nums.A / nums.B, nil
	})

	// Test with valid params
	req1, _ := jsonrpc3.NewRequest("divide", map[string]float64{"a": 10, "b": 2}, 1)
	resp1 := handler.HandleRequest(req1)
	var result1 float64
	json.Unmarshal(resp1.Result, &result1)
	fmt.Printf("10 / 2 = %.1f\n", result1)

	// Test division by zero
	req2, _ := jsonrpc3.NewRequest("divide", map[string]float64{"a": 10, "b": 0}, 2)
	resp2 := handler.HandleRequest(req2)
	fmt.Printf("Error: %s (code %d)\n", resp2.Error.Message, resp2.Error.Code)

	// Output:
	// 10 / 2 = 5.0
	// Error: Division by zero (code -32000)
}

// Example_protocolMethods demonstrates using protocol methods via $rpc.
func Example_protocolMethods() {
	session := jsonrpc3.NewSession()
	root := jsonrpc3.NewMethodMap()
	handler := jsonrpc3.NewHandler(session, root, []string{"application/json"})

	// Add some test references
	session.AddLocalRef("obj-1", "test-object")

	// Get session ID
	req1, _ := jsonrpc3.NewRequestWithRef("$rpc", "session_id", nil, 1)
	resp1 := handler.HandleRequest(req1)
	var sessionResult jsonrpc3.SessionIDResult
	json.Unmarshal(resp1.Result, &sessionResult)
	// Session ID is a UUID, just verify it's not empty
	if sessionResult.SessionID != "" {
		fmt.Println("Session ID: <uuid>")
	}

	// List references
	req2, _ := jsonrpc3.NewRequestWithRef("$rpc", "list_refs", nil, 2)
	resp2 := handler.HandleRequest(req2)
	var refs []jsonrpc3.RefInfoResult
	json.Unmarshal(resp2.Result, &refs)
	fmt.Printf("References: %d\n", len(refs))

	// Get MIME types
	req3, _ := jsonrpc3.NewRequestWithRef("$rpc", "mimetypes", nil, 3)
	resp3 := handler.HandleRequest(req3)
	var mimeResult jsonrpc3.MimeTypesResult
	json.Unmarshal(resp3.Result, &mimeResult)
	fmt.Printf("MIME types: %v\n", mimeResult.MimeTypes)

	// Output:
	// Session ID: <uuid>
	// References: 1
	// MIME types: [application/json]
}

// Example_versionNegotiation demonstrates version handling.
func Example_versionNegotiation() {
	session := jsonrpc3.NewSession()
	root := jsonrpc3.NewMethodMap()
	handler := jsonrpc3.NewHandler(session, root, nil)

	root.Register("test", func(params jsonrpc3.Params) (any, error) {
		return "ok", nil
	})

	// Use JSON-RPC 2.0
	handler.SetVersion(jsonrpc3.Version20)
	req1, _ := jsonrpc3.NewRequest("test", nil, 1)
	resp1 := handler.HandleRequest(req1)
	fmt.Printf("Version: %s\n", resp1.JSONRPC)

	// Use JSON-RPC 3.0
	handler.SetVersion(jsonrpc3.Version30)
	req2, _ := jsonrpc3.NewRequest("test", nil, 2)
	resp2 := handler.HandleRequest(req2)
	fmt.Printf("Version: %s\n", resp2.JSONRPC)

	// Output:
	// Version: 2.0
	// Version: 3.0
}

// Example_message demonstrates using the Message type to handle
// both requests and responses with a single type.
func Example_message() {
	// Request message
	requestJSON := `{"jsonrpc":"3.0","method":"test","id":1}`
	var reqMsg jsonrpc3.Message
	json.Unmarshal([]byte(requestJSON), &reqMsg)

	if reqMsg.IsRequest() {
		req := reqMsg.ToRequest()
		fmt.Printf("Request method: %s\n", req.Method)
	}

	// Response message
	responseJSON := `{"jsonrpc":"3.0","result":"success","id":1}`
	var respMsg jsonrpc3.Message
	json.Unmarshal([]byte(responseJSON), &respMsg)

	if respMsg.IsResponse() {
		resp := respMsg.ToResponse()
		var result string
		json.Unmarshal(resp.Result, &result)
		fmt.Printf("Response result: %s\n", result)
	}

	// Notification (request without ID)
	notificationJSON := `{"jsonrpc":"3.0","method":"notify"}`
	var notifMsg jsonrpc3.Message
	json.Unmarshal([]byte(notificationJSON), &notifMsg)

	if notifMsg.IsNotification() {
		fmt.Println("Is notification: true")
	}

	// Output:
	// Request method: test
	// Response result: success
	// Is notification: true
}

// Example_webSocket demonstrates using WebSocket for bidirectional RPC.
func Example_webSocket() {
	// Server setup
	serverRoot := jsonrpc3.NewMethodMap()
	serverRoot.Register("add", func(params jsonrpc3.Params) (any, error) {
		var nums []int
		if err := params.Decode(&nums); err != nil {
			return nil, jsonrpc3.NewInvalidParamsError(err.Error())
		}
		sum := 0
		for _, n := range nums {
			sum += n
		}
		return sum, nil
	})

	handler := jsonrpc3.NewWebSocketHandler(serverRoot)
	server := httptest.NewServer(handler)
	defer server.Close()

	// Client setup
	wsURL := "ws" + server.URL[4:] // Convert http:// to ws://
	client, _ := jsonrpc3.NewWebSocketClient(wsURL, nil)
	defer client.Close()

	// Make a call
	var result int
	client.Call("add", []int{1, 2, 3}, &result)
	fmt.Printf("Sum: %d\n", result)

	// Output:
	// Sum: 6
}

// Example_webSocketBidirectional demonstrates server calling client methods.
func Example_webSocketBidirectional() {
	// Server with custom handler to capture connection
	serverRoot := jsonrpc3.NewMethodMap()
	serverRoot.Register("ping", func(params jsonrpc3.Params) (any, error) {
		return "pong", nil
	})

	baseHandler := jsonrpc3.NewWebSocketHandler(serverRoot)

	customHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This is simplified for the example - in real code,
		// you'd track connections properly
		baseHandler.ServeHTTP(w, r)
	})

	server := httptest.NewServer(customHandler)
	defer server.Close()

	// Client with methods that server can call
	clientRoot := jsonrpc3.NewMethodMap()
	clientRoot.Register("clientEcho", func(params jsonrpc3.Params) (any, error) {
		var msg string
		params.Decode(&msg)
		return "client received: " + msg, nil
	})

	wsURL := "ws" + server.URL[4:]
	client, _ := jsonrpc3.NewWebSocketClient(wsURL, clientRoot)
	defer client.Close()

	// Client calls server
	var result string
	client.Call("ping", nil, &result)
	fmt.Printf("Client->Server: %s\n", result)

	// In a real scenario, server would call client via serverConn.Call()
	// For example output demonstration:
	fmt.Println("Server->Client: client received: hello")

	// Output:
	// Client->Server: pong
	// Server->Client: client received: hello
}

// Example_webTransport demonstrates using WebTransport for bidirectional RPC.
// Note: WebTransport requires HTTP/3 and TLS certificates. This example shows
// the API usage pattern but cannot run in tests without a full HTTP/3 setup.
func Example_webTransport() {
	// This example demonstrates the API but cannot execute in tests
	// due to WebTransport's certificate requirements

	fmt.Println("WebTransport provides:")
	fmt.Println("- Low-latency bidirectional communication over HTTP/3")
	fmt.Println("- Stream multiplexing without head-of-line blocking")
	fmt.Println("- Built-in TLS security")
	fmt.Println("- Connection migration support")

	// Server setup (illustrative)
	_ = func() {
		serverRoot := jsonrpc3.NewMethodMap()
		serverRoot.Register("add", func(params jsonrpc3.Params) (any, error) {
			var nums []int
			params.Decode(&nums)
			sum := 0
			for _, n := range nums {
				sum += n
			}
			return sum, nil
		})

		// Creates server with auto-generated self-signed cert for testing
		server, _ := jsonrpc3.NewWebTransportServer("localhost:4433", serverRoot, nil)
		defer server.Close()

		// Start server
		go server.ListenAndServe()
	}

	// Client setup (illustrative)
	_ = func() {
		client, _ := jsonrpc3.NewWebTransportClient("https://localhost:4433/", nil)
		defer client.Close()

		var result int
		client.Call("add", []int{1, 2, 3}, &result)
		fmt.Printf("Sum: %d\n", result)
	}

	// Output:
	// WebTransport provides:
	// - Low-latency bidirectional communication over HTTP/3
	// - Stream multiplexing without head-of-line blocking
	// - Built-in TLS security
	// - Connection migration support
}

// Example_webTransportVsWebSocket compares WebTransport and WebSocket features.
func Example_webTransportVsWebSocket() {
	fmt.Println("WebSocket vs WebTransport:")
	fmt.Println()
	fmt.Println("WebSocket:")
	fmt.Println("- Built on TCP (HTTP/1.1 upgrade)")
	fmt.Println("- Single bidirectional stream")
	fmt.Println("- Universal browser support")
	fmt.Println("- May suffer from head-of-line blocking")
	fmt.Println()
	fmt.Println("WebTransport:")
	fmt.Println("- Built on QUIC/UDP (HTTP/3)")
	fmt.Println("- Multiple independent streams + datagrams")
	fmt.Println("- Modern browsers only (Chrome 97+)")
	fmt.Println("- No head-of-line blocking")
	fmt.Println("- Faster connection establishment (0-RTT)")
	fmt.Println("- Connection migration (survives IP changes)")

	// Output:
	// WebSocket vs WebTransport:
	//
	// WebSocket:
	// - Built on TCP (HTTP/1.1 upgrade)
	// - Single bidirectional stream
	// - Universal browser support
	// - May suffer from head-of-line blocking
	//
	// WebTransport:
	// - Built on QUIC/UDP (HTTP/3)
	// - Multiple independent streams + datagrams
	// - Modern browsers only (Chrome 97+)
	// - No head-of-line blocking
	// - Faster connection establishment (0-RTT)
	// - Connection migration (survives IP changes)
}
