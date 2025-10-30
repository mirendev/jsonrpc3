package jsonrpc3

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dummyObject is a simple Object implementation for testing.
type dummyObject struct {
	name string
}

func (d *dummyObject) CallMethod(method string, params Params) (any, error) {
	return nil, NewMethodNotFoundError(method)
}

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

	// Add to session
	s.AddLocalRef("counter-1", counterObj)

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
	s, _, h := createTestHandler()

	// Add an object that will return method not found
	obj := NewMethodMap()
	s.AddLocalRef("obj-1", obj)

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

	s.AddLocalRef("obj-1", &dummyObject{name: "test"})

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

	codec := GetCodec("application/json")
	msgSet, err := codec.UnmarshalMessages(data)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")


	// Check if this was originally a batch
	var req *Request
	var batch Batch
	isBatch := msgSet.IsBatch
	if !msgSet.IsBatch {
		req, err = msgSet.ToRequest()
	} else {
		batch, err = msgSet.ToBatch()
	}
	require.NoError(t, err, "ToRequest/ToBatch should not error")
	assert.False(t, isBatch, "isBatch should be false")
	require.NotNil(t, req, "req should not be nil")
	assert.Nil(t, batch, "batch should be nil")
	assert.Equal(t, "test", req.Method)
}

func TestDecodeRequest_Batch(t *testing.T) {
	data := []byte(`[{"jsonrpc":"3.0","method":"test1","id":1},{"jsonrpc":"3.0","method":"test2","id":2}]`)

	codec := GetCodec("application/json")
	msgSet, err := codec.UnmarshalMessages(data)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")


	// Check if this was originally a batch
	var req *Request
	var batch Batch
	isBatch := msgSet.IsBatch
	if !msgSet.IsBatch {
		req, err = msgSet.ToRequest()
	} else {
		batch, err = msgSet.ToBatch()
	}
	require.NoError(t, err, "ToRequest/ToBatch should not error")
	assert.True(t, isBatch, "isBatch should be true")
	assert.Nil(t, req, "req should be nil")
	require.NotNil(t, batch, "batch should not be nil")
	assert.Len(t, batch, 2)
}

func TestDecodeRequest_BatchWithWhitespace(t *testing.T) {
	data := []byte(`
	[{"jsonrpc":"3.0","method":"test","id":1}]`)

	codec := GetCodec("application/json")
	msgSet, err := codec.UnmarshalMessages(data)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")


	// Check if this was originally a batch
	var req *Request
	var batch Batch
	isBatch := msgSet.IsBatch
	if !msgSet.IsBatch {
		req, err = msgSet.ToRequest()
	} else {
		batch, err = msgSet.ToBatch()
	}
	require.NoError(t, err, "ToRequest/ToBatch should not error")
	assert.True(t, isBatch, "isBatch should be true")
	assert.Nil(t, req, "req should be nil")
	require.NotNil(t, batch, "batch should not be nil")
}

func TestDecodeRequest_Invalid(t *testing.T) {
	data := []byte(`{invalid json}`)

	codec := GetCodec("application/json")
	_, err := codec.UnmarshalMessages(data)
	assert.Error(t, err, "should have error for invalid JSON")
}

func TestIntrospectionIntegration(t *testing.T) {
	// Create a session and handler with a MethodMap root object
	session := NewSession()
	root := NewMethodMap()
	root.Type = "RootObject"

	root.Register("hello", func(params Params) (any, error) {
		return "world", nil
	})

	mimeTypes := []string{MimeTypeJSON}
	handler := NewHandler(session, root, mimeTypes)

	// Test calling $methods on root object (no ref)
	t.Run("root object $methods", func(t *testing.T) {
		req := &Request{
			JSONRPC: Version30,
			Method:  "$methods",
			ID:      float64(1),
		}

		resp := handler.HandleRequest(req)
		require.NotNil(t, resp)
		require.Nil(t, resp.Error)

		// Decode result
		params := NewParamsWithFormat(resp.Result, MimeTypeJSON)
		var methods []string
		err := params.Decode(&methods)
		require.NoError(t, err)

		assert.Contains(t, methods, "hello")
		assert.Contains(t, methods, "$methods")
		assert.Contains(t, methods, "$type")
	})

	// Test calling $type on root object
	t.Run("root object $type", func(t *testing.T) {
		req := &Request{
			JSONRPC: Version30,
			Method:  "$type",
			ID:      float64(2),
		}

		resp := handler.HandleRequest(req)
		require.NotNil(t, resp)
		require.Nil(t, resp.Error)

		// Decode result
		params := NewParamsWithFormat(resp.Result, MimeTypeJSON)
		var typeStr string
		err := params.Decode(&typeStr)
		require.NoError(t, err)

		assert.Equal(t, "RootObject", typeStr)
	})

	// Test calling capabilities on $rpc
	t.Run("$rpc capabilities", func(t *testing.T) {
		req := &Request{
			JSONRPC: Version30,
			Ref:     "$rpc",
			Method:  "capabilities",
			ID:      float64(3),
		}

		resp := handler.HandleRequest(req)
		require.NotNil(t, resp)
		require.Nil(t, resp.Error)

		// Decode result
		params := NewParamsWithFormat(resp.Result, MimeTypeJSON)
		var capabilities []string
		err := params.Decode(&capabilities)
		require.NoError(t, err)

		assert.Contains(t, capabilities, "references")
		assert.Contains(t, capabilities, "introspection")
	})
}

// Counter is a simple object for testing batch-local references
type Counter struct {
	Value int
}

func (c *Counter) CallMethod(method string, params Params) (any, error) {
	switch method {
	case "increment":
		c.Value++
		return c.Value, nil
	case "getValue":
		return c.Value, nil
	default:
		return nil, NewMethodNotFoundError(method)
	}
}

// Database is a mock database object for testing
type Database struct {
	Name string
}

func (db *Database) CallMethod(method string, params Params) (any, error) {
	switch method {
	case "query":
		var sql string
		if err := params.Decode(&sql); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return map[string]any{
			"rows": []map[string]any{
				{"id": 1, "name": "Alice"},
				{"id": 2, "name": "Bob"},
			},
		}, nil
	case "getName":
		return db.Name, nil
	default:
		return nil, NewMethodNotFoundError(method)
	}
}

// Workspace is a mock workspace object
type Workspace struct {
	Name string
}

func (w *Workspace) CallMethod(method string, params Params) (any, error) {
	if method == "createDocument" {
		var p struct {
			Title string `json:"title"`
		}
		params.Decode(&p)
		doc := &Document{Title: p.Title}
		return doc, nil
	}
	return nil, NewMethodNotFoundError(method)
}

// Document is a mock document object
type Document struct {
	Title   string
	Content string
}

func (d *Document) CallMethod(method string, params Params) (any, error) {
	if method == "write" {
		var content string
		params.Decode(&content)
		d.Content = content
		return "written", nil
	}
	return nil, NewMethodNotFoundError(method)
}

// Helper function to marshal data for tests
func mustMarshal(v any) RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return RawMessage(data)
}

// TestBatchLocalRef_Basic tests basic batch-local reference usage
func TestBatchLocalRef_Basic(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()

	// Register method that creates a counter
	root.Register("createCounter", func(params Params) (any, error) {
		return &Counter{Value: 0}, nil
	})

	handler := NewHandler(session, root, nil)

	// Create batch: [createCounter, increment counter using \0]
	batch := Batch{
		{
			JSONRPC: Version30,
			Method:  "createCounter",
			ID:      0,
		},
		{
			JSONRPC: Version30,
			Ref:     "\\0",
			Method:  "increment",
			ID:      1,
		},
	}

	responses := handler.HandleBatch(batch)
	require.Len(t, responses, 2)

	// First response should contain a reference
	assert.Nil(t, responses[0].Error)
	assert.NotNil(t, responses[0].Result)

	// Second response should have incremented the counter
	assert.Nil(t, responses[1].Error)
	assert.NotNil(t, responses[1].Result)

	var result int
	params := NewParams(responses[1].Result)
	err := params.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 1, result)
}

// TestBatchLocalRef_DatabaseQuery tests the database query pipeline example
func TestBatchLocalRef_DatabaseQuery(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()

	// Register method that opens a database
	root.Register("openDatabase", func(params Params) (any, error) {
		var p struct {
			Name string `json:"name"`
		}
		if err := params.Decode(&p); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return &Database{Name: p.Name}, nil
	})

	handler := NewHandler(session, root, nil)

	// Create batch: [openDatabase, query using \0]
	batch := Batch{
		{
			JSONRPC: Version30,
			Method:  "openDatabase",
			Params:  mustMarshal(map[string]any{"name": "mydb"}),
			ID:      0,
		},
		{
			JSONRPC: Version30,
			Ref:     "\\0",
			Method:  "query",
			Params:  mustMarshal("SELECT * FROM users"),
			ID:      1,
		},
	}

	responses := handler.HandleBatch(batch)
	require.Len(t, responses, 2)

	// First response should contain a reference
	assert.Nil(t, responses[0].Error)

	// Second response should have query results
	assert.Nil(t, responses[1].Error)

	var queryResult map[string]any
	params := NewParams(responses[1].Result)
	err := params.Decode(&queryResult)
	require.NoError(t, err)
	assert.Contains(t, queryResult, "rows")
}

// TestBatchLocalRef_Chaining tests chaining multiple operations
func TestBatchLocalRef_Chaining(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()

	root.Register("createWorkspace", func(params Params) (any, error) {
		var p struct {
			Name string `json:"name"`
		}
		params.Decode(&p)
		ws := &Workspace{Name: p.Name}
		return ws, nil
	})

	handler := NewHandler(session, root, nil)

	// Create batch: [createWorkspace, createDocument on \0, write to \1]
	batch := Batch{
		{
			JSONRPC: Version30,
			Method:  "createWorkspace",
			Params:  mustMarshal(map[string]any{"name": "project-a"}),
			ID:      0,
		},
		{
			JSONRPC: Version30,
			Ref:     "\\0",
			Method:  "createDocument",
			Params:  mustMarshal(map[string]any{"title": "README"}),
			ID:      1,
		},
		{
			JSONRPC: Version30,
			Ref:     "\\1",
			Method:  "write",
			Params:  mustMarshal("# Hello World"),
			ID:      2,
		},
	}

	responses := handler.HandleBatch(batch)
	require.Len(t, responses, 3)

	// All should succeed
	for i, resp := range responses {
		assert.Nil(t, resp.Error, "Request %d should not have error", i)
	}
}

// TestBatchLocalRef_ForwardReference tests error on forward reference
func TestBatchLocalRef_ForwardReference(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	handler := NewHandler(session, root, nil)

	// Create batch with forward reference
	batch := Batch{
		{
			JSONRPC: Version30,
			Ref:     "\\1", // Forward reference!
			Method:  "someMethod",
			ID:      0,
		},
		{
			JSONRPC: Version30,
			Method:  "createCounter",
			ID:      1,
		},
	}

	responses := handler.HandleBatch(batch)
	require.Len(t, responses, 2)

	// First response should have error
	assert.NotNil(t, responses[0].Error)
	assert.Equal(t, CodeInvalidReference, responses[0].Error.Code)
	assert.Contains(t, responses[0].Error.Message, "Invalid reference")
}

// TestBatchLocalRef_FailedReference tests error when referenced request fails
func TestBatchLocalRef_FailedReference(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()

	// Register method that fails
	root.Register("failingMethod", func(params Params) (any, error) {
		return nil, &Error{
			Code:    -32000,
			Message: "Method failed",
		}
	})

	handler := NewHandler(session, root, nil)

	// Create batch where first request fails
	batch := Batch{
		{
			JSONRPC: Version30,
			Method:  "failingMethod",
			ID:      0,
		},
		{
			JSONRPC: Version30,
			Ref:     "\\0",
			Method:  "someMethod",
			ID:      1,
		},
	}

	responses := handler.HandleBatch(batch)
	require.Len(t, responses, 2)

	// First response should have error
	assert.NotNil(t, responses[0].Error)
	assert.Equal(t, -32000, responses[0].Error.Code)

	// Second response should also have error (referenced request failed)
	assert.NotNil(t, responses[1].Error)
	assert.Equal(t, CodeInvalidReference, responses[1].Error.Code)
	assert.Contains(t, responses[1].Error.Data, "failed")
}

// TestBatchLocalRef_NonReferenceResult tests error when result is not a reference
func TestBatchLocalRef_NonReferenceResult(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()

	// Register method that returns a plain value (not a reference)
	root.Register("add", func(params Params) (any, error) {
		var nums []int
		params.Decode(&nums)
		return nums[0] + nums[1], nil
	})

	handler := NewHandler(session, root, nil)

	// Create batch where first request returns non-reference
	batch := Batch{
		{
			JSONRPC: Version30,
			Method:  "add",
			Params:  mustMarshal([]int{2, 3}),
			ID:      0,
		},
		{
			JSONRPC: Version30,
			Ref:     "\\0",
			Method:  "someMethod",
			ID:      1,
		},
	}

	responses := handler.HandleBatch(batch)
	require.Len(t, responses, 2)

	// First response should succeed with result 5
	assert.Nil(t, responses[0].Error)

	// Second response should have type error
	assert.NotNil(t, responses[1].Error)
	assert.Equal(t, CodeReferenceTypeError, responses[1].Error.Code)
}

// TestBatchLocalRef_OutOfBounds tests error for out of bounds index
func TestBatchLocalRef_OutOfBounds(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	handler := NewHandler(session, root, nil)

	// Create batch with out of bounds reference
	batch := Batch{
		{
			JSONRPC: Version30,
			Method:  "someMethod",
			ID:      0,
		},
		{
			JSONRPC: Version30,
			Ref:     "\\5", // Out of bounds!
			Method:  "anotherMethod",
			ID:      1,
		},
	}

	responses := handler.HandleBatch(batch)
	require.Len(t, responses, 2)

	// Second response should have error
	assert.NotNil(t, responses[1].Error)
	assert.Equal(t, CodeInvalidReference, responses[1].Error.Code)
}

// TestBatchLocalRef_MixedWithRegularRefs tests mixing batch-local and regular refs
func TestBatchLocalRef_MixedWithRegularRefs(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()

	// Register a counter in the session
	sessionCounter := &Counter{Value: 10}
	handler := NewHandler(session, root, nil)
	session.AddLocalRef("session-counter", sessionCounter)

	// Register method that creates a new counter
	root.Register("createCounter", func(params Params) (any, error) {
		return &Counter{Value: 0}, nil
	})

	// Create batch: [createCounter, increment session counter, increment batch counter]
	batch := Batch{
		{
			JSONRPC: Version30,
			Method:  "createCounter",
			ID:      0,
		},
		{
			JSONRPC: Version30,
			Ref:     "session-counter", // Regular ref
			Method:  "increment",
			ID:      1,
		},
		{
			JSONRPC: Version30,
			Ref:     "\\0", // Batch-local ref
			Method:  "increment",
			ID:      2,
		},
	}

	responses := handler.HandleBatch(batch)
	require.Len(t, responses, 3)

	// All should succeed
	for i, resp := range responses {
		assert.Nil(t, resp.Error, "Request %d should not have error", i)
	}

	// Check session counter was incremented
	var sessionResult int
	params := NewParams(responses[1].Result)
	params.Decode(&sessionResult)
	assert.Equal(t, 11, sessionResult)

	// Check batch counter was incremented
	var batchResult int
	params = NewParams(responses[2].Result)
	params.Decode(&batchResult)
	assert.Equal(t, 1, batchResult)
}

// BidirectionalPair connects two Handler instances for bidirectional testing
// Simulates a scenario where both client and server can make calls to each other
type BidirectionalPair struct {
	clientSession *Session
	serverSession *Session
	clientHandler *Handler
	serverHandler *Handler
	nextID        int
}

// NewBidirectionalPair creates a pair of handlers that can call each other
func NewBidirectionalPair(clientRoot, serverRoot Object) *BidirectionalPair {
	clientSession := NewSession()
	serverSession := NewSession()

	clientHandler := NewHandler(clientSession, clientRoot, nil)
	serverHandler := NewHandler(serverSession, serverRoot, nil)

	return &BidirectionalPair{
		clientSession: clientSession,
		serverSession: serverSession,
		clientHandler: clientHandler,
		serverHandler: serverHandler,
		nextID:        1,
	}
}

// ClientCall makes a call from client to server
func (p *BidirectionalPair) ClientCall(method string, params any, result any) error {
	id := p.nextID
	p.nextID++

	req, err := NewRequest(method, params, id)
	if err != nil {
		return err
	}

	// Process request on server side
	resp := p.serverHandler.HandleRequest(req)

	// Track any local references from server as remote refs on client
	if resp.Result != nil {
		p.trackReferences(resp.Result, p.clientSession, p.serverSession)
	}

	if resp.Error != nil {
		return resp.Error
	}

	if result != nil {
		// Decode result
		params := NewParams(resp.Result)
		return params.Decode(result)
	}

	return nil
}

// ServerCallClient makes a call from server to client (callback)
// This is used when server invokes a method on a client-provided reference
func (p *BidirectionalPair) ServerCallClient(ref, method string, params any, result any) error {
	id := p.nextID
	p.nextID++

	req, err := NewRequestWithRef(ref, method, params, id)
	if err != nil {
		return err
	}

	// Process request on client side
	resp := p.clientHandler.HandleRequest(req)

	// Track any local references from client as remote refs on server
	if resp.Result != nil {
		p.trackReferences(resp.Result, p.serverSession, p.clientSession)
	}

	if resp.Error != nil {
		return resp.Error
	}

	if result != nil {
		// Decode result
		params := NewParams(resp.Result)
		return params.Decode(result)
	}

	return nil
}

// trackReferences scans the result for LocalReferences and tracks them as RemoteRefs
func (p *BidirectionalPair) trackReferences(data RawMessage, receiverSession, senderSession *Session) {
	// Parse as generic structure
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return
	}

	p.trackReferencesInValue(generic, receiverSession, senderSession)
}

// trackReferencesInValue recursively finds LocalReferences and tracks them
func (p *BidirectionalPair) trackReferencesInValue(value any, receiverSession, senderSession *Session) {
	switch v := value.(type) {
	case map[string]any:
		// Check if this is a LocalReference
		if refStr, ok := v["$ref"].(string); ok && len(v) == 1 {
			// This is a LocalReference - track it as RemoteRef in receiver session
			if obj := senderSession.GetLocalRef(refStr); obj != nil {
				receiverSession.AddRemoteRef(refStr, nil)
			}
		} else {
			// Recurse into map values
			for _, val := range v {
				p.trackReferencesInValue(val, receiverSession, senderSession)
			}
		}
	case []any:
		// Recurse into array elements
		for _, val := range v {
			p.trackReferencesInValue(val, receiverSession, senderSession)
		}
	}
}

// TestBidirectionalCallback demonstrates server calling back to client
func TestBidirectionalCallback(t *testing.T) {
	// Client-side callback object
	callbackCalled := false
	var callbackMessage string

	clientRoot := NewMethodMap()
	clientRoot.Register("onNotify", func(params Params) (any, error) {
		callbackCalled = true
		err := params.Decode(&callbackMessage)
		if err != nil {
			return nil, err
		}
		return "callback received: " + callbackMessage, nil
	})

	// Server-side method that accepts a callback
	serverRoot := NewMethodMap()
	var capturedCallbackRef string

	serverRoot.Register("subscribe", func(params Params) (any, error) {
		var p struct {
			Topic    string         `json:"topic"`
			Callback LocalReference `json:"callback"`
		}
		if err := params.Decode(&p); err != nil {
			return nil, err
		}

		capturedCallbackRef = p.Callback.Ref
		return map[string]any{
			"status": "subscribed",
			"topic":  p.Topic,
		}, nil
	})

	// Create bidirectional pair
	pair := NewBidirectionalPair(clientRoot, serverRoot)

	// Step 1: Client registers a local callback object
	callbackRef := "client-callback-1"
	callbackObj := clientRoot // Use the method map itself as the callback object
	pair.clientSession.AddLocalRef(callbackRef, callbackObj)

	// Step 2: Client calls server's subscribe method, passing callback reference
	var subscribeResult struct {
		Status string `json:"status"`
		Topic  string `json:"topic"`
	}
	err := pair.ClientCall("subscribe", map[string]any{
		"topic":    "updates",
		"callback": map[string]string{"$ref": callbackRef},
	}, &subscribeResult)

	require.NoError(t, err)
	assert.Equal(t, "subscribed", subscribeResult.Status)
	assert.Equal(t, "updates", subscribeResult.Topic)
	assert.Equal(t, callbackRef, capturedCallbackRef, "Server should have captured callback ref")

	// Step 3: Server tracks client's callback as a remote reference
	pair.serverSession.AddRemoteRef(capturedCallbackRef, nil)

	// Step 4: Server invokes callback on client
	var callbackResult string
	err = pair.ServerCallClient(capturedCallbackRef, "onNotify", "Hello from server!", &callbackResult)

	require.NoError(t, err)
	assert.True(t, callbackCalled, "Client callback should have been invoked")
	assert.Equal(t, "Hello from server!", callbackMessage)
	assert.Equal(t, "callback received: Hello from server!", callbackResult)
}

// TestBidirectionalCallbackWithObjectReturn tests bidirectional callbacks with object returns
func TestBidirectionalCallbackWithObjectReturn(t *testing.T) {
	// Client callback that returns an object
	type ClientCounter struct {
		Value int
	}

	clientCounter := &ClientCounter{Value: 0}

	clientRoot := NewMethodMap()
	clientRoot.Register("getCounter", func(params Params) (any, error) {
		return clientCounter, nil
	})

	// Server method that calls client callback and uses returned object
	serverRoot := NewMethodMap()
	var capturedCallbackRef string

	serverRoot.Register("registerCallback", func(params Params) (any, error) {
		var callback LocalReference
		if err := params.Decode(&callback); err != nil {
			return nil, err
		}
		capturedCallbackRef = callback.Ref
		return "registered", nil
	})

	// Create bidirectional pair
	pair := NewBidirectionalPair(clientRoot, serverRoot)

	// Client registers callback
	callbackRef := "client-callback-2"
	pair.clientSession.AddLocalRef(callbackRef, clientRoot)

	// Client calls server
	var registerResult string
	err := pair.ClientCall("registerCallback", LocalReference{Ref: callbackRef}, &registerResult)
	require.NoError(t, err)
	assert.Equal(t, "registered", registerResult)

	// Server tracks callback
	pair.serverSession.AddRemoteRef(capturedCallbackRef, nil)

	// Server calls client callback to get counter object
	var counterResult map[string]any
	err = pair.ServerCallClient(capturedCallbackRef, "getCounter", nil, &counterResult)
	require.NoError(t, err)

	// Counter should have been returned
	assert.Contains(t, counterResult, "Value")
	assert.Equal(t, float64(0), counterResult["Value"]) // JSON numbers are float64
}

// TestBidirectionalCallbackMultiple tests multiple callbacks
func TestBidirectionalCallbackMultiple(t *testing.T) {
	// Client with multiple callback methods
	callCounts := make(map[string]int)

	clientRoot := NewMethodMap()
	clientRoot.Register("onEvent", func(params Params) (any, error) {
		var event string
		params.Decode(&event)
		callCounts["onEvent"]++
		return "event: " + event, nil
	})
	clientRoot.Register("onError", func(params Params) (any, error) {
		var errMsg string
		params.Decode(&errMsg)
		callCounts["onError"]++
		return "error: " + errMsg, nil
	})

	serverRoot := NewMethodMap()
	var callbackRef string

	serverRoot.Register("registerListener", func(params Params) (any, error) {
		var ref LocalReference
		params.Decode(&ref)
		callbackRef = ref.Ref
		return "ok", nil
	})

	pair := NewBidirectionalPair(clientRoot, serverRoot)

	// Register callback
	ref := "listener-1"
	pair.clientSession.AddLocalRef(ref, clientRoot)

	var result string
	err := pair.ClientCall("registerListener", LocalReference{Ref: ref}, &result)
	require.NoError(t, err)

	pair.serverSession.AddRemoteRef(callbackRef, nil)

	// Server calls multiple methods on client callback
	var eventResult, errorResult string

	err = pair.ServerCallClient(callbackRef, "onEvent", "test-event", &eventResult)
	require.NoError(t, err)
	assert.Equal(t, "event: test-event", eventResult)

	err = pair.ServerCallClient(callbackRef, "onError", "test-error", &errorResult)
	require.NoError(t, err)
	assert.Equal(t, "error: test-error", errorResult)

	// Verify both callbacks were invoked
	assert.Equal(t, 1, callCounts["onEvent"])
	assert.Equal(t, 1, callCounts["onError"])
}

// TestBidirectionalCallbackError tests error handling in callbacks
func TestBidirectionalCallbackError(t *testing.T) {
	clientRoot := NewMethodMap()
	clientRoot.Register("failingCallback", func(params Params) (any, error) {
		return nil, &Error{
			Code:    CodeInternalError,
			Message: "callback failed",
		}
	})

	serverRoot := NewMethodMap()
	var callbackRef string

	serverRoot.Register("register", func(params Params) (any, error) {
		var ref LocalReference
		params.Decode(&ref)
		callbackRef = ref.Ref
		return "ok", nil
	})

	pair := NewBidirectionalPair(clientRoot, serverRoot)

	ref := "callback-err"
	pair.clientSession.AddLocalRef(ref, clientRoot)

	var result string
	err := pair.ClientCall("register", LocalReference{Ref: ref}, &result)
	require.NoError(t, err)

	pair.serverSession.AddRemoteRef(callbackRef, nil)

	// Server calls client callback that returns an error
	err = pair.ServerCallClient(callbackRef, "failingCallback", nil, &result)
	require.Error(t, err)

	// Verify it's an RPC error
	rpcErr, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, CodeInternalError, rpcErr.Code)
	assert.Contains(t, rpcErr.Message, "callback failed")
}
