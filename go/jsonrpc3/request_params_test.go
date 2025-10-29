package jsonrpc3

import (
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequest_GetParams_JSON verifies that JSON-decoded requests return JSON Params
func TestRequest_GetParams_JSON(t *testing.T) {
	// Create a request with JSON params
	type testParams struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	reqData := `{"jsonrpc":"3.0","method":"test","params":{"name":"Alice","age":30},"id":1}`
	req, _, isBatch, err := DecodeRequest([]byte(reqData), "application/json")
	require.NoError(t, err, "Failed to decode request")
	assert.False(t, isBatch, "Expected single request, got batch")

	// Get params and decode
	params := req.GetParams()
	var decoded testParams
	require.NoError(t, params.Decode(&decoded), "Failed to decode params")

	// Verify decoded values
	assert.Equal(t, "Alice", decoded.Name)
	assert.Equal(t, 30, decoded.Age)
}

// TestRequest_GetParams_CBOR verifies that CBOR-decoded requests return CBOR Params
func TestRequest_GetParams_CBOR(t *testing.T) {
	// Create a request with CBOR params
	type testParams struct {
		Name string `cbor:"name"`
		Age  int    `cbor:"age"`
	}

	// Create params
	paramsData, err := cbor.Marshal(map[string]any{"name": "Bob", "age": 25})
	require.NoError(t, err, "Failed to marshal params")

	// Create request
	req := &Request{
		JSONRPC: "3.0",
		Method:  "test",
		Params:  RawMessage(paramsData),
		ID:      1,
	}

	// Marshal request to CBOR
	reqData, err := cbor.Marshal(req)
	require.NoError(t, err, "Failed to marshal request")

	// Decode request
	decodedReq, _, isBatch, err := DecodeRequest(reqData, "application/cbor")
	require.NoError(t, err, "Failed to decode request")
	assert.False(t, isBatch, "Expected single request, got batch")

	// Get params and decode
	params := decodedReq.GetParams()
	var decoded testParams
	require.NoError(t, params.Decode(&decoded), "Failed to decode params")

	// Verify decoded values
	assert.Equal(t, "Bob", decoded.Name)
	assert.Equal(t, 25, decoded.Age)
}

// TestRequest_GetParams_CompactCBOR verifies that compact CBOR-decoded requests return CBOR Params
func TestRequest_GetParams_CompactCBOR(t *testing.T) {
	// Create a request with compact CBOR params
	type testParams struct {
		Name string `cbor:"name"`
		Age  int    `cbor:"age"`
	}

	// Create params using compact encoding
	paramsData, err := cborCompactEncMode.Marshal(map[string]any{"name": "Charlie", "age": 35})
	require.NoError(t, err, "Failed to marshal params")

	// Create compact request
	compactReq := &compactRequest{
		JSONRPC: "3.0",
		Method:  "test",
		Params:  RawMessage(paramsData),
		ID:      1,
	}

	// Marshal request to compact CBOR
	reqData, err := cborCompactEncMode.Marshal(compactReq)
	require.NoError(t, err, "Failed to marshal request")

	// Decode request
	decodedReq, _, isBatch, err := DecodeRequest(reqData, "application/cbor; format=compact")
	require.NoError(t, err, "Failed to decode request")
	assert.False(t, isBatch, "Expected single request, got batch")

	// Get params and decode
	params := decodedReq.GetParams()
	var decoded testParams
	require.NoError(t, params.Decode(&decoded), "Failed to decode params")

	// Verify decoded values
	assert.Equal(t, "Charlie", decoded.Name)
	assert.Equal(t, 35, decoded.Age)
}

// TestRequest_GetParams_Batch verifies that batch requests have format set correctly
func TestRequest_GetParams_Batch(t *testing.T) {
	// Create a batch request with JSON
	batchData := `[
		{"jsonrpc":"3.0","method":"test1","params":{"value":1},"id":1},
		{"jsonrpc":"3.0","method":"test2","params":{"value":2},"id":2}
	]`

	_, batch, isBatch, err := DecodeRequest([]byte(batchData), "application/json")
	require.NoError(t, err, "Failed to decode batch")
	assert.True(t, isBatch, "Expected batch request, got single")
	assert.Len(t, batch, 2, "Expected 2 requests in batch")

	// Verify each request can decode params
	for i, req := range batch {
		params := req.GetParams()
		var decoded map[string]any
		require.NoError(t, params.Decode(&decoded), "Failed to decode params for request %d", i)

		expectedValue := float64(i + 1) // JSON unmarshals numbers as float64
		assert.Equal(t, expectedValue, decoded["value"], "Request %d: unexpected value", i)
	}
}

// TestRequest_GetParams_DefaultFormat verifies backward compatibility with no format set
func TestRequest_GetParams_DefaultFormat(t *testing.T) {
	// Create a request manually without going through DecodeRequest
	type testParams struct {
		Value int `json:"value"`
	}

	paramsData, err := json.Marshal(testParams{Value: 42})
	require.NoError(t, err, "Failed to marshal params")

	req := &Request{
		JSONRPC: "3.0",
		Method:  "test",
		Params:  RawMessage(paramsData),
		ID:      1,
		// format field not set (empty string)
	}

	// GetParams should default to JSON
	params := req.GetParams()
	var decoded testParams
	require.NoError(t, params.Decode(&decoded), "Failed to decode params")

	assert.Equal(t, 42, decoded.Value)
}

// TestRequest_SetFormat verifies that SetFormat works correctly
func TestRequest_SetFormat(t *testing.T) {
	req := &Request{
		JSONRPC: "3.0",
		Method:  "test",
		ID:      1,
	}

	// Initially format should be empty
	assert.Empty(t, req.format, "Expected empty format")

	// Set format
	req.SetFormat("application/cbor")
	assert.Equal(t, "application/cbor", req.format)

	// Set another format
	req.SetFormat("application/cbor; format=compact")
	assert.Equal(t, "application/cbor; format=compact", req.format)
}
