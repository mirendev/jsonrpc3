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

// TestDateTime_JSON tests DateTime marshaling/unmarshaling with JSON
func TestDateTime_JSON(t *testing.T) {
	// Create datetime
	now := time.Date(2025, 10, 27, 10, 30, 0, 123456789, time.UTC)
	dt := NewDateTime(now)

	// Marshal to JSON
	data, err := json.Marshal(dt)
	require.NoError(t, err)

	// Check structure
	var raw map[string]any
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	assert.Equal(t, TypeDateTime, raw["$type"])
	assert.Contains(t, raw["value"], "2025-10-27T10:30:00")

	// Unmarshal back
	var dt2 DateTime
	err = json.Unmarshal(data, &dt2)
	require.NoError(t, err)
	assert.Equal(t, TypeDateTime, dt2.Type)
	assert.True(t, now.Equal(dt2.Value), "Times should be equal")
}

// TestDateTime_Timezone tests DateTime with timezone
func TestDateTime_Timezone(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	dt := NewDateTime(time.Date(2025, 10, 27, 10, 30, 0, 0, loc))

	data, err := json.Marshal(dt)
	require.NoError(t, err)

	var dt2 DateTime
	err = json.Unmarshal(data, &dt2)
	require.NoError(t, err)
	assert.True(t, dt.Value.Equal(dt2.Value))
}

// TestBytes_JSON tests Bytes marshaling/unmarshaling with JSON
func TestBytes_JSON(t *testing.T) {
	// Create bytes
	original := []byte("Hello World")
	b := NewBytes(original)

	// Marshal to JSON
	data, err := json.Marshal(b)
	require.NoError(t, err)

	// Check structure
	var raw map[string]any
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	assert.Equal(t, TypeBytes, raw["$type"])
	assert.Equal(t, "SGVsbG8gV29ybGQ=", raw["value"]) // Base64 of "Hello World"

	// Unmarshal back
	var b2 Bytes
	err = json.Unmarshal(data, &b2)
	require.NoError(t, err)
	assert.Equal(t, TypeBytes, b2.Type)
	assert.Equal(t, original, b2.Value)
}

// TestBytes_CBOR tests Bytes marshaling/unmarshaling with CBOR
func TestBytes_CBOR(t *testing.T) {
	// Create bytes
	original := []byte("Hello World")
	b := NewBytes(original)

	// Marshal to CBOR
	data, err := cbor.Marshal(b)
	require.NoError(t, err)

	// Unmarshal back
	var b2 Bytes
	err = cbor.Unmarshal(data, &b2)
	require.NoError(t, err)
	assert.Equal(t, TypeBytes, b2.Type)
	assert.Equal(t, original, b2.Value)
}

// TestBytes_Empty tests empty byte array
func TestBytes_Empty(t *testing.T) {
	b := NewBytes([]byte{})

	data, err := json.Marshal(b)
	require.NoError(t, err)

	var b2 Bytes
	err = json.Unmarshal(data, &b2)
	require.NoError(t, err)
	assert.Equal(t, 0, len(b2.Value))
}

// TestBigInt_JSON tests BigInt marshaling/unmarshaling
func TestBigInt_JSON(t *testing.T) {
	// Create big int
	n := new(big.Int)
	n.SetString("12345678901234567890123456789", 10)
	bi := NewBigInt(n)

	// Marshal to JSON
	data, err := json.Marshal(bi)
	require.NoError(t, err)

	// Check structure
	var raw map[string]any
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	assert.Equal(t, TypeBigInt, raw["$type"])
	assert.Equal(t, "12345678901234567890123456789", raw["value"])

	// Unmarshal back
	var bi2 BigInt
	err = json.Unmarshal(data, &bi2)
	require.NoError(t, err)
	assert.Equal(t, TypeBigInt, bi2.Type)
	assert.Equal(t, 0, n.Cmp(bi2.Value), "BigInts should be equal")
}

// TestBigInt_Negative tests negative BigInt
func TestBigInt_Negative(t *testing.T) {
	n := new(big.Int)
	n.SetString("-999999999999999999999999999", 10)
	bi := NewBigInt(n)

	data, err := json.Marshal(bi)
	require.NoError(t, err)

	var bi2 BigInt
	err = json.Unmarshal(data, &bi2)
	require.NoError(t, err)
	assert.Equal(t, 0, n.Cmp(bi2.Value))
}

// TestBigInt_FromString tests creating BigInt from string
func TestBigInt_FromString(t *testing.T) {
	bi, err := NewBigIntFromString("123456789012345678901234567890")
	require.NoError(t, err)
	assert.Equal(t, TypeBigInt, bi.Type)

	// Test invalid string
	_, err = NewBigIntFromString("not a number")
	assert.Error(t, err)
}

// TestRegExp_JSON tests RegExp marshaling/unmarshaling
func TestRegExp_JSON(t *testing.T) {
	// Create regexp
	pattern := "\\d+"
	flags := "i"
	re, err := NewRegExpFromPattern(pattern, flags)
	require.NoError(t, err)

	// Marshal to JSON
	data, err := json.Marshal(re)
	require.NoError(t, err)

	// Check structure
	var raw map[string]any
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	assert.Equal(t, TypeRegExp, raw["$type"])
	assert.Equal(t, pattern, raw["pattern"])
	assert.Equal(t, flags, raw["flags"])

	// Unmarshal back
	var re2 RegExp
	err = json.Unmarshal(data, &re2)
	require.NoError(t, err)
	assert.Equal(t, TypeRegExp, re2.Type)
	assert.Equal(t, pattern, re2.Pattern)
	assert.Equal(t, flags, re2.Flags)

	// Test matching
	compiled := re2.Regexp()
	assert.NotNil(t, compiled)
	assert.True(t, compiled.MatchString("123"))
}

// TestRegExp_NoFlags tests RegExp without flags
func TestRegExp_NoFlags(t *testing.T) {
	re, err := NewRegExpFromPattern("[a-z]+", "")
	require.NoError(t, err)

	data, err := json.Marshal(re)
	require.NoError(t, err)

	var re2 RegExp
	err = json.Unmarshal(data, &re2)
	require.NoError(t, err)
	assert.Equal(t, "", re2.Flags)

	compiled := re2.Regexp()
	assert.True(t, compiled.MatchString("hello"))
	assert.False(t, compiled.MatchString("HELLO"))
}

// TestRegExp_FromCompiled tests creating RegExp from compiled regexp
func TestRegExp_FromCompiled(t *testing.T) {
	compiled := regexp.MustCompile("^test.*$")
	re := NewRegExp(compiled)

	assert.Equal(t, TypeRegExp, re.Type)
	assert.Equal(t, "^test.*$", re.Pattern)

	data, err := json.Marshal(re)
	require.NoError(t, err)

	var re2 RegExp
	err = json.Unmarshal(data, &re2)
	require.NoError(t, err)
	assert.True(t, re2.Regexp().MatchString("testing"))
}

// TestRegExp_InvalidPattern tests invalid regexp pattern
func TestRegExp_InvalidPattern(t *testing.T) {
	_, err := NewRegExpFromPattern("[invalid", "")
	assert.Error(t, err)
}

// TestIsEnhancedType tests enhanced type detection
func TestIsEnhancedType(t *testing.T) {
	// Enhanced type
	data := map[string]any{
		"$type": "datetime",
		"value": "2025-10-27T10:30:00Z",
	}
	typeStr, isEnhanced := IsEnhancedType(data)
	assert.True(t, isEnhanced)
	assert.Equal(t, "datetime", typeStr)

	// Not enhanced type
	data = map[string]any{
		"value": "2025-10-27T10:30:00Z",
	}
	_, isEnhanced = IsEnhancedType(data)
	assert.False(t, isEnhanced)

	// Not a map
	_, isEnhanced = IsEnhancedType("string")
	assert.False(t, isEnhanced)
}

// TestDecodeEnhancedType tests enhanced type decoding
func TestDecodeEnhancedType(t *testing.T) {
	codec := GetCodec("application/json")

	// DateTime
	dt := NewDateTime(time.Date(2025, 10, 27, 10, 30, 0, 0, time.UTC))
	dtJSON, _ := json.Marshal(dt)
	var dtMap map[string]any
	json.Unmarshal(dtJSON, &dtMap)

	decoded, err := DecodeEnhancedType(dtMap, codec)
	require.NoError(t, err)
	assert.IsType(t, time.Time{}, decoded)

	// Bytes
	b := NewBytes([]byte("test"))
	bJSON, _ := json.Marshal(b)
	var bMap map[string]any
	json.Unmarshal(bJSON, &bMap)

	decoded, err = DecodeEnhancedType(bMap, codec)
	require.NoError(t, err)
	assert.IsType(t, []byte{}, decoded)
	assert.Equal(t, []byte("test"), decoded)

	// BigInt
	bi := NewBigInt(big.NewInt(12345))
	biJSON, _ := json.Marshal(bi)
	var biMap map[string]any
	json.Unmarshal(biJSON, &biMap)

	decoded, err = DecodeEnhancedType(biMap, codec)
	require.NoError(t, err)
	assert.IsType(t, &big.Int{}, decoded)

	// RegExp
	re, _ := NewRegExpFromPattern("test", "")
	reJSON, _ := json.Marshal(re)
	var reMap map[string]any
	json.Unmarshal(reJSON, &reMap)

	decoded, err = DecodeEnhancedType(reMap, codec)
	require.NoError(t, err)
	assert.IsType(t, &regexp.Regexp{}, decoded)

	// Non-enhanced type
	normalData := map[string]any{"foo": "bar"}
	decoded, err = DecodeEnhancedType(normalData, codec)
	require.NoError(t, err)
	assert.Equal(t, normalData, decoded)

	// Unknown enhanced type
	unknownData := map[string]any{
		"$type": "unknown",
		"value": "test",
	}
	decoded, err = DecodeEnhancedType(unknownData, codec)
	require.NoError(t, err)
	assert.Equal(t, unknownData, decoded)
}

// TestEnhancedType_RoundTrip tests full round-trip for all types
func TestEnhancedType_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{
			name:  "DateTime",
			value: NewDateTime(time.Now()),
		},
		{
			name:  "Bytes",
			value: NewBytes([]byte("test data")),
		},
		{
			name:  "BigInt",
			value: NewBigInt(big.NewInt(999999999)),
		},
		{
			name: "RegExp",
			value: func() *RegExp {
				re, _ := NewRegExpFromPattern(".*", "")
				return re
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// JSON round-trip
			jsonData, err := json.Marshal(tt.value)
			require.NoError(t, err)

			// CBOR round-trip
			cborData, err := cbor.Marshal(tt.value)
			require.NoError(t, err)

			// Verify both produce valid output
			assert.NotEmpty(t, jsonData)
			assert.NotEmpty(t, cborData)
		})
	}
}
