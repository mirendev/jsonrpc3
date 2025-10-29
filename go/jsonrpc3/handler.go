package jsonrpc3

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

// Handler dispatches JSON-RPC requests to Object handlers.
type Handler struct {
	session    *Session
	protocol   *ProtocolHandler
	rootObject Object            // Handles top-level method calls
	objects    map[string]Object // ref => Object for reference calls
	version    string            // Default version for responses
}

// NewHandler creates a new handler for a session.
// The rootObject handles top-level method calls (requests without a ref field).
func NewHandler(session *Session, rootObject Object, mimeTypes []string) *Handler {
	if mimeTypes == nil {
		mimeTypes = []string{"application/json"}
	}
	return &Handler{
		session:    session,
		protocol:   NewProtocolHandler(session, mimeTypes),
		rootObject: rootObject,
		objects:    make(map[string]Object),
		version:    Version30,
	}
}

// SetVersion sets the default JSON-RPC version for responses.
func (h *Handler) SetVersion(version string) {
	h.version = version
}

// AddObject registers an object to handle method calls on the given reference.
func (h *Handler) AddObject(ref string, obj Object) {
	h.objects[ref] = obj
	h.session.AddLocalRef(ref, obj)
}

// RemoveObject unregisters an object for the given reference.
func (h *Handler) RemoveObject(ref string) {
	delete(h.objects, ref)
	h.session.RemoveLocalRef(ref)
}

// HandleRequest processes a single request and returns a response.
// If the request is a notification (no ID), returns nil.
func (h *Handler) HandleRequest(req *Request) *Response {
	// Validate request
	if req.Method == "" {
		return h.errorResponse(req.ID, NewInvalidRequestError("method is required"))
	}

	// Handle protocol methods (on $rpc reference)
	if req.Ref == "$rpc" {
		params := req.GetParams()
		result, err := h.protocol.CallMethod(req.Method, params)
		if err != nil {
			// Convert Go error to JSON-RPC error
			var rpcErr *Error
			if e, ok := err.(*Error); ok {
				rpcErr = e
			} else {
				rpcErr = NewInternalError(err.Error())
			}
			return h.errorResponse(req.ID, rpcErr)
		}

		// Don't send response for notifications
		if req.IsNotification() {
			return nil
		}

		// Process result to auto-register references
		processedResult := processResult(h, result)

		// Use the request's format for the response
		format := req.format
		if format == "" {
			format = "application/json"
		}
		resp, marshalErr := NewSuccessResponseWithFormat(req.ID, processedResult, h.version, format)
		if marshalErr != nil {
			return h.errorResponse(req.ID, NewInternalError(fmt.Sprintf("failed to marshal result: %v", marshalErr)))
		}
		return resp
	}

	// Handle reference method calls
	if req.Ref != "" {
		return h.handleRefMethod(req)
	}

	// Handle regular method calls
	return h.handleMethod(req)
}

// handleMethod handles a regular method call (no ref).
func (h *Handler) handleMethod(req *Request) *Response {
	if h.rootObject == nil {
		return h.errorResponse(req.ID, NewMethodNotFoundError(req.Method))
	}

	params := req.GetParams()
	result, err := h.rootObject.CallMethod(req.Method, params)
	if err != nil {
		// Convert Go error to JSON-RPC error
		var rpcErr *Error
		if e, ok := err.(*Error); ok {
			rpcErr = e
		} else {
			rpcErr = NewInternalError(err.Error())
		}
		return h.errorResponse(req.ID, rpcErr)
	}

	// Don't send response for notifications
	if req.IsNotification() {
		return nil
	}

	// Process result to auto-register references
	processedResult := processResult(h, result)

	// Use the request's format for the response
	format := req.format
	if format == "" {
		format = "application/json"
	}
	resp, marshalErr := NewSuccessResponseWithFormat(req.ID, processedResult, h.version, format)
	if marshalErr != nil {
		return h.errorResponse(req.ID, NewInternalError(fmt.Sprintf("failed to marshal result: %v", marshalErr)))
	}
	return resp
}

// handleRefMethod handles a method call on a reference.
func (h *Handler) handleRefMethod(req *Request) *Response {
	// Look up the object
	obj, exists := h.objects[req.Ref]
	if !exists {
		return h.errorResponse(req.ID, NewReferenceNotFoundError(req.Ref))
	}

	params := req.GetParams()
	result, err := obj.CallMethod(req.Method, params)
	if err != nil {
		// Convert Go error to JSON-RPC error
		var rpcErr *Error
		if e, ok := err.(*Error); ok {
			rpcErr = e
		} else {
			rpcErr = NewInternalError(err.Error())
		}
		return h.errorResponse(req.ID, rpcErr)
	}

	// Don't send response for notifications
	if req.IsNotification() {
		return nil
	}

	// Process result to auto-register references
	processedResult := processResult(h, result)

	// Use the request's format for the response
	format := req.format
	if format == "" {
		format = "application/json"
	}
	resp, marshalErr := NewSuccessResponseWithFormat(req.ID, processedResult, h.version, format)
	if marshalErr != nil {
		return h.errorResponse(req.ID, NewInternalError(fmt.Sprintf("failed to marshal result: %v", marshalErr)))
	}
	return resp
}

// isBatchLocalRef checks if a reference is a batch-local reference (\0, \1, etc.)
func isBatchLocalRef(ref string) bool {
	return len(ref) > 0 && ref[0] == '\\'
}

// parseBatchLocalRef parses a batch-local reference and returns the index
// Returns the index and an error if the format is invalid
func parseBatchLocalRef(ref string) (int, error) {
	if !isBatchLocalRef(ref) {
		return 0, fmt.Errorf("not a batch-local reference: %s", ref)
	}

	indexStr := ref[1:] // Skip the backslash
	index := 0
	for _, ch := range indexStr {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid batch-local reference format: %s", ref)
		}
		index = index*10 + int(ch-'0')
	}

	return index, nil
}

// resolveBatchLocalRef resolves a batch-local reference against previous responses
// Returns the actual reference ID or an error
func resolveBatchLocalRef(ref string, currentIndex int, responses []Response) (string, error) {
	index, err := parseBatchLocalRef(ref)
	if err != nil {
		return "", err
	}

	// Validate index is not forward reference
	if index >= currentIndex {
		return "", fmt.Errorf("forward reference not allowed: \\%d refers to current or future request", index)
	}

	// Validate index is in bounds
	if index < 0 || index >= len(responses) {
		return "", fmt.Errorf("reference index out of bounds: \\%d", index)
	}

	// Get the response at that index
	resp := responses[index]

	// Check if the request failed
	if resp.Error != nil {
		return "", fmt.Errorf("referenced request \\%d failed", index)
	}

	// Parse the result to extract the reference
	var result map[string]any
	err = json.Unmarshal(resp.Result, &result)
	if err != nil {
		// If we can't unmarshal as a map, it's definitely not a reference
		return "", fmt.Errorf("result from request \\%d is not a reference", index)
	}

	// Check if result is a LocalReference
	if refStr, ok := result["$ref"].(string); ok {
		return refStr, nil
	}

	// Result is not a reference
	return "", fmt.Errorf("result from request \\%d is not a reference", index)
}

// HandleBatch processes a batch of requests and returns a batch response.
// Empty responses (from notifications) are filtered out.
// Supports batch-local references (\0, \1, etc.) that resolve to previous results.
func (h *Handler) HandleBatch(batch Batch) BatchResponse {
	if len(batch) == 0 {
		// Invalid batch - return error
		return BatchResponse{
			*h.errorResponse(nil, NewInvalidRequestError("batch is empty")),
		}
	}

	responses := make(BatchResponse, 0, len(batch))
	for i, req := range batch {
		// Make a copy of the request so we can modify it
		reqCopy := req

		// Resolve batch-local references
		if isBatchLocalRef(reqCopy.Ref) {
			actualRef, err := resolveBatchLocalRef(reqCopy.Ref, i, responses)
			if err != nil {
				// Create error response based on the type of error
				var rpcErr *Error
				errMsg := err.Error()

				// Check if it's a type error (non-reference result)
				if strings.Contains(errMsg, "is not a reference") {
					rpcErr = &Error{
						Code:    CodeReferenceTypeError,
						Message: "Reference type error",
						Data:    errMsg,
					}
				} else {
					rpcErr = NewInvalidReferenceError(errMsg)
				}

				resp := h.errorResponse(reqCopy.ID, rpcErr)
				if resp != nil {
					responses = append(responses, *resp)
				}
				continue
			}
			// Replace batch-local ref with actual ref
			reqCopy.Ref = actualRef
		}

		resp := h.HandleRequest(&reqCopy)
		if resp != nil {
			responses = append(responses, *resp)
		}
	}

	// If all requests were notifications, return empty array
	return responses
}

// errorResponse creates an error response.
func (h *Handler) errorResponse(id any, err *Error) *Response {
	return NewErrorResponse(id, err, h.version)
}

// DecodeRequest decodes a JSON-RPC request from bytes using the specified mimetype.
// It handles both single requests and batch requests.
// Supported mimetypes:
//   - "application/json": JSON encoding
//   - "application/cbor": Standard CBOR encoding (string keys)
//   - "application/cbor; format=compact": Compact CBOR encoding (integer keys)
// Returns (request, batch, isBatch, error).
func DecodeRequest(data []byte, mimetype string) (*Request, Batch, bool, error) {
	mt := ParseMimeType(mimetype)

	if mt.IsCBOR() {
		return decodeRequestCBOR(data, mt)
	}

	// Default to JSON for backward compatibility
	return decodeRequestJSON(data)
}

// decodeRequestJSON decodes a JSON-RPC request from JSON bytes.
func decodeRequestJSON(data []byte) (*Request, Batch, bool, error) {
	// Try to detect if this is a batch by checking the first non-whitespace character
	for _, b := range data {
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		if b == '[' {
			// This is a batch
			var batch Batch
			if err := json.Unmarshal(data, &batch); err != nil {
				return nil, nil, false, err
			}
			// Set format for all requests in batch
			for i := range batch {
				batch[i].SetFormat("application/json")
			}
			return nil, batch, true, nil
		}
		break
	}

	// This is a single request
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, nil, false, err
	}
	req.SetFormat("application/json")
	return &req, nil, false, nil
}

// decodeRequestCBOR decodes a JSON-RPC request from CBOR bytes.
// Handles both standard CBOR (string keys) and compact CBOR (integer keys)
// based on the mimetype format parameter.
func decodeRequestCBOR(data []byte, mt MimeType) (*Request, Batch, bool, error) {
	if mt.IsCompact() {
		// Decode compact CBOR with integer keys
		return decodeRequestCompactCBOR(data)
	}

	// Decode standard CBOR with string keys
	return decodeRequestStandardCBOR(data)
}

// decodeRequestStandardCBOR decodes standard CBOR with string keys
func decodeRequestStandardCBOR(data []byte) (*Request, Batch, bool, error) {
	// Try to decode as a CBOR array (batch)
	var batch Batch
	if err := cbor.Unmarshal(data, &batch); err == nil && len(batch) > 0 {
		// Check if it's actually a batch by verifying the first element looks like a request
		if batch[0].Method != "" {
			// Set format for all requests in batch
			for i := range batch {
				batch[i].SetFormat("application/cbor")
			}
			return nil, batch, true, nil
		}
	}

	// Try as a single request
	var req Request
	if err := cbor.Unmarshal(data, &req); err != nil {
		return nil, nil, false, err
	}
	req.SetFormat("application/cbor")
	return &req, nil, false, nil
}

// decodeRequestCompactCBOR decodes compact CBOR with integer keys
func decodeRequestCompactCBOR(data []byte) (*Request, Batch, bool, error) {
	// Try to decode as a CBOR array (batch)
	var compactBatch []compactRequest
	if err := cbor.Unmarshal(data, &compactBatch); err == nil && len(compactBatch) > 0 {
		// Check if it's actually a batch by verifying the first element looks like a request
		if compactBatch[0].Method != "" {
			// Convert compact batch to standard batch
			batch := make(Batch, len(compactBatch))
			for i, cr := range compactBatch {
				batch[i] = *fromCompactRequest(&cr)
				batch[i].SetFormat("application/cbor; format=compact")
			}
			return nil, batch, true, nil
		}
	}

	// Try as a single request
	var compactReq compactRequest
	if err := cbor.Unmarshal(data, &compactReq); err != nil {
		return nil, nil, false, err
	}

	req := fromCompactRequest(&compactReq)
	req.SetFormat("application/cbor; format=compact")
	return req, nil, false, nil
}

// EncodeResponse encodes a response to JSON.
// For CBOR encoding, use EncodeResponseWithFormat.
func EncodeResponse(resp *Response) ([]byte, error) {
	return json.Marshal(resp)
}

// EncodeResponseWithFormat encodes a response using the specified mimetype.
// Supported mimetypes:
//   - "application/json": JSON encoding
//   - "application/cbor": Standard CBOR encoding (string keys)
//   - "application/cbor; format=compact": Compact CBOR encoding (integer keys)
func EncodeResponseWithFormat(resp *Response, mimetype string) ([]byte, error) {
	mt := ParseMimeType(mimetype)
	codec := GetCodec(mimetype)

	if mt.IsCBOR() && mt.IsCompact() {
		// Convert to compact format with integer keys
		compactResp := toCompactResponse(resp)
		return codec.Marshal(compactResp)
	}

	return codec.Marshal(resp)
}

// EncodeBatchResponse encodes a batch response to JSON.
// For CBOR encoding, use EncodeBatchResponseWithFormat.
func EncodeBatchResponse(batch BatchResponse) ([]byte, error) {
	return json.Marshal(batch)
}

// EncodeBatchResponseWithFormat encodes a batch response using the specified mimetype.
// Supported mimetypes:
//   - "application/json": JSON encoding
//   - "application/cbor": Standard CBOR encoding (string keys)
//   - "application/cbor; format=compact": Compact CBOR encoding (integer keys)
func EncodeBatchResponseWithFormat(batch BatchResponse, mimetype string) ([]byte, error) {
	mt := ParseMimeType(mimetype)
	codec := GetCodec(mimetype)

	if mt.IsCBOR() && mt.IsCompact() {
		// Convert batch to compact format with integer keys
		compactBatch := make([]compactResponse, len(batch))
		for i, resp := range batch {
			compactBatch[i] = *toCompactResponse(&resp)
		}
		return codec.Marshal(compactBatch)
	}

	return codec.Marshal(batch)
}
