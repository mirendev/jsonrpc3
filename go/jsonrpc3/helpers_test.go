package jsonrpc3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	t.Run("$methods includes $method", func(t *testing.T) {
		result, err := m.CallMethod("$methods", nil)
		require.NoError(t, err)

		methods, ok := result.([]string)
		require.True(t, ok, "$methods should return []string")
		assert.Contains(t, methods, "$method")
	})
}

func TestMethodMapMethodIntrospection(t *testing.T) {
	m := NewMethodMap()

	// Register method with full metadata
	m.Register("add", func(params Params) (any, error) {
		return 42, nil
	},
		WithDescription("Adds two numbers and returns the sum"),
		WithParams(map[string]string{
			"a": "number",
			"b": "number",
		}))

	// Register method with positional params
	m.Register("multiply", func(params Params) (any, error) {
		return 100, nil
	},
		WithDescription("Multiplies all provided numbers"),
		WithPositionalParams([]string{"number", "number", "...number"}))

	// Register method without metadata
	m.Register("noMeta", func(params Params) (any, error) {
		return "test", nil
	})

	t.Run("$method with named params", func(t *testing.T) {
		params := marshalParams(t, "add")
		result, err := m.CallMethod("$method", params)
		require.NoError(t, err)

		info, ok := result.(map[string]any)
		require.True(t, ok, "$method should return map")

		assert.Equal(t, "add", info["name"])
		assert.Equal(t, "Adds two numbers and returns the sum", info["description"])

		methodParams, ok := info["params"].(map[string]string)
		require.True(t, ok, "params should be a map")
		assert.Equal(t, "number", methodParams["a"])
		assert.Equal(t, "number", methodParams["b"])
	})

	t.Run("$method with positional params", func(t *testing.T) {
		params := marshalParams(t, "multiply")
		result, err := m.CallMethod("$method", params)
		require.NoError(t, err)

		info, ok := result.(map[string]any)
		require.True(t, ok, "$method should return map")

		assert.Equal(t, "multiply", info["name"])
		assert.Equal(t, "Multiplies all provided numbers", info["description"])

		paramList, ok := info["params"].([]string)
		require.True(t, ok, "params should be an array")
		assert.Equal(t, []string{"number", "number", "...number"}, paramList)
	})

	t.Run("$method without metadata", func(t *testing.T) {
		params := marshalParams(t, "noMeta")
		result, err := m.CallMethod("$method", params)
		require.NoError(t, err)

		info, ok := result.(map[string]any)
		require.True(t, ok, "$method should return map")

		assert.Equal(t, "noMeta", info["name"])
		assert.NotContains(t, info, "description")
		assert.NotContains(t, info, "params")
	})

	t.Run("$method non-existent method", func(t *testing.T) {
		params := marshalParams(t, "doesNotExist")
		result, err := m.CallMethod("$method", params)
		require.NoError(t, err)
		assert.Nil(t, result, "$method should return null for non-existent methods")
	})

	t.Run("$method invalid params", func(t *testing.T) {
		params := marshalParams(t, 123) // Not a string
		_, err := m.CallMethod("$method", params)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expects a string parameter")
	})
}

// marshalParams is a helper function to marshal parameters for testing
func marshalParams(t *testing.T, v any) Params {
	codec := GetCodec(MimeTypeJSON)
	data, err := codec.Marshal(v)
	require.NoError(t, err)
	return NewParamsWithFormat(data, MimeTypeJSON)
}
