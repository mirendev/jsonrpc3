package jsonrpc3

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Peer represents a bidirectional JSON-RPC 3.0 peer over a stream-based transport.
// It supports full bidirectional communication where both peers can initiate method
// calls at any time. Works with any io.Reader/io.Writer pair (pipes, sockets, etc.).
type Peer struct {
	reader      io.Reader
	writer      io.Writer
	session     *Session
	handler     *Handler
	rootObject  Object
	contentType string

	// Request tracking for concurrent requests
	pendingReqs sync.Map // id (any) -> chan *Response
	nextID      atomic.Int64

	// Reference ID generation
	refPrefix  string // Random connection-specific prefix for refs
	refCounter atomic.Int64

	// Message channels
	writeChan chan MessageSetConvertible // Messages to encode and send
	closeChan chan struct{}
	closeOnce sync.Once

	// Context for cancelling operations
	ctx    context.Context
	cancel context.CancelFunc

	// Error tracking
	connErr error
	errMu   sync.RWMutex
}

// NewPeer creates a new bidirectional JSON-RPC 3.0 peer over a stream-based transport.
// The rootObject handles incoming method calls from the remote peer.
// Default encoding is MimeTypeJSON.
//
// Options:
//   - WithContentType(contentType) - specify encoding format
//
// Example:
//
//	// Using pipes
//	r1, w1 := io.Pipe()
//	r2, w2 := io.Pipe()
//	peer1 := NewPeer(r1, w2, serverRoot)
//	peer2 := NewPeer(r2, w1, clientRoot)
func NewPeer(reader io.Reader, writer io.Writer, rootObject Object, opts ...ClientOption) (*Peer, error) {
	// Apply options with defaults
	options := &clientOptions{
		contentType: MimeTypeJSON,
	}
	for _, opt := range opts {
		opt(options)
	}

	// Create context for lifecycle management
	ctx, cancel := context.WithCancel(context.Background())

	// Create session and handler
	session := NewSession()
	mimeTypes := []string{options.contentType}
	handler := NewHandler(session, rootObject, mimeTypes)

	// Generate random connection ID prefix
	refPrefix := generateConnID()

	peer := &Peer{
		reader:      reader,
		writer:      writer,
		session:     session,
		handler:     handler,
		rootObject:  rootObject,
		contentType: options.contentType,
		refPrefix:   refPrefix,
		writeChan:   make(chan MessageSetConvertible, 100),
		closeChan:   make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Start read and write loops
	go peer.readLoop()
	go peer.writeLoop()

	return peer, nil
}

// Stdio creates a new Peer using standard input and output streams.
// The rootObject handles incoming method calls from the remote peer.
// Default encoding is MimeTypeJSON.
func Stdio(rootObject Object, opts ...ClientOption) (*Peer, error) {
	if mimetype := os.Getenv("JSONRPC_CONTENT_TYPE"); mimetype != "" {
		switch mimetype {
		case MimeTypeJSON:
			opts = append(opts, WithJSON())
			// Default, do nothing
		case MimeTypeCBOR:
			opts = append(opts, WithCBOR())
		case MimeTypeCBORCompact:
			opts = append(opts, WithCompactCBOR())
		default:
			return nil, fmt.Errorf("unsupported JSON-RPC content type: %s", mimetype)
		}
	} else {
		opts = append([]ClientOption{WithJSON()}, opts...)
	}
	return NewPeer(os.Stdin, os.Stdout, rootObject, opts...)
}

// Call invokes a method on the remote peer and waits for the response.
// Use the ToRef option to call a method on a remote object reference.
func (p *Peer) Call(method string, params any, result any, opts ...CallOption) error {
	// Check if connection is closed
	if err := p.getError(); err != nil {
		return err
	}

	// Apply options
	var options callOptions
	for _, opt := range opts {
		opt.apply(&options)
	}

	// Generate request ID
	// Use float64 because JSON unmarshaling converts numbers to float64
	id := float64(p.nextID.Add(1))

	// Create request
	req, err := NewRequestWithFormat(method, params, id, p.contentType)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if options.ref != nil {
		req.Ref = options.ref.Ref
	}

	// Create response channel
	respChan := make(chan *Response, 1)
	p.pendingReqs.Store(id, respChan)
	defer p.pendingReqs.Delete(id)

	// Send request via write channel (will be encoded in writeLoop)
	select {
	case p.writeChan <- req:
		// Successfully queued for sending
	case <-p.closeChan:
		return fmt.Errorf("connection closed")
	}

	// Wait for response with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	select {
	case resp := <-respChan:
		if resp.Error != nil {
			return resp.Error
		}

		// Decode result if provided
		if result != nil && resp.Result != nil {
			params := NewParamsWithFormat(resp.Result, p.contentType)
			if err := params.Decode(result); err != nil {
				return fmt.Errorf("failed to decode result: %w", err)
			}
		}
		return nil

	case <-ctx.Done():
		return ctx.Err()

	case <-p.closeChan:
		return fmt.Errorf("connection closed")
	}
}

// Notify sends a notification (no response expected).
// Use the ToRef option to send a notification to a remote object reference.
func (p *Peer) Notify(method string, params any, opts ...CallOption) error {
	// Check if connection is closed
	if err := p.getError(); err != nil {
		return err
	}

	// Apply options
	var options callOptions
	for _, opt := range opts {
		opt.apply(&options)
	}

	// Create notification (nil ID)
	req, err := NewRequestWithFormat(method, params, nil, p.contentType)
	if err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}

	if options.ref != nil {
		req.Ref = options.ref.Ref
	}

	// Send notification via write channel (will be encoded in writeLoop)
	select {
	case p.writeChan <- req:
		return nil
	case <-p.closeChan:
		return fmt.Errorf("connection closed")
	}
}

// RegisterObject registers a local object that the remote peer can call.
// If ref is empty, a reference ID is auto-generated using the connection's prefix and counter.
// Returns the reference ID that was used (either the provided one or the generated one).
func (p *Peer) RegisterObject(ref string, obj Object) string {
	if ref == "" {
		counter := p.refCounter.Add(1)
		ref = fmt.Sprintf("%s-%d", p.refPrefix, counter)
	}
	p.handler.session.AddLocalRef(ref, obj)
	return ref
}

// UnregisterObject removes a registered local object.
func (p *Peer) UnregisterObject(ref string) {
	p.handler.session.RemoveLocalRef(ref)
}

// CallBatch sends a batch of requests and returns the results.
// This is the low-level batch interface required by the Caller interface.
// For a more convenient builder-based API, use the ExecuteBatch function.
//
// Example:
//
//	requests := []BatchRequest{
//	    {Method: "method1", Params: params1},
//	    {Method: "method2", Params: params2},
//	}
//	results, err := peer.CallBatch(requests)
func (p *Peer) CallBatch(batchReqs []BatchRequest) (*BatchResults, error) {
	// Check if connection is closed
	if err := p.getError(); err != nil {
		return nil, err
	}

	// Check if batch is empty
	if len(batchReqs) == 0 {
		return &BatchResults{responses: []*Response{}}, nil
	}

	// Convert BatchRequest to Request objects
	requests := make([]*Request, len(batchReqs))
	for i, breq := range batchReqs {
		var id any
		if !breq.IsNotification {
			id = float64(p.nextID.Add(1))
		}

		req, err := NewRequest(breq.Method, breq.Params, id)
		if err != nil {
			return nil, fmt.Errorf("failed to create request %d: %w", i, err)
		}

		if breq.Ref != "" {
			req.Ref = breq.Ref
		}

		requests[i] = req
	}

	// Create response channels for each request with an ID
	responseChans := make(map[any]chan *Response)
	for _, req := range requests {
		if req.ID != nil {
			respChan := make(chan *Response, 1)
			responseChans[req.ID] = respChan
			p.pendingReqs.Store(req.ID, respChan)
		}
	}

	// Clean up pending requests when done
	defer func() {
		for id := range responseChans {
			p.pendingReqs.Delete(id)
		}
	}()

	// Create batch message
	msgSet := &MessageSet{
		Messages: make([]Message, len(requests)),
		IsBatch:  true,
	}
	for i, req := range requests {
		msgSet.Messages[i] = Message{
			JSONRPC: req.JSONRPC,
			Method:  req.Method,
			Params:  req.Params,
			ID:      req.ID,
			Ref:     req.Ref,
		}
	}

	// Send batch via write channel
	select {
	case p.writeChan <- msgSet:
		// Successfully queued for sending
	case <-p.closeChan:
		return nil, fmt.Errorf("connection closed")
	}

	// Wait for all responses with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	responses := make([]*Response, len(requests))
	responsesReceived := 0

	// Collect responses
	for i, req := range requests {
		// Skip notifications (they don't have responses)
		if req.ID == nil {
			responses[i] = nil
			responsesReceived++
			continue
		}

		respChan, ok := responseChans[req.ID]
		if !ok {
			return nil, fmt.Errorf("missing response channel for request %v", req.ID)
		}

		select {
		case resp := <-respChan:
			responses[i] = resp
			responsesReceived++

		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for response %d: %w", i, ctx.Err())

		case <-p.closeChan:
			return nil, fmt.Errorf("connection closed while waiting for responses")
		}
	}

	return &BatchResults{responses: responses}, nil
}

// GetSession returns the peer's session.
func (p *Peer) GetSession() *Session {
	return p.session
}

// Close closes the peer connection gracefully.
func (p *Peer) Close() error {
	var err error
	p.closeOnce.Do(func() {
		// Cancel context to stop any pending operations
		p.cancel()

		close(p.closeChan)

		// Dispose all refs
		p.session.DisposeAll()

		// Wake up all pending requests
		p.pendingReqs.Range(func(key, value any) bool {
			if ch, ok := value.(chan *Response); ok {
				close(ch)
			}
			p.pendingReqs.Delete(key)
			return true
		})
	})
	return err
}

// readLoop continuously reads messages from the reader.
func (p *Peer) readLoop() {
	defer p.Close()

	codec := GetCodec(p.contentType)
	decoder := codec.NewMessageDecoder(p.reader)

	for {
		select {
		case <-p.closeChan:
			return
		default:
		}

		// Decode message set from stream (could be single message or batch)
		msgSet, err := decoder.Decode()
		if err != nil {
			if err != io.EOF {
				p.setError(fmt.Errorf("read error: %w", err))
			}
			return
		}

		// Check if all messages are requests or all are responses
		allRequests := true
		allResponses := true
		for i := range msgSet.Messages {
			if !msgSet.Messages[i].IsRequest() {
				allRequests = false
			}
			if !msgSet.Messages[i].IsResponse() {
				allResponses = false
			}
		}

		// Handle batch requests as a unit (preserves batch-local references)
		if allRequests && msgSet.IsBatch {
			go p.handleIncomingBatch(&msgSet)
		} else if allRequests {
			// Single request
			msg := &msgSet.Messages[0]
			go p.handleIncomingRequest(msg)
		} else if allResponses {
			// Process responses individually (no batch context needed)
			for i := range msgSet.Messages {
				msg := &msgSet.Messages[i]
				p.handleIncomingResponse(msg)
			}
		} else {
			// Mixed request/response batches are invalid
			// Send error responses for all requests in the batch
			go p.handleInvalidBatch(&msgSet)
		}
	}
}

// handleIncomingRequest processes an incoming request from the remote peer.
func (p *Peer) handleIncomingRequest(msg *Message) {
	req := msg.ToRequest()
	if req == nil {
		return
	}

	// Handle request via handler
	resp := p.handler.HandleRequest(req)

	// Send response if not a notification (will be encoded in writeLoop)
	if resp != nil {
		select {
		case p.writeChan <- resp:
		case <-p.closeChan:
		}
	}
}

// sendErrorForBatch sends error responses for all non-notification requests in a MessageSet.
func (p *Peer) sendErrorForBatch(msgSet *MessageSet, rpcErr *Error) {
	var responses BatchResponse

	// Create error responses for all requests (ignore responses)
	for i := range msgSet.Messages {
		msg := &msgSet.Messages[i]
		if msg.IsRequest() {
			req := msg.ToRequest()
			if req != nil && !req.IsNotification() {
				errResp := NewErrorResponse(req.ID, rpcErr, Version30)
				responses = append(responses, *errResp)
			}
		}
	}

	// Send error responses if any (will be encoded in writeLoop)
	if len(responses) > 0 {
		select {
		case p.writeChan <- responses:
		case <-p.closeChan:
		}
	}
}

// handleIncomingBatch processes a batch of incoming requests from the remote peer.
// Batch requests must be processed together to support batch-local references (\0, \1, etc.)
func (p *Peer) handleIncomingBatch(msgSet *MessageSet) {
	// Convert MessageSet to Batch
	batch, err := msgSet.ToBatch()
	if err != nil {
		// Failed to convert - send error responses for all requests
		p.sendErrorForBatch(msgSet, NewInternalError("failed to parse batch: "+err.Error()))
		return
	}

	// Handle batch via handler (preserves batch-local references)
	responses := p.handler.HandleBatch(batch)

	// Send batch response as a unit (will be encoded in writeLoop)
	if len(responses) > 0 {
		select {
		case p.writeChan <- responses:
		case <-p.closeChan:
		}
	}
}

// handleInvalidBatch handles a batch with mixed requests and responses.
// Sends error responses for all requests in the batch.
func (p *Peer) handleInvalidBatch(msgSet *MessageSet) {
	p.sendErrorForBatch(msgSet, NewInvalidRequestError("mixed requests and responses in batch"))
}

// handleIncomingResponse processes an incoming response to our request.
func (p *Peer) handleIncomingResponse(msg *Message) {
	resp := msg.ToResponse()
	if resp == nil {
		return
	}

	// Find pending request
	if value, ok := p.pendingReqs.Load(resp.ID); ok {
		if ch, ok := value.(chan *Response); ok {
			select {
			case ch <- resp:
			default:
				// Channel full or closed, ignore
			}
		}
	}
}

// writeLoop continuously sends queued messages to the writer.
func (p *Peer) writeLoop() {
	codec := GetCodec(p.contentType)

	for {
		select {
		case item := <-p.writeChan:
			// Convert to MessageSet
			msgSet := item.ToMessageSet()

			// Encode the message set
			data, err := codec.MarshalMessages(msgSet)
			if err != nil {
				p.notifyPendingRequest(item, fmt.Errorf("marshal error: %w", err))
				p.setError(fmt.Errorf("marshal error: %w", err))
				return
			}

			// Write to stream with newline for NDJSON compatibility
			if _, err := p.writer.Write(data); err != nil {
				p.notifyPendingRequest(item, fmt.Errorf("write error: %w", err))
				p.setError(fmt.Errorf("write error: %w", err))
				return
			}
			// Add newline after each JSON message (NDJSON format)
			if _, err := p.writer.Write([]byte{'\n'}); err != nil {
				p.notifyPendingRequest(item, fmt.Errorf("write error: %w", err))
				p.setError(fmt.Errorf("write error: %w", err))
				return
			}

		case <-p.closeChan:
			return
		}
	}
}

// notifyPendingRequest checks if the item is a Request with a pending waiter,
// and if so, sends a synthetic error response to unblock the caller.
func (p *Peer) notifyPendingRequest(item any, err error) {
	// Check if this is a Request (not a Response or BatchResponse)
	req, ok := item.(*Request)
	if !ok {
		return
	}

	// Only handle requests with IDs (not notifications)
	if req.ID == nil {
		return
	}

	// Check if there's a pending waiter for this request
	if value, ok := p.pendingReqs.Load(req.ID); ok {
		if respChan, ok := value.(chan *Response); ok {
			// Create synthetic error response
			syntheticResp := &Response{
				JSONRPC: req.JSONRPC,
				Error:   NewInternalError(fmt.Sprintf("Failed to send request: %v", err)),
				ID:      req.ID,
			}

			// Try to send the error response to the waiting channel
			select {
			case respChan <- syntheticResp:
				// Successfully notified the waiter
			default:
				// Channel full or closed, ignore
			}
		}
	}
}

// setError stores a connection error.
func (p *Peer) setError(err error) {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	if p.connErr == nil {
		p.connErr = err
	}
}

// getError retrieves any connection error.
func (p *Peer) getError() error {
	p.errMu.RLock()
	defer p.errMu.RUnlock()
	return p.connErr
}
