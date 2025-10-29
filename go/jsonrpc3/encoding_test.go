package jsonrpc3

import (
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCodec_JSON(t *testing.T) {
	tests := []struct {
		name     string
		mimetype string
	}{
		{
			name:     "json mimetype",
			mimetype: "json",
		},
		{
			name:     "application/json mimetype",
			mimetype: "application/json",
		},
		{
			name:     "empty string defaults to json",
			mimetype: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := GetCodec(tt.mimetype)

			// Test marshaling
			data, err := codec.Marshal("hello")
			require.NoError(t, err, "Marshal should not error")
			assert.Equal(t, `"hello"`, string(data))

			// Test unmarshaling
			var result string
			err = codec.Unmarshal(data, &result)
			require.NoError(t, err, "Unmarshal should not error")
			assert.Equal(t, "hello", result)
		})
	}
}

func TestGetCodec_CBOR(t *testing.T) {
	tests := []struct {
		name     string
		mimetype string
	}{
		{
			name:     "cbor string",
			mimetype: "cbor",
		},
		{
			name:     "application/cbor mimetype",
			mimetype: "application/cbor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := GetCodec(tt.mimetype)

			// Test marshaling
			testData := map[string]any{"key": "value", "num": 42}
			data, err := codec.Marshal(testData)
			require.NoError(t, err, "Marshal should not error")

			// Verify it's CBOR with string keys
			var stringMap map[string]any
			err = cbor.Unmarshal(data, &stringMap)
			require.NoError(t, err, "Should unmarshal as CBOR with string keys")
			assert.Contains(t, stringMap, "key")
			assert.Contains(t, stringMap, "num")

			// Test unmarshaling
			var result map[string]any
			err = codec.Unmarshal(data, &result)
			require.NoError(t, err, "Unmarshal should not error")
			assert.Equal(t, "value", result["key"])
			assert.Equal(t, uint64(42), result["num"]) // CBOR unmarshals positive integers as uint64
		})
	}
}

func TestGetCodec_CompactCBOR(t *testing.T) {
	codec := GetCodec("application/cbor; format=compact")

	// Test marshaling - should use compact mode
	req := &Request{
		JSONRPC: "3.0",
		Method:  "test",
		ID:      123,
	}

	// Marshal the request using codec
	data, err := codec.Marshal(req)
	require.NoError(t, err, "Marshal should not error")

	// Verify it uses string keys (since we're marshaling Request directly, not compactRequest)
	var stringMap map[string]any
	err = cbor.Unmarshal(data, &stringMap)
	require.NoError(t, err, "Should unmarshal as CBOR")

	// Test unmarshaling
	var decoded Request
	err = codec.Unmarshal(data, &decoded)
	require.NoError(t, err, "Unmarshal should not error")
	assert.Equal(t, "test", decoded.Method)
	assert.Equal(t, "3.0", decoded.JSONRPC)
}

func TestGetCodec_RoundTrip_JSON(t *testing.T) {
	codec := GetCodec("application/json")

	original := map[string]any{
		"name":   "Alice",
		"age":    30,
		"active": true,
	}

	// Marshal
	data, err := codec.Marshal(original)
	require.NoError(t, err, "Marshal should not error")

	// Unmarshal
	var result map[string]any
	err = codec.Unmarshal(data, &result)
	require.NoError(t, err, "Unmarshal should not error")

	assert.Equal(t, "Alice", result["name"])
	assert.Equal(t, float64(30), result["age"]) // JSON numbers are float64
	assert.Equal(t, true, result["active"])
}

func TestGetCodec_RoundTrip_CBOR(t *testing.T) {
	codec := GetCodec("application/cbor")

	original := map[string]any{
		"name":   "Bob",
		"age":    25,
		"active": false,
	}

	// Marshal
	data, err := codec.Marshal(original)
	require.NoError(t, err, "Marshal should not error")

	// Unmarshal
	var result map[string]any
	err = codec.Unmarshal(data, &result)
	require.NoError(t, err, "Unmarshal should not error")

	assert.Equal(t, "Bob", result["name"])
	assert.Equal(t, uint64(25), result["age"]) // CBOR unmarshals positive integers as uint64
	assert.Equal(t, false, result["active"])
}

func TestGetCodec_RoundTrip_CompactCBOR(t *testing.T) {
	codec := GetCodec("application/cbor; format=compact")

	original := map[string]any{
		"name":  "Charlie",
		"count": 100,
	}

	// Marshal
	data, err := codec.Marshal(original)
	require.NoError(t, err, "Marshal should not error")

	// Unmarshal
	var result map[string]any
	err = codec.Unmarshal(data, &result)
	require.NoError(t, err, "Unmarshal should not error")

	assert.Equal(t, "Charlie", result["name"])
	assert.Equal(t, uint64(100), result["count"]) // CBOR unmarshals positive integers as uint64
}

func TestCodec_MarshalNil(t *testing.T) {
	tests := []struct {
		name     string
		mimetype string
	}{
		{name: "JSON", mimetype: "application/json"},
		{name: "CBOR", mimetype: "application/cbor"},
		{name: "Compact CBOR", mimetype: "application/cbor; format=compact"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := GetCodec(tt.mimetype)

			data, err := codec.Marshal(nil)
			require.NoError(t, err, "Marshaling nil should not error")
			assert.NotNil(t, data, "Should return non-nil data")
		})
	}
}

func TestCodec_UnmarshalEmptyData(t *testing.T) {
	tests := []struct {
		name     string
		mimetype string
	}{
		{name: "JSON", mimetype: "application/json"},
		{name: "CBOR", mimetype: "application/cbor"},
		{name: "Compact CBOR", mimetype: "application/cbor; format=compact"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := GetCodec(tt.mimetype)

			var result string
			// Empty data should cause an error
			err := codec.Unmarshal([]byte{}, &result)
			assert.Error(t, err, "Unmarshaling empty data should error")
		})
	}
}

func TestCodec_ComplexStructures(t *testing.T) {
	type Person struct {
		Name    string   `json:"name" cbor:"name"`
		Age     int      `json:"age" cbor:"age"`
		Hobbies []string `json:"hobbies" cbor:"hobbies"`
	}

	tests := []struct {
		name     string
		mimetype string
	}{
		{name: "JSON", mimetype: "application/json"},
		{name: "CBOR", mimetype: "application/cbor"},
		{name: "Compact CBOR", mimetype: "application/cbor; format=compact"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := GetCodec(tt.mimetype)

			original := Person{
				Name:    "Dave",
				Age:     35,
				Hobbies: []string{"coding", "reading", "gaming"},
			}

			// Marshal
			data, err := codec.Marshal(original)
			require.NoError(t, err, "Marshal should not error")

			// Unmarshal
			var result Person
			err = codec.Unmarshal(data, &result)
			require.NoError(t, err, "Unmarshal should not error")

			assert.Equal(t, original.Name, result.Name)
			assert.Equal(t, original.Age, result.Age)
			assert.Equal(t, original.Hobbies, result.Hobbies)
		})
	}
}

func TestCodec_FormatComparison(t *testing.T) {
	data := map[string]any{
		"name":    "Test",
		"value":   123,
		"active":  true,
		"details": map[string]any{"key": "value"},
	}

	// Get codecs for each format
	jsonCodec := GetCodec("application/json")
	cborCodec := GetCodec("application/cbor")
	compactCodec := GetCodec("application/cbor; format=compact")

	// Marshal with each codec
	jsonData, err := jsonCodec.Marshal(data)
	require.NoError(t, err, "JSON marshal should not error")

	cborData, err := cborCodec.Marshal(data)
	require.NoError(t, err, "CBOR marshal should not error")

	compactData, err := compactCodec.Marshal(data)
	require.NoError(t, err, "Compact CBOR marshal should not error")

	t.Logf("JSON size: %d bytes", len(jsonData))
	t.Logf("Standard CBOR size: %d bytes", len(cborData))
	t.Logf("Compact CBOR size: %d bytes", len(compactData))

	// All formats should produce different encodings
	assert.NotEqual(t, string(jsonData), string(cborData), "JSON and CBOR should differ")

	// Note: Compact and standard CBOR might be similar for this simple structure
	// since the compact format's main benefit is for the Request/Response structures
	// with integer keys
}

// TestCodec_UsageExample demonstrates how to use the Codec abstraction
// to marshal and unmarshal data in a format-agnostic way.
func TestCodec_UsageExample(t *testing.T) {
	// Function that encodes data using any format
	encodeData := func(data any, mimetype string) ([]byte, error) {
		codec := GetCodec(mimetype)
		return codec.Marshal(data)
	}

	// Function that decodes data using any format
	decodeData := func(data []byte, mimetype string, result any) error {
		codec := GetCodec(mimetype)
		return codec.Unmarshal(data, result)
	}

	// Test with different formats
	original := map[string]string{"message": "hello", "status": "ok"}

	formats := []string{"application/json", "application/cbor", "application/cbor; format=compact"}
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			// Encode
			data, err := encodeData(original, format)
			require.NoError(t, err, "Encode should not error")

			// Decode
			var result map[string]string
			err = decodeData(data, format, &result)
			require.NoError(t, err, "Decode should not error")

			// Verify
			assert.Equal(t, "hello", result["message"])
			assert.Equal(t, "ok", result["status"])
		})
	}
}
