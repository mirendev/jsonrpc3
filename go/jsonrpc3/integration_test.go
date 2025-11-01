package jsonrpc3

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_BasicWorkflow tests a complete request/response cycle.
func TestIntegration_BasicWorkflow(t *testing.T) {
	// Create session and handler
	session := NewSession()
	root := NewMethodMap()
	handler := NewHandler(session, root, NewNoOpCaller(), nil)

	// Register a simple method
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

	// Create and encode request
	req, err := NewRequest("add", []int{1, 2, 3, 4, 5}, 1)
	require.NoError(t, err, "NewRequest() should not error")

	reqData, err := json.Marshal(req)
	require.NoError(t, err, "json.Marshal(req) should not error")

	// Decode request
	codec := GetCodec("application/json")
	msgSet, err := codec.UnmarshalMessages(reqData)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")


	decodedReq, err := msgSet.ToRequest()
	require.NoError(t, err, "ToRequest() should not error")
	assert.False(t, msgSet.IsBatch, "expected single request, not batch")

	// Handle request
	resp := handler.HandleRequest(decodedReq)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	require.Nil(t, resp.Error, "should not have error")

	// Decode result
	var result int
	require.NoError(t, json.Unmarshal(resp.Result, &result), "should unmarshal result")
	assert.Equal(t, 15, result)
}

// Counter implements Object for integration testing
type IntegrationCounter struct {
	Value int
}

func (c *IntegrationCounter) CallMethod(method string, params Params, caller Caller) (any, error) {
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

// TestIntegration_References tests creating and using local references.
func TestIntegration_References(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	handler := NewHandler(session, root, NewNoOpCaller(), nil)

	// Register a method that returns an Object
	// It will be automatically registered and returned as a Reference
	root.Register("create_counter", func(params Params, caller Caller) (any, error) {
		return &IntegrationCounter{Value: 0}, nil
	})

	// Create counter - it gets auto-registered
	req1, _ := NewRequest("create_counter", nil, 1)
	resp1 := handler.HandleRequest(req1)
	require.Nil(t, resp1.Error, "create_counter should not error")

	var localRef Reference
	require.NoError(t, json.Unmarshal(resp1.Result, &localRef), "should unmarshal reference")

	// Increment counter using the reference
	req2, _ := NewRequestWithRef(localRef.Ref, "increment", nil, 2)
	resp2 := handler.HandleRequest(req2)
	require.Nil(t, resp2.Error, "increment should not error")

	var count int
	require.NoError(t, json.Unmarshal(resp2.Result, &count), "should unmarshal count")
	assert.Equal(t, 1, count)

	// Increment again
	req3, _ := NewRequestWithRef(localRef.Ref, "increment", nil, 3)
	resp3 := handler.HandleRequest(req3)
	require.Nil(t, resp3.Error, "increment should not error")

	require.NoError(t, json.Unmarshal(resp3.Result, &count), "should unmarshal count")
	assert.Equal(t, 2, count)
}

// TestIntegration_ProtocolMethods tests using protocol methods via $rpc.
func TestIntegration_ProtocolMethods(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	handler := NewHandler(session, root, NewNoOpCaller(), []string{"application/json", "application/cbor"})

	// Add some references
	session.AddLocalRef("obj-1", &dummyObject{name: "test1"})
	session.AddLocalRef("obj-2", &dummyObject{name: "test2"})
	session.AddRemoteRef("remote-1", nil)

	// Get session ID
	req1, _ := NewRequestWithRef("$rpc", "session_id", nil, 1)
	resp1 := handler.HandleRequest(req1)
	require.Nil(t, resp1.Error, "session_id should not error")

	var sessionResult SessionIDResult
	require.NoError(t, json.Unmarshal(resp1.Result, &sessionResult), "should unmarshal session_id")
	assert.Equal(t, session.ID(), sessionResult.SessionID)

	// List refs
	req2, _ := NewRequestWithRef("$rpc", "list_refs", nil, 2)
	resp2 := handler.HandleRequest(req2)
	require.Nil(t, resp2.Error, "list_refs should not error")

	var refs []RefInfoResult
	require.NoError(t, json.Unmarshal(resp2.Result, &refs), "should unmarshal list_refs")
	assert.Len(t, refs, 3)

	// Get ref info
	req3, _ := NewRequestWithRef("$rpc", "ref_info", RefInfoParams{Ref: "obj-1"}, 3)
	resp3 := handler.HandleRequest(req3)
	require.Nil(t, resp3.Error, "ref_info should not error")

	var refInfo RefInfoResult
	require.NoError(t, json.Unmarshal(resp3.Result, &refInfo), "should unmarshal ref_info")
	assert.Equal(t, "obj-1", refInfo.Ref)
	assert.Equal(t, "local", refInfo.Direction)

	// Dispose one ref
	req4, _ := NewRequestWithRef("$rpc", "dispose", DisposeParams{Ref: "obj-1"}, 4)
	resp4 := handler.HandleRequest(req4)
	require.Nil(t, resp4.Error, "dispose should not error")

	// Verify it's gone
	assert.False(t, session.HasLocalRef("obj-1"), "obj-1 should have been disposed")

	// Get mime types
	req5, _ := NewRequestWithRef("$rpc", "mimetypes", nil, 5)
	resp5 := handler.HandleRequest(req5)
	require.Nil(t, resp5.Error, "mimetypes should not error")

	var mimeResult MimeTypesResult
	require.NoError(t, json.Unmarshal(resp5.Result, &mimeResult), "should unmarshal mimetypes")
	assert.Len(t, mimeResult.MimeTypes, 2)

	// Dispose all
	req6, _ := NewRequestWithRef("$rpc", "dispose_all", nil, 6)
	resp6 := handler.HandleRequest(req6)
	require.Nil(t, resp6.Error, "dispose_all should not error")

	var disposeResult DisposeAllResult
	require.NoError(t, json.Unmarshal(resp6.Result, &disposeResult), "should unmarshal dispose_all")
	assert.Equal(t, 1, disposeResult.LocalCount)
	assert.Equal(t, 1, disposeResult.RemoteCount)
}

// TestIntegration_BatchRequests tests batch request handling.
func TestIntegration_BatchRequests(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	handler := NewHandler(session, root, NewNoOpCaller(), nil)

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

	// Create batch request
	req1, _ := NewRequest("multiply", []int{2, 3}, 1)
	req2, _ := NewRequest("multiply", []int{4, 5}, 2)
	req3, _ := NewNotification("multiply", []int{6, 7}) // Notification

	batch := Batch{*req1, *req2, *req3}

	// Encode batch
	batchData, err := json.Marshal(batch)
	require.NoError(t, err, "json.Marshal(batch) should not error")

	// Decode batch
	codec := GetCodec("application/json")
	msgSet, err := codec.UnmarshalMessages(batchData)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")


	decodedBatch, err := msgSet.ToBatch()
	require.NoError(t, err, "ToBatch() should not error")
	assert.True(t, msgSet.IsBatch, "expected batch request")

	// Handle batch
	responses := handler.HandleBatch(decodedBatch)
	assert.Len(t, responses, 2, "notification should not have response")

	// Check first response
	var result1 int
	require.NoError(t, json.Unmarshal(responses[0].Result, &result1), "should unmarshal result 1")
	assert.Equal(t, 6, result1)

	// Check second response
	var result2 int
	require.NoError(t, json.Unmarshal(responses[1].Result, &result2), "should unmarshal result 2")
	assert.Equal(t, 20, result2)
}

// TestIntegration_ErrorPropagation tests error handling across layers.
func TestIntegration_ErrorPropagation(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	handler := NewHandler(session, root, NewNoOpCaller(), nil)

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
				Code:    -32000, // Custom error code
				Message: "Division by zero",
				Data:    "Cannot divide by zero",
			}
		}
		return nums[0] / nums[1], nil
	})

	// Test invalid params
	req1, _ := NewRequest("divide", "not an array", 1)
	resp1 := handler.HandleRequest(req1)
	require.NotNil(t, resp1.Error, "should have error for invalid params")
	assert.Equal(t, CodeInvalidParams, resp1.Error.Code)

	// Test custom error
	req2, _ := NewRequest("divide", []float64{10, 0}, 2)
	resp2 := handler.HandleRequest(req2)
	require.NotNil(t, resp2.Error, "should have error for division by zero")
	assert.Equal(t, -32000, resp2.Error.Code)
	assert.Equal(t, "Division by zero", resp2.Error.Message)

	// Test successful division
	req3, _ := NewRequest("divide", []float64{10, 2}, 3)
	resp3 := handler.HandleRequest(req3)
	require.Nil(t, resp3.Error, "should not have error")

	var result float64
	require.NoError(t, json.Unmarshal(resp3.Result, &result), "should unmarshal result")
	assert.Equal(t, 5.0, result)
}

// IntegrationDatabase implements Object for integration testing
type IntegrationDatabase struct {
	Name   string
	Tables map[string][]string
}

func (db *IntegrationDatabase) CallMethod(method string, params Params, caller Caller) (any, error) {
	switch method {
	case "create_table":
		var tableName string
		if err := params.Decode(&tableName); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		db.Tables[tableName] = []string{}
		return true, nil

	case "insert":
		var data struct {
			Table string `json:"table"`
			Value string `json:"value"`
		}
		if err := params.Decode(&data); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		if _, exists := db.Tables[data.Table]; !exists {
			return nil, &Error{
				Code:    -32001,
				Message: "Table not found",
				Data:    data.Table,
			}
		}
		db.Tables[data.Table] = append(db.Tables[data.Table], data.Value)
		return true, nil

	case "query":
		var tableName string
		if err := params.Decode(&tableName); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		data, exists := db.Tables[tableName]
		if !exists {
			return nil, &Error{
				Code:    -32001,
				Message: "Table not found",
				Data:    tableName,
			}
		}
		return data, nil

	default:
		return nil, NewMethodNotFoundError(method)
	}
}

// TestIntegration_ComplexReferenceScenario tests a complex scenario with multiple references.
func TestIntegration_ComplexReferenceScenario(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	handler := NewHandler(session, root, NewNoOpCaller(), nil)

	// Method to create a database reference
	// Returns an Object that will be auto-registered
	root.Register("create_database", func(params Params, caller Caller) (any, error) {
		var name string
		if err := params.Decode(&name); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return &IntegrationDatabase{
			Name:   name,
			Tables: make(map[string][]string),
		}, nil
	})

	// Create database
	req1, _ := NewRequest("create_database", "mydb", 1)
	resp1 := handler.HandleRequest(req1)
	require.Nil(t, resp1.Error, "create_database should not error")

	var dbRef Reference
	require.NoError(t, json.Unmarshal(resp1.Result, &dbRef), "should unmarshal db reference")

	// Create table
	req2, _ := NewRequestWithRef(dbRef.Ref, "create_table", "users", 2)
	resp2 := handler.HandleRequest(req2)
	require.Nil(t, resp2.Error, "create_table should not error")

	// Insert data
	insertData := map[string]string{"table": "users", "value": "alice"}
	req3, _ := NewRequestWithRef(dbRef.Ref, "insert", insertData, 3)
	resp3 := handler.HandleRequest(req3)
	require.Nil(t, resp3.Error, "insert should not error")

	insertData2 := map[string]string{"table": "users", "value": "bob"}
	req4, _ := NewRequestWithRef(dbRef.Ref, "insert", insertData2, 4)
	resp4 := handler.HandleRequest(req4)
	require.Nil(t, resp4.Error, "insert should not error")

	// Query data
	req5, _ := NewRequestWithRef(dbRef.Ref, "query", "users", 5)
	resp5 := handler.HandleRequest(req5)
	require.Nil(t, resp5.Error, "query should not error")

	var users []string
	require.NoError(t, json.Unmarshal(resp5.Result, &users), "should unmarshal users")
	assert.Len(t, users, 2)
	assert.Equal(t, "alice", users[0])
	assert.Equal(t, "bob", users[1])

	// Test error: query non-existent table
	req6, _ := NewRequestWithRef(dbRef.Ref, "query", "posts", 6)
	resp6 := handler.HandleRequest(req6)
	require.NotNil(t, resp6.Error, "should have error for non-existent table")
	assert.Equal(t, -32001, resp6.Error.Code)

	// Note: We can't dispose using protocol methods since the ref is in handler.objects, not session
	// This is actually correct - objects managed by the handler are separate from session refs
}

// TestIntegration_VersionNegotiation tests version handling.
func TestIntegration_VersionNegotiation(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	handler := NewHandler(session, root, NewNoOpCaller(), nil)

	// Set handler to use 2.0
	handler.SetVersion(Version20)

	root.Register("test", func(params Params, caller Caller) (any, error) {
		return "ok", nil
	})

	// Send 2.0 request
	req, _ := NewRequest("test", nil, 1)
	req.JSONRPC = Version20

	resp := handler.HandleRequest(req)
	require.NotNil(t, resp, "HandleRequest() should not return nil")
	assert.Equal(t, Version20, resp.JSONRPC)

	// Set handler to use 3.0
	handler.SetVersion(Version30)

	// Send 3.0 request
	req2, _ := NewRequest("test", nil, 2)
	resp2 := handler.HandleRequest(req2)
	require.NotNil(t, resp2, "HandleRequest() should not return nil")
	assert.Equal(t, Version30, resp2.JSONRPC)
}
