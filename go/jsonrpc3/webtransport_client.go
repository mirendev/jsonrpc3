package jsonrpc3

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/webtransport-go"
)

// WebTransportClient represents a WebTransport client for JSON-RPC 3.0.
// It supports full bidirectional communication where both client and server
// can initiate method calls at any time, built on HTTP/3 and QUIC.
type WebTransportClient struct {
	url         string
	session     *webtransport.Session
	wtSession   *Session // JSON-RPC session
	handler     *Handler
	rootObject  Object
	contentType string

	// Control stream for all JSON-RPC messages
	controlStream *webtransport.Stream

	// Request tracking for concurrent requests
	pendingReqs sync.Map // id (any) -> chan *Response
	nextID      atomic.Int64

	// Reference ID generation
	refPrefix  string // Random connection-specific prefix for refs
	refCounter atomic.Int64

	// Message channels
	writeChan chan any // Messages to encode and send
	closeChan chan struct{}
	closeOnce sync.Once

	// Context for cancelling operations
	ctx    context.Context
	cancel context.CancelFunc

	// Error tracking
	connErr error
	errMu   sync.RWMutex
}

// NewWebTransportClient creates a new WebTransport client and connects to the server.
// The rootObject handles incoming method calls from the server.
// Default encoding is MimeTypeCBOR.
//
// Options:
//   - WithContentType(contentType) - specify encoding format
//   - WithTLSConfig(tlsConfig) - customize TLS configuration
func NewWebTransportClient(url string, rootObject Object, opts ...ClientOption) (*WebTransportClient, error) {
	// Apply options with defaults
	options := &clientOptions{
		contentType: MimeTypeCBOR,
	}
	for _, opt := range opts {
		opt(options)
	}

	return newWebTransportClient(url, rootObject, options)
}

// newWebTransportClient is the internal constructor with options
func newWebTransportClient(url string, rootObject Object, options *clientOptions) (*WebTransportClient, error) {
	// Create context for lifecycle management
	ctx, cancel := context.WithCancel(context.Background())

	// Set up request headers for protocol negotiation
	reqHdr := http.Header{}
	reqHdr.Set("Content-Type", options.contentType)

	// Connect to WebTransport server
	dialer := webtransport.Dialer{
		TLSClientConfig: options.tlsConfig,
	}
	resp, session, err := dialer.Dial(ctx, url, reqHdr)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to %s: %w", url, err)
	}
	// Note: Do NOT close resp.Body - it will close the underlying HTTP/3 connection
	// and reset the WebTransport streams

	// Determine accepted content type from response
	acceptedContentType := options.contentType
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		acceptedContentType = ct
	}

	// Open control stream for bidirectional communication
	controlStream, err := session.OpenStreamSync(ctx)
	if err != nil {
		cancel()
		session.CloseWithError(0, "failed to open control stream")
		return nil, fmt.Errorf("failed to open control stream: %w", err)
	}

	// Create JSON-RPC session and handler
	wtSession := NewSession()
	mimeTypes := []string{acceptedContentType}
	handler := NewHandler(wtSession, rootObject, mimeTypes)

	client := &WebTransportClient{
		url:           url,
		session:       session,
		wtSession:     wtSession,
		handler:       handler,
		rootObject:    rootObject,
		contentType:   acceptedContentType,
		controlStream: controlStream,
		refPrefix:     generateConnID(),
		writeChan:     make(chan any, 100),
		closeChan:     make(chan struct{}),
		ctx:           ctx,
		cancel:        cancel,
	}

	// Start read and write loops
	go client.readLoop()
	go client.writeLoop()

	return client, nil
}

// Call invokes a method on the server and waits for the response.
// Use the ToRef option to call a method on a remote object reference.
func (c *WebTransportClient) Call(method string, params any, opts ...CallOption) (Value, error) {
	// Check if connection is closed
	if err := c.getError(); err != nil {
		return Value{}, err
	}

	// Apply options
	var options callOptions
	for _, opt := range opts {
		opt.apply(&options)
	}

	// Generate request ID
	// Use float64 because JSON unmarshaling converts numbers to float64
	id := float64(c.nextID.Add(1))

	// Create request
	req, err := NewRequestWithFormat(method, params, id, c.contentType)
	if err != nil {
		return Value{}, fmt.Errorf("failed to create request: %w", err)
	}

	if options.ref != nil {
		req.Ref = options.ref.Ref
	}

	// Create response channel
	respChan := make(chan *Response, 1)
	c.pendingReqs.Store(id, respChan)
	defer c.pendingReqs.Delete(id)

	// Send request via write channel (will be encoded in writeLoop)
	select {
	case c.writeChan <- req:
		// Successfully queued for sending
	case <-c.closeChan:
		return Value{}, fmt.Errorf("connection closed")
	}

	// Wait for response with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	select {
	case resp := <-respChan:
		if resp.Error != nil {
			return Value{}, resp.Error
		}

		// Return Value for lazy decoding
		if resp.Result != nil {
			codec := GetCodec(c.contentType)
			return NewValueWithCodec(resp.Result, codec), nil
		}
		return NilValue, nil

	case <-ctx.Done():
		return Value{}, fmt.Errorf("request timeout")

	case <-c.closeChan:
		return Value{}, fmt.Errorf("connection closed")
	}
}

// Notify sends a notification (no response expected).
// Use the ToRef option to send a notification to a remote object reference.
func (c *WebTransportClient) Notify(method string, params any, opts ...CallOption) error {
	// Check if connection is closed
	if err := c.getError(); err != nil {
		return err
	}

	// Apply options
	var options callOptions
	for _, opt := range opts {
		opt.apply(&options)
	}

	// Create notification (nil ID)
	req, err := NewRequestWithFormat(method, params, nil, c.contentType)
	if err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}

	if options.ref != nil {
		req.Ref = options.ref.Ref
	}

	// Send notification via write channel (will be encoded in writeLoop)
	select {
	case c.writeChan <- req:
		return nil
	case <-c.closeChan:
		return fmt.Errorf("connection closed")
	}
}

// CallBatch sends a batch of requests and returns the results.
func (c *WebTransportClient) CallBatch(batchReqs []BatchRequest) (*BatchResults, error) {
	// Check if connection is closed
	if err := c.getError(); err != nil {
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
			id = float64(c.nextID.Add(1))
		}

		req, err := NewRequestWithFormat(breq.Method, breq.Params, id, c.contentType)
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
			c.pendingReqs.Store(req.ID, respChan)
		}
	}

	// Clean up pending requests when done
	defer func() {
		for id := range responseChans {
			c.pendingReqs.Delete(id)
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

	// Send batch message
	select {
	case c.writeChan <- msgSet:
		// Successfully queued for sending
	case <-c.closeChan:
		return nil, fmt.Errorf("connection closed")
	}

	// Wait for all responses with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	responses := make([]*Response, len(requests))

	// Collect responses
	for i, req := range requests {
		// Skip notifications (they don't have responses)
		if req.ID == nil {
			responses[i] = nil
			continue
		}

		respChan, ok := responseChans[req.ID]
		if !ok {
			return nil, fmt.Errorf("missing response channel for request %v", req.ID)
		}

		select {
		case resp := <-respChan:
			responses[i] = resp

		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for response %d: %w", i, ctx.Err())

		case <-c.closeChan:
			return nil, fmt.Errorf("connection closed while waiting for responses")
		}
	}

	return &BatchResults{responses: responses}, nil
}

// RegisterObject registers a local object that the server can call.
// If ref is empty, a reference ID is auto-generated using the connection's prefix and counter.
// Returns a Reference instance that can be used to identify the object.
func (c *WebTransportClient) RegisterObject(ref string, obj Object) Reference {
	if ref == "" {
		counter := c.refCounter.Add(1)
		ref = fmt.Sprintf("%s-%d", c.refPrefix, counter)
	}
	c.handler.session.AddLocalRef(ref, obj)
	return NewReference(ref)
}

// UnregisterObject removes a registered local object.
func (c *WebTransportClient) UnregisterObject(ref Reference) {
	c.handler.session.RemoveLocalRef(ref.Ref)
}

// GetSession returns the client's session.
func (c *WebTransportClient) GetSession() *Session {
	return c.wtSession
}

// Close closes the WebTransport connection gracefully.
func (c *WebTransportClient) Close() error {
	var err error
	c.closeOnce.Do(func() {
		// Cancel context to stop any pending operations
		c.cancel()

		close(c.closeChan)

		// Close control stream
		if c.controlStream != nil {
			c.controlStream.Close()
		}

		// Close session
		if c.session != nil {
			err = c.session.CloseWithError(0, "client closed")
		}

		// Dispose all refs
		c.wtSession.DisposeAll()

		// Wake up all pending requests
		c.pendingReqs.Range(func(key, value any) bool {
			if ch, ok := value.(chan *Response); ok {
				close(ch)
			}
			c.pendingReqs.Delete(key)
			return true
		})
	})
	return err
}

// readLoop continuously reads messages from the control stream.
func (c *WebTransportClient) readLoop() {
	defer c.Close()

	// Give server a moment to accept the stream before first read
	// This prevents a race condition where the client tries to read before
	// the server has accepted the stream
	time.Sleep(300 * time.Millisecond)

	codec := GetCodec(c.contentType)
	decoder := codec.NewMessageDecoder(c.controlStream)

	for {
		select {
		case <-c.closeChan:
			return
		default:
		}

		// Decode message set from stream (could be single message or batch)
		msgSet, err := decoder.Decode()
		if err != nil {
			c.setError(fmt.Errorf("read error: %w", err))
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
			go c.handleIncomingBatch(&msgSet)
		} else if allRequests {
			// Single request
			msg := &msgSet.Messages[0]
			go c.handleIncomingRequest(msg)
		} else if allResponses {
			// Process responses individually (no batch context needed)
			for i := range msgSet.Messages {
				msg := &msgSet.Messages[i]
				c.handleIncomingResponse(msg)
			}
		} else {
			// Mixed request/response batches are invalid
			// Send error responses for all requests in the batch
			go c.handleInvalidBatch(&msgSet)
		}
	}
}

// handleIncomingRequest processes an incoming request from the server.
func (c *WebTransportClient) handleIncomingRequest(msg *Message) {
	req := msg.ToRequest()
	if req == nil {
		return
	}

	// Handle request via handler
	resp := c.handler.HandleRequest(req)

	// Send response if not a notification (will be encoded in writeLoop)
	if resp != nil {
		select {
		case c.writeChan <- resp:
		case <-c.closeChan:
		}
	}
}

// sendErrorForBatch sends error responses for all non-notification requests in a MessageSet.
func (c *WebTransportClient) sendErrorForBatch(msgSet *MessageSet, rpcErr *Error) {
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
		case c.writeChan <- responses:
		case <-c.closeChan:
		}
	}
}

// handleIncomingBatch processes a batch of incoming requests from the server.
// Batch requests must be processed together to support batch-local references (\0, \1, etc.)
func (c *WebTransportClient) handleIncomingBatch(msgSet *MessageSet) {
	// Convert MessageSet to Batch
	batch, err := msgSet.ToBatch()
	if err != nil {
		// Failed to convert - send error responses for all requests
		c.sendErrorForBatch(msgSet, NewInternalError("failed to parse batch: "+err.Error()))
		return
	}

	// Handle batch via handler (preserves batch-local references)
	responses := c.handler.HandleBatch(batch)

	// Send batch response as a unit (will be encoded in writeLoop)
	if len(responses) > 0 {
		select {
		case c.writeChan <- responses:
		case <-c.closeChan:
		}
	}
}

// handleInvalidBatch handles a batch with mixed requests and responses.
// Sends error responses for all requests in the batch.
func (c *WebTransportClient) handleInvalidBatch(msgSet *MessageSet) {
	c.sendErrorForBatch(msgSet, NewInvalidRequestError("mixed requests and responses in batch"))
}

// handleIncomingResponse processes an incoming response to our request.
func (c *WebTransportClient) handleIncomingResponse(msg *Message) {
	resp := msg.ToResponse()
	if resp == nil {
		return
	}

	// Find pending request
	if value, ok := c.pendingReqs.Load(resp.ID); ok {
		if ch, ok := value.(chan *Response); ok {
			select {
			case ch <- resp:
			default:
				// Channel full or closed, ignore
			}
		}
	}
}

// writeLoop continuously sends queued messages to the control stream.
func (c *WebTransportClient) writeLoop() {
	codec := GetCodec(c.contentType)

	for {
		select {
		case item := <-c.writeChan:
			// Check if item implements MessageSetConvertible
			convertible, ok := item.(MessageSetConvertible)
			if !ok {
				c.notifyPendingRequest(item, fmt.Errorf("unexpected message type: %T", item))
				c.setError(fmt.Errorf("unexpected message type: %T", item))
				return
			}

			// Convert to MessageSet
			msgSet := convertible.ToMessageSet()

			// Encode the message set
			data, err := codec.MarshalMessages(msgSet)
			if err != nil {
				c.notifyPendingRequest(item, fmt.Errorf("marshal error: %w", err))
				c.setError(fmt.Errorf("marshal error: %w", err))
				return
			}

			// Write to stream
			if _, err := c.controlStream.Write(data); err != nil {
				c.notifyPendingRequest(item, fmt.Errorf("write error: %w", err))
				c.setError(fmt.Errorf("write error: %w", err))
				return
			}

		case <-c.closeChan:
			return
		}
	}
}

// notifyPendingRequest checks if the item is a Request with a pending waiter,
// and if so, sends a synthetic error response to unblock the caller.
func (c *WebTransportClient) notifyPendingRequest(item any, err error) {
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
	if value, ok := c.pendingReqs.Load(req.ID); ok {
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
func (c *WebTransportClient) setError(err error) {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.connErr == nil {
		c.connErr = err
	}
}

// getError retrieves any connection error.
func (c *WebTransportClient) getError() error {
	c.errMu.RLock()
	defer c.errMu.RUnlock()
	return c.connErr
}
