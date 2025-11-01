package jsonrpc3

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCounter is a simple counter object for testing
type testCounter struct {
	value int
}

func (c *testCounter) CallMethod(method string, params Params, caller Caller) (any, error) {
	switch method {
	case "increment":
		c.value++
		return c.value, nil
	case "getValue":
		return c.value, nil
	default:
		return nil, NewMethodNotFoundError(method)
	}
}

// TestHTTPIntegration_CompleteWorkflow tests a complete client-server workflow
func TestHTTPIntegration_CompleteWorkflow(t *testing.T) {
	// Create server
	root := NewMethodMap()

	// Register calculator methods
	root.Register("add", func(params Params, caller Caller) (any, error) {
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

	root.Register("multiply", func(params Params, caller Caller) (any, error) {
		var nums []int
		if err := params.Decode(&nums); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		result := 1
		for _, n := range nums {
			result *= n
		}
		return result, nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, nil)

	// Test add
	val, err := client.Call("add", []int{10, 20, 30})
	require.NoError(t, err)
	var addResult int
	err = val.Decode(&addResult)
	require.NoError(t, err)
	assert.Equal(t, 60, addResult)

	// Test multiply
	val2, err := client.Call("multiply", []int{2, 3, 4})
	require.NoError(t, err)
	var mulResult int
	err = val2.Decode(&mulResult)
	require.NoError(t, err)
	assert.Equal(t, 24, mulResult)

	// Test batch
	batchReqs := []BatchRequest{
		{Method: "add", Params: []int{1, 2, 3}},
		{Method: "multiply", Params: []int{2, 3, 4}},
	}
	results, err := client.CallBatch(batchReqs)
	require.NoError(t, err)
	require.Equal(t, 2, results.Len())

	var batchAdd, batchMul int
	require.NoError(t, results.DecodeResult(0, &batchAdd))
	require.NoError(t, results.DecodeResult(1, &batchMul))
	assert.Equal(t, 6, batchAdd)
	assert.Equal(t, 24, batchMul)
}

// TestHTTPIntegration_ProtocolMethods tests $rpc protocol methods
func TestHTTPIntegration_ProtocolMethods(t *testing.T) {
	// Create server
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, nil)

	// Get session ID - first request creates a session
	val, err := client.Call("session_id", nil, ToRef(Protocol))
	require.NoError(t, err)
	var sessionResult1 SessionIDResult
	err = val.Decode(&sessionResult1)
	require.NoError(t, err)
	assert.NotEmpty(t, sessionResult1.SessionID)

	// Get session ID again - should be the same (session persists)
	val2, err := client.Call("session_id", nil, ToRef(Protocol))
	require.NoError(t, err)
	var sessionResult2 SessionIDResult
	err = val2.Decode(&sessionResult2)
	require.NoError(t, err)
	assert.NotEmpty(t, sessionResult2.SessionID)
	assert.Equal(t, sessionResult1.SessionID, sessionResult2.SessionID, "Session should persist across requests")

	// Get mime types - should support all 3 formats
	val3, err := client.Call("mimetypes", nil, ToRef(Protocol))
	require.NoError(t, err)
	var mimeResult MimeTypesResult
	err = val3.Decode(&mimeResult)
	require.NoError(t, err)
	assert.Len(t, mimeResult.MimeTypes, 3)
	assert.Contains(t, mimeResult.MimeTypes, "application/json")
	assert.Contains(t, mimeResult.MimeTypes, "application/cbor")
	assert.Contains(t, mimeResult.MimeTypes, "application/cbor; format=compact")
}

// TestHTTPIntegration_ErrorHandling tests error handling over HTTP
func TestHTTPIntegration_ErrorHandling(t *testing.T) {
	// Create server
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)

	root.Register("divide", func(params Params, caller Caller) (any, error) {
		var nums []float64
		if err := params.Decode(&nums); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		if len(nums) != 2 {
			return nil, NewInvalidParamsError("expected exactly 2 numbers")
		}
		if nums[1] == 0 {
			return nil, &Error{
				Code:    -32000,
				Message: "Division by zero",
				Data:    "Cannot divide by zero",
			}
		}
		return nums[0] / nums[1], nil
	})

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, nil)

	// Test invalid params
	_, err := client.Call("divide", "not an array")
	require.Error(t, err)
	rpcErr, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, CodeInvalidParams, rpcErr.Code)

	// Test custom error
	_, err = client.Call("divide", []float64{10, 0})
	require.Error(t, err)
	rpcErr, ok = err.(*Error)
	require.True(t, ok)
	assert.Equal(t, -32000, rpcErr.Code)
	assert.Equal(t, "Division by zero", rpcErr.Message)

	// Test successful call
	val, err := client.Call("divide", []float64{10, 2})
	require.NoError(t, err)
	var result float64
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 5.0, result)
}

// TestHTTPIntegration_CBOR tests CBOR encoding over HTTP
func TestHTTPIntegration_CBOR(t *testing.T) {
	// Create server with CBOR support
	root := NewMethodMap()
	root.Register("echo", func(params Params, caller Caller) (any, error) {
		var data map[string]any
		if err := params.Decode(&data); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return data, nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	// Create client with CBOR
	client := NewHTTPClient(server.URL, nil)
	client.SetContentType("application/cbor")

	// Make call
	input := map[string]any{
		"name":   "Alice",
		"age":    30,
		"active": true,
	}
	val, err := client.Call("echo", input)
	require.NoError(t, err)
	var output map[string]any
	err = val.Decode(&output)
	require.NoError(t, err)

	assert.Equal(t, "Alice", output["name"])
	assert.Equal(t, uint64(30), output["age"]) // CBOR unmarshals to uint64
	assert.Equal(t, true, output["active"])
}

// TestHTTPIntegration_MixedBatch tests batch with both success and errors
func TestHTTPIntegration_MixedBatch(t *testing.T) {
	// Create server
	root := NewMethodMap()
	root.Register("test", func(params Params, caller Caller) (any, error) {
		return "ok", nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, nil)

	// Create batch with valid and invalid methods
	batchReqs := []BatchRequest{
		{Method: "test", Params: nil},
		{Method: "nonexistent", Params: nil},
		{Method: "test", Params: nil},
	}

	results, err := client.CallBatch(batchReqs)
	require.NoError(t, err)
	require.Equal(t, 3, results.Len())

	// First should succeed
	var result1 string
	err = results.DecodeResult(0, &result1)
	require.NoError(t, err)
	assert.Equal(t, "ok", result1)

	// Second should fail
	resp2, err := results.GetResponse(1)
	require.NoError(t, err)
	assert.NotNil(t, resp2.Error)
	assert.Equal(t, CodeMethodNotFound, resp2.Error.Code)

	// Third should succeed
	var result3 string
	err = results.DecodeResult(2, &result3)
	require.NoError(t, err)
	assert.Equal(t, "ok", result3)
}

// TestHTTPIntegration_Notifications tests notifications don't return responses
func TestHTTPIntegration_Notifications(t *testing.T) {
	// Create server
	root := NewMethodMap()

	logMessages := []string{}
	root.Register("log", func(params Params, caller Caller) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		logMessages = append(logMessages, msg)
		return nil, nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()
	defer httpHandler.Close()

	// Create client
	client := NewHTTPClient(server.URL, nil)

	// Send notifications
	err := client.Notify("log", "message 1")
	require.NoError(t, err)

	err = client.Notify("log", "message 2")
	require.NoError(t, err)

	err = client.Notify("log", "message 3")
	require.NoError(t, err)

	// Verify messages were logged
	assert.Len(t, logMessages, 3)
	assert.Equal(t, "message 1", logMessages[0])
	assert.Equal(t, "message 2", logMessages[1])
	assert.Equal(t, "message 3", logMessages[2])
}

// TestHTTPIntegration_SessionPersistence tests that sessions persist across requests
func TestHTTPIntegration_SessionPersistence(t *testing.T) {
	// Create server
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()
	defer httpHandler.Close()

	// Create client
	client := NewHTTPClient(server.URL, nil)

	// First request - should get a session ID
	assert.Empty(t, client.SessionID())
	val, err := client.Call("session_id", nil, ToRef(Protocol))
	require.NoError(t, err)
	var sessionResult1 SessionIDResult
	err = val.Decode(&sessionResult1)
	require.NoError(t, err)
	assert.NotEmpty(t, sessionResult1.SessionID)

	// Client should now have the session ID
	clientSessionID := client.SessionID()
	assert.NotEmpty(t, clientSessionID)
	assert.Equal(t, sessionResult1.SessionID, clientSessionID)

	// Second request - should reuse the same session
	val2, err := client.Call("session_id", nil, ToRef(Protocol))
	require.NoError(t, err)
	var sessionResult2 SessionIDResult
	err = val2.Decode(&sessionResult2)
	require.NoError(t, err)
	assert.Equal(t, sessionResult1.SessionID, sessionResult2.SessionID, "Session should persist across requests")
	assert.Equal(t, clientSessionID, client.SessionID(), "Client session ID should not change")
}

// TestHTTPIntegration_SessionObjectReferences tests object references across requests
func TestHTTPIntegration_SessionObjectReferences(t *testing.T) {
	// Create server
	root := NewMethodMap()
	root.Register("createCounter", func(params Params, caller Caller) (any, error) {
		counter := &testCounter{value: 0}
		return counter, nil
	})

	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()
	defer httpHandler.Close()

	// Create client
	client := NewHTTPClient(server.URL, nil)

	// Create a counter - returns a local reference
	val, err := client.Call("createCounter", nil)
	require.NoError(t, err)
	var localRef Reference
	err = val.Decode(&localRef)
	require.NoError(t, err)
	assert.NotEmpty(t, localRef.Ref)

	// Call increment on the reference - should work because session persists
	val2, err := client.Call("increment", nil, ToRef(localRef))
	require.NoError(t, err)
	var incResult int
	err = val2.Decode(&incResult)
	require.NoError(t, err)
	assert.Equal(t, 1, incResult)

	// Call increment again - should maintain state
	val3, err := client.Call("increment", nil, ToRef(localRef))
	require.NoError(t, err)
	err = val3.Decode(&incResult)
	require.NoError(t, err)
	assert.Equal(t, 2, incResult)

	// Get value
	val4, err := client.Call("getValue", nil, ToRef(localRef))
	require.NoError(t, err)
	var getResult int
	err = val4.Decode(&getResult)
	require.NoError(t, err)
	assert.Equal(t, 2, getResult)
}

// TestHTTPIntegration_MultipleClients tests that different clients get different sessions
func TestHTTPIntegration_MultipleClients(t *testing.T) {
	// Create server
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()
	defer httpHandler.Close()

	// Create two clients
	client1 := NewHTTPClient(server.URL, nil)
	client2 := NewHTTPClient(server.URL, nil)

	// Get session IDs for both clients
	val1, err := client1.Call("session_id", nil, ToRef(Protocol))
	require.NoError(t, err)
	var session1 SessionIDResult
	err = val1.Decode(&session1)
	require.NoError(t, err)

	val2, err := client2.Call("session_id", nil, ToRef(Protocol))
	require.NoError(t, err)
	var session2 SessionIDResult
	err = val2.Decode(&session2)
	require.NoError(t, err)

	// Should have different session IDs
	assert.NotEqual(t, session1.SessionID, session2.SessionID)
	assert.NotEqual(t, client1.SessionID(), client2.SessionID())
}

// TestHTTPIntegration_SessionReset tests that clearing session ID creates a new session
func TestHTTPIntegration_SessionReset(t *testing.T) {
	// Create server
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	server := httptest.NewServer(httpHandler)
	defer server.Close()
	defer httpHandler.Close()

	// Create client
	client := NewHTTPClient(server.URL, nil)

	// Get initial session ID
	val1, err := client.Call("session_id", nil, ToRef(Protocol))
	require.NoError(t, err)
	var session1 SessionIDResult
	err = val1.Decode(&session1)
	require.NoError(t, err)
	firstSessionID := client.SessionID()
	assert.Equal(t, session1.SessionID, firstSessionID)

	// Clear session ID
	client.SetSessionID("")
	assert.Empty(t, client.SessionID())

	// Next request should get a new session
	val2, err := client.Call("session_id", nil, ToRef(Protocol))
	require.NoError(t, err)
	var session2 SessionIDResult
	err = val2.Decode(&session2)
	require.NoError(t, err)
	secondSessionID := client.SessionID()

	assert.NotEqual(t, firstSessionID, secondSessionID)
	assert.Equal(t, session2.SessionID, secondSessionID)
}
