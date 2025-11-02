package jsonrpc3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPClient_Call(t *testing.T) {
	// Create test server
	root := NewMethodMap()
	root.Register("add", func(ctx context.Context, params Params, caller Caller) (any, error) {
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

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, nil)

	// Make call
	val, err := client.Call("add", []int{1, 2, 3, 4, 5})
	require.NoError(t, err)
	var result int
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 15, result)
}

func TestHTTPClient_CallMethodNotFound(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	_, err := client.Call("nonexistent", nil)
	require.Error(t, err)

	// Should be a JSON-RPC error
	rpcErr, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, CodeMethodNotFound, rpcErr.Code)
}

func TestHTTPClient_Notify(t *testing.T) {
	root := NewMethodMap()

	called := false
	root.Register("log", func(ctx context.Context, params Params, caller Caller) (any, error) {
		called = true
		return nil, nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	err := client.Notify("log", "test message")
	require.NoError(t, err)
	assert.True(t, called)
}

func TestHTTPClient_Batch(t *testing.T) {
	root := NewMethodMap()
	root.Register("add", func(ctx context.Context, params Params, caller Caller) (any, error) {
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

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Use ExecuteBatch with fluent builder API
	results, err := ExecuteBatch(client, func(b *BatchBuilder) error {
		b.Call("add", []int{1, 2, 3})
		b.Call("add", []int{4, 5, 6})
		b.Call("add", []int{7, 8, 9})
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, 3, results.Len())

	result1, err := results.GetResult(0)
	require.NoError(t, err)
	// CBOR returns uint64, JSON returns float64
	assert.Contains(t, []any{uint64(6), float64(6)}, result1)

	result2, err := results.GetResult(1)
	require.NoError(t, err)
	assert.Contains(t, []any{uint64(15), float64(15)}, result2)

	result3, err := results.GetResult(2)
	require.NoError(t, err)
	assert.Contains(t, []any{uint64(24), float64(24)}, result3)
}

func TestHTTPClient_BatchWithNotifications(t *testing.T) {
	root := NewMethodMap()

	callCount := 0
	root.Register("test", func(ctx context.Context, params Params, caller Caller) (any, error) {
		callCount++
		return "ok", nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Create batch with mix of requests and notifications
	batchReqs := []BatchRequest{
		{Method: "test"},
		{Method: "test", IsNotification: true},
		{Method: "test"},
	}

	results, err := client.CallBatch(batchReqs)
	require.NoError(t, err)

	// All three responses (nil for notification)
	require.Equal(t, 3, results.Len())

	// Check that first and third have responses
	result1, err := results.GetResult(0)
	require.NoError(t, err)
	assert.Equal(t, "ok", result1)

	// Middle one is notification (nil response)
	resp2, err := results.GetResponse(1)
	require.NoError(t, err)
	assert.Nil(t, resp2)

	result3, err := results.GetResult(2)
	require.NoError(t, err)
	assert.Equal(t, "ok", result3)

	// All three methods should have been called
	assert.Equal(t, 3, callCount)
}

func TestHTTPClient_BatchAllNotifications(t *testing.T) {
	root := NewMethodMap()

	callCount := 0
	root.Register("test", func(ctx context.Context, params Params, caller Caller) (any, error) {
		callCount++
		return "ok", nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Create batch with all notifications
	batchReqs := []BatchRequest{
		{Method: "test", IsNotification: true},
		{Method: "test", IsNotification: true},
	}

	results, err := client.CallBatch(batchReqs)
	require.NoError(t, err)

	// All responses are nil for notifications
	require.Equal(t, 2, results.Len())

	resp1, _ := results.GetResponse(0)
	assert.Nil(t, resp1)

	resp2, _ := results.GetResponse(1)
	assert.Nil(t, resp2)

	// Both methods should have been called on server
	assert.Equal(t, 2, callCount)
}

func TestHTTPClient_SetContentType(t *testing.T) {
	client := NewHTTPClient("http://example.com", nil)

	// Default should be CBOR
	assert.Equal(t, "application/cbor", client.contentType)

	// Set to JSON
	client.SetContentType("application/json")
	assert.Equal(t, "application/json", client.contentType)

	// Set to compact CBOR
	client.SetContentType("application/cbor; format=compact")
	assert.Equal(t, "application/cbor; format=compact", client.contentType)
}

func TestHTTPClient_CBOR(t *testing.T) {
	root := NewMethodMap()
	root.Register("echo", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return msg, nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)
	client.SetContentType("application/cbor")

	val, err := client.Call("echo", "hello world")
	require.NoError(t, err)
	var result string
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

func TestHTTPClient_HTTPError(t *testing.T) {
	// Create server that returns HTTP error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	_, err := client.Call("test", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP error 500")
}

func TestHTTPClient_EmptyBatch(t *testing.T) {
	client := NewHTTPClient("http://example.com", nil)

	results, err := client.CallBatch([]BatchRequest{})
	require.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "batch cannot be empty")
}

func TestHTTPClient_GenerateID(t *testing.T) {
	client := NewHTTPClient("http://example.com", nil)

	// IDs should be sequential
	id1 := client.generateID()
	id2 := client.generateID()
	id3 := client.generateID()

	assert.Equal(t, int64(1), id1)
	assert.Equal(t, int64(2), id2)
	assert.Equal(t, int64(3), id3)
}

func TestHTTPClient_NilResult(t *testing.T) {
	root := NewMethodMap()
	root.Register("test", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return "ok", nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Call without result pointer (ignore result)
	_, err := client.Call("test", nil)
	require.NoError(t, err)
}

func TestBatchResults_DecodeResultWithError(t *testing.T) {
	results := &BatchResults{
		responses: []*Response{
			{
				Error: NewMethodNotFoundError("test"),
			},
		},
	}

	var decoded string
	err := results.DecodeResult(0, &decoded)
	require.Error(t, err)

	rpcErr, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, CodeMethodNotFound, rpcErr.Code)
}
