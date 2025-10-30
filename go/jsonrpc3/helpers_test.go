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
}
