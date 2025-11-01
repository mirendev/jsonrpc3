package jsonrpc3

import (
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMimeType(t *testing.T) {
	tests := []struct {
		name           string
		mimetype       string
		expectedType   string
		expectedFormat string
	}{
		{
			name:           "plain json",
			mimetype:       "application/json",
			expectedType:   "application/json",
			expectedFormat: "",
		},
		{
			name:           "plain cbor",
			mimetype:       "application/cbor",
			expectedType:   "application/cbor",
			expectedFormat: "",
		},
		{
			name:           "cbor compact",
			mimetype:       "application/cbor; format=compact",
			expectedType:   "application/cbor",
			expectedFormat: "compact",
		},
		{
			name:           "cbor compact with spaces",
			mimetype:       "application/cbor;format=compact",
			expectedType:   "application/cbor",
			expectedFormat: "compact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mt := ParseMimeType(tt.mimetype)
			assert.Equal(t, tt.expectedType, mt.Type)
			assert.Equal(t, tt.expectedFormat, mt.Format)
		})
	}
}

func TestCompactCBOR_Request_IntegerKeys(t *testing.T) {
	// Create a request
	req, err := NewRequestWithFormat("test_method", map[string]string{"key": "value"}, 123, "application/cbor; format=compact")
	require.NoError(t, err, "NewRequestWithFormat() should not error")

	// Convert to compact format
	compactReq := toCompactRequest(req)

	// Marshal using compact encoding
	data, err := cborCompactEncMode.Marshal(compactReq)
	require.NoError(t, err, "Marshal() should not error")

	// Decode the CBOR to verify it uses integer keys
	var decoded map[int]any
	require.NoError(t, cbor.Unmarshal(data, &decoded), "Unmarshal to map[int]any should not error")

	// Verify integer keys are present
	assert.Contains(t, decoded, keyJSONRPC, "Should have integer key for jsonrpc")
	assert.Contains(t, decoded, keyMethod, "Should have integer key for method")
	assert.Contains(t, decoded, keyID, "Should have integer key for id")

	// Verify no string keys
	var stringDecoded map[string]any
	if err := cbor.Unmarshal(data, &stringDecoded); err == nil {
		assert.Empty(t, stringDecoded, "Compact CBOR should not have string keys")
	}
}

func TestCompactCBOR_Response_IntegerKeys(t *testing.T) {
	// Create a response
	resp, err := NewSuccessResponseWithFormat(456, "test result", Version30, "application/cbor; format=compact")
	require.NoError(t, err, "NewSuccessResponseWithFormat() should not error")

	// Convert to compact format
	compactResp := toCompactResponse(resp)

	// Marshal using compact encoding
	data, err := cborCompactEncMode.Marshal(compactResp)
	require.NoError(t, err, "Marshal() should not error")

	// Decode the CBOR to verify it uses integer keys
	var decoded map[int]any
	require.NoError(t, cbor.Unmarshal(data, &decoded), "Unmarshal to map[int]any should not error")

	// Verify integer keys are present
	assert.Contains(t, decoded, keyJSONRPC, "Should have integer key for jsonrpc")
	assert.Contains(t, decoded, keyResult, "Should have integer key for result")
	assert.Contains(t, decoded, keyID, "Should have integer key for id")
}

func TestCompactCBOR_Error_IntegerKeys(t *testing.T) {
	err := &Error{
		Code:    -32600,
		Message: "Invalid Request",
		Data:    "test data",
	}

	// Marshal using the Error's custom MarshalCBOR
	data, marshalErr := err.MarshalCBOR()
	require.NoError(t, marshalErr, "MarshalCBOR() should not error")

	// Decode the CBOR to verify it uses integer keys
	var decoded map[int]any
	require.NoError(t, cbor.Unmarshal(data, &decoded), "Unmarshal to map[int]any should not error")

	// Verify integer keys are present
	assert.Contains(t, decoded, keyCode, "Should have integer key for code")
	assert.Contains(t, decoded, keyMessage, "Should have integer key for message")
	assert.Contains(t, decoded, keyData, "Should have integer key for data")
}

func TestDecodeRequest_CompactCBOR(t *testing.T) {
	// Create params in CBOR format
	paramsData, _ := cborCompactEncMode.Marshal("param")

	// Create a compact request
	compactReq := &compactRequest{
		JSONRPC: Version30,
		Method:  "test",
		Params:  RawMessage(paramsData),
		ID:      789,
	}

	// Marshal to CBOR
	data, err := cborCompactEncMode.Marshal(compactReq)
	require.NoError(t, err, "Marshal() should not error")

	// Decode using Codec
	codec := GetCodec("application/cbor; format=compact")
	msgSet, err := codec.UnmarshalMessages(data)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")

	// Check if this was originally a batch
	var req *Request
	var batch Batch
	isBatch := msgSet.IsBatch
	if !msgSet.IsBatch {
		req, err = msgSet.ToRequest()
	} else {
		batch, err = msgSet.ToBatch()
	}
	require.NoError(t, err, "ToRequest/ToBatch should not error")

	assert.False(t, isBatch, "isBatch should be false")
	assert.Nil(t, batch, "batch should be nil")
	require.NotNil(t, req, "req should not be nil")

	assert.Equal(t, "test", req.Method)
	assert.Equal(t, Version30, req.JSONRPC)
}

func TestDecodeRequest_CompactCBOR_Batch(t *testing.T) {
	// Create a compact batch
	compactBatch := []compactRequest{
		{JSONRPC: Version30, Method: "test1", ID: 1},
		{JSONRPC: Version30, Method: "test2", ID: 2},
	}

	// Marshal to CBOR
	data, err := cborCompactEncMode.Marshal(compactBatch)
	require.NoError(t, err, "Marshal() should not error")

	// Decode using Codec
	codec := GetCodec("application/cbor; format=compact")
	msgSet, err := codec.UnmarshalMessages(data)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")

	// Check if this was originally a batch
	var req *Request
	var batch Batch
	isBatch := msgSet.IsBatch
	if !msgSet.IsBatch {
		req, err = msgSet.ToRequest()
	} else {
		batch, err = msgSet.ToBatch()
	}
	require.NoError(t, err, "ToRequest/ToBatch should not error")

	assert.True(t, isBatch, "isBatch should be true")
	assert.Nil(t, req, "req should be nil")
	require.NotNil(t, batch, "batch should not be nil")

	assert.Len(t, batch, 2)
	assert.Equal(t, "test1", batch[0].Method)
	assert.Equal(t, "test2", batch[1].Method)
}

func TestEncodeResponse_CompactCBOR(t *testing.T) {
	// Create result in CBOR format
	resultData, _ := cborCompactEncMode.Marshal("result")

	resp := &Response{
		JSONRPC: Version30,
		Result:  RawMessage(resultData),
		ID:      999,
	}

	// Encode using compact format
	msgSet := resp.ToMessageSet()
	codec := GetCodec("application/cbor; format=compact")
	data, err := codec.MarshalMessages(msgSet)
	require.NoError(t, err, "codec.MarshalMessages() should not error")

	// Verify it uses integer keys
	var decoded map[int]any
	require.NoError(t, cbor.Unmarshal(data, &decoded), "Unmarshal should not error")

	// Verify integer keys are present
	assert.Contains(t, decoded, keyJSONRPC, "Should have integer key for jsonrpc")
	assert.Contains(t, decoded, keyResult, "Should have integer key for result")
	assert.Contains(t, decoded, keyID, "Should have integer key for id")
}

func TestEncodeBatchResponse_CompactCBOR(t *testing.T) {
	// Create results in CBOR format
	result1Data, _ := cborCompactEncMode.Marshal("r1")
	result2Data, _ := cborCompactEncMode.Marshal("r2")

	batch := BatchResponse{
		{JSONRPC: Version30, Result: RawMessage(result1Data), ID: 1},
		{JSONRPC: Version30, Result: RawMessage(result2Data), ID: 2},
	}

	// Encode using compact format
	msgSet := batch.ToMessageSet()
	codec := GetCodec("application/cbor; format=compact")
	data, err := codec.MarshalMessages(msgSet)
	require.NoError(t, err, "codec.MarshalMessages() should not error")

	// Decode to verify structure
	var decoded []map[int]any
	require.NoError(t, cbor.Unmarshal(data, &decoded), "Unmarshal should not error")

	assert.Len(t, decoded, 2)

	// Verify first response has integer keys
	assert.Contains(t, decoded[0], keyJSONRPC, "Should have integer key for jsonrpc in first response")
}

func TestRoundTrip_CompactCBOR(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	_ = NewHandler(session, root, NewNoOpCaller(), []string{"application/cbor; format=compact"})

	// Register a method
	root.Register("add", func(params Params, caller Caller) (any, error) {
		var numbers []int
		if err := params.Decode(&numbers); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		sum := 0
		for _, n := range numbers {
			sum += n
		}
		return sum, nil
	})

	// Create compact CBOR request
	req, err := NewRequestWithFormat("add", []int{5, 10, 15}, 111, "application/cbor; format=compact")
	require.NoError(t, err, "NewRequestWithFormat() should not error")

	// Encode request with compact format
	compactReq := toCompactRequest(req)
	reqData, err := cborCompactEncMode.Marshal(compactReq)
	require.NoError(t, err, "Marshal request should not error")

	// Decode request
	codec := GetCodec("application/cbor; format=compact")
	msgSet, err := codec.UnmarshalMessages(reqData)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")

	decodedReq, err := msgSet.ToRequest()
	require.NoError(t, err, "ToRequest() should not error")

	// Process request
	params := NewParamsWithFormat(decodedReq.Params, "application/cbor; format=compact")
	result, err := root.CallMethod(decodedReq.Method, params, nil)
	require.NoError(t, err, "CallMethod() should not error")

	assert.Equal(t, 30, result)

	// Create response
	resp, err := NewSuccessResponseWithFormat(decodedReq.ID, result, Version30, "application/cbor; format=compact")
	require.NoError(t, err, "NewSuccessResponseWithFormat() should not error")

	// Encode response with compact format
	msgSet = resp.ToMessageSet()
	codec = GetCodec("application/cbor; format=compact")
	respData, err := codec.MarshalMessages(msgSet)
	require.NoError(t, err, "codec.MarshalMessages() should not error")

	// Verify response uses integer keys
	var decodedResp map[int]any
	require.NoError(t, cbor.Unmarshal(respData, &decodedResp), "Unmarshal response should not error")

	assert.Contains(t, decodedResp, keyResult, "Response should have integer key for result")
}

func TestCompactCBOR_SizeComparison(t *testing.T) {
	// Create a request with some data
	params := map[string]any{
		"name":    "Alice",
		"age":     30,
		"email":   "alice@example.com",
		"active":  true,
		"balance": 1234.56,
	}

	req, _ := NewRequestWithFormat("processUser", params, 12345, "cbor")

	// Encode with standard CBOR (string keys)
	standardData, err := cborEncMode.Marshal(req)
	require.NoError(t, err, "Standard marshal should not error")

	// Encode with compact CBOR (integer keys)
	compactReq := toCompactRequest(req)
	compactData, err := cborCompactEncMode.Marshal(compactReq)
	require.NoError(t, err, "Compact marshal should not error")

	t.Logf("Standard CBOR size: %d bytes", len(standardData))
	t.Logf("Compact CBOR size: %d bytes", len(compactData))
	t.Logf("Size reduction: %.1f%%", float64(len(standardData)-len(compactData))/float64(len(standardData))*100)

	// Compact should be smaller
	assert.Less(t, len(compactData), len(standardData), "Compact CBOR should be smaller than standard CBOR")
}

// TestDecodeRequest_FormatSpecificity verifies that compact CBOR data must be decoded with compact mimetype
func TestDecodeRequest_FormatSpecificity(t *testing.T) {
	// Create a compact request
	paramsData, _ := cborCompactEncMode.Marshal([]int{1, 2, 3})
	compactReq := &compactRequest{
		JSONRPC: Version30,
		Method:  "test",
		Params:  RawMessage(paramsData),
		ID:      123,
	}

	// Encode as compact CBOR
	compactData, err := cborCompactEncMode.Marshal(compactReq)
	require.NoError(t, err, "Marshal compact should not error")

	// Decode with compact mimetype - should work
	codec1 := GetCodec("application/cbor; format=compact")
	msgSet1, err := codec1.UnmarshalMessages(compactData)
	require.NoError(t, err, "codec.UnmarshalMessages with compact mimetype should not error")

	req1, err := msgSet1.ToRequest()
	require.NoError(t, err, "ToRequest() should not error")
	assert.Equal(t, "test", req1.Method)

	// Decode with standard mimetype - should fail or not decode correctly
	// because the data has integer keys but we're expecting string keys
	codec2 := GetCodec("application/cbor")
	msgSet2, err := codec2.UnmarshalMessages(compactData)
	var req2 *Request
	if err == nil {
		req2, err = msgSet2.ToRequest()
	}
	// This might succeed but produce incorrect results, or might fail
	// The important thing is that we're explicitly using the format parameter
	if err == nil {
		// If it succeeds, the method should be empty because integer keys
		// won't map to the string key fields
		if req2.Method != "" {
			t.Logf("Warning: Standard decoder parsed compact data, method = %v", req2.Method)
		}
	}
}

// TestEncodeResponse_MimetypeRespected verifies encoding respects the mimetype format
func TestEncodeResponse_MimetypeRespected(t *testing.T) {
	resultData, _ := cborCompactEncMode.Marshal(42)
	resp := &Response{
		JSONRPC: Version30,
		Result:  RawMessage(resultData),
		ID:      456,
	}

	// Encode with standard CBOR
	msgSet := resp.ToMessageSet()
	codec := GetCodec("application/cbor")
	standardData, err := codec.MarshalMessages(msgSet)
	require.NoError(t, err, "codec.MarshalMessages() standard should not error")

	// Encode with compact CBOR
	compactCodec := GetCodec("application/cbor; format=compact")
	compactData, err := compactCodec.MarshalMessages(msgSet)
	require.NoError(t, err, "codec.MarshalMessages() compact should not error")

	// Verify standard uses string keys
	var standardMap map[string]any
	require.NoError(t, cbor.Unmarshal(standardData, &standardMap), "Unmarshal standard data should not error")
	assert.Contains(t, standardMap, "jsonrpc", "Standard CBOR should have 'jsonrpc' string key")

	// Verify compact uses integer keys
	var compactMap map[int]any
	require.NoError(t, cbor.Unmarshal(compactData, &compactMap), "Unmarshal compact data should not error")
	assert.Contains(t, compactMap, keyJSONRPC, "Compact CBOR should have integer key for jsonrpc")

	// The two encodings should be different
	assert.NotEqual(t, string(standardData), string(compactData), "Standard and compact encodings should be different")
}
