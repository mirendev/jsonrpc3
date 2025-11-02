package jsonrpc3

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRawMessage_MarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		msg  RawMessage
		want string
	}{
		{
			name: "string value",
			msg:  RawMessage(`"hello"`),
			want: `"hello"`,
		},
		{
			name: "object value",
			msg:  RawMessage(`{"key":"value"}`),
			want: `{"key":"value"}`,
		},
		{
			name: "array value",
			msg:  RawMessage(`[1,2,3]`),
			want: `[1,2,3]`,
		},
		{
			name: "null value",
			msg:  nil,
			want: `null`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.msg)
			require.NoError(t, err, "MarshalJSON() should not error")
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestRawMessage_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		data string
		want RawMessage
	}{
		{
			name: "string value",
			data: `"hello"`,
			want: RawMessage(`"hello"`),
		},
		{
			name: "object value",
			data: `{"key":"value"}`,
			want: RawMessage(`{"key":"value"}`),
		},
		{
			name: "array value",
			data: `[1,2,3]`,
			want: RawMessage(`[1,2,3]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got RawMessage
			err := json.Unmarshal([]byte(tt.data), &got)
			if err != nil {
				t.Fatalf("UnmarshalJSON() error = %v", err)
			}
			if string(got) != string(tt.want) {
				t.Errorf("UnmarshalJSON() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestRawMessage_MarshalCBOR(t *testing.T) {
	tests := []struct {
		name  string
		msg   RawMessage
		isNil bool
	}{
		{
			name:  "string value",
			msg:   mustMarshalCBOR(t, "hello"),
			isNil: false,
		},
		{
			name:  "integer value",
			msg:   mustMarshalCBOR(t, 42),
			isNil: false,
		},
		{
			name:  "map value",
			msg:   mustMarshalCBOR(t, map[string]any{"key": "value"}),
			isNil: false,
		},
		{
			name:  "array value",
			msg:   mustMarshalCBOR(t, []int{1, 2, 3}),
			isNil: false,
		},
		{
			name:  "nil value",
			msg:   nil,
			isNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cbor.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("MarshalCBOR() error = %v", err)
			}

			// Unmarshal to verify it's valid CBOR
			var result any
			if err := cbor.Unmarshal(got, &result); err != nil {
				t.Fatalf("unmarshal result error = %v", err)
			}

			if tt.isNil && result != nil {
				t.Errorf("expected nil, got %v", result)
			}
		})
	}
}

func TestRawMessage_UnmarshalCBOR(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{
			name:  "string value",
			value: "hello",
		},
		{
			name:  "integer value",
			value: int64(42),
		},
		{
			name:  "map value",
			value: map[string]any{"key": "value"},
		},
		{
			name:  "array value",
			value: []any{int64(1), int64(2), int64(3)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal the test value to CBOR
			data, err := cbor.Marshal(tt.value)
			if err != nil {
				t.Fatalf("marshal error = %v", err)
			}

			// Unmarshal into RawMessage
			var msg RawMessage
			if err := cbor.Unmarshal(data, &msg); err != nil {
				t.Fatalf("UnmarshalCBOR() error = %v", err)
			}

			// Verify the data matches
			if string(msg) != string(data) {
				t.Errorf("UnmarshalCBOR() data mismatch")
			}
		})
	}
}

func TestMarshalValue(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		format string
	}{
		{
			name:   "json string",
			value:  "hello",
			format: "json",
		},
		{
			name:   "json map",
			value:  map[string]any{"key": "value"},
			format: "json",
		},
		{
			name:   "cbor string",
			value:  "hello",
			format: "cbor",
		},
		{
			name:   "cbor map",
			value:  map[string]any{"key": "value"},
			format: "cbor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := MarshalValue(tt.value, tt.format)
			if err != nil {
				t.Fatalf("MarshalValue() error = %v", err)
			}

			// Verify we can unmarshal it back
			var result any
			if tt.format == "cbor" {
				err = cbor.Unmarshal(msg, &result)
			} else {
				err = json.Unmarshal(msg, &result)
			}
			if err != nil {
				t.Fatalf("unmarshal error = %v", err)
			}
		})
	}
}

func TestRawMessage_UnmarshalValue(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		format string
	}{
		{
			name:   "json string",
			value:  "hello",
			format: "json",
		},
		{
			name:   "json integer",
			value:  42,
			format: "json",
		},
		{
			name:   "cbor string",
			value:  "hello",
			format: "cbor",
		},
		{
			name:   "cbor integer",
			value:  42,
			format: "cbor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal the value to RawMessage
			msg, err := MarshalValue(tt.value, tt.format)
			if err != nil {
				t.Fatalf("MarshalValue() error = %v", err)
			}

			// Unmarshal back
			var result any
			if err := msg.UnmarshalValue(&result, tt.format); err != nil {
				t.Fatalf("UnmarshalValue() error = %v", err)
			}

			// For JSON integers, they come back as float64
			if tt.format == "json" {
				if i, ok := tt.value.(int); ok {
					if f, ok := result.(float64); ok && int(f) == i {
						return
					}
				}
			}

			// For CBOR integers, they come back as int64
			if tt.format == "cbor" {
				if i, ok := tt.value.(int); ok {
					if i64, ok := result.(int64); ok && int64(i) == i64 {
						return
					}
				}
			}
		})
	}
}

func TestNewRequestWithFormat_CBOR(t *testing.T) {
	params := map[string]any{"key": "value"}

	req, err := NewRequestWithFormat("test", params, 1, "cbor")
	if err != nil {
		require.NoError(t, err, "NewRequestWithFormat() should not error")
	}

	if req.Method != "test" {
		t.Errorf("Method = %v, want test", req.Method)
	}

	// Decode params as CBOR
	var result map[string]any
	if err := cbor.Unmarshal(req.Params, &result); err != nil {
		t.Fatalf("unmarshal params error = %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("params = %v, want {key: value}", result)
	}
}

func TestNewSuccessResponseWithFormat_CBOR(t *testing.T) {
	result := map[string]any{"status": "ok"}

	resp, err := NewSuccessResponseWithFormat(1, result, Version30, "cbor")
	if err != nil {
		require.NoError(t, err, "NewSuccessResponseWithFormat() should not error")
	}

	if resp.JSONRPC != Version30 {
		assert.Equal(t, Version30, resp.JSONRPC, "JSONRPC")
	}

	// Decode result as CBOR
	var decoded map[string]any
	if err := cbor.Unmarshal(resp.Result, &decoded); err != nil {
		t.Fatalf("unmarshal result error = %v", err)
	}

	if decoded["status"] != "ok" {
		t.Errorf("result = %v, want {status: ok}", decoded)
	}
}

func TestNewParamsWithFormat_CBOR(t *testing.T) {
	data := map[string]any{"key": "value"}
	cborData, err := cbor.Marshal(data)
	if err != nil {
		require.NoError(t, err, "marshal should not error")
	}

	params := NewParamsWithFormat(RawMessage(cborData), "cbor")

	var result map[string]any
	if err := params.Decode(&result); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("result = %v, want {key: value}", result)
	}
}

func TestCBORParams_Decode(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{
			name:  "string",
			value: "hello",
		},
		{
			name:  "integer",
			value: 42,
		},
		{
			name:  "map",
			value: map[string]any{"key": "value"},
		},
		{
			name:  "array",
			value: []int{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to CBOR
			data, err := cbor.Marshal(tt.value)
			if err != nil {
				t.Fatalf("marshal error = %v", err)
			}

			// Create CBOR params
			params := &cborParams{data: RawMessage(data)}

			// Decode
			var result any
			if err := params.Decode(&result); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
		})
	}
}

func TestCBORParams_DecodeNil(t *testing.T) {
	params := &cborParams{data: nil}

	var result any
	if err := params.Decode(&result); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

// Helper function to marshal to CBOR
func mustMarshalCBOR(t *testing.T, v any) RawMessage {
	t.Helper()
	data, err := cbor.Marshal(v)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	return RawMessage(data)
}

// TestCBORIntegration demonstrates end-to-end CBOR usage with Handler
func TestCBORIntegration(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	_ = NewHandler(session, root, NewNoOpCaller(), []string{"application/cbor"})

	// Register a method that adds two numbers
	root.Register("add", func(ctx context.Context, params Params, caller Caller) (any, error) {
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

	// Create a CBOR-encoded request
	req, err := NewRequestWithFormat("add", []int{10, 20, 30}, 1, "cbor")
	if err != nil {
		require.NoError(t, err, "NewRequestWithFormat() should not error")
	}

	// Process the request using CBOR params
	params := NewParamsWithFormat(req.Params, "cbor")
	result, err := root.CallMethod(context.Background(), "add", params, nil)
	if err != nil {
		require.NoError(t, err, "CallMethod() should not error")
	}

	if result != 60 {
		t.Errorf("result = %v, want 60", result)
	}

	// Create a CBOR-encoded response
	resp, err := NewSuccessResponseWithFormat(1, result, Version30, "cbor")
	if err != nil {
		require.NoError(t, err, "NewSuccessResponseWithFormat() should not error")
	}

	// Decode the response
	var finalResult int
	if err := cbor.Unmarshal(resp.Result, &finalResult); err != nil {
		t.Fatalf("unmarshal response error = %v", err)
	}

	if finalResult != 60 {
		t.Errorf("finalResult = %v, want 60", finalResult)
	}
}

// TestMixedFormatIntegration demonstrates that JSON and CBOR can coexist
func TestMixedFormatIntegration(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	_ = NewHandler(session, root, NewNoOpCaller(), []string{"application/json", "application/cbor"})

	// Register an echo method
	root.Register("echo", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return msg, nil
	})

	// Test with JSON
	jsonReq, _ := NewRequestWithFormat("echo", "hello json", 1, "json")
	jsonParams := NewParamsWithFormat(jsonReq.Params, "json")
	jsonResult, err := root.CallMethod(context.Background(), "echo", jsonParams, nil)
	if err != nil {
		require.NoError(t, err, "JSON CallMethod() should not error")
	}
	if jsonResult != "hello json" {
		t.Errorf("JSON result = %v, want 'hello json'", jsonResult)
	}

	// Test with CBOR
	cborReq, _ := NewRequestWithFormat("echo", "hello cbor", 2, "cbor")
	cborParams := NewParamsWithFormat(cborReq.Params, "cbor")
	cborResult, err := root.CallMethod(context.Background(), "echo", cborParams, nil)
	if err != nil {
		require.NoError(t, err, "CBOR CallMethod() should not error")
	}
	if cborResult != "hello cbor" {
		t.Errorf("CBOR result = %v, want 'hello cbor'", cborResult)
	}
}

// TestCBORComplexTypes tests CBOR with complex data structures
func TestCBORComplexTypes(t *testing.T) {
	type Person struct {
		Name string
		Age  int
		Tags []string
	}

	session := NewSession()
	root := NewMethodMap()
	_ = NewHandler(session, root, NewNoOpCaller(), nil)

	root.Register("process_person", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var person Person
		if err := params.Decode(&person); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		// Return person with age incremented
		person.Age++
		return person, nil
	})

	// Create a CBOR request with complex type
	inputPerson := Person{
		Name: "Alice",
		Age:  30,
		Tags: []string{"developer", "gopher"},
	}

	req, err := NewRequestWithFormat("process_person", inputPerson, 1, "cbor")
	if err != nil {
		require.NoError(t, err, "NewRequestWithFormat() should not error")
	}

	// Process with CBOR params
	params := NewParamsWithFormat(req.Params, "cbor")
	result, err := root.CallMethod(context.Background(), "process_person", params, nil)
	if err != nil {
		require.NoError(t, err, "CallMethod() should not error")
	}

	// Verify result
	outputPerson, ok := result.(Person)
	if !ok {
		t.Fatalf("result type = %T, want Person", result)
	}

	if outputPerson.Name != "Alice" {
		t.Errorf("Name = %v, want Alice", outputPerson.Name)
	}
	if outputPerson.Age != 31 {
		t.Errorf("Age = %v, want 31", outputPerson.Age)
	}
	if len(outputPerson.Tags) != 2 {
		t.Errorf("Tags length = %v, want 2", len(outputPerson.Tags))
	}
}

// TestDecodeRequest_CBOR_Single tests decoding a single CBOR request
func TestDecodeRequest_CBOR_Single(t *testing.T) {
	// Create params in CBOR format
	paramsData, err := cbor.Marshal("test_param")
	if err != nil {
		require.NoError(t, err, "cbor.Marshal(params) should not error")
	}

	// Create a request
	req := Request{
		JSONRPC: Version30,
		Method:  "test_method",
		Params:  RawMessage(paramsData),
		ID:      1,
	}

	// Encode to CBOR
	data, err := cbor.Marshal(req)
	if err != nil {
		require.NoError(t, err, "cbor.Marshal() should not error")
	}

	// Decode
	codec := GetCodec("application/cbor")
	msgSet, err := codec.UnmarshalMessages(data)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")


	// Check if this was originally a batch
	var decodedReq *Request
	var batch Batch
	isBatch := msgSet.IsBatch
	if !msgSet.IsBatch {
		decodedReq, err = msgSet.ToRequest()
	} else {
		batch, err = msgSet.ToBatch()
	}
	require.NoError(t, err, "ToRequest/ToBatch should not error")

	if isBatch {
		t.Error("isBatch should be false")
	}
	if decodedReq == nil {
		t.Fatal("decodedReq should not be nil")
	}
	if batch != nil {
		t.Error("batch should be nil")
	}

	if decodedReq.Method != "test_method" {
		t.Errorf("Method = %v, want test_method", decodedReq.Method)
	}
	if decodedReq.JSONRPC != Version30 {
		assert.Equal(t, Version30, decodedReq.JSONRPC, "JSONRPC")
	}
}

// TestDecodeRequest_CBOR_Batch tests decoding a CBOR batch request
func TestDecodeRequest_CBOR_Batch(t *testing.T) {
	// Create batch requests
	batch := Batch{
		{JSONRPC: Version30, Method: "test1", ID: 1},
		{JSONRPC: Version30, Method: "test2", ID: 2},
		{JSONRPC: Version30, Method: "test3", ID: 3},
	}

	// Encode to CBOR
	data, err := cbor.Marshal(batch)
	if err != nil {
		require.NoError(t, err, "cbor.Marshal() should not error")
	}

	// Decode
	codec := GetCodec("application/cbor")
	msgSet, err := codec.UnmarshalMessages(data)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")


	// Check if this was originally a batch
	var decodedReq *Request
	var decodedBatch Batch
	isBatch := msgSet.IsBatch
	if !msgSet.IsBatch {
		decodedReq, err = msgSet.ToRequest()
	} else {
		decodedBatch, err = msgSet.ToBatch()
	}
	require.NoError(t, err, "ToRequest/ToBatch should not error")

	if !isBatch {
		t.Error("isBatch should be true")
	}
	if decodedReq != nil {
		t.Error("decodedReq should be nil")
	}
	if decodedBatch == nil {
		t.Fatal("decodedBatch should not be nil")
	}

	if len(decodedBatch) != 3 {
		t.Errorf("len(decodedBatch) = %v, want 3", len(decodedBatch))
	}

	if decodedBatch[0].Method != "test1" {
		t.Errorf("decodedBatch[0].Method = %v, want test1", decodedBatch[0].Method)
	}
	if decodedBatch[1].Method != "test2" {
		t.Errorf("decodedBatch[1].Method = %v, want test2", decodedBatch[1].Method)
	}
	if decodedBatch[2].Method != "test3" {
		t.Errorf("decodedBatch[2].Method = %v, want test3", decodedBatch[2].Method)
	}
}

// TestDecodeRequest_CBOR_WithParams tests decoding CBOR request with parameters
func TestDecodeRequest_CBOR_WithParams(t *testing.T) {
	// Create request with complex params
	params := map[string]any{
		"name":  "Alice",
		"age":   30,
		"tags":  []string{"developer", "gopher"},
		"score": 95.5,
	}

	paramsData, err := cbor.Marshal(params)
	if err != nil {
		require.NoError(t, err, "cbor.Marshal(params) should not error")
	}

	req := Request{
		JSONRPC: Version30,
		Method:  "process",
		Params:  RawMessage(paramsData),
		ID:      "test-id",
	}

	// Encode request to CBOR
	data, err := cbor.Marshal(req)
	if err != nil {
		require.NoError(t, err, "cbor.Marshal(req) should not error")
	}

	// Decode
	codec := GetCodec("application/cbor")
	msgSet, err := codec.UnmarshalMessages(data)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")


	decodedReq, err := msgSet.ToRequest()
	require.NoError(t, err, "ToRequest() should not error")

	// Decode params
	var decodedParams map[string]any
	if err := cbor.Unmarshal(decodedReq.Params, &decodedParams); err != nil {
		t.Fatalf("cbor.Unmarshal(params) error = %v", err)
	}

	if decodedParams["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", decodedParams["name"])
	}
	// CBOR may decode integers as uint64 depending on value
	age := decodedParams["age"]
	if ageInt, ok := age.(int64); ok {
		if ageInt != 30 {
			t.Errorf("age = %v, want 30", age)
		}
	} else if ageUint, ok := age.(uint64); ok {
		if ageUint != 30 {
			t.Errorf("age = %v, want 30", age)
		}
	} else {
		t.Errorf("age has unexpected type %T", age)
	}
}

// TestEncodeResponseWithFormat_CBOR tests encoding responses to CBOR
func TestEncodeResponseWithFormat_CBOR(t *testing.T) {
	// Create result in CBOR format
	resultData, err := cbor.Marshal("success")
	if err != nil {
		require.NoError(t, err, "cbor.Marshal(result) should not error")
	}

	resp := &Response{
		JSONRPC: Version30,
		Result:  RawMessage(resultData),
		ID:      1,
	}

	// Encode to CBOR
	msgSet := resp.ToMessageSet()
	codec := GetCodec("application/cbor")
	data, err := codec.MarshalMessages(msgSet)
	if err != nil {
		require.NoError(t, err, "codec.MarshalMessages() should not error")
	}

	// Decode back
	var decoded Response
	if err := cbor.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("cbor.Unmarshal() error = %v", err)
	}

	if decoded.JSONRPC != Version30 {
		assert.Equal(t, Version30, decoded.JSONRPC, "JSONRPC")
	}
	// CBOR may decode integers as int64 or uint64 depending on value
	switch id := decoded.ID.(type) {
	case int64:
		if id != 1 {
			t.Errorf("ID = %v, want 1", decoded.ID)
		}
	case uint64:
		if id != 1 {
			t.Errorf("ID = %v, want 1", decoded.ID)
		}
	case int:
		if id != 1 {
			t.Errorf("ID = %v, want 1", decoded.ID)
		}
	default:
		t.Errorf("ID has unexpected type %T", decoded.ID)
	}
}

// TestEncodeBatchResponseWithFormat_CBOR tests encoding batch responses to CBOR
func TestEncodeBatchResponseWithFormat_CBOR(t *testing.T) {
	// Create results in CBOR format
	result1Data, _ := cbor.Marshal("result1")
	result2Data, _ := cbor.Marshal("result2")

	batch := BatchResponse{
		{JSONRPC: Version30, Result: RawMessage(result1Data), ID: 1},
		{JSONRPC: Version30, Result: RawMessage(result2Data), ID: 2},
	}

	// Encode to CBOR
	msgSet := batch.ToMessageSet()
	codec := GetCodec("application/cbor")
	data, err := codec.MarshalMessages(msgSet)
	if err != nil {
		require.NoError(t, err, "codec.MarshalMessages() should not error")
	}

	// Decode back
	var decoded BatchResponse
	if err := cbor.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("cbor.Unmarshal() error = %v", err)
	}

	if len(decoded) != 2 {
		t.Errorf("len(decoded) = %v, want 2", len(decoded))
	}
}

// TestRoundTrip_CBOR tests full round-trip with CBOR encoding
func TestRoundTrip_CBOR(t *testing.T) {
	session := NewSession()
	root := NewMethodMap()
	_ = NewHandler(session, root, NewNoOpCaller(), []string{"application/cbor"})

	// Register a method
	root.Register("multiply", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var numbers []int
		if err := params.Decode(&numbers); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		result := 1
		for _, n := range numbers {
			result *= n
		}
		return result, nil
	})

	// Create CBOR request
	req, err := NewRequestWithFormat("multiply", []int{2, 3, 4}, 123, "cbor")
	if err != nil {
		require.NoError(t, err, "NewRequestWithFormat() should not error")
	}

	// Encode request
	reqData, err := cbor.Marshal(req)
	if err != nil {
		require.NoError(t, err, "cbor.Marshal(req) should not error")
	}

	// Decode request
	codec := GetCodec("application/cbor")
	msgSet, err := codec.UnmarshalMessages(reqData)
	require.NoError(t, err, "codec.UnmarshalMessages() should not error")


	decodedReq, err := msgSet.ToRequest()
	require.NoError(t, err, "ToRequest() should not error")

	// Process with CBOR params
	params := NewParamsWithFormat(decodedReq.Params, "cbor")
	result, err := root.CallMethod(context.Background(), decodedReq.Method, params, nil)
	if err != nil {
		require.NoError(t, err, "CallMethod() should not error")
	}

	// Create response
	resp, err := NewSuccessResponseWithFormat(decodedReq.ID, result, Version30, "cbor")
	if err != nil {
		require.NoError(t, err, "NewSuccessResponseWithFormat() should not error")
	}

	// Encode response
	msgSet = resp.ToMessageSet()
	codec = GetCodec("application/cbor")
	respData, err := codec.MarshalMessages(msgSet)
	if err != nil {
		require.NoError(t, err, "codec.MarshalMessages() should not error")
	}

	// Decode response
	var finalResp Response
	if err := cbor.Unmarshal(respData, &finalResp); err != nil {
		t.Fatalf("cbor.Unmarshal(response) error = %v", err)
	}

	// Decode result
	var finalResult int
	if err := cbor.Unmarshal(finalResp.Result, &finalResult); err != nil {
		t.Fatalf("cbor.Unmarshal(result) error = %v", err)
	}

	if finalResult != 24 {
		t.Errorf("finalResult = %v, want 24", finalResult)
	}
}
