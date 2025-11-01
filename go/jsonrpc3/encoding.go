package jsonrpc3

import (
	"encoding/json"
	"io"

	"github.com/fxamacker/cbor/v2"
)

// MIME type constants for JSON-RPC 3.0 encoding formats
const (
	MimeTypeJSON        = "application/json"
	MimeTypeCBOR        = "application/cbor"
	MimeTypeCBORCompact = "application/cbor; format=compact"
)

// MessageMarshaller marshals MessageSet to bytes.
type MessageMarshaller interface {
	MarshalMessages(msgs MessageSet) ([]byte, error)
}

// MessageUnmarshaller unmarshals bytes to MessageSet.
type MessageUnmarshaller interface {
	UnmarshalMessages(data []byte) (MessageSet, error)
}

// MessageEncoder encodes Message objects to a stream.
type MessageEncoder interface {
	Encode(msg *Message) error
}

// MessageDecoder decodes MessageSet objects from a stream.
// Each call to Decode reads one complete JSON/CBOR object, which could be
// a single message or a batch (array of messages).
type MessageDecoder interface {
	Decode() (MessageSet, error)
}

// MessageEncoderFactory creates MessageEncoders for streaming writes.
type MessageEncoderFactory interface {
	NewMessageEncoder(w io.Writer) MessageEncoder
}

// MessageDecoderFactory creates MessageDecoders for streaming reads.
type MessageDecoderFactory interface {
	NewMessageDecoder(r io.Reader) MessageDecoder
}

// Codec combines message marshalling, unmarshalling, and streaming encoding/decoding for a specific format.
type Codec interface {
	MessageMarshaller
	MessageUnmarshaller
	MessageDecoderFactory
	MessageEncoderFactory
	Marshaller   // Legacy support for generic encoding
	Unmarshaller // Legacy support for generic decoding
	MimeType() string
}

// Legacy interfaces for generic encoding (kept for backward compatibility with RawMessage encoding)

// Marshaller provides an abstraction for encoding data.
type Marshaller interface {
	Marshal(v any) ([]byte, error)
}

// Unmarshaller provides an abstraction for decoding data.
type Unmarshaller interface {
	Unmarshal(data []byte, v any) error
}

// jsonCodec implements JSON encoding/decoding.
type jsonCodec struct {
	mimetype string
}

func (c jsonCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (c jsonCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (c jsonCodec) MimeType() string {
	return c.mimetype
}

// MarshalMessages marshals a MessageSet to JSON bytes.
func (c jsonCodec) MarshalMessages(msgs MessageSet) ([]byte, error) {
	if !msgs.IsBatch {
		// Single message - encode without array wrapper
		return json.Marshal(msgs.Messages[0])
	}
	// Batch - encode as array
	return json.Marshal(msgs.Messages)
}

// UnmarshalMessages unmarshals JSON bytes to a MessageSet.
func (c jsonCodec) UnmarshalMessages(data []byte) (MessageSet, error) {
	// Detect batch by checking for leading [ character
	for _, b := range data {
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		if b == '[' {
			// Batch request/response
			var messages []Message
			if err := json.Unmarshal(data, &messages); err != nil {
				return MessageSet{}, err
			}
			msgSet := MessageSet{Messages: messages, IsBatch: true}
			// Set format on all messages
			for i := range msgSet.Messages {
				msgSet.Messages[i].SetFormat(c.mimetype)
			}
			return msgSet, nil
		}
		break
	}

	// Single message
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return MessageSet{}, err
	}

	msgSet := MessageSet{Messages: []Message{msg}, IsBatch: false}
	// Set format on all messages
	for i := range msgSet.Messages {
		msgSet.Messages[i].SetFormat(c.mimetype)
	}
	return msgSet, nil
}

// jsonMessageEncoder implements MessageEncoder for JSON.
type jsonMessageEncoder struct {
	enc *json.Encoder
}

func (e *jsonMessageEncoder) Encode(msg *Message) error {
	return e.enc.Encode(msg)
}

// NewMessageEncoder creates a JSON MessageEncoder.
func (c jsonCodec) NewMessageEncoder(w io.Writer) MessageEncoder {
	return &jsonMessageEncoder{enc: json.NewEncoder(w)}
}

// jsonMessageDecoder implements MessageDecoder for JSON.
type jsonMessageDecoder struct {
	dec   *json.Decoder
	codec jsonCodec
}

func (d *jsonMessageDecoder) Decode() (MessageSet, error) {
	// Decode one JSON value from stream (could be object or array)
	var raw json.RawMessage
	if err := d.dec.Decode(&raw); err != nil {
		return MessageSet{}, err
	}

	// Use UnmarshalMessages which has batch detection logic
	return d.codec.UnmarshalMessages(raw)
}

// NewMessageDecoder creates a JSON MessageDecoder.
func (c jsonCodec) NewMessageDecoder(r io.Reader) MessageDecoder {
	return &jsonMessageDecoder{dec: json.NewDecoder(r), codec: c}
}

// cborCodec implements standard CBOR encoding/decoding with string keys.
type cborCodec struct {
	mimetype string
}

func (c cborCodec) Marshal(v any) ([]byte, error) {
	return cborEncMode.Marshal(v)
}

func (c cborCodec) Unmarshal(data []byte, v any) error {
	return cbor.Unmarshal(data, v)
}

func (c cborCodec) MimeType() string {
	return c.mimetype
}

// MarshalMessages marshals a MessageSet to CBOR bytes.
func (c cborCodec) MarshalMessages(msgs MessageSet) ([]byte, error) {
	if !msgs.IsBatch {
		// Single message - encode without array wrapper
		return cborEncMode.Marshal(msgs.Messages[0])
	}
	// Batch - encode as array
	return cborEncMode.Marshal(msgs.Messages)
}

// UnmarshalMessages unmarshals CBOR bytes to a MessageSet.
func (c cborCodec) UnmarshalMessages(data []byte) (MessageSet, error) {
	// Try to decode as a batch first
	var messages []Message
	if err := cbor.Unmarshal(data, &messages); err == nil && len(messages) > 0 {
		// Check if it's actually a batch by seeing if the first element looks like a message
		if messages[0].JSONRPC != "" || messages[0].Method != "" || messages[0].Result != nil || messages[0].Error != nil {
			msgSet := MessageSet{Messages: messages, IsBatch: true}
			// Set format on all messages
			for i := range msgSet.Messages {
				msgSet.Messages[i].SetFormat(c.mimetype)
			}
			return msgSet, nil
		}
	}

	// Try as single message
	var msg Message
	if err := cbor.Unmarshal(data, &msg); err != nil {
		return MessageSet{}, err
	}
	msgSet := MessageSet{Messages: []Message{msg}, IsBatch: false}
	// Set format on all messages
	for i := range msgSet.Messages {
		msgSet.Messages[i].SetFormat(c.mimetype)
	}
	return msgSet, nil
}

// cborMessageEncoder implements MessageEncoder for CBOR.
type cborMessageEncoder struct {
	enc *cbor.Encoder
}

func (e *cborMessageEncoder) Encode(msg *Message) error {
	return e.enc.Encode(msg)
}

// NewMessageEncoder creates a CBOR MessageEncoder.
func (c cborCodec) NewMessageEncoder(w io.Writer) MessageEncoder {
	return &cborMessageEncoder{enc: cborEncMode.NewEncoder(w)}
}

// cborMessageDecoder implements MessageDecoder for CBOR.
type cborMessageDecoder struct {
	dec   *cbor.Decoder
	codec cborCodec
}

func (d *cborMessageDecoder) Decode() (MessageSet, error) {
	// Decode one CBOR value from stream (could be object or array)
	var raw RawMessage
	if err := d.dec.Decode(&raw); err != nil {
		return MessageSet{}, err
	}

	// Use UnmarshalMessages which has batch detection logic
	return d.codec.UnmarshalMessages(raw)
}

// NewMessageDecoder creates a CBOR MessageDecoder.
func (c cborCodec) NewMessageDecoder(r io.Reader) MessageDecoder {
	return &cborMessageDecoder{dec: cbor.NewDecoder(r), codec: c}
}

// compactCBORCodec implements compact CBOR encoding with integer keys.
// Decoding uses the standard CBOR unmarshaler since it handles both formats.
type compactCBORCodec struct {
	mimetype string
}

func (c compactCBORCodec) Marshal(v any) ([]byte, error) {
	return cborCompactEncMode.Marshal(v)
}

func (c compactCBORCodec) Unmarshal(data []byte, v any) error {
	// CBOR unmarshaler handles both standard and compact formats
	return cbor.Unmarshal(data, v)
}

func (c compactCBORCodec) MimeType() string {
	return c.mimetype
}

// MarshalMessages marshals a MessageSet to compact CBOR bytes with integer keys.
func (c compactCBORCodec) MarshalMessages(msgs MessageSet) ([]byte, error) {
	if !msgs.IsBatch {
		// Single message - convert to compact format and encode
		compact := toCompactMessage(&msgs.Messages[0])
		return cborCompactEncMode.Marshal(compact)
	}
	// Batch - convert all messages to compact format and encode as array
	compactMsgs := make([]compactMessage, len(msgs.Messages))
	for i, msg := range msgs.Messages {
		compactMsgs[i] = *toCompactMessage(&msg)
	}
	return cborCompactEncMode.Marshal(compactMsgs)
}

// UnmarshalMessages unmarshals compact CBOR bytes to a MessageSet.
func (c compactCBORCodec) UnmarshalMessages(data []byte) (MessageSet, error) {
	// Try to decode as a batch first
	var compactMsgs []compactMessage
	if err := cbor.Unmarshal(data, &compactMsgs); err == nil && len(compactMsgs) > 0 {
		// Check if it's actually a batch by seeing if the first element looks like a message
		if compactMsgs[0].JSONRPC != "" || compactMsgs[0].Method != "" || compactMsgs[0].Result != nil || compactMsgs[0].Error != nil {
			messages := make([]Message, len(compactMsgs))
			for i, cm := range compactMsgs {
				messages[i] = *fromCompactMessage(&cm)
			}
			msgSet := MessageSet{Messages: messages, IsBatch: true}
			// Set format on all messages
			for i := range msgSet.Messages {
				msgSet.Messages[i].SetFormat(c.mimetype)
			}
			return msgSet, nil
		}
	}

	// Try as single message
	var compactMsg compactMessage
	if err := cbor.Unmarshal(data, &compactMsg); err != nil {
		return MessageSet{}, err
	}
	msg := fromCompactMessage(&compactMsg)
	msgSet := MessageSet{Messages: []Message{*msg}, IsBatch: false}
	// Set format on all messages
	for i := range msgSet.Messages {
		msgSet.Messages[i].SetFormat(c.mimetype)
	}
	return msgSet, nil
}

// compactCBORMessageEncoder implements MessageEncoder for compact CBOR with integer keys.
type compactCBORMessageEncoder struct {
	enc *cbor.Encoder
}

func (e *compactCBORMessageEncoder) Encode(msg *Message) error {
	// Convert to compact format and encode
	compact := toCompactMessage(msg)
	return e.enc.Encode(compact)
}

// NewMessageEncoder creates a compact CBOR MessageEncoder.
func (c compactCBORCodec) NewMessageEncoder(w io.Writer) MessageEncoder {
	return &compactCBORMessageEncoder{enc: cborCompactEncMode.NewEncoder(w)}
}

// compactCBORMessageDecoder implements MessageDecoder for compact CBOR with integer keys.
type compactCBORMessageDecoder struct {
	dec   *cbor.Decoder
	codec compactCBORCodec
}

func (d *compactCBORMessageDecoder) Decode() (MessageSet, error) {
	// Decode one CBOR value from stream (could be object or array)
	var raw RawMessage
	if err := d.dec.Decode(&raw); err != nil {
		return MessageSet{}, err
	}

	// Use UnmarshalMessages which has batch detection logic
	return d.codec.UnmarshalMessages(raw)
}

// NewMessageDecoder creates a compact CBOR MessageDecoder.
func (c compactCBORCodec) NewMessageDecoder(r io.Reader) MessageDecoder {
	return &compactCBORMessageDecoder{dec: cbor.NewDecoder(r), codec: c}
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
			return compactCBORCodec{mimetype: mimetype}
		}
		return cborCodec{mimetype: mimetype}
	}

	// Handle bare "cbor" string for backward compatibility
	if mimetype == "cbor" {
		return cborCodec{mimetype: MimeTypeCBOR}
	}

	// Default to JSON
	return jsonCodec{mimetype: mimetype}
}
