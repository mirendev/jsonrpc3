package jsonrpc3

import (
	"github.com/fxamacker/cbor/v2"
)

// Integer key mappings for compact CBOR format as per JSON-RPC 3.0 spec
const (
	// Request/Response common keys
	keyJSONRPC = 1
	keyRef     = 2
	keyMethod  = 3
	keyParams  = 4
	keyID      = 5
	keyResult  = 6
	keyError   = 7

	// Error keys
	keyCode    = 1
	keyMessage = 2
	keyData    = 3
)

// compactRequest is the compact CBOR representation of Request using integer keys
type compactRequest struct {
	JSONRPC string     `cbor:"1,keyasint"`
	Ref     string     `cbor:"2,keyasint,omitempty"`
	Method  string     `cbor:"3,keyasint"`
	Params  RawMessage `cbor:"4,keyasint,omitempty"`
	ID      any        `cbor:"5,keyasint,omitempty"`
}

// compactResponse is the compact CBOR representation of Response using integer keys
type compactResponse struct {
	JSONRPC string     `cbor:"1,keyasint"`
	Result  RawMessage `cbor:"6,keyasint,omitempty"`
	Error   *Error     `cbor:"7,keyasint,omitempty"`
	ID      any        `cbor:"5,keyasint"`
}

// compactError is the compact CBOR representation of Error using integer keys
type compactError struct {
	Code    int    `cbor:"1,keyasint"`
	Message string `cbor:"2,keyasint"`
	Data    any    `cbor:"3,keyasint,omitempty"`
}

// compactMessage is the compact CBOR representation of Message using integer keys.
// It can represent both requests and responses.
type compactMessage struct {
	JSONRPC string     `cbor:"1,keyasint"`
	Ref     string     `cbor:"2,keyasint,omitempty"`
	Method  string     `cbor:"3,keyasint,omitempty"` // Request field
	Params  RawMessage `cbor:"4,keyasint,omitempty"` // Request field
	ID      any        `cbor:"5,keyasint,omitempty"`
	Result  RawMessage `cbor:"6,keyasint,omitempty"` // Response field
	Error   *Error     `cbor:"7,keyasint,omitempty"` // Response field
}

// toCompactMessage converts a Message to compact format
func toCompactMessage(msg *Message) *compactMessage {
	return &compactMessage{
		JSONRPC: msg.JSONRPC,
		Ref:     msg.Ref,
		Method:  msg.Method,
		Params:  msg.Params,
		ID:      msg.ID,
		Result:  msg.Result,
		Error:   msg.Error,
	}
}

// fromCompactMessage converts from compact format to Message
func fromCompactMessage(cm *compactMessage) *Message {
	return &Message{
		JSONRPC: cm.JSONRPC,
		Ref:     cm.Ref,
		Method:  cm.Method,
		Params:  cm.Params,
		ID:      cm.ID,
		Result:  cm.Result,
		Error:   cm.Error,
	}
}

// toCompactRequest converts a Request to compact format
func toCompactRequest(req *Request) *compactRequest {
	return &compactRequest{
		JSONRPC: req.JSONRPC,
		Ref:     req.Ref,
		Method:  req.Method,
		Params:  req.Params,
		ID:      req.ID,
	}
}

// fromCompactRequest converts from compact format to Request
func fromCompactRequest(cr *compactRequest) *Request {
	return &Request{
		JSONRPC: cr.JSONRPC,
		Ref:     cr.Ref,
		Method:  cr.Method,
		Params:  cr.Params,
		ID:      cr.ID,
	}
}

// toCompactResponse converts a Response to compact format
func toCompactResponse(resp *Response) *compactResponse {
	var compactErr *Error
	if resp.Error != nil {
		// Error remains as-is since it will be marshaled with its own compact format
		compactErr = resp.Error
	}

	return &compactResponse{
		JSONRPC: resp.JSONRPC,
		Result:  resp.Result,
		Error:   compactErr,
		ID:      resp.ID,
	}
}

// fromCompactResponse converts from compact format to Response
func fromCompactResponse(cr *compactResponse) *Response {
	return &Response{
		JSONRPC: cr.JSONRPC,
		Result:  cr.Result,
		Error:   cr.Error,
		ID:      cr.ID,
	}
}

// MarshalCBOR implements custom CBOR marshaling for Error to support integer keys in compact mode
func (e *Error) MarshalCBOR() ([]byte, error) {
	// Check if we're using compact mode by trying to use the compact encoder
	// Note: This will be called by the compact encoder, so we need to use the standard approach
	return cbor.Marshal(map[int]any{
		keyCode:    e.Code,
		keyMessage: e.Message,
		keyData:    e.Data,
	})
}

// UnmarshalCBOR implements custom CBOR unmarshaling for Error to support integer keys
func (e *Error) UnmarshalCBOR(data []byte) error {
	// Try integer keys first (compact format)
	var compactMap map[int]any
	if err := cbor.Unmarshal(data, &compactMap); err == nil {
		if code, ok := compactMap[keyCode]; ok {
			switch v := code.(type) {
			case int:
				e.Code = v
			case int64:
				e.Code = int(v)
			case uint64:
				e.Code = int(v)
			}
		}
		if msg, ok := compactMap[keyMessage]; ok {
			if s, ok := msg.(string); ok {
				e.Message = s
			}
		}
		if data, ok := compactMap[keyData]; ok {
			e.Data = data
		}
		return nil
	}

	// Fall back to string keys (standard format)
	var stringMap map[string]any
	if err := cbor.Unmarshal(data, &stringMap); err != nil {
		return err
	}

	if code, ok := stringMap["code"]; ok {
		switch v := code.(type) {
		case int:
			e.Code = v
		case int64:
			e.Code = int(v)
		case uint64:
			e.Code = int(v)
		}
	}
	if msg, ok := stringMap["message"]; ok {
		if s, ok := msg.(string); ok {
			e.Message = s
		}
	}
	if data, ok := stringMap["data"]; ok {
		e.Data = data
	}

	return nil
}
