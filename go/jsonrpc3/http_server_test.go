package jsonrpc3

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPHandler_BasicCall(t *testing.T) {
	// Create handler with a simple method
	root := NewMethodMap()
	root.Register("echo", func(params Params) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return msg, nil
	})

	httpHandler := NewHTTPHandler(root)

	// Create request
	req, _ := NewRequest("echo", "hello", 1)
	reqData, _ := GetCodec("application/json").Marshal(req)

	// Make HTTP request
	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	// Check response
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	// Decode response
	var resp Response
	codec := GetCodec("application/json")
	err := codec.Unmarshal(recorder.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Nil(t, resp.Error)

	var result string
	params := NewParams(resp.Result)
	require.NoError(t, params.Decode(&result))
	assert.Equal(t, "hello", result)
}

func TestHTTPHandler_MethodNotAllowed(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)

	// Try GET request
	httpReq := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
}

func TestHTTPHandler_MethodNotFound(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)

	// Create request for non-existent method
	req, _ := NewRequest("nonexistent", nil, 1)
	reqData, _ := GetCodec("application/json").Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	assert.Equal(t, http.StatusOK, recorder.Code)

	// Decode response
	var resp Response
	codec := GetCodec("application/json")
	err := codec.Unmarshal(recorder.Body.Bytes(), &resp)
	require.NoError(t, err)

	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeMethodNotFound, resp.Error.Code)
}

func TestHTTPHandler_Notification(t *testing.T) {
	root := NewMethodMap()

	called := false
	root.Register("log", func(params Params) (any, error) {
		called = true
		return nil, nil
	})

	httpHandler := NewHTTPHandler(root)

	// Create notification (no ID)
	req, _ := NewNotification("log", "test message")
	reqData, _ := GetCodec("application/json").Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	// Should return 204 No Content for notifications
	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.True(t, called, "handler should have been called")
}

func TestHTTPHandler_BatchRequest(t *testing.T) {
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

	// Create batch request
	req1, _ := NewRequest("add", []int{1, 2, 3}, 1)
	req2, _ := NewRequest("add", []int{4, 5, 6}, 2)
	batch := Batch{*req1, *req2}

	reqData, _ := GetCodec("application/json").Marshal(batch)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	assert.Equal(t, http.StatusOK, recorder.Code)

	// Decode batch response
	var batchResp BatchResponse
	codec := GetCodec("application/json")
	err := codec.Unmarshal(recorder.Body.Bytes(), &batchResp)
	require.NoError(t, err)

	assert.Len(t, batchResp, 2)

	var result1, result2 int
	params1 := NewParams(batchResp[0].Result)
	params2 := NewParams(batchResp[1].Result)
	require.NoError(t, params1.Decode(&result1))
	require.NoError(t, params2.Decode(&result2))

	assert.Equal(t, 6, result1)
	assert.Equal(t, 15, result2)
}

func TestHTTPHandler_BatchWithNotifications(t *testing.T) {
	root := NewMethodMap()
	root.Register("test", func(params Params) (any, error) {
		return "ok", nil
	})

	httpHandler := NewHTTPHandler(root)

	// Create batch with notifications only
	notif1, _ := NewNotification("test", nil)
	notif2, _ := NewNotification("test", nil)
	batch := Batch{*notif1, *notif2}

	reqData, _ := GetCodec("application/json").Marshal(batch)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	// All notifications should return 204 No Content
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestHTTPHandler_InvalidJSON(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("invalid json")))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	assert.Equal(t, http.StatusOK, recorder.Code)

	// Should return parse error
	var resp Response
	codec := GetCodec("application/json")
	err := codec.Unmarshal(recorder.Body.Bytes(), &resp)
	require.NoError(t, err)

	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeParseError, resp.Error.Code)
}

func TestHTTPHandler_CBOR(t *testing.T) {
	root := NewMethodMap()
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

	// Create request with CBOR
	req, _ := NewRequestWithFormat("multiply", []int{2, 3, 4}, 1, "application/cbor")
	codec := GetCodec("application/cbor")
	reqData, _ := codec.Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/cbor")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/cbor", recorder.Header().Get("Content-Type"))

	// Decode CBOR response
	var resp Response
	err := codec.Unmarshal(recorder.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Nil(t, resp.Error)

	var result int
	params := NewParamsWithFormat(resp.Result, "application/cbor")
	require.NoError(t, params.Decode(&result))
	assert.Equal(t, 24, result)
}

func TestHTTPHandler_DefaultContentType(t *testing.T) {
	root := NewMethodMap()
	root.Register("test", func(params Params) (any, error) {
		return "ok", nil
	})

	httpHandler := NewHTTPHandler(root)

	// Create request without Content-Type header
	req, _ := NewRequest("test", nil, 1)
	reqData, _ := GetCodec("application/json").Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	// No Content-Type header set
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	assert.Equal(t, http.StatusOK, recorder.Code)

	// Should default to JSON
	var resp Response
	codec := GetCodec("application/json")
	err := codec.Unmarshal(recorder.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)
}

func TestHTTPHandler_ReadBodyError(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)

	// Create a reader that always errors
	errorReader := &errorReader{}
	httpReq := httptest.NewRequest(http.MethodPost, "/", errorReader)
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

// errorReader is a reader that always returns an error
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

func TestHTTPHandler_SessionPersistence(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	// First request - should create a new session
	req1, _ := NewRequest("$rpc.session_id", nil, 1)
	reqData1, _ := GetCodec("application/json").Marshal(req1)

	httpReq1 := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData1))
	httpReq1.Header.Set("Content-Type", "application/json")
	recorder1 := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder1, httpReq1)

	assert.Equal(t, http.StatusOK, recorder1.Code)
	sessionID := recorder1.Header().Get("RPC-Session-Id")
	assert.NotEmpty(t, sessionID)

	// Decode first response to get session ID from result
	var resp1 Response
	codec := GetCodec("application/json")
	err := codec.Unmarshal(recorder1.Body.Bytes(), &resp1)
	require.NoError(t, err)

	var result1 SessionIDResult
	params1 := NewParams(resp1.Result)
	require.NoError(t, params1.Decode(&result1))
	firstSessionID := result1.SessionID

	// Second request with same session ID
	req2, _ := NewRequest("$rpc.session_id", nil, 2)
	reqData2, _ := codec.Marshal(req2)

	httpReq2 := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData2))
	httpReq2.Header.Set("Content-Type", "application/json")
	httpReq2.Header.Set("RPC-Session-Id", sessionID)
	recorder2 := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder2, httpReq2)

	assert.Equal(t, http.StatusOK, recorder2.Code)
	assert.Equal(t, sessionID, recorder2.Header().Get("RPC-Session-Id"))

	// Decode second response
	var resp2 Response
	err = codec.Unmarshal(recorder2.Body.Bytes(), &resp2)
	require.NoError(t, err)

	var result2 SessionIDResult
	params2 := NewParams(resp2.Result)
	require.NoError(t, params2.Decode(&result2))

	// Should be the same session
	assert.Equal(t, firstSessionID, result2.SessionID)
}

func TestHTTPHandler_SessionCreation(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	// Request without session ID should create new session
	req, _ := NewRequest("$rpc.session_id", nil, 1)
	reqData, _ := GetCodec("application/json").Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	assert.Equal(t, http.StatusOK, recorder.Code)
	sessionID := recorder.Header().Get("RPC-Session-Id")
	assert.NotEmpty(t, sessionID, "Response should include RPC-Session-Id header")
}

func TestHTTPHandler_SessionExpiration(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	// Set very short TTL for testing
	httpHandler.SetSessionTTL(100 * time.Millisecond)

	// Create a session
	req, _ := NewRequest("$rpc.session_id", nil, 1)
	reqData, _ := GetCodec("application/json").Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	sessionID := recorder.Header().Get("RPC-Session-Id")
	assert.NotEmpty(t, sessionID)

	// Wait for session to expire
	time.Sleep(150 * time.Millisecond)

	// Manually trigger cleanup
	httpHandler.cleanupExpiredSessions()

	// Verify session was removed
	httpHandler.sessionMutex.RLock()
	_, exists := httpHandler.sessions[sessionID]
	httpHandler.sessionMutex.RUnlock()

	assert.False(t, exists, "Session should be expired and removed")
}

func TestHTTPHandler_SessionCleanupLoop(t *testing.T) {
	root := NewMethodMap()
	httpHandler := NewHTTPHandler(root)

	// Create a session
	req, _ := NewRequest("$rpc.session_id", nil, 1)
	reqData, _ := GetCodec("application/json").Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqData))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, httpReq)

	sessionID := recorder.Header().Get("RPC-Session-Id")
	assert.NotEmpty(t, sessionID)

	// Verify session exists
	httpHandler.sessionMutex.RLock()
	_, exists := httpHandler.sessions[sessionID]
	httpHandler.sessionMutex.RUnlock()
	assert.True(t, exists)

	// Close the handler (stops cleanup loop)
	httpHandler.Close()

	// Cleanup goroutine should stop without error
	// If it doesn't stop, the test will hang
}
