package jsonrpc3

import (
	"encoding/json"
	"math/big"
	"regexp"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValue_IsReference tests reference detection
func TestValue_IsReference(t *testing.T) {
	codec := GetCodec("application/json")

	// Test with a reference
	ref := NewReference("test-ref-123")
	data, err := codec.Marshal(ref)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	assert.True(t, val.IsReference(), "Should detect reference")

	// Test with non-reference
	data2, err := codec.Marshal(map[string]any{"name": "test", "value": 42})
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	assert.False(t, val2.IsReference(), "Should not detect reference for regular object")

	// Test with primitive
	data3, err := codec.Marshal("hello")
	require.NoError(t, err)

	val3 := NewValueWithCodec(data3, codec)
	assert.False(t, val3.IsReference(), "Should not detect reference for string")
}

// TestValue_AsReference tests reference extraction
func TestValue_AsReference(t *testing.T) {
	codec := GetCodec("application/json")

	// Test extracting a reference
	ref := NewReference("test-ref-123")
	data, err := codec.Marshal(ref)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	extractedRef, err := val.AsReference()
	require.NoError(t, err)
	assert.Equal(t, "test-ref-123", extractedRef.Ref)

	// Test with non-reference should error
	data2, err := codec.Marshal(map[string]any{"name": "test"})
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	_, err = val2.AsReference()
	assert.Error(t, err, "Should error when extracting non-reference")
}

// TestValue_IsEnhanced tests enhanced type detection
func TestValue_IsEnhanced(t *testing.T) {
	codec := GetCodec("application/json")

	// Test DateTime
	dt := NewDateTime(time.Now())
	data, err := codec.Marshal(dt)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	typeName, isEnhanced := val.IsEnhanced()
	assert.True(t, isEnhanced, "Should detect DateTime as enhanced")
	assert.Equal(t, TypeDateTime, typeName)

	// Test Bytes
	bytes := NewBytes([]byte("hello world"))
	data2, err := codec.Marshal(bytes)
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	typeName2, isEnhanced2 := val2.IsEnhanced()
	assert.True(t, isEnhanced2, "Should detect Bytes as enhanced")
	assert.Equal(t, TypeBytes, typeName2)

	// Test BigInt
	bigInt := NewBigInt(big.NewInt(123456789))
	data3, err := codec.Marshal(bigInt)
	require.NoError(t, err)

	val3 := NewValueWithCodec(data3, codec)
	typeName3, isEnhanced3 := val3.IsEnhanced()
	assert.True(t, isEnhanced3, "Should detect BigInt as enhanced")
	assert.Equal(t, TypeBigInt, typeName3)

	// Test RegExp
	re := NewRegExp(regexp.MustCompile(`test.*`))
	data4, err := codec.Marshal(re)
	require.NoError(t, err)

	val4 := NewValueWithCodec(data4, codec)
	typeName4, isEnhanced4 := val4.IsEnhanced()
	assert.True(t, isEnhanced4, "Should detect RegExp as enhanced")
	assert.Equal(t, TypeRegExp, typeName4)

	// Test non-enhanced
	data5, err := codec.Marshal(map[string]any{"name": "test"})
	require.NoError(t, err)

	val5 := NewValueWithCodec(data5, codec)
	typeName5, isEnhanced5 := val5.IsEnhanced()
	assert.False(t, isEnhanced5, "Should not detect regular object as enhanced")
	assert.Empty(t, typeName5)
}

// TestValue_AsEnhanced tests enhanced type extraction
func TestValue_AsEnhanced(t *testing.T) {
	codec := GetCodec("application/json")

	// Test DateTime extraction
	now := time.Now().Truncate(time.Millisecond) // Truncate to avoid precision issues
	dt := NewDateTime(now)
	data, err := codec.Marshal(dt)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	enhanced, err := val.AsEnhanced()
	require.NoError(t, err)
	extractedTime, ok := enhanced.(time.Time)
	require.True(t, ok, "Should extract time.Time")
	assert.True(t, extractedTime.Equal(now), "Times should match")

	// Test Bytes extraction
	testBytes := []byte("hello world")
	bytes := NewBytes(testBytes)
	data2, err := codec.Marshal(bytes)
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	enhanced2, err := val2.AsEnhanced()
	require.NoError(t, err)
	extractedBytes, ok := enhanced2.([]byte)
	require.True(t, ok, "Should extract []byte")
	assert.Equal(t, testBytes, extractedBytes)

	// Test BigInt extraction
	testBigInt := big.NewInt(123456789)
	bigInt := NewBigInt(testBigInt)
	data3, err := codec.Marshal(bigInt)
	require.NoError(t, err)

	val3 := NewValueWithCodec(data3, codec)
	enhanced3, err := val3.AsEnhanced()
	require.NoError(t, err)
	extractedBigInt, ok := enhanced3.(*big.Int)
	require.True(t, ok, "Should extract *big.Int")
	assert.Equal(t, testBigInt.String(), extractedBigInt.String())

	// Test RegExp extraction
	testRegex := regexp.MustCompile(`test.*`)
	re := NewRegExp(testRegex)
	data4, err := codec.Marshal(re)
	require.NoError(t, err)

	val4 := NewValueWithCodec(data4, codec)
	enhanced4, err := val4.AsEnhanced()
	require.NoError(t, err)
	extractedRegex, ok := enhanced4.(*regexp.Regexp)
	require.True(t, ok, "Should extract *regexp.Regexp")
	assert.Equal(t, testRegex.String(), extractedRegex.String())

	// Test non-enhanced should error
	data5, err := codec.Marshal(map[string]any{"name": "test"})
	require.NoError(t, err)

	val5 := NewValueWithCodec(data5, codec)
	_, err = val5.AsEnhanced()
	assert.Error(t, err, "Should error when extracting non-enhanced type")
}

// TestValue_Interface_AutoDecodeEnhanced tests automatic decoding in Interface()
func TestValue_Interface_AutoDecodeEnhanced(t *testing.T) {
	codec := GetCodec("application/json")

	// Test DateTime auto-decode
	now := time.Now().Truncate(time.Millisecond)
	dt := NewDateTime(now)
	data, err := codec.Marshal(dt)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	result, err := val.Interface()
	require.NoError(t, err)
	extractedTime, ok := result.(time.Time)
	require.True(t, ok, "Interface() should auto-decode DateTime to time.Time")
	assert.True(t, extractedTime.Equal(now))

	// Test Bytes auto-decode
	testBytes := []byte("hello")
	bytes := NewBytes(testBytes)
	data2, err := codec.Marshal(bytes)
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	result2, err := val2.Interface()
	require.NoError(t, err)
	extractedBytes, ok := result2.([]byte)
	require.True(t, ok, "Interface() should auto-decode Bytes to []byte")
	assert.Equal(t, testBytes, extractedBytes)

	// Test non-enhanced returns as-is
	testObj := map[string]any{"name": "test", "value": 42}
	data3, err := codec.Marshal(testObj)
	require.NoError(t, err)

	val3 := NewValueWithCodec(data3, codec)
	result3, err := val3.Interface()
	require.NoError(t, err)
	resultObj, ok := result3.(map[string]any)
	require.True(t, ok, "Interface() should return map[string]any for regular object")
	assert.Equal(t, "test", resultObj["name"])
	// Note: JSON unmarshaling converts numbers to float64
	assert.Equal(t, float64(42), resultObj["value"])
}

// TestValue_Detection_WithCBOR tests detection with CBOR encoding
func TestValue_Detection_WithCBOR(t *testing.T) {
	codec := GetCodec("application/cbor")

	// Test reference detection with CBOR
	ref := NewReference("cbor-ref-123")
	data, err := codec.Marshal(ref)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	assert.True(t, val.IsReference())
	extractedRef, err := val.AsReference()
	require.NoError(t, err)
	assert.Equal(t, "cbor-ref-123", extractedRef.Ref)

	// Test enhanced type with CBOR
	testBytes := []byte{0x01, 0x02, 0x03, 0xFF}
	bytes := NewBytes(testBytes)
	data2, err := codec.Marshal(bytes)
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	typeName, isEnhanced := val2.IsEnhanced()
	assert.True(t, isEnhanced)
	assert.Equal(t, TypeBytes, typeName)

	enhanced, err := val2.AsEnhanced()
	require.NoError(t, err)
	extractedBytes, ok := enhanced.([]byte)
	require.True(t, ok)
	assert.Equal(t, testBytes, extractedBytes)
}

// TestValue_Kind_BasicTypes tests Kind detection for basic types
func TestValue_Kind_BasicTypes(t *testing.T) {
	codec := GetCodec("application/json")

	tests := []struct {
		name     string
		value    any
		expected Kind
	}{
		{"null", nil, NullKind},
		{"bool", true, BoolKind},
		{"int", 42, FloatKind}, // JSON numbers are float64
		{"float", 3.14, FloatKind},
		{"string", "hello", StringKind},
		{"array", []any{1, 2, 3}, ArrayKind},
		{"object", map[string]any{"key": "value"}, ObjectKind},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := codec.Marshal(tt.value)
			require.NoError(t, err)

			val := NewValueWithCodec(data, codec)
			assert.Equal(t, tt.expected, val.Kind(), "Kind mismatch for %s", tt.name)
		})
	}
}

// TestValue_Kind_Reference tests Kind detection for references
func TestValue_Kind_Reference(t *testing.T) {
	codec := GetCodec("application/json")

	ref := NewReference("test-ref")
	data, err := codec.Marshal(ref)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	assert.Equal(t, ReferenceKind, val.Kind())
	assert.Equal(t, "reference", val.Kind().String())
}

// TestValue_Kind_EnhancedTypes tests Kind detection for all enhanced types
func TestValue_Kind_EnhancedTypes(t *testing.T) {
	codec := GetCodec("application/json")

	tests := []struct {
		name     string
		value    any
		expected Kind
		kindStr  string
	}{
		{"datetime", NewDateTime(time.Now()), DateTimeKind, "datetime"},
		{"bytes", NewBytes([]byte("test")), BytesKind, "bytes"},
		{"bigint", NewBigInt(big.NewInt(123)), BigIntKind, "bigint"},
		{"regexp", NewRegExp(regexp.MustCompile("test")), RegExpKind, "regexp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := codec.Marshal(tt.value)
			require.NoError(t, err)

			val := NewValueWithCodec(data, codec)
			assert.Equal(t, tt.expected, val.Kind(), "Kind mismatch for %s", tt.name)
			assert.Equal(t, tt.kindStr, val.Kind().String(), "Kind string mismatch for %s", tt.name)
		})
	}
}

// TestValue_Kind_WithCBOR tests Kind detection with CBOR encoding
func TestValue_Kind_WithCBOR(t *testing.T) {
	codec := GetCodec("application/cbor")

	// Test Reference
	ref := NewReference("cbor-ref")
	data, err := codec.Marshal(ref)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	assert.Equal(t, ReferenceKind, val.Kind())

	// Test enhanced type
	bytes := NewBytes([]byte{0x01, 0x02, 0x03})
	data2, err := codec.Marshal(bytes)
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	assert.Equal(t, BytesKind, val2.Kind())
}

// TestValue_ReferenceAccessor tests the Reference() accessor method
func TestValue_ReferenceAccessor(t *testing.T) {
	codec := GetCodec("application/json")

	// Test with valid reference
	ref := NewReference("test-ref-456")
	data, err := codec.Marshal(ref)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	result := val.Reference()
	assert.Equal(t, "test-ref-456", result.Ref)

	// Test with non-reference returns empty Reference
	data2, err := codec.Marshal("not a reference")
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	result2 := val2.Reference()
	assert.Equal(t, Reference{}, result2)
	assert.Empty(t, result2.Ref)
}

// TestValue_DateTimeAccessor tests the DateTime() accessor method
func TestValue_DateTimeAccessor(t *testing.T) {
	codec := GetCodec("application/json")

	// Test with valid DateTime
	testTime := time.Date(2025, 10, 31, 12, 30, 0, 0, time.UTC)
	dt := NewDateTime(testTime)
	data, err := codec.Marshal(dt)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	result := val.DateTime()
	assert.True(t, result.Equal(testTime))

	// Test with non-datetime returns zero time
	data2, err := codec.Marshal("not a datetime")
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	result2 := val2.DateTime()
	assert.True(t, result2.IsZero())
}

// TestValue_BytesAccessor tests the Bytes() accessor method
func TestValue_BytesAccessor(t *testing.T) {
	codec := GetCodec("application/json")

	// Test with valid Bytes
	testBytes := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	b := NewBytes(testBytes)
	data, err := codec.Marshal(b)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	result := val.Bytes()
	assert.Equal(t, testBytes, result)

	// Test with non-bytes returns nil
	data2, err := codec.Marshal("not bytes")
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	result2 := val2.Bytes()
	assert.Nil(t, result2)
}

// TestValue_BigIntAccessor tests the BigInt() accessor method
func TestValue_BigIntAccessor(t *testing.T) {
	codec := GetCodec("application/json")

	// Test with valid BigInt
	testBigInt := big.NewInt(999999999999999999)
	bi := NewBigInt(testBigInt)
	data, err := codec.Marshal(bi)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	result := val.BigInt()
	require.NotNil(t, result)
	assert.Equal(t, testBigInt.String(), result.String())

	// Test with non-bigint returns nil
	data2, err := codec.Marshal(42)
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	result2 := val2.BigInt()
	assert.Nil(t, result2)
}

// TestValue_RegExpAccessor tests the RegExp() accessor method
func TestValue_RegExpAccessor(t *testing.T) {
	codec := GetCodec("application/json")

	// Test with valid RegExp
	testPattern := regexp.MustCompile(`^test\d+$`)
	re := NewRegExp(testPattern)
	data, err := codec.Marshal(re)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	result := val.RegExp()
	require.NotNil(t, result)
	assert.Equal(t, testPattern.String(), result.String())
	assert.True(t, result.MatchString("test123"))
	assert.False(t, result.MatchString("test"))

	// Test with non-regexp returns nil
	data2, err := codec.Marshal("not a regexp")
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	result2 := val2.RegExp()
	assert.Nil(t, result2)
}

// TestValue_AccessorConsistency tests that accessor methods are consistent with Kind
func TestValue_AccessorConsistency(t *testing.T) {
	codec := GetCodec("application/json")

	tests := []struct {
		name     string
		value    any
		kind     Kind
		accessor func(v *Value) bool
	}{
		{
			name:  "reference",
			value: NewReference("test"),
			kind:  ReferenceKind,
			accessor: func(v *Value) bool {
				return v.Reference().Ref != ""
			},
		},
		{
			name:  "datetime",
			value: NewDateTime(time.Now()),
			kind:  DateTimeKind,
			accessor: func(v *Value) bool {
				return !v.DateTime().IsZero()
			},
		},
		{
			name:  "bytes",
			value: NewBytes([]byte("test")),
			kind:  BytesKind,
			accessor: func(v *Value) bool {
				return v.Bytes() != nil
			},
		},
		{
			name:  "bigint",
			value: NewBigInt(big.NewInt(123)),
			kind:  BigIntKind,
			accessor: func(v *Value) bool {
				return v.BigInt() != nil
			},
		},
		{
			name:  "regexp",
			value: NewRegExp(regexp.MustCompile("test")),
			kind:  RegExpKind,
			accessor: func(v *Value) bool {
				return v.RegExp() != nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := codec.Marshal(tt.value)
			require.NoError(t, err)

			val := NewValueWithCodec(data, codec)
			assert.Equal(t, tt.kind, val.Kind())
			assert.True(t, tt.accessor(&val), "Accessor should return non-zero value for matching kind")
		})
	}
}

// TestValue_MarshalJSON tests JSON marshaling of Value
func TestValue_MarshalJSON(t *testing.T) {
	codec := GetCodec("application/json")

	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{"null", nil, "null"},
		{"bool", true, "true"},
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{"string", "hello", `"hello"`},
		{"array", []any{1, 2, 3}, "[1,2,3]"},
		{"object", map[string]any{"key": "value"}, `{"key":"value"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := codec.Marshal(tt.value)
			require.NoError(t, err)

			val := NewValueWithCodec(data, codec)
			marshaled, err := val.MarshalJSON()
			require.NoError(t, err)

			assert.JSONEq(t, tt.expected, string(marshaled))
		})
	}
}

// TestValue_MarshalJSON_Reference tests JSON marshaling of Reference values
func TestValue_MarshalJSON_Reference(t *testing.T) {
	codec := GetCodec("application/json")

	ref := NewReference("test-ref-789")
	data, err := codec.Marshal(ref)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	marshaled, err := val.MarshalJSON()
	require.NoError(t, err)

	// Should marshal to {"$ref":"test-ref-789"}
	var result Reference
	err = json.Unmarshal(marshaled, &result)
	require.NoError(t, err)
	assert.Equal(t, "test-ref-789", result.Ref)
}

// TestValue_MarshalJSON_EnhancedTypes tests JSON marshaling of enhanced types
func TestValue_MarshalJSON_EnhancedTypes(t *testing.T) {
	codec := GetCodec("application/json")

	// Test DateTime
	testTime := time.Date(2025, 10, 31, 12, 30, 0, 0, time.UTC)
	dt := NewDateTime(testTime)
	data, err := codec.Marshal(dt)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	marshaled, err := val.MarshalJSON()
	require.NoError(t, err)

	var result DateTime
	err = json.Unmarshal(marshaled, &result)
	require.NoError(t, err)
	assert.Equal(t, TypeDateTime, result.Type)
	assert.True(t, result.Value.Equal(testTime))

	// Test Bytes
	testBytes := []byte{0xCA, 0xFE, 0xBA, 0xBE}
	b := NewBytes(testBytes)
	data2, err := codec.Marshal(b)
	require.NoError(t, err)

	val2 := NewValueWithCodec(data2, codec)
	marshaled2, err := val2.MarshalJSON()
	require.NoError(t, err)

	var result2 Bytes
	err = json.Unmarshal(marshaled2, &result2)
	require.NoError(t, err)
	assert.Equal(t, testBytes, result2.Value)
}

// TestValue_MarshalCBOR tests CBOR marshaling of Value
func TestValue_MarshalCBOR(t *testing.T) {
	jsonCodec := GetCodec("application/json")
	cborCodec := GetCodec("application/cbor")

	tests := []struct {
		name  string
		value any
	}{
		{"null", nil},
		{"bool", true},
		{"int", 42},
		{"float", 3.14},
		{"string", "hello"},
		{"array", []any{1, 2, 3}},
		{"object", map[string]any{"key": "value"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create Value from JSON
			data, err := jsonCodec.Marshal(tt.value)
			require.NoError(t, err)

			val := NewValueWithCodec(data, jsonCodec)

			// Marshal to CBOR
			cborData, err := val.MarshalCBOR()
			require.NoError(t, err)

			// Unmarshal back and verify
			var result any
			err = cborCodec.Unmarshal(cborData, &result)
			require.NoError(t, err)

			// For comparing, we need to normalize through JSON first
			// because JSON doesn't distinguish between int and float
			jsonOriginal, _ := jsonCodec.Marshal(tt.value)
			var jsonNormalized any
			jsonCodec.Unmarshal(jsonOriginal, &jsonNormalized)

			// Now marshal both to CBOR and compare
			cborExpected, _ := cborCodec.Marshal(jsonNormalized)
			var expectedResult any
			cborCodec.Unmarshal(cborExpected, &expectedResult)

			assert.Equal(t, expectedResult, result)
		})
	}
}

// TestValue_MarshalCBOR_Reference tests CBOR marshaling of Reference values
func TestValue_MarshalCBOR_Reference(t *testing.T) {
	codec := GetCodec("application/cbor")

	ref := NewReference("cbor-ref-999")
	data, err := codec.Marshal(ref)
	require.NoError(t, err)

	val := NewValueWithCodec(data, codec)
	marshaled, err := val.MarshalCBOR()
	require.NoError(t, err)

	// Unmarshal and verify
	var result Reference
	err = cbor.Unmarshal(marshaled, &result)
	require.NoError(t, err)
	assert.Equal(t, "cbor-ref-999", result.Ref)
}

// TestValue_MarshalInNestedStructure tests Value in nested structures
func TestValue_MarshalInNestedStructure(t *testing.T) {
	codec := GetCodec("application/json")

	// Create a Value
	data, err := codec.Marshal(map[string]any{"nested": "value"})
	require.NoError(t, err)
	val := NewValueWithCodec(data, codec)

	// Embed in a structure
	type Container struct {
		ID     int    `json:"id"`
		Result Value  `json:"result"`
		Status string `json:"status"`
	}

	container := Container{
		ID:     123,
		Result: val,
		Status: "ok",
	}

	// Marshal the container
	marshaled, err := json.Marshal(container)
	require.NoError(t, err)

	// Unmarshal and verify
	var result struct {
		ID     int            `json:"id"`
		Result map[string]any `json:"result"`
		Status string         `json:"status"`
	}
	err = json.Unmarshal(marshaled, &result)
	require.NoError(t, err)

	assert.Equal(t, 123, result.ID)
	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, "value", result.Result["nested"])
}
