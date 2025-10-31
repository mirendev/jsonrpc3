package jsonrpc3

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// bothLifecycleCounter implements both Dispose and Close
type bothLifecycleCounter struct {
	disposeCount int
	closeCount   int
}

func (c *bothLifecycleCounter) CallMethod(method string, params Params) (any, error) {
	return nil, NewMethodNotFoundError(method)
}

func (c *bothLifecycleCounter) Dispose() error {
	c.disposeCount++
	return nil
}

func (c *bothLifecycleCounter) Close() error {
	c.closeCount++
	return nil
}

// disposableCounter tracks Dispose calls
type disposableCounter struct {
	value         int
	disposeCount  int
	disposeErrors int
}

func (c *disposableCounter) CallMethod(method string, params Params) (any, error) {
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

func (c *disposableCounter) Dispose() error {
	c.disposeCount++
	return nil
}

// closeableCounter tracks Close calls
type closeableCounter struct {
	value       int
	closeCount  int
	closeErrors int
}

func (c *closeableCounter) CallMethod(method string, params Params) (any, error) {
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

func (c *closeableCounter) Close() error {
	c.closeCount++
	return nil
}

// TestLifecycle_Dispose verifies Dispose() is called when ref is removed
func TestLifecycle_Dispose(t *testing.T) {
	session := NewSession()
	counter := &disposableCounter{value: 0}

	// Add ref
	session.AddLocalRef("ref-1", counter)
	assert.Len(t, session.ListLocalRefs(), 1)
	assert.Equal(t, 0, counter.disposeCount, "Dispose not called yet")

	// Remove ref - should call Dispose()
	removed := session.RemoveLocalRef("ref-1")
	assert.True(t, removed)
	assert.Len(t, session.ListLocalRefs(), 0)
	assert.Equal(t, 1, counter.disposeCount, "Dispose should be called once")
}

// TestLifecycle_Close verifies Close() is called when ref is removed
func TestLifecycle_Close(t *testing.T) {
	session := NewSession()
	counter := &closeableCounter{value: 0}

	// Add ref
	session.AddLocalRef("ref-1", counter)
	assert.Equal(t, 0, counter.closeCount, "Close not called yet")

	// Remove ref - should call Close()
	removed := session.RemoveLocalRef("ref-1")
	assert.True(t, removed)
	assert.Equal(t, 1, counter.closeCount, "Close should be called once")
}

// TestLifecycle_DisposeAll verifies DisposeAll calls lifecycle methods
func TestLifecycle_DisposeAll(t *testing.T) {
	session := NewSession()

	counter1 := &disposableCounter{value: 0}
	counter2 := &closeableCounter{value: 0}
	counter3 := &testCounter{value: 0} // No lifecycle methods

	session.AddLocalRef("ref-1", counter1)
	session.AddLocalRef("ref-2", counter2)
	session.AddLocalRef("ref-3", counter3)

	assert.Len(t, session.ListLocalRefs(), 3)

	// Dispose all - should call lifecycle methods
	localCount, _ := session.DisposeAll()
	assert.Equal(t, 3, localCount)
	assert.Len(t, session.ListLocalRefs(), 0)

	assert.Equal(t, 1, counter1.disposeCount, "Dispose should be called")
	assert.Equal(t, 1, counter2.closeCount, "Close should be called")
}

// TestLifecycle_PreferDispose verifies Dispose() is preferred over Close()
func TestLifecycle_PreferDispose(t *testing.T) {
	session := NewSession()

	// Object that implements both Dispose and Close
	obj := &bothLifecycleCounter{}

	session.AddLocalRef("ref-1", obj)

	// Remove ref - should call Dispose() but not Close()
	session.RemoveLocalRef("ref-1")

	assert.Equal(t, 1, obj.disposeCount, "Dispose should be called")
	assert.Equal(t, 0, obj.closeCount, "Close should NOT be called when Dispose exists")
}

// TestLifecycle_HTTPDelete verifies DELETE calls dispose
func TestLifecycle_HTTPDelete(t *testing.T) {
	root := NewMethodMap()
	counter := &disposableCounter{value: 0}

	root.Register("createCounter", func(params Params) (any, error) {
		return counter, nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	// Create session with object reference
	req, _ := NewRequest("createCounter", nil, 1)
	reqData, _ := GetCodec("application/json").Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	sessionID := recorder.Header().Get("RPC-Session-Id")
	assert.NotEmpty(t, sessionID)
	assert.Equal(t, 0, counter.disposeCount, "Dispose not called yet")

	// Delete session via HTTP DELETE
	deleteReq := httptest.NewRequest(http.MethodDelete, "/", nil)
	deleteReq.Header.Set("RPC-Session-Id", sessionID)
	deleteRecorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(deleteRecorder, deleteReq)

	// Verify Dispose was called
	assert.Equal(t, http.StatusNoContent, deleteRecorder.Code)
	assert.Equal(t, 1, counter.disposeCount, "Dispose should be called on DELETE")
}

// TestLifecycle_SessionCleanup verifies cleanup calls dispose
func TestLifecycle_SessionCleanup(t *testing.T) {
	root := NewMethodMap()
	counter := &closeableCounter{value: 0}

	root.Register("createCounter", func(params Params) (any, error) {
		return counter, nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	// Set very short TTL
	httpHandler.SetSessionTTL(1) // 1 nanosecond

	// Create session
	req, _ := NewRequest("createCounter", nil, 1)
	reqData, _ := GetCodec("application/json").Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	sessionID := recorder.Header().Get("RPC-Session-Id")
	assert.NotEmpty(t, sessionID)
	assert.Equal(t, 0, counter.closeCount, "Close not called yet")

	// Trigger cleanup
	httpHandler.cleanupExpiredSessions()

	// Verify Close was called
	assert.Equal(t, 1, counter.closeCount, "Close should be called during cleanup")

	// Verify session was removed
	httpHandler.sessionMutex.RLock()
	_, exists := httpHandler.sessions[sessionID]
	httpHandler.sessionMutex.RUnlock()
	assert.False(t, exists, "Session should be removed")
}

// TestLifecycle_RemoveNonExistent verifies removing non-existent ref is safe
func TestLifecycle_RemoveNonExistent(t *testing.T) {
	session := NewSession()

	// Remove non-existent ref - should not panic
	removed := session.RemoveLocalRef("non-existent")
	assert.False(t, removed)
}

// TestLifecycle_FullIntegration verifies lifecycle management in client-server scenario
func TestLifecycle_FullIntegration(t *testing.T) {
	root := NewMethodMap()
	disposeCounter := &disposableCounter{}
	closeCounter := &closeableCounter{}

	root.Register("createDisposable", func(params Params) (any, error) {
		return disposeCounter, nil
	})

	root.Register("createCloseable", func(params Params) (any, error) {
		return closeCounter, nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Create disposable object
	var ref1 Reference
	err := client.Call("createDisposable", nil, &ref1)
	assert.NoError(t, err)

	// Create closeable object
	var ref2 Reference
	err = client.Call("createCloseable", nil, &ref2)
	assert.NoError(t, err)

	sessionID := client.SessionID()
	assert.NotEmpty(t, sessionID, "Should have session ID")

	// Verify objects work
	var result int
	err = client.CallRef(ref1.Ref, "increment", nil, &result)
	assert.NoError(t, err)
	assert.Equal(t, 1, result)

	// Neither should be disposed yet
	assert.Equal(t, 0, disposeCounter.disposeCount)
	assert.Equal(t, 0, closeCounter.closeCount)

	// Delete session - should dispose both objects
	err = client.DeleteSession()
	assert.NoError(t, err)

	// Verify both were disposed
	assert.Equal(t, 1, disposeCounter.disposeCount, "Disposable.Dispose() should be called")
	assert.Equal(t, 1, closeCounter.closeCount, "Closeable.Close() should be called")

	// Verify client session cleared
	assert.Empty(t, client.SessionID())
}
