package jsonrpc3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPClient_Call(t *testing.T) {
	// Create test server
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

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, nil)

	// Make call
	var result int
	err := client.Call("add", []int{1, 2, 3, 4, 5}, &result)
	require.NoError(t, err)
	assert.Equal(t, 15, result)
}

func TestHTTPClient_CallMethodNotFound(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	var result string
	err := client.Call("nonexistent", nil, &result)
	require.Error(t, err)

	// Should be a JSON-RPC error
	rpcErr, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, CodeMethodNotFound, rpcErr.Code)
}

func TestHTTPClient_Notify(t *testing.T) {
	root := NewMethodMap()

	called := false
	root.Register("log", func(params Params) (any, error) {
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

func TestHTTPClient_CallBatch(t *testing.T) {
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

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Create batch requests
	requests := []BatchRequest{
		{Method: "add", Params: []int{1, 2, 3}},
		{Method: "add", Params: []int{4, 5, 6}},
		{Method: "add", Params: []int{7, 8, 9}},
	}

	results, err := client.CallBatch(requests)
	require.NoError(t, err)
	require.Len(t, results, 3)

	var result1, result2, result3 int
	require.NoError(t, results[0].Decode(&result1))
	require.NoError(t, results[1].Decode(&result2))
	require.NoError(t, results[2].Decode(&result3))

	assert.Equal(t, 6, result1)
	assert.Equal(t, 15, result2)
	assert.Equal(t, 24, result3)
}

func TestHTTPClient_CallBatchWithNotifications(t *testing.T) {
	root := NewMethodMap()

	callCount := 0
	root.Register("test", func(params Params) (any, error) {
		callCount++
		return "ok", nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Create batch with mix of requests and notifications
	requests := []BatchRequest{
		{Method: "test", Params: nil},
		{Method: "test", Params: nil, IsNotification: true},
		{Method: "test", Params: nil},
	}

	results, err := client.CallBatch(requests)
	require.NoError(t, err)

	// Should have 2 responses (notifications don't get responses)
	require.Len(t, results, 2)

	// All three methods should have been called
	assert.Equal(t, 3, callCount)
}

func TestHTTPClient_CallBatchAllNotifications(t *testing.T) {
	root := NewMethodMap()
	root.Register("test", func(params Params) (any, error) {
		return "ok", nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Create batch with all notifications
	requests := []BatchRequest{
		{Method: "test", Params: nil, IsNotification: true},
		{Method: "test", Params: nil, IsNotification: true},
	}

	results, err := client.CallBatch(requests)
	require.NoError(t, err)

	// No responses for all notifications
	assert.Nil(t, results)
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
	root.Register("echo", func(params Params) (any, error) {
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

	var result string
	err := client.Call("echo", "hello world", &result)
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

	var result string
	err := client.Call("test", nil, &result)
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
	root.Register("test", func(params Params) (any, error) {
		return "ok", nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Call without result pointer (ignore result)
	err := client.Call("test", nil, nil)
	require.NoError(t, err)
}

func TestBatchResult_DecodeWithError(t *testing.T) {
	result := BatchResult{
		Error: NewMethodNotFoundError("test"),
	}

	var decoded string
	err := result.Decode(&decoded)
	require.Error(t, err)

	rpcErr, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, CodeMethodNotFound, rpcErr.Code)
}
