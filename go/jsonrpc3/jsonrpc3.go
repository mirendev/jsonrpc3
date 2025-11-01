// Package jsonrpc3 implements JSON-RPC 3.0, a backward-compatible superset
// of JSON-RPC 2.0 that adds object references and bidirectional method calls.
package jsonrpc3

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// Protocol version constants
const (
	Version20 = "2.0"
	Version30 = "3.0"
)

// Standard error codes from JSON-RPC 2.0
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// JSON-RPC 3.0 extended error codes
const (
	CodeInvalidReference   = -32001
	CodeReferenceNotFound  = -32002
	CodeReferenceTypeError = -32003
)

// Request represents a JSON-RPC request message.
type Request struct {
	JSONRPC string     `json:"jsonrpc"`
	Ref     string     `json:"ref,omitempty"` // Remote reference to invoke method on
	Method  string     `json:"method"`
	Params  RawMessage `json:"params,omitempty"`
	ID      any        `json:"id,omitempty"` // Can be string, number, or null
	format  string     // mimetype format (not serialized)
}

// IsNotification returns true if this is a notification (no ID).
func (r *Request) IsNotification() bool {
	return r.ID == nil
}

// GetParams returns a Params interface configured for the request's format.
// This allows the params to be decoded using the correct decoder (JSON, CBOR, or compact CBOR).
func (r *Request) GetParams() Params {
	if r.format == "" {
		// Default to JSON for backward compatibility
		return NewParams(r.Params)
	}
	return NewParamsWithFormat(r.Params, r.format)
}

// SetFormat sets the mimetype format for this request.
// This is typically called by DecodeRequest when decoding a request.
func (r *Request) SetFormat(mimetype string) {
	r.format = mimetype
}

// Response represents a JSON-RPC response message.
type Response struct {
	JSONRPC string     `json:"jsonrpc"`
	Result  RawMessage `json:"result,omitempty"`
	Error   *Error     `json:"error,omitempty"`
	ID      any        `json:"id"`
}

// Error represents a JSON-RPC error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("jsonrpc error %d: %s (data: %v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// Message represents a JSON-RPC message that can be either a request or response.
// It contains all possible fields from both Request and Response types.
// Use IsRequest() or IsResponse() to determine the message type, then convert
// using ToRequest() or ToResponse().
type Message struct {
	JSONRPC string     `json:"jsonrpc"`
	Ref     string     `json:"ref,omitempty"`    // Request field
	Method  string     `json:"method,omitempty"` // Request field
	Params  RawMessage `json:"params,omitempty"` // Request field
	Result  RawMessage `json:"result,omitempty"` // Response field
	Error   *Error     `json:"error,omitempty"`  // Response field
	ID      any        `json:"id,omitempty"`     // Both request and response
	format  string     // mimetype format (not serialized)
}

// IsRequest returns true if this message is a request (has method field).
func (m *Message) IsRequest() bool {
	return m.Method != ""
}

// IsResponse returns true if this message is a response (has result or error field).
func (m *Message) IsResponse() bool {
	return m.Result != nil || m.Error != nil
}

// IsNotification returns true if this is a request notification (has method but no ID).
func (m *Message) IsNotification() bool {
	return m.IsRequest() && m.ID == nil
}

// SetFormat sets the format field for tracking the encoding mimetype.
func (m *Message) SetFormat(mimetype string) {
	m.format = mimetype
}

// ToRequest converts this message to a Request.
// Returns nil if this is not a request message.
func (m *Message) ToRequest() *Request {
	if !m.IsRequest() {
		return nil
	}
	req := &Request{
		JSONRPC: m.JSONRPC,
		Ref:     m.Ref,
		Method:  m.Method,
		Params:  m.Params,
		ID:      m.ID,
		format:  m.format,
	}
	return req
}

// ToResponse converts this message to a Response.
// Returns nil if this is not a response message.
func (m *Message) ToResponse() *Response {
	if !m.IsResponse() {
		return nil
	}
	return &Response{
		JSONRPC: m.JSONRPC,
		Result:  m.Result,
		Error:   m.Error,
		ID:      m.ID,
	}
}

// MessageSet represents one or more messages for encoding/decoding.
// A batch of 1 is treated the same as a non-batch for encoding purposes.
type MessageSet struct {
	Messages []Message
	IsBatch  bool // True if originally decoded from a JSON/CBOR array
}

// MessageSetConvertible is an interface for types that can be converted to MessageSet.
// This allows polymorphic handling of Request, Response, and BatchResponse in writeLoops.
type MessageSetConvertible interface {
	ToMessageSet() MessageSet
}

// ToMessageSet returns the MessageSet itself (satisfies MessageSetConvertible).
func (m *MessageSet) ToMessageSet() MessageSet {
	return *m
}

// ToMessageSet converts a Request to a MessageSet.
func (r *Request) ToMessageSet() MessageSet {
	return MessageSet{
		Messages: []Message{{
			JSONRPC: r.JSONRPC,
			Ref:     r.Ref,
			Method:  r.Method,
			Params:  r.Params,
			ID:      r.ID,
		}},
		IsBatch: false,
	}
}

// ToMessageSet converts a Response to a MessageSet.
func (r *Response) ToMessageSet() MessageSet {
	return MessageSet{
		Messages: []Message{{
			JSONRPC: r.JSONRPC,
			Result:  r.Result,
			Error:   r.Error,
			ID:      r.ID,
		}},
		IsBatch: false,
	}
}

// ToMessageSet converts a BatchResponse to a MessageSet.
func (b BatchResponse) ToMessageSet() MessageSet {
	messages := make([]Message, len(b))
	for i, resp := range b {
		messages[i] = Message{
			JSONRPC: resp.JSONRPC,
			Result:  resp.Result,
			Error:   resp.Error,
			ID:      resp.ID,
		}
	}
	return MessageSet{
		Messages: messages,
		IsBatch:  true,
	}
}

// NewMessageSetFromRequest creates a MessageSet from a single Request.
func NewMessageSetFromRequest(req *Request) MessageSet {
	return MessageSet{
		Messages: []Message{{
			JSONRPC: req.JSONRPC,
			Ref:     req.Ref,
			Method:  req.Method,
			Params:  req.Params,
			ID:      req.ID,
		}},
		IsBatch: false,
	}
}

// ToRequest extracts a single Request from the MessageSet.
// Returns an error if the MessageSet doesn't contain exactly one request message.
func (ms MessageSet) ToRequest() (*Request, error) {
	if len(ms.Messages) != 1 {
		return nil, fmt.Errorf("MessageSet contains %d messages, expected 1", len(ms.Messages))
	}

	msg := &ms.Messages[0]
	if !msg.IsRequest() {
		return nil, fmt.Errorf("message is not a request")
	}

	return &Request{
		JSONRPC: msg.JSONRPC,
		Ref:     msg.Ref,
		Method:  msg.Method,
		Params:  msg.Params,
		ID:      msg.ID,
		format:  msg.format,
	}, nil
}

// ToBatch extracts a batch of Requests from the MessageSet.
// Returns an error if any message is not a request.
func (ms MessageSet) ToBatch() (Batch, error) {
	batch := make(Batch, len(ms.Messages))
	for i, msg := range ms.Messages {
		if !msg.IsRequest() {
			return nil, fmt.Errorf("message at index %d is not a request", i)
		}
		batch[i] = Request{
			JSONRPC: msg.JSONRPC,
			Ref:     msg.Ref,
			Method:  msg.Method,
			Params:  msg.Params,
			ID:      msg.ID,
			format:  msg.format,
		}
	}
	return batch, nil
}

// ToResponse extracts a single Response from the MessageSet.
// Returns an error if the MessageSet doesn't contain exactly one response message.
func (ms MessageSet) ToResponse() (*Response, error) {
	if len(ms.Messages) != 1 {
		return nil, fmt.Errorf("MessageSet contains %d messages, expected 1", len(ms.Messages))
	}

	msg := &ms.Messages[0]
	if !msg.IsResponse() {
		return nil, fmt.Errorf("message is not a response")
	}

	return &Response{
		JSONRPC: msg.JSONRPC,
		Result:  msg.Result,
		Error:   msg.Error,
		ID:      msg.ID,
	}, nil
}

// ToBatchResponse extracts a batch of Responses from the MessageSet.
// Returns an error if any message is not a response.
func (ms MessageSet) ToBatchResponse() (BatchResponse, error) {
	batch := make(BatchResponse, len(ms.Messages))
	for i, msg := range ms.Messages {
		if !msg.IsResponse() {
			return nil, fmt.Errorf("message at index %d is not a response", i)
		}
		batch[i] = Response{
			JSONRPC: msg.JSONRPC,
			Result:  msg.Result,
			Error:   msg.Error,
			ID:      msg.ID,
		}
	}
	return batch, nil
}

// Standard error constructors
func NewParseError(data any) *Error {
	return &Error{Code: CodeParseError, Message: "Parse error", Data: data}
}

func NewInvalidRequestError(data any) *Error {
	return &Error{Code: CodeInvalidRequest, Message: "Invalid Request", Data: data}
}

func NewMethodNotFoundError(method string) *Error {
	return &Error{Code: CodeMethodNotFound, Message: "Method not found", Data: method}
}

func NewInvalidParamsError(data any) *Error {
	return &Error{Code: CodeInvalidParams, Message: "Invalid params", Data: data}
}

func NewInternalError(data any) *Error {
	return &Error{Code: CodeInternalError, Message: "Internal error", Data: data}
}

// JSON-RPC 3.0 error constructors
func NewInvalidReferenceError(data any) *Error {
	return &Error{Code: CodeInvalidReference, Message: "Invalid reference", Data: data}
}

func NewReferenceNotFoundError(ref string) *Error {
	return &Error{Code: CodeReferenceNotFound, Message: "Reference not found", Data: ref}
}

func NewReferenceTypeError(data any) *Error {
	return &Error{Code: CodeReferenceTypeError, Message: "Reference type error", Data: data}
}

// NewError creates a custom error with the specified code, message, and data.
func NewError(code int, message string, data any) *Error {
	return &Error{Code: code, Message: message, Data: data}
}

// Reference represents a reference to a local object using {"$ref": "id"} format.
// This is used when passing references in params or returning them in results.
type Reference struct {
	Ref string `json:"$ref"`
}

// NewReference creates a new local reference.
func NewReference(ref string) Reference {
	return Reference{Ref: ref}
}

// Call invokes a method on the remote reference using the provided Caller.
func (r Reference) Call(caller Caller, method string, params any) (Value, error) {
	return caller.Call(method, params, ToRef(r))
}

// Notify sends a notification to the remote reference using the provided Caller.
func (r Reference) Notify(caller Caller, method string, params any) error {
	return caller.Notify(method, params, ToRef(r))
}

// Protocol is the special reference used for protocol introspection methods.
// Use this with ToRef() to call protocol methods like session_id, list_refs, etc.
// Example: caller.Call("session_id", nil, &result, ToRef(Protocol))
var Protocol = NewReference("$rpc")

// Params provides access to method parameters in a transport-agnostic way.
type Params interface {
	// Decode unmarshals the parameters into the provided value.
	Decode(v any) error
}

// Object represents something that can handle method calls.
// This is the core abstraction for both top-level handlers and references.
type Object interface {
	// CallMethod invokes a method on this object.
	// The caller parameter allows methods to make callbacks on the same connection.
	CallMethod(method string, params Params, caller Caller) (any, error)
}

// jsonParams implements Params for JSON-encoded parameters.
type jsonParams struct {
	data RawMessage
}

// Decode implements Params.Decode for JSON.
func (p *jsonParams) Decode(v any) error {
	if p.data == nil {
		return nil
	}
	return json.Unmarshal(p.data, v)
}

// NewParams creates a Params from RawMessage.
// Defaults to JSON format for backward compatibility.
func NewParams(data RawMessage) Params {
	return &jsonParams{data: data}
}

// cborParams implements Params for CBOR-encoded parameters.
type cborParams struct {
	data RawMessage
}

// Decode implements Params.Decode for CBOR.
func (p *cborParams) Decode(v any) error {
	if p.data == nil {
		return nil
	}
	return cbor.Unmarshal(p.data, v)
}

// NewParamsWithFormat creates a Params from RawMessage with the specified mimetype.
// Supported mimetypes:
//   - "json" or "application/json": JSON decoding
//   - "cbor" or "application/cbor": CBOR decoding (works for both standard and compact)
//   - "application/cbor; format=compact": CBOR decoding (compact format is encoding-only, decoding is same)
func NewParamsWithFormat(data RawMessage, mimetype string) Params {
	mt := ParseMimeType(mimetype)

	if mt.IsCBOR() || mimetype == "cbor" {
		// CBOR params (works for both standard and compact formats)
		return &cborParams{data: data}
	}

	// Default to JSON
	return &jsonParams{data: data}
}

// Kind represents the kind of value stored in a Value.
type Kind uint

const (
	InvalidKind Kind = iota
	NullKind
	BoolKind
	IntKind
	FloatKind
	StringKind
	ArrayKind
	ObjectKind
	// JSON-RPC 3.0 special types
	ReferenceKind // Reference to a remote object
	DateTimeKind  // Enhanced DateTime type
	BytesKind     // Enhanced Bytes type
	BigIntKind    // Enhanced BigInt type
	RegExpKind    // Enhanced RegExp type
)

// String returns the string representation of the Kind.
func (k Kind) String() string {
	switch k {
	case InvalidKind:
		return "invalid"
	case NullKind:
		return "null"
	case BoolKind:
		return "bool"
	case IntKind:
		return "int"
	case FloatKind:
		return "float"
	case StringKind:
		return "string"
	case ArrayKind:
		return "array"
	case ObjectKind:
		return "object"
	case ReferenceKind:
		return "reference"
	case DateTimeKind:
		return "datetime"
	case BytesKind:
		return "bytes"
	case BigIntKind:
		return "bigint"
	case RegExpKind:
		return "regexp"
	default:
		return "unknown"
	}
}

// Value represents a JSON-RPC result value with lazy decoding.
// It provides reflect.Value-like access to result data without requiring
// upfront decoding into a specific type.
type Value struct {
	data    RawMessage
	codec   Codec
	cached  any
	decoded bool
}

// ensureDecoded decodes the value if it hasn't been decoded yet.
func (v *Value) ensureDecoded() error {
	if v.decoded {
		return nil
	}

	if v.data == nil {
		v.cached = nil
		v.decoded = true
		return nil
	}

	if err := v.codec.Unmarshal(v.data, &v.cached); err != nil {
		return err
	}

	v.decoded = true
	return nil
}

// Kind returns the kind of value.
func (v *Value) Kind() Kind {
	if err := v.ensureDecoded(); err != nil {
		return InvalidKind
	}

	if v.cached == nil {
		return NullKind
	}

	// Check for Reference first (has $ref field)
	if v.IsReference() {
		return ReferenceKind
	}

	// Check for Enhanced types (has $type field)
	if typeName, isEnhanced := v.IsEnhanced(); isEnhanced {
		switch typeName {
		case TypeDateTime:
			return DateTimeKind
		case TypeBytes:
			return BytesKind
		case TypeBigInt:
			return BigIntKind
		case TypeRegExp:
			return RegExpKind
		}
	}

	// Check basic types
	switch v.cached.(type) {
	case bool:
		return BoolKind
	case float64, int64, int, int32, uint, uint32, uint64:
		// JSON numbers are always float64, but we check other types too
		return FloatKind
	case string:
		return StringKind
	case []any:
		return ArrayKind
	case map[string]any, map[interface{}]interface{}:
		return ObjectKind
	default:
		return InvalidKind
	}
}

// IsNull returns true if the value is null.
func (v *Value) IsNull() bool {
	if err := v.ensureDecoded(); err != nil {
		return false
	}
	return v.cached == nil
}

// String returns the value as a string.
// In the case of non-string values, it coerces the value to a string.
func (v *Value) String() string {
	if err := v.ensureDecoded(); err != nil {
		return ""
	}
	if s, ok := v.cached.(string); ok {
		return s
	}

	return fmt.Sprint(v.cached)
}

// Int returns the value as an int64.
// Returns 0 if the value is not numeric.
func (v *Value) Int() int64 {
	if err := v.ensureDecoded(); err != nil {
		return 0
	}

	switch n := v.cached.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case uint:
		return int64(n)
	case uint32:
		return int64(n)
	case uint64:
		return int64(n)
	default:
		return 0
	}
}

// Float returns the value as a float64.
// Returns 0 if the value is not numeric.
func (v *Value) Float() float64 {
	if err := v.ensureDecoded(); err != nil {
		return 0
	}

	switch n := v.cached.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case uint:
		return float64(n)
	case uint32:
		return float64(n)
	case uint64:
		return float64(n)
	default:
		return 0
	}
}

// Bool returns the value as a bool.
// Returns false if the value is not a bool.
func (v *Value) Bool() bool {
	if err := v.ensureDecoded(); err != nil {
		return false
	}
	if b, ok := v.cached.(bool); ok {
		return b
	}
	return false
}

// Reference returns the value as a Reference.
// Returns an empty Reference if the value is not a reference.
func (v *Value) Reference() Reference {
	if v.Kind() != ReferenceKind {
		return Reference{}
	}

	ref, err := v.AsReference()
	if err != nil {
		return Reference{}
	}
	return ref
}

// DateTime returns the value as a time.Time.
// Returns zero time if the value is not a DateTime enhanced type.
func (v *Value) DateTime() time.Time {
	if v.Kind() != DateTimeKind {
		return time.Time{}
	}

	enhanced, err := v.AsEnhanced()
	if err != nil {
		return time.Time{}
	}

	if t, ok := enhanced.(time.Time); ok {
		return t
	}
	return time.Time{}
}

// Bytes returns the value as a byte slice.
// Returns nil if the value is not a Bytes enhanced type.
func (v *Value) Bytes() []byte {
	if v.Kind() != BytesKind {
		return nil
	}

	enhanced, err := v.AsEnhanced()
	if err != nil {
		return nil
	}

	if b, ok := enhanced.([]byte); ok {
		return b
	}
	return nil
}

// BigInt returns the value as a *big.Int.
// Returns nil if the value is not a BigInt enhanced type.
func (v *Value) BigInt() *big.Int {
	if v.Kind() != BigIntKind {
		return nil
	}

	enhanced, err := v.AsEnhanced()
	if err != nil {
		return nil
	}

	if bi, ok := enhanced.(*big.Int); ok {
		return bi
	}
	return nil
}

// RegExp returns the value as a *regexp.Regexp.
// Returns nil if the value is not a RegExp enhanced type.
func (v *Value) RegExp() *regexp.Regexp {
	if v.Kind() != RegExpKind {
		return nil
	}

	enhanced, err := v.AsEnhanced()
	if err != nil {
		return nil
	}

	if re, ok := enhanced.(*regexp.Regexp); ok {
		return re
	}
	return nil
}

// Len returns the length of an array or object.
// Returns 0 if the value is not an array or object.
func (v *Value) Len() int {
	if err := v.ensureDecoded(); err != nil {
		return 0
	}

	switch val := v.cached.(type) {
	case []any:
		return len(val)
	case map[string]any:
		return len(val)
	default:
		return 0
	}
}

// Index returns the element at index i in an array.
// Returns an invalid Value if this is not an array or index is out of bounds.
func (v *Value) Index(i int) Value {
	if err := v.ensureDecoded(); err != nil {
		return Value{}
	}

	arr, ok := v.cached.([]any)
	if !ok || i < 0 || i >= len(arr) {
		return Value{}
	}

	// Marshal to get the raw bytes for the element
	data, err := v.codec.Marshal(arr[i])
	if err != nil {
		return Value{}
	}

	return Value{
		data:    data,
		codec:   v.codec,
		cached:  arr[i],
		decoded: true,
	}
}

// MapIndex returns the value for key in an object.
// Returns an invalid Value if this is not an object or key doesn't exist.
func (v *Value) MapIndex(key string) Value {
	if err := v.ensureDecoded(); err != nil {
		return Value{}
	}

	obj, ok := v.cached.(map[string]any)
	if !ok {
		return Value{}
	}

	val, exists := obj[key]
	if !exists {
		return Value{cached: nil, decoded: true, codec: v.codec}
	}

	// Marshal to get the raw bytes for the value
	data, err := v.codec.Marshal(val)
	if err != nil {
		return Value{}
	}

	return Value{
		data:    data,
		codec:   v.codec,
		cached:  val,
		decoded: true,
	}
}

// IsReference checks if the value is a Reference (has $ref field).
func (v *Value) IsReference() bool {
	if err := v.ensureDecoded(); err != nil {
		return false
	}

	// Check for JSON map (map[string]any)
	if obj, ok := v.cached.(map[string]any); ok {
		_, hasRef := obj["$ref"]
		return hasRef
	}

	// Check for CBOR map (map[interface{}]interface{})
	if obj, ok := v.cached.(map[interface{}]interface{}); ok {
		_, hasRef := obj["$ref"]
		return hasRef
	}

	return false
}

// AsReference extracts the value as a Reference.
// Returns an error if the value is not a reference.
func (v *Value) AsReference() (Reference, error) {
	if !v.IsReference() {
		return Reference{}, fmt.Errorf("value is not a reference")
	}

	var ref Reference
	if err := v.codec.Unmarshal(v.data, &ref); err != nil {
		return Reference{}, fmt.Errorf("failed to decode reference: %w", err)
	}

	return ref, nil
}

// IsEnhanced checks if the value is an enhanced type (has $type field).
// Returns the type name if it's an enhanced type, or empty string if not.
func (v *Value) IsEnhanced() (string, bool) {
	if err := v.ensureDecoded(); err != nil {
		return "", false
	}

	return IsEnhancedType(v.cached)
}

// AsEnhanced decodes the value as an enhanced type, returning the native Go type.
// For DateTime returns time.Time, for Bytes returns []byte, for BigInt returns *big.Int,
// for RegExp returns *regexp.Regexp. Returns an error if not an enhanced type.
func (v *Value) AsEnhanced() (any, error) {
	typeName, isEnhanced := v.IsEnhanced()
	if !isEnhanced {
		return nil, fmt.Errorf("value is not an enhanced type")
	}

	result, err := DecodeEnhancedType(v.cached, v.codec)
	if err != nil {
		return nil, fmt.Errorf("failed to decode enhanced type %s: %w", typeName, err)
	}

	return result, nil
}

// Decode unmarshals the value into the provided variable.
// This is useful when you know the expected type and want to decode into a struct.
//
// Automatic type detection:
//   - When decoding into a Reference, automatically detects if the value has a $ref field
//   - When decoding into time.Time, *big.Int, []byte, or *regexp.Regexp, automatically
//     detects and decodes enhanced types (DateTime, BigInt, Bytes, RegExp)
func (v *Value) Decode(target any) error {
	if v.data == nil {
		return nil
	}

	return v.codec.Unmarshal(v.data, target)
}

// Interface returns the value as an any (fully decoded).
// This gives you direct access to the Go value.
//
// If the value is an enhanced type (DateTime, Bytes, BigInt, RegExp), it will be
// automatically decoded to its native Go type (time.Time, []byte, *big.Int, *regexp.Regexp).
// References are not automatically decoded by Interface() - use AsReference() for that.
func (v *Value) Interface() (any, error) {
	if err := v.ensureDecoded(); err != nil {
		return nil, err
	}

	// Check if it's an enhanced type and auto-decode
	if _, isEnhanced := v.IsEnhanced(); isEnhanced {
		decoded, err := DecodeEnhancedType(v.cached, v.codec)
		if err != nil {
			// If decoding fails, return the raw value
			return v.cached, nil
		}
		return decoded, nil
	}

	return v.cached, nil
}

// Raw returns the raw encoded bytes.
func (v *Value) Raw() []byte {
	return []byte(v.data)
}

// MarshalJSON implements json.Marshaler for Value.
// This allows Value to be marshaled directly to JSON.
// Uses a value receiver to ensure it works whether Value is used as a pointer or value.
func (v Value) MarshalJSON() ([]byte, error) {
	if err := v.ensureDecoded(); err != nil {
		return nil, err
	}

	// Marshal the cached value to JSON
	return json.Marshal(v.cached)
}

// MarshalCBOR implements cbor.Marshaler for Value.
// This allows Value to be marshaled directly to CBOR.
// Uses a value receiver to ensure it works whether Value is used as a pointer or value.
func (v Value) MarshalCBOR() ([]byte, error) {
	if err := v.ensureDecoded(); err != nil {
		return nil, err
	}

	// Marshal the cached value to CBOR
	return cbor.Marshal(v.cached)
}

// NilValue is a global nil Value with JSON codec.
// Use this instead of creating new empty values.
var NilValue = Value{
	data:  nil,
	codec: GetCodec(MimeTypeJSON),
}

// NewValueWithCodec creates a Value from RawMessage with the specified codec.
func NewValueWithCodec(data RawMessage, codec Codec) Value {
	return Value{
		data:  data,
		codec: codec,
	}
}

// Batch represents a batch request (array of requests).
type Batch []Request

// ToMessageSet converts a Batch to a MessageSet.
func (b Batch) ToMessageSet() MessageSet {
	messages := make([]Message, len(b))
	for i, req := range b {
		messages[i] = Message{
			JSONRPC: req.JSONRPC,
			Ref:     req.Ref,
			Method:  req.Method,
			Params:  req.Params,
			ID:      req.ID,
		}
	}
	return MessageSet{
		Messages: messages,
		IsBatch:  true,
	}
}

// BatchResponse represents a batch response (array of responses).
type BatchResponse []Response

// NewRequest creates a new JSON-RPC 3.0 request with JSON encoding.
func NewRequest(method string, params any, id any) (*Request, error) {
	return NewRequestWithFormat(method, params, id, "json")
}

// NewRequestWithFormat creates a new JSON-RPC 3.0 request with the specified encoding format.
// Supported formats:
//   - "json" or "application/json": JSON encoding
//   - "cbor" or "application/cbor": Standard CBOR encoding (string keys)
//   - "application/cbor; format=compact": Compact CBOR encoding (integer keys)
//
// Note: This function always creates a Request with standard structure. The compact format
// applies only to the wire format when encoding the entire request.
func NewRequestWithFormat(method string, params any, id any, mimetype string) (*Request, error) {
	var paramsRaw RawMessage
	if params != nil {
		codec := GetCodec(mimetype)
		data, err := codec.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		paramsRaw = RawMessage(data)
	}

	return &Request{
		JSONRPC: Version30,
		Method:  method,
		Params:  paramsRaw,
		ID:      id,
	}, nil
}

// NewRequestWithRef creates a new JSON-RPC 3.0 request with a remote reference.
func NewRequestWithRef(ref, method string, params any, id any) (*Request, error) {
	req, err := NewRequest(method, params, id)
	if err != nil {
		return nil, err
	}
	req.Ref = ref
	return req, nil
}

// NewNotification creates a new JSON-RPC notification (no ID).
func NewNotification(method string, params any) (*Request, error) {
	return NewRequest(method, params, nil)
}

// NewSuccessResponse creates a successful JSON-RPC response with JSON encoding.
func NewSuccessResponse(id any, result any, version string) (*Response, error) {
	return NewSuccessResponseWithFormat(id, result, version, "json")
}

// NewSuccessResponseWithFormat creates a successful JSON-RPC response with the specified encoding format.
// Supported formats:
//   - "json" or "application/json": JSON encoding
//   - "cbor" or "application/cbor": Standard CBOR encoding (string keys)
//   - "application/cbor; format=compact": Compact CBOR encoding (integer keys)
//
// Note: This function always creates a Response with standard structure. The compact format
// applies only to the wire format when encoding the entire response.
func NewSuccessResponseWithFormat(id any, result any, version string, mimetype string) (*Response, error) {
	var resultRaw RawMessage
	if result != nil {
		codec := GetCodec(mimetype)
		data, err := codec.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result: %w", err)
		}
		resultRaw = RawMessage(data)
	}

	if version == "" {
		version = Version30
	}

	return &Response{
		JSONRPC: version,
		Result:  resultRaw,
		ID:      id,
	}, nil
}

// NewErrorResponse creates an error JSON-RPC response.
func NewErrorResponse(id any, err *Error, version string) *Response {
	if version == "" {
		version = Version30
	}

	return &Response{
		JSONRPC: version,
		Error:   err,
		ID:      id,
	}
}
