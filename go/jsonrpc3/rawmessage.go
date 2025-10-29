package jsonrpc3

import (
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
)

var (
	// Standard CBOR encoding mode
	cborEncMode cbor.EncMode

	// Compact CBOR encoding mode (canonical, sorted keys, no indefinite lengths)
	cborCompactEncMode cbor.EncMode
)

func init() {
	// Create standard CBOR encoding mode
	var err error
	cborEncMode, err = cbor.EncOptions{}.EncMode()
	if err != nil {
		panic(err)
	}

	// Create compact CBOR encoding mode with canonical settings
	cborCompactEncMode, err = cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		panic(err)
	}
}

// RawMessage is a raw encoded message that can handle both JSON and CBOR formats.
// It implements the marshaling interfaces for both json and cbor packages.
type RawMessage []byte

// MarshalJSON returns m as the JSON encoding of m.
// It implements json.Marshaler.
func (m RawMessage) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return m, nil
}

// UnmarshalJSON sets *m to a copy of data.
// It implements json.Unmarshaler.
func (m *RawMessage) UnmarshalJSON(data []byte) error {
	if m == nil {
		return &json.InvalidUnmarshalError{Type: nil}
	}
	*m = append((*m)[0:0], data...)
	return nil
}

// MarshalCBOR returns m as the CBOR encoding of m.
// It implements cbor.Marshaler.
func (m RawMessage) MarshalCBOR() ([]byte, error) {
	if m == nil {
		return cbor.Marshal(nil)
	}
	return m, nil
}

// UnmarshalCBOR sets *m to a copy of data.
// It implements cbor.Unmarshaler.
func (m *RawMessage) UnmarshalCBOR(data []byte) error {
	if m == nil {
		return &cbor.UnmarshalTypeError{}
	}
	*m = append((*m)[0:0], data...)
	return nil
}

// MarshalValue encodes a value into a RawMessage using the specified mimetype.
// Supported mimetypes:
//   - "json" or "application/json": JSON encoding
//   - "cbor" or "application/cbor": Standard CBOR encoding
//   - "application/cbor; format=compact": Compact/canonical CBOR encoding
func MarshalValue(v any, mimetype string) (RawMessage, error) {
	if v == nil {
		return nil, nil
	}

	codec := GetCodec(mimetype)
	data, err := codec.Marshal(v)
	if err != nil {
		return nil, err
	}
	return RawMessage(data), nil
}

// UnmarshalValue decodes a RawMessage into a value using the specified mimetype.
// Supported mimetypes:
//   - "json" or "application/json": JSON decoding
//   - "cbor" or "application/cbor": CBOR decoding (works for both standard and compact)
//   - "application/cbor; format=compact": CBOR decoding (compact format is encoding-only)
func (m RawMessage) UnmarshalValue(v any, mimetype string) error {
	if len(m) == 0 {
		return nil
	}

	codec := GetCodec(mimetype)
	return codec.Unmarshal(m, v)
}
