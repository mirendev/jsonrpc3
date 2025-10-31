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

func (c *testCounter) CallMethod(method string, params Params) (any, error) {
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

	root.Register("multiply", func(params Params) (any, error) {
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
	var addResult int
	err := client.Call("add", []int{10, 20, 30}, &addResult)
	require.NoError(t, err)
	assert.Equal(t, 60, addResult)

	// Test multiply
	var mulResult int
	err = client.Call("multiply", []int{2, 3, 4}, &mulResult)
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
	var sessionResult1 SessionIDResult
	err := client.CallRef("$rpc", "session_id", nil, &sessionResult1)
	require.NoError(t, err)
	assert.NotEmpty(t, sessionResult1.SessionID)

	// Get session ID again - should be the same (session persists)
	var sessionResult2 SessionIDResult
	err = client.CallRef("$rpc", "session_id", nil, &sessionResult2)
	require.NoError(t, err)
	assert.NotEmpty(t, sessionResult2.SessionID)
	assert.Equal(t, sessionResult1.SessionID, sessionResult2.SessionID, "Session should persist across requests")

	// Get mime types - should support all 3 formats
	var mimeResult MimeTypesResult
	err = client.CallRef("$rpc", "mimetypes", nil, &mimeResult)
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

	root.Register("divide", func(params Params) (any, error) {
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
	var result float64
	err := client.Call("divide", "not an array", &result)
	require.Error(t, err)
	rpcErr, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, CodeInvalidParams, rpcErr.Code)

	// Test custom error
	err = client.Call("divide", []float64{10, 0}, &result)
	require.Error(t, err)
	rpcErr, ok = err.(*Error)
	require.True(t, ok)
	assert.Equal(t, -32000, rpcErr.Code)
	assert.Equal(t, "Division by zero", rpcErr.Message)

	// Test successful call
	err = client.Call("divide", []float64{10, 2}, &result)
	require.NoError(t, err)
	assert.Equal(t, 5.0, result)
}

// TestHTTPIntegration_CBOR tests CBOR encoding over HTTP
func TestHTTPIntegration_CBOR(t *testing.T) {
	// Create server with CBOR support
	root := NewMethodMap()
	root.Register("echo", func(params Params) (any, error) {
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
	var output map[string]any
	err := client.Call("echo", input, &output)
	require.NoError(t, err)

	assert.Equal(t, "Alice", output["name"])
	assert.Equal(t, uint64(30), output["age"]) // CBOR unmarshals to uint64
	assert.Equal(t, true, output["active"])
}

// TestHTTPIntegration_MixedBatch tests batch with both success and errors
func TestHTTPIntegration_MixedBatch(t *testing.T) {
	// Create server
	root := NewMethodMap()
	root.Register("test", func(params Params) (any, error) {
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
	root.Register("log", func(params Params) (any, error) {
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
	var sessionResult1 SessionIDResult
	err := client.CallRef("$rpc", "session_id", nil, &sessionResult1)
	require.NoError(t, err)
	assert.NotEmpty(t, sessionResult1.SessionID)

	// Client should now have the session ID
	clientSessionID := client.SessionID()
	assert.NotEmpty(t, clientSessionID)
	assert.Equal(t, sessionResult1.SessionID, clientSessionID)

	// Second request - should reuse the same session
	var sessionResult2 SessionIDResult
	err = client.CallRef("$rpc", "session_id", nil, &sessionResult2)
	require.NoError(t, err)
	assert.Equal(t, sessionResult1.SessionID, sessionResult2.SessionID, "Session should persist across requests")
	assert.Equal(t, clientSessionID, client.SessionID(), "Client session ID should not change")
}

// TestHTTPIntegration_SessionObjectReferences tests object references across requests
func TestHTTPIntegration_SessionObjectReferences(t *testing.T) {
	// Create server
	root := NewMethodMap()
	root.Register("createCounter", func(params Params) (any, error) {
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
	var localRef Reference
	err := client.Call("createCounter", nil, &localRef)
	require.NoError(t, err)
	assert.NotEmpty(t, localRef.Ref)

	// Call increment on the reference - should work because session persists
	var incResult int
	err = client.CallRef(localRef.Ref, "increment", nil, &incResult)
	require.NoError(t, err)
	assert.Equal(t, 1, incResult)

	// Call increment again - should maintain state
	err = client.CallRef(localRef.Ref, "increment", nil, &incResult)
	require.NoError(t, err)
	assert.Equal(t, 2, incResult)

	// Get value
	var getResult int
	err = client.CallRef(localRef.Ref, "getValue", nil, &getResult)
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
	var session1, session2 SessionIDResult
	err := client1.CallRef("$rpc", "session_id", nil, &session1)
	require.NoError(t, err)

	err = client2.CallRef("$rpc", "session_id", nil, &session2)
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
	var session1 SessionIDResult
	err := client.CallRef("$rpc", "session_id", nil, &session1)
	require.NoError(t, err)
	firstSessionID := client.SessionID()
	assert.Equal(t, session1.SessionID, firstSessionID)

	// Clear session ID
	client.SetSessionID("")
	assert.Empty(t, client.SessionID())

	// Next request should get a new session
	var session2 SessionIDResult
	err = client.CallRef("$rpc", "session_id", nil, &session2)
	require.NoError(t, err)
	secondSessionID := client.SessionID()

	assert.NotEqual(t, firstSessionID, secondSessionID)
	assert.Equal(t, session2.SessionID, secondSessionID)
}
