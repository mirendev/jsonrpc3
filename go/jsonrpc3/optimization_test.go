package jsonrpc3

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSessionOptimization_StatelessRequest verifies that stateless requests
// do NOT get a session ID and do NOT store a session
func TestSessionOptimization_StatelessRequest(t *testing.T) {
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
	defer httpHandler.Close()

	// Make a stateless request (returns int, not an object)
	req, _ := NewRequest("add", []int{1, 2, 3}, 1)
	reqData, _ := GetCodec("application/json").Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	// Verify NO session ID header in response
	sessionID := recorder.Header().Get("RPC-Session-Id")
	assert.Empty(t, sessionID, "Stateless request should NOT get session ID")

	// Verify session was NOT stored
	httpHandler.sessionMutex.RLock()
	sessionCount := len(httpHandler.sessions)
	httpHandler.sessionMutex.RUnlock()
	assert.Equal(t, 0, sessionCount, "Stateless session should NOT be stored")
}

// TestSessionOptimization_StatefulRequest verifies that requests returning
// object references DO get a session ID and DO store the session
func TestSessionOptimization_StatefulRequest(t *testing.T) {
	root := NewMethodMap()
	root.Register("createCounter", func(params Params) (any, error) {
		return &testCounter{value: 0}, nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	// Make a stateful request (returns object reference)
	req, _ := NewRequest("createCounter", nil, 1)
	reqData, _ := GetCodec("application/json").Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	// Verify session ID header IS present
	sessionID := recorder.Header().Get("RPC-Session-Id")
	assert.NotEmpty(t, sessionID, "Stateful request SHOULD get session ID")

	// Verify session WAS stored
	httpHandler.sessionMutex.RLock()
	sessionCount := len(httpHandler.sessions)
	entry, exists := httpHandler.sessions[sessionID]
	httpHandler.sessionMutex.RUnlock()

	assert.Equal(t, 1, sessionCount, "Stateful session SHOULD be stored")
	assert.True(t, exists, "Session should exist in map")
	assert.Len(t, entry.session.ListLocalRefs(), 1, "Session should have 1 ref")
}

// TestSessionOptimization_ExistingSession verifies that existing sessions
// continue to return their session ID even if no new refs are created
func TestSessionOptimization_ExistingSession(t *testing.T) {
	root := NewMethodMap()
	root.Register("createCounter", func(params Params) (any, error) {
		return &testCounter{value: 0}, nil
	})
	root.Register("echo", func(params Params) (any, error) {
		var msg string
		params.Decode(&msg)
		return msg, nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	// First request: Create an object (stateful)
	req1, _ := NewRequest("createCounter", nil, 1)
	reqData1, _ := GetCodec("application/json").Marshal(req1)

	httpReq1 := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData1))
	httpReq1.Header.Set("Content-Type", "application/json")
	recorder1 := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder1, httpReq1)

	sessionID1 := recorder1.Header().Get("RPC-Session-Id")
	assert.NotEmpty(t, sessionID1, "First request should get session ID")

	// Second request: Use existing session with stateless operation
	req2, _ := NewRequest("echo", "hello", 2)
	reqData2, _ := GetCodec("application/json").Marshal(req2)

	httpReq2 := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData2))
	httpReq2.Header.Set("Content-Type", "application/json")
	httpReq2.Header.Set("RPC-Session-Id", sessionID1) // Send existing session ID
	recorder2 := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder2, httpReq2)

	sessionID2 := recorder2.Header().Get("RPC-Session-Id")
	assert.Equal(t, sessionID1, sessionID2, "Existing session should return same ID")
}

// TestSessionDeletion verifies that DELETE method deletes sessions
func TestSessionDeletion(t *testing.T) {
	root := NewMethodMap()
	root.Register("createCounter", func(params Params) (any, error) {
		return &testCounter{value: 0}, nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	// Create a session with an object reference
	req, _ := NewRequest("createCounter", nil, 1)
	reqData, _ := GetCodec("application/json").Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	sessionID := recorder.Header().Get("RPC-Session-Id")
	assert.NotEmpty(t, sessionID, "Should get session ID")

	// Verify session exists
	httpHandler.sessionMutex.RLock()
	_, exists := httpHandler.sessions[sessionID]
	httpHandler.sessionMutex.RUnlock()
	assert.True(t, exists, "Session should exist before deletion")

	// Delete the session using DELETE method
	deleteReq := httptest.NewRequest(http.MethodDelete, "/", nil)
	deleteReq.Header.Set("RPC-Session-Id", sessionID)
	deleteRecorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(deleteRecorder, deleteReq)

	// Verify 204 No Content response
	assert.Equal(t, http.StatusNoContent, deleteRecorder.Code, "DELETE should return 204")

	// Verify session was deleted
	httpHandler.sessionMutex.RLock()
	_, exists = httpHandler.sessions[sessionID]
	httpHandler.sessionMutex.RUnlock()
	assert.False(t, exists, "Session should be deleted")
}

// TestSessionDeletion_NoSessionID verifies DELETE without session ID returns 204
func TestSessionDeletion_NoSessionID(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	// Delete without session ID
	deleteReq := httptest.NewRequest(http.MethodDelete, "/", nil)
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, deleteReq)

	// Should still return 204
	assert.Equal(t, http.StatusNoContent, recorder.Code, "DELETE without session ID should return 204")
}

// TestSessionDeletion_NonExistentSession verifies DELETE with non-existent session ID is safe
func TestSessionDeletion_NonExistentSession(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	// Delete non-existent session
	deleteReq := httptest.NewRequest(http.MethodDelete, "/", nil)
	deleteReq.Header.Set("RPC-Session-Id", "non-existent-session-id")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, deleteReq)

	// Should return 204 (idempotent)
	assert.Equal(t, http.StatusNoContent, recorder.Code, "DELETE non-existent session should return 204")
}

// TestHTTPClient_DeleteSession verifies client can delete sessions
func TestHTTPClient_DeleteSession(t *testing.T) {
	root := NewMethodMap()
	root.Register("createCounter", func(params Params) (any, error) {
		return &testCounter{value: 0}, nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Create a stateful session (with object reference)
	val, err := client.Call("createCounter", nil)
	assert.NoError(t, err, "Should create counter")
	var localRef Reference
	err = val.Decode(&localRef)
	assert.NoError(t, err)

	sessionID := client.SessionID()
	assert.NotEmpty(t, sessionID, "Should have session ID")

	// Verify session exists on server
	httpHandler.sessionMutex.RLock()
	_, exists := httpHandler.sessions[sessionID]
	httpHandler.sessionMutex.RUnlock()
	assert.True(t, exists, "Session should exist on server")

	// Delete session via client
	err = client.DeleteSession()
	assert.NoError(t, err, "DeleteSession should succeed")

	// Verify client session ID is cleared
	assert.Empty(t, client.SessionID(), "Client session ID should be cleared")

	// Verify session was deleted on server
	httpHandler.sessionMutex.RLock()
	_, exists = httpHandler.sessions[sessionID]
	httpHandler.sessionMutex.RUnlock()
	assert.False(t, exists, "Session should be deleted on server")
}

// TestHTTPClient_DeleteSession_NoSession verifies DeleteSession is safe when no session exists
func TestHTTPClient_DeleteSession_NoSession(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Try to delete when no session exists
	err := client.DeleteSession()
	assert.NoError(t, err, "DeleteSession with no session should succeed")
	assert.Empty(t, client.SessionID(), "Session ID should remain empty")
}
