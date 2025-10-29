package jsonrpc3

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	handler.AddObject("session-counter", sessionCounter)

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

// Helper function to marshal data for tests
func mustMarshal(v any) RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return RawMessage(data)
}
