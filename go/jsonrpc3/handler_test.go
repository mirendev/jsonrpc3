package jsonrpc3

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestHandler creates a handler with a MethodMap for testing
func createTestHandler() (*Session, *MethodMap, *Handler) {
	s := NewSession()
	root := NewMethodMap()
	h := NewHandler(s, root, nil)
	return s, root, h
}

func TestNewHandler(t *testing.T) {
	s, root, h := createTestHandler()

	require.NotNil(t, h, "NewHandler() should not return nil")
	assert.Equal(t, s, h.session, "session not set correctly")
	assert.Equal(t, Version30, h.version)
	_ = root // unused in this test
}

func TestHandler_RegisterMethod(t *testing.T) {
	_, root, h := createTestHandler()

	called := false
	root.Register("test", func(params Params) (any, error) {
		called = true
		return "ok", nil
	})

	req := &Request{
		JSONRPC: Version30,
		Method:  "test",
		ID:      1,
	}

	resp := h.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	assert.True(t, called, "method handler should be called")
	assert.Nil(t, resp.Error, "should not have error")
}

func TestHandler_HandleMethod(t *testing.T) {
	_, root, h := createTestHandler()

	root.Register("echo", func(params Params) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, err
		}
		return msg, nil
	})

	req := &Request{
		JSONRPC: Version30,
		Method:  "echo",
		Params:  RawMessage(`"hello"`),
		ID:      1,
	}

	resp := h.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	require.Nil(t, resp.Error, "should not have error")

	var result string
	require.NoError(t, json.Unmarshal(resp.Result, &result), "should unmarshal result")
	assert.Equal(t, "hello", result)
}

func TestHandler_MethodNotFound(t *testing.T) {
	_, _, h := createTestHandler()

	req := &Request{
		JSONRPC: Version30,
		Method:  "non_existent",
		ID:      1,
	}

	resp := h.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	require.NotNil(t, resp.Error, "should have error for non-existent method")
	assert.Equal(t, CodeMethodNotFound, resp.Error.Code)
}

func TestHandler_MethodError(t *testing.T) {
	_, root, h := createTestHandler()

	root.Register("error", func(params Params) (any, error) {
		return nil, errors.New("something went wrong")
	})

	req := &Request{
		JSONRPC: Version30,
		Method:  "error",
		ID:      1,
	}

	resp := h.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	require.NotNil(t, resp.Error, "should have error")
	assert.Equal(t, CodeInternalError, resp.Error.Code)
}

func TestHandler_MethodRPCError(t *testing.T) {
	_, root, h := createTestHandler()

	root.Register("invalid_params", func(params Params) (any, error) {
		return nil, NewInvalidParamsError("wrong format")
	})

	req := &Request{
		JSONRPC: Version30,
		Method:  "invalid_params",
		ID:      1,
	}

	resp := h.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	require.NotNil(t, resp.Error, "should have error")
	assert.Equal(t, CodeInvalidParams, resp.Error.Code)
}

func TestHandler_Notification(t *testing.T) {
	_, root, h := createTestHandler()

	called := false
	root.Register("notify", func(params Params) (any, error) {
		called = true
		return "ok", nil
	})

	req := &Request{
		JSONRPC: Version30,
		Method:  "notify",
		ID:      nil, // Notification has no ID
	}

	resp := h.HandleRequest(req)
	assert.Nil(t, resp, "HandleRequest() should return nil for notification")
	assert.True(t, called, "notification handler should be called")
}

func TestHandler_RefMethod(t *testing.T) {
	s, _, h := createTestHandler()

	// Create a counter object
	type Counter struct {
		Value int
	}

	counter := &Counter{Value: 10}
	counterObj := NewMethodMap()
	counterObj.Register("increment", func(params Params) (any, error) {
		counter.Value++
		return counter.Value, nil
	})

	// Add to handler's objects map
	h.AddObject("counter-1", counterObj)

	req := &Request{
		JSONRPC: Version30,
		Ref:     "counter-1",
		Method:  "increment",
		ID:      1,
	}

	resp := h.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	require.Nil(t, resp.Error, "should not have error")

	var result int
	require.NoError(t, json.Unmarshal(resp.Result, &result), "should unmarshal result")
	assert.Equal(t, 11, result)
	assert.Equal(t, 11, counter.Value)
	_ = s // unused in this test
}

func TestHandler_RefMethodNotFound(t *testing.T) {
	_, _, h := createTestHandler()

	req := &Request{
		JSONRPC: Version30,
		Ref:     "non-existent",
		Method:  "test",
		ID:      1,
	}

	resp := h.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	require.NotNil(t, resp.Error, "should have error for non-existent ref")
	assert.Equal(t, CodeReferenceNotFound, resp.Error.Code)
}

func TestHandler_RefMethodHandlerNotFound(t *testing.T) {
	_, _, h := createTestHandler()

	// Add an object that will return method not found
	obj := NewMethodMap()
	h.AddObject("obj-1", obj)

	req := &Request{
		JSONRPC: Version30,
		Ref:     "obj-1",
		Method:  "non_existent",
		ID:      1,
	}

	resp := h.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	require.NotNil(t, resp.Error, "should have error for non-existent method")
	assert.Equal(t, CodeMethodNotFound, resp.Error.Code)
}

func TestHandler_ProtocolMethod(t *testing.T) {
	s, _, h := createTestHandler()

	req := &Request{
		JSONRPC: Version30,
		Ref:     "$rpc",
		Method:  "session_id",
		ID:      1,
	}

	resp := h.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	require.Nil(t, resp.Error, "should not have error")

	var result SessionIDResult
	require.NoError(t, json.Unmarshal(resp.Result, &result), "should unmarshal result")
	assert.Equal(t, s.ID(), result.SessionID)
}

func TestHandler_ProtocolMethodNotification(t *testing.T) {
	s, _, h := createTestHandler()

	s.AddLocalRef("obj-1", "test")

	req := &Request{
		JSONRPC: Version30,
		Ref:     "$rpc",
		Method:  "dispose",
		Params:  RawMessage(`{"ref":"obj-1"}`),
		ID:      nil, // Notification
	}

	resp := h.HandleRequest(req)
	assert.Nil(t, resp, "HandleRequest() should return nil for notification")

	// Verify the ref was disposed
	assert.False(t, s.HasLocalRef("obj-1"), "ref should have been disposed")
}

func TestHandler_InvalidRequest(t *testing.T) {
	_, _, h := createTestHandler()

	req := &Request{
		JSONRPC: Version30,
		Method:  "", // Empty method
		ID:      1,
	}

	resp := h.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	require.NotNil(t, resp.Error, "should have error for invalid request")
	assert.Equal(t, CodeInvalidRequest, resp.Error.Code)
}

func TestHandler_HandleBatch(t *testing.T) {
	_, root, h := createTestHandler()

	root.Register("echo", func(params Params) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, err
		}
		return msg, nil
	})

	batch := Batch{
		{JSONRPC: Version30, Method: "echo", Params: RawMessage(`"hello"`), ID: 1},
		{JSONRPC: Version30, Method: "echo", Params: RawMessage(`"world"`), ID: 2},
	}

	responses := h.HandleBatch(batch)
	require.Len(t, responses, 2)

	var result1 string
	require.NoError(t, json.Unmarshal(responses[0].Result, &result1), "should unmarshal result 1")
	assert.Equal(t, "hello", result1)

	var result2 string
	require.NoError(t, json.Unmarshal(responses[1].Result, &result2), "should unmarshal result 2")
	assert.Equal(t, "world", result2)
}

func TestHandler_HandleBatchWithNotifications(t *testing.T) {
	_, root, h := createTestHandler()

	called := 0
	root.Register("test", func(params Params) (any, error) {
		called++
		return "ok", nil
	})

	batch := Batch{
		{JSONRPC: Version30, Method: "test", ID: 1},
		{JSONRPC: Version30, Method: "test", ID: nil}, // Notification
		{JSONRPC: Version30, Method: "test", ID: 2},
	}

	responses := h.HandleBatch(batch)
	assert.Len(t, responses, 2, "notification should not have response")
	assert.Equal(t, 3, called)
}

func TestHandler_HandleBatchAllNotifications(t *testing.T) {
	_, root, h := createTestHandler()

	called := 0
	root.Register("test", func(params Params) (any, error) {
		called++
		return "ok", nil
	})

	batch := Batch{
		{JSONRPC: Version30, Method: "test", ID: nil},
		{JSONRPC: Version30, Method: "test", ID: nil},
	}

	responses := h.HandleBatch(batch)
	assert.Empty(t, responses, "all notifications")
	assert.Equal(t, 2, called)
}

func TestHandler_HandleBatchEmpty(t *testing.T) {
	_, _, h := createTestHandler()

	batch := Batch{}

	responses := h.HandleBatch(batch)
	require.Len(t, responses, 1, "should have error response")
	require.NotNil(t, responses[0].Error, "should have error for empty batch")
	assert.Equal(t, CodeInvalidRequest, responses[0].Error.Code)
}

func TestHandler_SetVersion(t *testing.T) {
	_, root, h := createTestHandler()
	h.SetVersion(Version20)

	root.Register("test", func(params Params) (any, error) {
		return "ok", nil
	})

	req := &Request{
		JSONRPC: Version20,
		Method:  "test",
		ID:      1,
	}

	resp := h.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	assert.Equal(t, Version20, resp.JSONRPC)
}

func TestDecodeRequest_Single(t *testing.T) {
	data := []byte(`{"jsonrpc":"3.0","method":"test","id":1}`)

	req, batch, isBatch, err := DecodeRequest(data, "application/json")
	require.NoError(t, err, "DecodeRequest() should not error")
	assert.False(t, isBatch, "isBatch should be false")
	require.NotNil(t, req, "req should not be nil")
	assert.Nil(t, batch, "batch should be nil")
	assert.Equal(t, "test", req.Method)
}

func TestDecodeRequest_Batch(t *testing.T) {
	data := []byte(`[{"jsonrpc":"3.0","method":"test1","id":1},{"jsonrpc":"3.0","method":"test2","id":2}]`)

	req, batch, isBatch, err := DecodeRequest(data, "application/json")
	require.NoError(t, err, "DecodeRequest() should not error")
	assert.True(t, isBatch, "isBatch should be true")
	assert.Nil(t, req, "req should be nil")
	require.NotNil(t, batch, "batch should not be nil")
	assert.Len(t, batch, 2)
}

func TestDecodeRequest_BatchWithWhitespace(t *testing.T) {
	data := []byte(`
	[{"jsonrpc":"3.0","method":"test","id":1}]`)

	req, batch, isBatch, err := DecodeRequest(data, "application/json")
	require.NoError(t, err, "DecodeRequest() should not error")
	assert.True(t, isBatch, "isBatch should be true")
	assert.Nil(t, req, "req should be nil")
	require.NotNil(t, batch, "batch should not be nil")
}

func TestDecodeRequest_Invalid(t *testing.T) {
	data := []byte(`{invalid json}`)

	_, _, _, err := DecodeRequest(data, "application/json")
	assert.Error(t, err, "should have error for invalid JSON")
}

func TestEncodeResponse(t *testing.T) {
	resp := &Response{
		JSONRPC: Version30,
		Result:  RawMessage(`"ok"`),
		ID:      1,
	}

	data, err := EncodeResponse(resp)
	require.NoError(t, err, "EncodeResponse() should not error")

	var decoded Response
	require.NoError(t, json.Unmarshal(data, &decoded), "should unmarshal encoded response")
	assert.Equal(t, Version30, decoded.JSONRPC)
}

func TestEncodeBatchResponse(t *testing.T) {
	batch := BatchResponse{
		{JSONRPC: Version30, Result: RawMessage(`"ok1"`), ID: 1},
		{JSONRPC: Version30, Result: RawMessage(`"ok2"`), ID: 2},
	}

	data, err := EncodeBatchResponse(batch)
	require.NoError(t, err, "EncodeBatchResponse() should not error")

	var decoded BatchResponse
	require.NoError(t, json.Unmarshal(data, &decoded), "should unmarshal encoded batch")
	assert.Len(t, decoded, 2)
}
