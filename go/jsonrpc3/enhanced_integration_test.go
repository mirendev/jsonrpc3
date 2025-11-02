package jsonrpc3

import (
	"context"
	"math/big"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnhancedIntegration_DateTime tests datetime in RPC calls
func TestEnhancedIntegration_DateTime(t *testing.T) {
	root := NewMethodMap()

	// Register method that returns datetime
	root.Register("getCurrentTime", func(ctx context.Context, params Params, caller Caller) (any, error) {
		testTime := time.Date(2025, 12, 25, 10, 0, 0, 0, time.UTC)
		return NewDateTime(testTime), nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Call method that returns DateTime
	val, err := client.Call("getCurrentTime", nil)
	require.NoError(t, err)
	var result DateTime
	err = val.Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, TypeDateTime, result.Type)
	assert.Equal(t, 2025, result.Value.Year())
	assert.Equal(t, time.December, result.Value.Month())
	assert.Equal(t, 25, result.Value.Day())
}

// TestEnhancedIntegration_Bytes tests binary data in RPC calls
func TestEnhancedIntegration_Bytes(t *testing.T) {
	root := NewMethodMap()

	// Register method that returns binary data
	root.Register("getBinaryData", func(ctx context.Context, params Params, caller Caller) (any, error) {
		data := []byte{1, 2, 3, 4, 5}
		return NewBytes(data), nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Call method that returns Bytes
	val, err := client.Call("getBinaryData", nil)
	require.NoError(t, err)
	var result Bytes
	err = val.Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, TypeBytes, result.Type)
	assert.Equal(t, []byte{1, 2, 3, 4, 5}, result.Value)
}

// TestEnhancedIntegration_BigInt tests big integers in RPC calls
func TestEnhancedIntegration_BigInt(t *testing.T) {
	root := NewMethodMap()

	// Register method that returns a large number
	root.Register("getLargeNumber", func(ctx context.Context, params Params, caller Caller) (any, error) {
		// Return a moderately large number
		n, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
		return NewBigInt(n), nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)
	client.SetContentType("application/json") // Force JSON for debugging

	// Call to get large number - decode as raw map first
	val, err := client.Call("getLargeNumber", nil)
	require.NoError(t, err)
	var rawResult map[string]any
	err = val.Decode(&rawResult)
	require.NoError(t, err)

	// Verify structure
	assert.Equal(t, TypeBigInt, rawResult["$type"])
	assert.Equal(t, "123456789012345678901234567890", rawResult["value"])

	// Now try with BigInt type
	val2, err := client.Call("getLargeNumber", nil)
	require.NoError(t, err)
	var result BigInt
	err = val2.Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, TypeBigInt, result.Type)
	require.NotNil(t, result.Value, "BigInt Value should not be nil")
	assert.Equal(t, "123456789012345678901234567890", result.Value.String())
}

// TestEnhancedIntegration_RegExp tests regular expressions in RPC calls
func TestEnhancedIntegration_RegExp(t *testing.T) {
	root := NewMethodMap()

	// Register method that returns a regexp pattern
	root.Register("getEmailPattern", func(ctx context.Context, params Params, caller Caller) (any, error) {
		// Email validation pattern
		pattern := `^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`
		re := regexp.MustCompile(pattern)
		return NewRegExp(re), nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Call method that returns RegExp
	val, err := client.Call("getEmailPattern", nil)
	require.NoError(t, err)
	var result RegExp
	err = val.Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, TypeRegExp, result.Type)
	assert.NotEmpty(t, result.Pattern)

	// Test the returned pattern works
	compiled := result.Regexp()
	assert.NotNil(t, compiled)
	assert.True(t, compiled.MatchString("user@example.com"))
	assert.False(t, compiled.MatchString("invalid-email"))
}

// TestEnhancedIntegration_Mixed tests multiple enhanced types together
func TestEnhancedIntegration_Mixed(t *testing.T) {
	root := NewMethodMap()

	// Register method that returns multiple enhanced types
	root.Register("getSystemInfo", func(ctx context.Context, params Params, caller Caller) (any, error) {
		logPattern, _ := NewRegExpFromPattern(".*ERROR.*", "i")
		return map[string]any{
			"timestamp":  NewDateTime(time.Now()),
			"processId":  NewBigInt(big.NewInt(12345)),
			"logData":    NewBytes([]byte("System started")),
			"logPattern": logPattern,
		}, nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	val, err := client.Call("getSystemInfo", nil)
	require.NoError(t, err)
	var result map[string]any
	err = val.Decode(&result)
	require.NoError(t, err)

	// Verify all enhanced types are present
	assert.NotNil(t, result["timestamp"])
	assert.NotNil(t, result["processId"])
	assert.NotNil(t, result["logData"])
	assert.NotNil(t, result["logPattern"])
}

// TestEnhancedIntegration_Batch tests enhanced types in batch requests
func TestEnhancedIntegration_Batch(t *testing.T) {
	root := NewMethodMap()

	root.Register("getCurrentTime", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return NewDateTime(time.Date(2025, 10, 27, 12, 0, 0, 0, time.UTC)), nil
	})

	root.Register("getRandomBytes", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return NewBytes([]byte{1, 2, 3, 4, 5}), nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Batch request with multiple calls
	requests := []BatchRequest{
		{Method: "getCurrentTime", Params: nil},
		{Method: "getRandomBytes", Params: nil},
	}

	results, err := client.CallBatch(requests)
	require.NoError(t, err)
	require.Equal(t, 2, results.Len())

	// Decode first result (DateTime)
	var dt DateTime
	err = results.DecodeResult(0, &dt)
	require.NoError(t, err)
	assert.Equal(t, TypeDateTime, dt.Type)

	// Decode second result (Bytes)
	var b Bytes
	err = results.DecodeResult(1, &b)
	require.NoError(t, err)
	assert.Equal(t, TypeBytes, b.Type)
}

// TestEnhancedIntegration_CBOR tests enhanced types with CBOR encoding
func TestEnhancedIntegration_CBOR(t *testing.T) {
	root := NewMethodMap()

	root.Register("getBinaryData", func(ctx context.Context, params Params, caller Caller) (any, error) {
		data := []byte{0xFF, 0xFE, 0xFD, 0xFC}
		return NewBytes(data), nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)
	client.SetContentType("application/cbor")

	// Get binary data via CBOR
	val, err := client.Call("getBinaryData", nil)
	require.NoError(t, err)
	var result Bytes
	err = val.Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, TypeBytes, result.Type)
	assert.Equal(t, []byte{0xFF, 0xFE, 0xFD, 0xFC}, result.Value)
}

// TestEnhancedIntegration_CustomType tests handling of unknown enhanced types
func TestEnhancedIntegration_CustomType(t *testing.T) {
	root := NewMethodMap()

	// Register method that returns custom enhanced type
	root.Register("getCustomData", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return map[string]any{
			"$type": "myapp.customtype",
			"value": "custom data",
		}, nil
	})

	httpHandler := NewHTTPHandler(root)
	defer httpHandler.Close()

	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)

	// Unknown enhanced types are returned as-is
	val, err := client.Call("getCustomData", nil)
	require.NoError(t, err)
	var result map[string]any
	err = val.Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "myapp.customtype", result["$type"])
	assert.Equal(t, "custom data", result["value"])
}
