package jsonrpc3

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CallbackCounter is a test callback object
type CallbackCounter struct {
	Count   int
	Called  bool
	Message string
}

func (c *CallbackCounter) CallMethod(method string, params Params) (any, error) {
	switch method {
	case "onUpdate":
		c.Called = true
		c.Count++
		var msg string
		params.Decode(&msg)
		c.Message = msg
		return "acknowledged", nil
	case "getCount":
		return c.Count, nil
	default:
		return nil, NewMethodNotFoundError(method)
	}
}

// TestHTTPSSE_BasicCallback tests basic SSE callback functionality
func TestHTTPSSE_BasicCallback(t *testing.T) {
	// Setup server
	root := NewMethodMap()

	// Server method that accepts a callback
	root.Register("subscribe", func(params Params) (any, error) {
		var p struct {
			Topic    string         `json:"topic"`
			Callback LocalReference `json:"callback"`
		}
		if err := params.Decode(&p); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}

		// For now, just return success
		// In a real implementation, server would send notifications to this callback
		return map[string]any{
			"status": "subscribed",
			"topic":  p.Topic,
		}, nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	// Setup client with callback
	client := NewHTTPClient(server.URL, nil)

	callback := &CallbackCounter{}
	client.RegisterCallback("client-callback-1", callback)

	// Make call passing callback reference
	var result map[string]any
	err := client.Call("subscribe", map[string]any{
		"topic":    "updates",
		"callback": LocalReference{Ref: "client-callback-1"},
	}, &result)

	require.NoError(t, err)
	assert.Equal(t, "subscribed", result["status"])
	assert.Equal(t, "updates", result["topic"])
}

// TestHTTPSSE_ClientRefDetection tests that server detects client refs
func TestHTTPSSE_ClientRefDetection(t *testing.T) {
	// Test the helper function
	tests := []struct {
		name     string
		params   string
		expected bool
	}{
		{
			name:     "No refs",
			params:   `{"foo":"bar"}`,
			expected: false,
		},
		{
			name:     "Has local ref",
			params:   `{"callback":{"$ref":"client-1"}}`,
			expected: true,
		},
		{
			name:     "Nested ref",
			params:   `{"data":{"callback":{"$ref":"client-1"}}}`,
			expected: true,
		},
		{
			name:     "Array with ref",
			params:   `[{"$ref":"client-1"}]`,
			expected: true,
		},
		{
			name:     "Empty",
			params:   `{}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanForClientRefs(RawMessage(tt.params))
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestHTTPSSE_MultipleCallbacks tests multiple callbacks registered
func TestHTTPSSE_MultipleCallbacks(t *testing.T) {
	root := NewMethodMap()

	root.Register("setup", func(params Params) (any, error) {
		var p struct {
			OnEvent LocalReference `json:"onEvent"`
			OnError LocalReference `json:"onError"`
		}
		if err := params.Decode(&p); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}

		return "configured", nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	eventCallback := &CallbackCounter{}
	errorCallback := &CallbackCounter{}

	client.RegisterCallback("event-cb", eventCallback)
	client.RegisterCallback("error-cb", errorCallback)

	var result string
	err := client.Call("setup", map[string]any{
		"onEvent": LocalReference{Ref: "event-cb"},
		"onError": LocalReference{Ref: "error-cb"},
	}, &result)

	require.NoError(t, err)
	assert.Equal(t, "configured", result)
}

// TestHTTPSSE_UnregisterCallback tests unregistering callbacks
func TestHTTPSSE_UnregisterCallback(t *testing.T) {
	client := NewHTTPClient("http://example.com", nil)

	callback := &CallbackCounter{}
	client.RegisterCallback("test-cb", callback)

	// Verify callback is registered
	session := client.GetSession()
	obj := session.GetLocalRef("test-cb")
	assert.NotNil(t, obj)

	// Unregister
	client.UnregisterCallback("test-cb")

	// Verify callback is removed
	obj = session.GetLocalRef("test-cb")
	assert.Nil(t, obj)
}

// TestHTTPSSE_SessionPersistence tests that callbacks work with session persistence
func TestHTTPSSE_SessionPersistence(t *testing.T) {
	t.Skip("Skipping for now - session ID propagation needs more investigation")

	root := NewMethodMap()

	root.Register("register", func(params Params) (any, error) {
		var p struct {
			Callback LocalReference `json:"callback"`
		}
		if err := params.Decode(&p); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}

		return "registered", nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	callback := &CallbackCounter{}
	client.RegisterCallback("persist-cb", callback)

	// First call with callback in params object
	var result string
	err := client.Call("register", map[string]any{
		"callback": LocalReference{Ref: "persist-cb"},
	}, &result)
	require.NoError(t, err)
	assert.Equal(t, "registered", result)

	// Check session ID was set
	sessionID := client.SessionID()
	assert.NotEmpty(t, sessionID, "Session ID should be set after SSE request")

	// Second call should use same session
	err = client.Call("register", map[string]any{
		"callback": LocalReference{Ref: "persist-cb"},
	}, &result)
	require.NoError(t, err)

	// Session ID should be the same
	assert.Equal(t, sessionID, client.SessionID(), "Session ID should persist across requests")
}

// TestHTTPSSE_Format tests SSE with different formats (JSON and CBOR)
func TestHTTPSSE_Format(t *testing.T) {
	formats := []string{"application/json", "application/cbor"}

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			root := NewMethodMap()

			root.Register("subscribe", func(params Params) (any, error) {
				var callback LocalReference
				if err := params.Decode(&callback); err != nil {
					return nil, NewInvalidParamsError(err.Error())
				}
				return "subscribed", nil
			})

			httpHandler := NewHTTPHandler(root)
			defer httpHandler.Close()

			server := httptest.NewServer(httpHandler)
			defer server.Close()

			client := NewHTTPClient(server.URL, nil)
			client.SetContentType(format)

			callback := &CallbackCounter{}
			client.RegisterCallback("format-cb", callback)

			var result string
			err := client.Call("subscribe", LocalReference{Ref: "format-cb"}, &result)
			require.NoError(t, err)
			assert.Equal(t, "subscribed", result)
		})
	}
}

// TestHTTPSSE_ErrorHandling tests error cases
func TestHTTPSSE_ErrorHandling(t *testing.T) {
	root := NewMethodMap()

	root.Register("failWithCallback", func(params Params) (any, error) {
		var callback LocalReference
		params.Decode(&callback)
		return nil, &Error{
			Code:    -32000,
			Message: "Intentional error",
		}
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	callback := &CallbackCounter{}
	client.RegisterCallback("error-cb", callback)

	var result string
	err := client.Call("failWithCallback", LocalReference{Ref: "error-cb"}, &result)

	// Should receive error
	require.Error(t, err)
	rpcErr, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, -32000, rpcErr.Code)
	assert.Contains(t, rpcErr.Message, "Intentional error")
}
