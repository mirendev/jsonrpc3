package jsonrpc3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMethodMapIntrospection(t *testing.T) {
	// Create a MethodMap with some methods
	m := NewMethodMap()
	m.Type = "TestObject"

	m.Register("add", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return 42, nil
	},
		WithDescription("Adds two numbers"),
		WithParams(map[string]string{"a": "number", "b": "number"}),
		WithCategory("math"))

	m.Register("subtract", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return 10, nil
	})

	t.Run("$methods returns array of method info", func(t *testing.T) {
		result, err := m.CallMethod(context.Background(), "$methods", nil, nil)
		require.NoError(t, err)

		methods, ok := result.([]map[string]any)
		require.True(t, ok, "$methods should return []map[string]any")

		// Should have at least 4 methods: add, subtract, $methods, $type
		assert.GreaterOrEqual(t, len(methods), 4)

		// Find the add method
		var addMethod map[string]any
		for _, method := range methods {
			if method["name"] == "add" {
				addMethod = method
				break
			}
		}
		require.NotNil(t, addMethod, "add method should be in result")
		assert.Equal(t, "Adds two numbers", addMethod["description"])
		assert.Equal(t, "math", addMethod["category"])

		params, ok := addMethod["params"].(map[string]string)
		require.True(t, ok, "params should be a map")
		assert.Equal(t, "number", params["a"])
		assert.Equal(t, "number", params["b"])

		// Find the subtract method (no metadata)
		var subtractMethod map[string]any
		for _, method := range methods {
			if method["name"] == "subtract" {
				subtractMethod = method
				break
			}
		}
		require.NotNil(t, subtractMethod, "subtract method should be in result")
		assert.Equal(t, "subtract", subtractMethod["name"])
		assert.NotContains(t, subtractMethod, "description")
		assert.NotContains(t, subtractMethod, "category")
		assert.NotContains(t, subtractMethod, "params")

		// Verify introspection methods are included
		var hasMethodsMethod, hasTypeMethod bool
		for _, method := range methods {
			if method["name"] == "$methods" {
				hasMethodsMethod = true
			}
			if method["name"] == "$type" {
				hasTypeMethod = true
			}
		}
		assert.True(t, hasMethodsMethod, "$methods should include itself")
		assert.True(t, hasTypeMethod, "$methods should include $type")
	})

	t.Run("$type", func(t *testing.T) {
		result, err := m.CallMethod(context.Background(), "$type", nil, nil)
		require.NoError(t, err)

		typeStr, ok := result.(string)
		require.True(t, ok, "$type should return string")
		assert.Equal(t, "TestObject", typeStr)
	})

	t.Run("$type default", func(t *testing.T) {
		// Test default type when Type field is not set
		m2 := NewMethodMap()
		result, err := m2.CallMethod(context.Background(), "$type", nil, nil)
		require.NoError(t, err)

		typeStr, ok := result.(string)
		require.True(t, ok, "$type should return string")
		assert.Equal(t, "MethodMap", typeStr)
	})
}

func TestMethodMapWithPositionalParams(t *testing.T) {
	m := NewMethodMap()

	// Register method with positional params
	m.Register("multiply", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return 100, nil
	},
		WithDescription("Multiplies all provided numbers"),
		WithPositionalParams([]string{"number", "number", "...number"}),
		WithCategory("math"))

	result, err := m.CallMethod(context.Background(), "$methods", nil, nil)
	require.NoError(t, err)

	methods, ok := result.([]map[string]any)
	require.True(t, ok, "$methods should return []map[string]any")

	// Find the multiply method
	var multiplyMethod map[string]any
	for _, method := range methods {
		if method["name"] == "multiply" {
			multiplyMethod = method
			break
		}
	}
	require.NotNil(t, multiplyMethod, "multiply method should be in result")
	assert.Equal(t, "Multiplies all provided numbers", multiplyMethod["description"])
	assert.Equal(t, "math", multiplyMethod["category"])

	paramList, ok := multiplyMethod["params"].([]string)
	require.True(t, ok, "params should be an array")
	assert.Equal(t, []string{"number", "number", "...number"}, paramList)
}
