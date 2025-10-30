package jsonrpc3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapabilities(t *testing.T) {
	session := NewSession()
	mimeTypes := []string{MimeTypeJSON, MimeTypeCBOR, MimeTypeCBORCompact}
	handler := NewProtocolHandler(session, mimeTypes)

	// Call capabilities method
	result, err := handler.CallMethod("capabilities", nil)
	require.NoError(t, err)

	capabilities, ok := result.([]string)
	require.True(t, ok, "capabilities should return []string")

	// Check for expected capabilities
	assert.Contains(t, capabilities, "references")
	assert.Contains(t, capabilities, "batch-local-references")
	assert.Contains(t, capabilities, "bidirectional-calls")
	assert.Contains(t, capabilities, "introspection")
	assert.Contains(t, capabilities, "cbor-encoding")
	assert.Contains(t, capabilities, "cbor-compact-encoding")
}

func TestMethodMapIntrospection(t *testing.T) {
	// Create a MethodMap with some methods
	m := NewMethodMap()
	m.Type = "TestObject"

	m.Register("add", func(params Params) (any, error) {
		return 42, nil
	})

	m.Register("subtract", func(params Params) (any, error) {
		return 10, nil
	})

	t.Run("$methods", func(t *testing.T) {
		result, err := m.CallMethod("$methods", nil)
		require.NoError(t, err)

		methods, ok := result.([]string)
		require.True(t, ok, "$methods should return []string")

		// Should contain user methods and introspection methods
		assert.Contains(t, methods, "add")
		assert.Contains(t, methods, "subtract")
		assert.Contains(t, methods, "$methods")
		assert.Contains(t, methods, "$type")
	})

	t.Run("$type", func(t *testing.T) {
		result, err := m.CallMethod("$type", nil)
		require.NoError(t, err)

		typeStr, ok := result.(string)
		require.True(t, ok, "$type should return string")
		assert.Equal(t, "TestObject", typeStr)
	})

	t.Run("$type default", func(t *testing.T) {
		// Test default type when Type field is not set
		m2 := NewMethodMap()
		result, err := m2.CallMethod("$type", nil)
		require.NoError(t, err)

		typeStr, ok := result.(string)
		require.True(t, ok, "$type should return string")
		assert.Equal(t, "MethodMap", typeStr)
	})
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
