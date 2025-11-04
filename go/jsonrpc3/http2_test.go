package jsonrpc3

import (
	"context"
	"crypto/tls"
	"fmt"
	"testing"
	"time"
)

// TestHTTP2_BasicCall tests basic method calls over HTTP/2.
func TestHTTP2_BasicCall(t *testing.T) {
	// Create server with simple method
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

	server := NewHTTP2Server(root, nil)

	// Start server in background
	addr := "localhost:13001"
	go func() {
		if err := server.ListenAndServe(addr); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(200 * time.Millisecond)

	// Create client with insecure TLS for testing
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	clientRoot := NewMethodMap()
	client, err := NewHTTP2Client(
		fmt.Sprintf("https://%s", addr),
		clientRoot,
		WithContentType(MimeTypeJSON),
		WithTLSConfig(tlsConfig),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Call add method
	params := map[string]any{"a": 5.0, "b": 3.0}
	result, err := client.Call("add", params)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// Check result
	var sum float64
	if err := result.Decode(&sum); err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	if sum != 8.0 {
		t.Errorf("Expected 8, got %v", sum)
	}
}

// TestHTTP2_Notification tests notifications over HTTP/2.
func TestHTTP2_Notification(t *testing.T) {
	root := NewMethodMap()
	root.Register("add", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return nil, nil
	})

	server := NewHTTP2Server(root, nil)

	addr := "localhost:13002"
	go func() {
		if err := server.ListenAndServe(addr); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(200 * time.Millisecond)

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	clientRoot := NewMethodMap()
	client, err := NewHTTP2Client(
		fmt.Sprintf("https://%s", addr),
		clientRoot,
		WithContentType(MimeTypeJSON),
		WithTLSConfig(tlsConfig),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Send notification
	params := map[string]any{"a": 5.0, "b": 3.0}
	err = client.Notify("add", params)
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	// Notification should not block
	time.Sleep(50 * time.Millisecond)
}

// TestHTTP2_BatchRequest tests batch requests over HTTP/2.
func TestHTTP2_BatchRequest(t *testing.T) {
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
	root.Register("subtract", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&args); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return args.A - args.B, nil
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

	server := NewHTTP2Server(root, nil)

	addr := "localhost:13003"
	go func() {
		if err := server.ListenAndServe(addr); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(200 * time.Millisecond)

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	clientRoot := NewMethodMap()
	client, err := NewHTTP2Client(
		fmt.Sprintf("https://%s", addr),
		clientRoot,
		WithContentType(MimeTypeJSON),
		WithTLSConfig(tlsConfig),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Create batch requests
	batch := []BatchRequest{
		{Method: "add", Params: map[string]any{"a": 5.0, "b": 3.0}, IsNotification: false},
		{Method: "subtract", Params: map[string]any{"a": 10.0, "b": 4.0}, IsNotification: false},
		{Method: "multiply", Params: map[string]any{"a": 3.0, "b": 7.0}, IsNotification: false},
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
	if sum, ok := res0.(float64); !ok || sum != 8.0 {
		t.Errorf("Expected 8, got %v", res0)
	}

	// Check second result (subtract)
	res1, err := results.GetResult(1)
	if err != nil {
		t.Fatalf("Failed to get result 1: %v", err)
	}
	if diff, ok := res1.(float64); !ok || diff != 6.0 {
		t.Errorf("Expected 6, got %v", res1)
	}

	// Check third result (multiply)
	res2, err := results.GetResult(2)
	if err != nil {
		t.Fatalf("Failed to get result 2: %v", err)
	}
	if product, ok := res2.(float64); !ok || product != 21.0 {
		t.Errorf("Expected 21, got %v", res2)
	}
}

// TestHTTP2_ErrorHandling tests error responses over HTTP/2.
func TestHTTP2_ErrorHandling(t *testing.T) {
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

	server := NewHTTP2Server(root, nil)

	addr := "localhost:13004"
	go func() {
		if err := server.ListenAndServe(addr); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(200 * time.Millisecond)

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	clientRoot := NewMethodMap()
	client, err := NewHTTP2Client(
		fmt.Sprintf("https://%s", addr),
		clientRoot,
		WithContentType(MimeTypeJSON),
		WithTLSConfig(tlsConfig),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Call divide by zero
	params := map[string]any{"a": 10.0, "b": 0.0}
	_, err = client.Call("divide", params)

	// Should get error
	if err == nil {
		t.Fatal("Expected error for divide by zero")
	}

	rpcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("Expected RPC error, got %T", err)
	}

	if rpcErr.Code != -32000 {
		t.Errorf("Expected error code -32000, got %d", rpcErr.Code)
	}
}

// TestHTTP2_ConcurrentCalls tests concurrent calls over HTTP/2.
func TestHTTP2_ConcurrentCalls(t *testing.T) {
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

	server := NewHTTP2Server(root, nil)

	addr := "localhost:13005"
	go func() {
		if err := server.ListenAndServe(addr); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(200 * time.Millisecond)

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	clientRoot := NewMethodMap()
	client, err := NewHTTP2Client(
		fmt.Sprintf("https://%s", addr),
		clientRoot,
		WithContentType(MimeTypeJSON),
		WithTLSConfig(tlsConfig),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Make concurrent calls
	numCalls := 10
	results := make(chan float64, numCalls)
	errors := make(chan error, numCalls)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < numCalls; i++ {
		go func(n float64) {
			params := map[string]any{"a": n, "b": 1.0}
			result, err := client.Call("add", params)
			if err != nil {
				errors <- err
				return
			}

			var sum float64
			if err := result.Decode(&sum); err != nil {
				errors <- err
				return
			}

			results <- sum
		}(float64(i))
	}

	// Collect results
	for i := 0; i < numCalls; i++ {
		select {
		case <-results:
			// Success
		case err := <-errors:
			t.Fatalf("Call failed: %v", err)
		case <-ctx.Done():
			t.Fatal("Timeout waiting for concurrent calls")
		}
	}
}

// TestHTTP2_CBOR tests CBOR encoding over HTTP/2.
func TestHTTP2_CBOR(t *testing.T) {
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

	server := NewHTTP2Server(root, []string{MimeTypeCBOR})

	addr := "localhost:13006"
	go func() {
		if err := server.ListenAndServe(addr); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(200 * time.Millisecond)

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	clientRoot := NewMethodMap()
	client, err := NewHTTP2Client(
		fmt.Sprintf("https://%s", addr),
		clientRoot,
		WithContentType(MimeTypeCBOR),
		WithTLSConfig(tlsConfig),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Call method with CBOR encoding
	params := map[string]any{"a": 5.0, "b": 3.0}
	result, err := client.Call("add", params)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var sum float64
	if err := result.Decode(&sum); err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	if sum != 8.0 {
		t.Errorf("Expected 8, got %v", sum)
	}
}

// TestHTTP2_BidirectionalBatch tests server-to-client batch calls.
func TestHTTP2_BidirectionalBatch(t *testing.T) {
	// Server object that will make batch calls to client
	serverRoot := NewMethodMap()
	serverRoot.Register("processBatch", func(ctx context.Context, params Params, caller Caller) (any, error) {
		// Server makes a batch call to the client
		batch := []BatchRequest{
			{Method: "add", Params: map[string]any{"a": 5.0, "b": 3.0}, IsNotification: false},
			{Method: "multiply", Params: map[string]any{"a": 4.0, "b": 2.0}, IsNotification: false},
		}

		results, err := caller.CallBatch(batch)
		if err != nil {
			return nil, NewInternalError(fmt.Sprintf("batch call failed: %v", err))
		}

		// Aggregate results
		res0, _ := results.GetResult(0)
		res1, _ := results.GetResult(1)

		return map[string]any{
			"addResult":      res0,
			"multiplyResult": res1,
		}, nil
	})

	server := NewHTTP2Server(serverRoot, nil)

	addr := "localhost:13008"
	go func() {
		if err := server.ListenAndServe(addr); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(200 * time.Millisecond)

	// Client with methods that server can call
	clientRoot := NewMethodMap()
	clientRoot.Register("add", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&args); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return args.A + args.B, nil
	})
	clientRoot.Register("multiply", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&args); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return args.A * args.B, nil
	})

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	client, err := NewHTTP2Client(
		fmt.Sprintf("https://%s", addr),
		clientRoot,
		WithContentType(MimeTypeJSON),
		WithTLSConfig(tlsConfig),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Call server method that will batch-call back to client
	result, err := client.Call("processBatch", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var response map[string]any
	if err := result.Decode(&response); err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	// Check that server received correct results from client batch
	if addResult, ok := response["addResult"].(float64); !ok || addResult != 8.0 {
		t.Errorf("Expected addResult 8.0, got %v", response["addResult"])
	}

	if multiplyResult, ok := response["multiplyResult"].(float64); !ok || multiplyResult != 8.0 {
		t.Errorf("Expected multiplyResult 8.0, got %v", response["multiplyResult"])
	}
}
