package jsonrpc3

import (
	"fmt"
	"sync/atomic"
)

// BatchBuilder represents a batch request builder that collects multiple requests
// to be sent as a single batch. It automatically manages batch-local references
// when chaining method calls.
type BatchBuilder struct {
	requests  []*Request
	rawParams []any // Store raw params before marshaling
	idGen     func() any
}

// BatchBuilderPromise represents a pending result in a batch request.
// It can be used to chain method calls that reference the result of a previous call
// using batch-local references (\0, \1, \2, etc.).
type BatchBuilderPromise struct {
	batch *BatchBuilder
	index int // Position in batch array (used for \0, \1, \2 references)
}

// BatchResults contains the responses from a batch request.
type BatchResults struct {
	responses []*Response
	format    string // MIME type format of the results (e.g., "application/json" or "application/cbor")
}

// newBatchBuilder creates a new BatchBuilder with the given ID generator function.
func newBatchBuilder(idGen func() any) *BatchBuilder {
	return &BatchBuilder{
		requests:  make([]*Request, 0),
		rawParams: make([]any, 0),
		idGen:     idGen,
	}
}

// Call adds a method call to the batch and returns a BatchBuilderPromise
// that can be used to chain additional calls.
//
// Example:
//
//	counter := batch.Call("createCounter", nil)
//	val := counter.Call("increment", nil)
func (b *BatchBuilder) Call(method string, params any) *BatchBuilderPromise {
	id := b.idGen()

	// Store request metadata without marshaling params yet
	req := &Request{
		JSONRPC: "3.0",
		Method:  method,
		Params:  RawMessage{}, // Empty placeholder
		ID:      id,
	}

	b.requests = append(b.requests, req)
	b.rawParams = append(b.rawParams, params)

	return &BatchBuilderPromise{
		batch: b,
		index: len(b.requests) - 1,
	}
}

// Call chains a method call on the result of the previous call.
// This automatically uses a batch-local reference (e.g., \0, \1) to reference
// the result from the promise's position in the batch.
//
// Example:
//
//	// Call method on the result of the first call
//	counter := batch.Call("createCounter", nil)
//	val := counter.Call("increment", nil) // Uses \0 to reference counter
func (p *BatchBuilderPromise) Call(method string, params any) *BatchBuilderPromise {
	id := p.batch.idGen()

	// Create batch-local reference: \0, \1, \2, etc.
	ref := fmt.Sprintf("\\%d", p.index)

	// Store request metadata without marshaling params yet
	req := &Request{
		JSONRPC: "3.0",
		Method:  method,
		Params:  RawMessage{}, // Empty placeholder
		ID:      id,
		Ref:     ref,
	}

	p.batch.requests = append(p.batch.requests, req)
	p.batch.rawParams = append(p.batch.rawParams, params)

	return &BatchBuilderPromise{
		batch: p.batch,
		index: len(p.batch.requests) - 1,
	}
}

// GetResult extracts and decodes the result at the specified index.
// Returns an error if the index is out of bounds or if the response contains an error.
// The result is decoded using the format specified when the BatchResults was created.
func (br *BatchResults) GetResult(index int) (any, error) {
	if index < 0 || index >= len(br.responses) {
		return nil, fmt.Errorf("index %d out of bounds (batch size: %d)", index, len(br.responses))
	}

	resp := br.responses[index]
	if resp == nil {
		return nil, fmt.Errorf("no response at index %d (notification)", index)
	}

	if resp.Error != nil {
		return nil, &Error{
			Code:    resp.Error.Code,
			Message: resp.Error.Message,
			Data:    resp.Error.Data,
		}
	}

	// Decode the raw result using the appropriate format
	format := br.format
	if format == "" {
		format = MimeTypeJSON // default to JSON
	}
	var result any
	if err := resp.Result.UnmarshalValue(&result, format); err != nil {
		return nil, fmt.Errorf("failed to decode result: %w", err)
	}

	return result, nil
}

// DecodeResult decodes the result at the specified index into the provided pointer.
// Returns an error if the index is out of bounds or if the response contains an error.
func (br *BatchResults) DecodeResult(index int, result any) error {
	if index < 0 || index >= len(br.responses) {
		return fmt.Errorf("index %d out of bounds (batch size: %d)", index, len(br.responses))
	}

	resp := br.responses[index]
	if resp == nil {
		return fmt.Errorf("no response at index %d (notification)", index)
	}

	if resp.Error != nil {
		return &Error{
			Code:    resp.Error.Code,
			Message: resp.Error.Message,
			Data:    resp.Error.Data,
		}
	}

	// Decode the raw result using the appropriate format
	format := br.format
	if format == "" {
		format = MimeTypeJSON // default to JSON
	}
	if err := resp.Result.UnmarshalValue(result, format); err != nil {
		return fmt.Errorf("failed to decode result: %w", err)
	}

	return nil
}

// GetResults returns all decoded results from the batch.
// Returns an error at the first response that contains an error.
// All results are decoded from JSON format.
func (br *BatchResults) GetResults() ([]any, error) {
	results := make([]any, len(br.responses))
	for i, resp := range br.responses {
		if resp.Error != nil {
			return nil, fmt.Errorf("error at index %d: %w", i, &Error{
				Code:    resp.Error.Code,
				Message: resp.Error.Message,
				Data:    resp.Error.Data,
			})
		}

		// Decode the raw result
		var result any
		if err := resp.Result.UnmarshalValue(&result, MimeTypeJSON); err != nil {
			return nil, fmt.Errorf("failed to decode result at index %d: %w", i, err)
		}
		results[i] = result
	}
	return results, nil
}

// GetResponse returns the raw Response at the specified index.
func (br *BatchResults) GetResponse(index int) (*Response, error) {
	if index < 0 || index >= len(br.responses) {
		return nil, fmt.Errorf("index %d out of bounds (batch size: %d)", index, len(br.responses))
	}
	return br.responses[index], nil
}

// Len returns the number of responses in the batch.
func (br *BatchResults) Len() int {
	return len(br.responses)
}

// HasError returns true if any response in the batch contains an error.
func (br *BatchResults) HasError() bool {
	for _, resp := range br.responses {
		if resp.Error != nil {
			return true
		}
	}
	return false
}

// BatchRequest represents a request in a batch call.
type BatchRequest struct {
	Ref            string // Optional reference to invoke method on
	Method         string // Method name to call
	Params         any    // Method parameters
	IsNotification bool   // If true, no response will be returned (ID will be nil)
}

// ExecuteBatch executes a batch of requests using the builder pattern with automatic
// batch-local reference management. The provided function receives a BatchBuilder
// that collects requests. When the function returns, the batch is sent via the
// provided Caller and responses are returned.
//
// Example:
//
//	resp, err := ExecuteBatch(peer, func(b *BatchBuilder) error {
//	    counter := b.Call("createCounter", nil)
//	    counter.Call("increment", nil) // Uses \0 reference
//	    counter.Call("increment", nil) // Uses \0 reference
//	    counter.Call("getValue", nil)  // Uses \0 reference
//	    return nil
//	})
func ExecuteBatch(caller Caller, fn func(*BatchBuilder) error) (*BatchResults, error) {
	// Create a simple counter for generating IDs
	var idCounter atomic.Int64

	// Create batch with ID generator
	batch := newBatchBuilder(func() any {
		return float64(idCounter.Add(1))
	})

	// Execute user's batch building function
	if err := fn(batch); err != nil {
		return nil, fmt.Errorf("batch builder error: %w", err)
	}

	// Convert builder requests to BatchRequest format
	batchReqs := make([]BatchRequest, len(batch.requests))
	for i, req := range batch.requests {
		batchReqs[i] = BatchRequest{
			Ref:            req.Ref,
			Method:         req.Method,
			Params:         batch.rawParams[i], // Use raw params, not marshaled
			IsNotification: req.ID == nil,
		}
	}

	// Send batch via the caller
	return caller.CallBatch(batchReqs)
}
