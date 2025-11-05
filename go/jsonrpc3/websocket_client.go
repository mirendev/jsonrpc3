package jsonrpc3

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketClient represents a WebSocket client for JSON-RPC 3.0.
// It supports full bidirectional communication where both client and server
// can initiate method calls at any time.
type WebSocketClient struct {
	url         string
	conn        *websocket.Conn
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
	writeChan chan []byte
	closeChan chan struct{}
	closeOnce sync.Once

	// Context for cancelling operations
	ctx    context.Context
	cancel context.CancelFunc

	// Error tracking
	connErr error
	errMu   sync.RWMutex
}

// clientOptions holds common configuration options for JSON-RPC clients.
type clientOptions struct {
	contentType string
	tlsConfig   *tls.Config
	headers     http.Header
}

// ClientOption is a functional option for configuring a WebSocketClient.
type ClientOption func(*clientOptions)

// WithContentType sets the content type for encoding/decoding messages.
// Supported formats: "application/json", "application/cbor", "application/cbor; format=compact"
// This option works for both WebSocket and WebTransport clients.
func WithContentType(contentType string) ClientOption {
	return func(o *clientOptions) {
		o.contentType = contentType
	}
}

func WithJSON() ClientOption {
	return WithContentType(MimeTypeJSON)
}

func WithCBOR() ClientOption {
	return WithContentType(MimeTypeCBOR)
}

func WithCompactCBOR() ClientOption {
	return WithContentType(MimeTypeCBORCompact)
}

// WithTLSConfig sets a custom TLS configuration for the WebTransport connection.
func WithTLSConfig(tlsConfig *tls.Config) ClientOption {
	return func(o *clientOptions) {
		o.tlsConfig = tlsConfig
	}
}

// WithHeader adds a custom header to be sent with requests.
// For WebSocket clients, headers are sent during the initial handshake.
// Can be called multiple times to add multiple headers.
func WithHeader(key, value string) ClientOption {
	return func(o *clientOptions) {
		if o.headers == nil {
			o.headers = make(http.Header)
		}
		o.headers.Add(key, value)
	}
}

// WithHeaders sets multiple custom headers to be sent with requests.
// This replaces any headers previously set.
func WithHeaders(headers http.Header) ClientOption {
	return func(o *clientOptions) {
		o.headers = headers.Clone()
	}
}

// NewWebSocketClient creates a new WebSocket client and connects to the server.
// The rootObject handles incoming method calls from the server.
// Default encoding is "application/cbor".
//
// Options:
//   - WithContentType(contentType) - specify encoding format
func NewWebSocketClient(url string, rootObject Object, opts ...ClientOption) (*WebSocketClient, error) {
	// Apply options with defaults
	options := &clientOptions{
		contentType: MimeTypeCBOR,
	}
	for _, opt := range opts {
		opt(options)
	}

	return newWebSocketClient(url, rootObject, options)
}

// newWebSocketClient is the internal constructor with options
func newWebSocketClient(url string, rootObject Object, options *clientOptions) (*WebSocketClient, error) {
	// Create context for lifecycle management
	ctx, cancel := context.WithCancel(context.Background())

	// Propose protocol via Sec-WebSocket-Protocol header
	protocol := "jsonrpc3"
	switch options.contentType {
	case MimeTypeCBOR:
		protocol = "jsonrpc3.cbor"
	case MimeTypeCBORCompact:
		protocol = "jsonrpc3.cbor-compact"
	default:
		protocol = "jsonrpc3.json"
	}

	// Connect to WebSocket using context
	dialer := websocket.Dialer{
		Subprotocols: []string{protocol},
	}

	// Prepare headers for WebSocket handshake
	headers := options.headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}

	conn, resp, err := dialer.DialContext(ctx, url, headers)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to %s: %w", url, err)
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
	}()

	// Determine accepted protocol
	acceptedProtocol := conn.Subprotocol()
	acceptedContentType := options.contentType
	switch acceptedProtocol {
	case "jsonrpc3.json":
		acceptedContentType = MimeTypeJSON
	case "jsonrpc3.cbor":
		acceptedContentType = MimeTypeCBOR
	case "jsonrpc3.cbor-compact":
		acceptedContentType = MimeTypeCBORCompact
	}

	// Create client
	session := NewSession()

	// Generate random connection ID prefix
	refPrefix := generateConnID()

	// Create client first (without handler)
	client := &WebSocketClient{
		url:         url,
		conn:        conn,
		session:     session,
		handler:     nil, // Will be set below
		rootObject:  rootObject,
		contentType: acceptedContentType,
		refPrefix:   refPrefix,
		writeChan:   make(chan []byte, 100),
		closeChan:   make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Now create handler with client as caller
	mimeTypes := []string{acceptedContentType}
	client.handler = NewHandler(session, rootObject, client, mimeTypes)

	// Start read and write loops
	go client.readLoop()
	go client.writeLoop()

	return client, nil
}

// Call invokes a method on the server and waits for the response.
// Use the ToRef option to call a method on a remote object reference.
func (c *WebSocketClient) Call(method string, params any, opts ...CallOption) (Value, error) {
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

	// Encode and send request
	codec := GetCodec(c.contentType)
	msgSet := req.ToMessageSet()
	reqData, err := codec.MarshalMessages(msgSet)
	if err != nil {
		return Value{}, fmt.Errorf("failed to encode request: %w", err)
	}

	// Send via write channel
	select {
	case c.writeChan <- reqData:
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
func (c *WebSocketClient) Notify(method string, params any, opts ...CallOption) error {
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

	// Encode and send
	codec := GetCodec(c.contentType)
	msgSet := req.ToMessageSet()
	reqData, err := codec.MarshalMessages(msgSet)
	if err != nil {
		return fmt.Errorf("failed to encode notification: %w", err)
	}

	// Send via write channel
	select {
	case c.writeChan <- reqData:
		return nil
	case <-c.closeChan:
		return fmt.Errorf("connection closed")
	}
}

// CallBatch sends a batch of requests and returns the results.
func (c *WebSocketClient) CallBatch(batchReqs []BatchRequest) (*BatchResults, error) {
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

	// Encode and send batch
	codec := GetCodec(c.contentType)
	batchData, err := codec.MarshalMessages(*msgSet)
	if err != nil {
		return nil, fmt.Errorf("failed to encode batch: %w", err)
	}

	// Send via write channel
	select {
	case c.writeChan <- batchData:
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
func (c *WebSocketClient) RegisterObject(ref string, obj Object) Reference {
	if ref == "" {
		counter := c.refCounter.Add(1)
		ref = fmt.Sprintf("%s-%d", c.refPrefix, counter)
	}
	c.handler.session.AddLocalRef(ref, obj)
	return NewReference(ref)
}

// UnregisterObject removes a registered local object.
func (c *WebSocketClient) UnregisterObject(ref Reference) {
	c.handler.session.RemoveLocalRef(ref.Ref)
}

// GetSession returns the client's session.
func (c *WebSocketClient) GetSession() *Session {
	return c.session
}

// Close closes the WebSocket connection gracefully.
func (c *WebSocketClient) Close() error {
	var err error
	c.closeOnce.Do(func() {
		// Cancel context to stop any pending operations
		c.cancel()

		close(c.closeChan)

		// Send close message
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
		c.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))

		// Close connection
		err = c.conn.Close()

		// Dispose all refs
		c.session.DisposeAll()

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

// readLoop continuously reads messages from the WebSocket connection.
func (c *WebSocketClient) readLoop() {
	defer c.Close()

	codec := GetCodec(c.contentType)

	for {
		select {
		case <-c.closeChan:
			return
		default:
		}

		// Read message
		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			c.setError(fmt.Errorf("read error: %w", err))
			return
		}

		if messageType != websocket.BinaryMessage && messageType != websocket.TextMessage {
			continue
		}

		// Decode as MessageSet
		msgSet, err := codec.UnmarshalMessages(data)
		if err != nil {
			// Invalid message, ignore
			continue
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
			go c.handleIncomingBatch(codec, &msgSet)
		} else if allRequests {
			// Single request
			msg := &msgSet.Messages[0]
			go c.handleIncomingRequest(codec, msg)
		} else if allResponses {
			// Process responses individually (no batch context needed)
			for i := range msgSet.Messages {
				msg := &msgSet.Messages[i]
				c.handleIncomingResponse(msg)
			}
		} else {
			// Mixed request/response batches are invalid
			// Send error responses for all requests in the batch
			go c.sendErrorForBatch(codec, &msgSet, NewInvalidRequestError("mixed requests and responses in batch"))
		}
	}
}

// handleIncomingRequest processes an incoming request from the server.
func (c *WebSocketClient) handleIncomingRequest(codec Codec, msg *Message) {
	req := msg.ToRequest()
	if req == nil {
		return
	}

	// Handle request via handler
	resp := c.handler.HandleRequest(req)

	// Send response if not a notification
	if resp != nil {
		msgSet := resp.ToMessageSet()
		respData, err := codec.MarshalMessages(msgSet)
		if err != nil {
			return
		}

		select {
		case c.writeChan <- respData:
		case <-c.closeChan:
		}
	}
}

// sendErrorForBatch sends error responses for all non-notification requests in a MessageSet.
func (c *WebSocketClient) sendErrorForBatch(codec Codec, msgSet *MessageSet, rpcErr *Error) {
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

	// Send error responses if any
	if len(responses) > 0 {
		batchMsgSet := responses.ToMessageSet()
		respData, err := codec.MarshalMessages(batchMsgSet)
		if err != nil {
			// Can't encode error responses - nothing more we can do
			return
		}

		select {
		case c.writeChan <- respData:
		case <-c.closeChan:
		}
	}
}

// handleIncomingBatch processes a batch of incoming requests from the server.
// Batch requests must be processed together to support batch-local references (\0, \1, etc.)
func (c *WebSocketClient) handleIncomingBatch(codec Codec, msgSet *MessageSet) {
	// Convert MessageSet to Batch
	batch, err := msgSet.ToBatch()
	if err != nil {
		// Failed to convert - send error responses for all requests
		c.sendErrorForBatch(codec, msgSet, NewInternalError("failed to parse batch: "+err.Error()))
		return
	}

	// Handle batch via handler (preserves batch-local references)
	responses := c.handler.HandleBatch(batch)

	// Send batch response as a unit
	if len(responses) > 0 {
		batchMsgSet := responses.ToMessageSet()
		respData, err := codec.MarshalMessages(batchMsgSet)
		if err != nil {
			// Failed to encode responses - send error responses for all requests
			c.sendErrorForBatch(codec, msgSet, NewInternalError("failed to encode response: "+err.Error()))
			return
		}

		select {
		case c.writeChan <- respData:
		case <-c.closeChan:
		}
	}
}

// handleIncomingResponse processes an incoming response to our request.
func (c *WebSocketClient) handleIncomingResponse(msg *Message) {
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

// writeLoop continuously sends queued messages to the WebSocket connection.
func (c *WebSocketClient) writeLoop() {
	for {
		select {
		case data := <-c.writeChan:
			if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
				c.setError(fmt.Errorf("write error: %w", err))
				return
			}

		case <-c.closeChan:
			return
		}
	}
}

// setError stores a connection error.
func (c *WebSocketClient) setError(err error) {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.connErr == nil {
		c.connErr = err
	}
}

// getError retrieves any connection error.
func (c *WebSocketClient) getError() error {
	c.errMu.RLock()
	defer c.errMu.RUnlock()
	return c.connErr
}
