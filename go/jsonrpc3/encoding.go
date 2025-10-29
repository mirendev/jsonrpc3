package jsonrpc3

import (
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
)

// Marshaller provides an abstraction for encoding data.
type Marshaller interface {
	Marshal(v any) ([]byte, error)
}

// Unmarshaller provides an abstraction for decoding data.
type Unmarshaller interface {
	Unmarshal(data []byte, v any) error
}

// Codec combines a Marshaller and Unmarshaller for a specific encoding format.
type Codec struct {
	Marshaller
	Unmarshaller
}

// jsonCodec implements JSON encoding/decoding.
type jsonCodec struct{}

func (c jsonCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (c jsonCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// cborCodec implements standard CBOR encoding/decoding with string keys.
type cborCodec struct{}

func (c cborCodec) Marshal(v any) ([]byte, error) {
	return cborEncMode.Marshal(v)
}

func (c cborCodec) Unmarshal(data []byte, v any) error {
	return cbor.Unmarshal(data, v)
}

// compactCBORCodec implements compact CBOR encoding with integer keys.
// Decoding uses the standard CBOR unmarshaler since it handles both formats.
type compactCBORCodec struct{}

func (c compactCBORCodec) Marshal(v any) ([]byte, error) {
	return cborCompactEncMode.Marshal(v)
}

func (c compactCBORCodec) Unmarshal(data []byte, v any) error {
	// CBOR unmarshaler handles both standard and compact formats
	return cbor.Unmarshal(data, v)
}

// GetCodec returns the appropriate Codec for the given mimetype.
// Supported mimetypes:
//   - "json" or "application/json": JSON encoding/decoding
//   - "cbor" or "application/cbor": Standard CBOR encoding/decoding (string keys)
//   - "application/cbor; format=compact": Compact CBOR encoding (integer keys)
//
// Returns a Codec containing both Marshaller and Unmarshaller.
func GetCodec(mimetype string) Codec {
	mt := ParseMimeType(mimetype)

	if mt.IsCBOR() {
		if mt.IsCompact() {
			return Codec{compactCBORCodec{}, compactCBORCodec{}}
		}
		return Codec{cborCodec{}, cborCodec{}}
	}

	// Handle bare "cbor" string for backward compatibility
	if mimetype == "cbor" {
		return Codec{cborCodec{}, cborCodec{}}
	}

	// Default to JSON
	return Codec{jsonCodec{}, jsonCodec{}}
}
