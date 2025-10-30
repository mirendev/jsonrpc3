package jsonrpc3

import (
	"encoding/json"
	"io"

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

// Decoder provides an abstraction for streaming decoding from an io.Reader.
type Decoder interface {
	Decode(v any) error
}

// Encoder provides an abstraction for streaming encoding to an io.Writer.
type Encoder interface {
	Encode(v any) error
}

// DecoderFactory creates a new Decoder for streaming reads.
type DecoderFactory interface {
	NewDecoder(r io.Reader) Decoder
}

// EncoderFactory creates a new Encoder for streaming writes.
type EncoderFactory interface {
	NewEncoder(w io.Writer) Encoder
}

// Codec combines marshalling, unmarshalling, and streaming encoding/decoding for a specific format.
type Codec struct {
	Marshaller
	Unmarshaller
	DecoderFactory
	EncoderFactory
}

// jsonCodec implements JSON encoding/decoding.
type jsonCodec struct{}

func (c jsonCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (c jsonCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (c jsonCodec) NewDecoder(r io.Reader) Decoder {
	return json.NewDecoder(r)
}

func (c jsonCodec) NewEncoder(w io.Writer) Encoder {
	return json.NewEncoder(w)
}

// cborCodec implements standard CBOR encoding/decoding with string keys.
type cborCodec struct{}

func (c cborCodec) Marshal(v any) ([]byte, error) {
	return cborEncMode.Marshal(v)
}

func (c cborCodec) Unmarshal(data []byte, v any) error {
	return cbor.Unmarshal(data, v)
}

func (c cborCodec) NewDecoder(r io.Reader) Decoder {
	return cbor.NewDecoder(r)
}

func (c cborCodec) NewEncoder(w io.Writer) Encoder {
	return cborEncMode.NewEncoder(w)
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

func (c compactCBORCodec) NewDecoder(r io.Reader) Decoder {
	return cbor.NewDecoder(r)
}

func (c compactCBORCodec) NewEncoder(w io.Writer) Encoder {
	return cborCompactEncMode.NewEncoder(w)
}

// GetCodec returns the appropriate Codec for the given mimetype.
// Supported mimetypes:
//   - "json" or "application/json": JSON encoding/decoding
//   - "cbor" or "application/cbor": Standard CBOR encoding/decoding (string keys)
//   - "application/cbor; format=compact": Compact CBOR encoding (integer keys)
//
// Returns a Codec containing marshalling, unmarshalling, and streaming support.
func GetCodec(mimetype string) Codec {
	mt := ParseMimeType(mimetype)

	if mt.IsCBOR() {
		if mt.IsCompact() {
			c := compactCBORCodec{}
			return Codec{c, c, c, c}
		}
		c := cborCodec{}
		return Codec{c, c, c, c}
	}

	// Handle bare "cbor" string for backward compatibility
	if mimetype == "cbor" {
		c := cborCodec{}
		return Codec{c, c, c, c}
	}

	// Default to JSON
	c := jsonCodec{}
	return Codec{c, c, c, c}
}
