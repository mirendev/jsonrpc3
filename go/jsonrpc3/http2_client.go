package jsonrpc3

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
)

// HTTP2Client represents an HTTP/2 client for JSON-RPC 3.0.
// It uses HTTP/2 streams for efficient bidirectional communication.
type HTTP2Client struct {
	url         string
	session     *Session
	handler     *Handler
	rootObject  Object
	contentType string

	// HTTP/2 connection
	httpClient *http.Client
	stream     io.ReadWriteCloser
	cancel     context.CancelFunc

	// Request tracking for concurrent requests
	pendingReqs sync.Map // id (any) -> chan *Response
	nextID      atomic.Int64

	// Reference ID generation
	refPrefix  string
	refCounter atomic.Int64

	// Message channels
	writeChan chan any
	closeChan chan struct{}
	closeOnce sync.Once

	// Context for operations
	ctx context.Context

	// Error tracking
	connErr error
	errMu   sync.RWMutex
}

// NewHTTP2Client creates a new HTTP/2 client and connects to the server.
// The rootObject handles incoming method calls from the server.
// Default encoding is MimeTypeCBOR.
//
// Options:
//   - WithContentType(contentType) - specify encoding format
//   - WithTLSConfig(tlsConfig) - customize TLS configuration
func NewHTTP2Client(url string, rootObject Object, opts ...ClientOption) (*HTTP2Client, error) {
	// Apply options with defaults
	options := &clientOptions{
		contentType: MimeTypeCBOR,
	}
	for _, opt := range opts {
		opt(options)
	}

	return newHTTP2Client(url, rootObject, options)
}

// newHTTP2Client is the internal constructor with options.
func newHTTP2Client(url string, rootObject Object, options *clientOptions) (*HTTP2Client, error) {
	// Create context for lifecycle management
	ctx, cancel := context.WithCancel(context.Background())

	// Create HTTP/2 transport
	transport := &http2.Transport{
		TLSClientConfig: options.tlsConfig,
	}

	httpClient := &http.Client{
		Transport: transport,
		// Don't follow redirects
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Create JSON-RPC session
	session := NewSession()

	// Create client first (without handler)
	client := &HTTP2Client{
		url:         url,
		session:     session,
		handler:     nil, // Will be set below
		rootObject:  rootObject,
		contentType: options.contentType,
		httpClient:  httpClient,
		refPrefix:   generateConnID(),
		writeChan:   make(chan any, 100),
		closeChan:   make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Create handler with client as caller
	mimeTypes := []string{options.contentType}
	client.handler = NewHandler(session, rootObject, client, mimeTypes)

	// Connect to server
	if err := client.connect(); err != nil {
		cancel()
		return nil, err
	}

	// Start read and write loops
	go client.readLoop()
	go client.writeLoop()

	return client, nil
}

// connect establishes the HTTP/2 connection to the server.
func (c *HTTP2Client) connect() error {
	// Create streaming request
	pr, pw := io.Pipe()

	req, err := http.NewRequestWithContext(c.ctx, http.MethodPost, c.url, pr)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", c.contentType)
	req.Header.Set("Accept", c.contentType)

	// Make request in background
	respChan := make(chan *http.Response, 1)
	errChan := make(chan error, 1)

	go func() {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			errChan <- err
			return
		}
		respChan <- resp
	}()

	// Wait for response or timeout
	select {
	case err := <-errChan:
		pw.Close()
		return fmt.Errorf("failed to connect: %w", err)
	case resp := <-respChan:
		// Check status
		if resp.StatusCode != http.StatusOK {
			pw.Close()
			resp.Body.Close()
			return fmt.Errorf("server returned status %d", resp.StatusCode)
		}

		// Create bidirectional stream
		c.stream = &http2ClientStream{
			reader: resp.Body,
			writer: pw,
		}

		return nil
	case <-time.After(5 * time.Second):
		pw.Close()
		return fmt.Errorf("connection timeout")
	}
}

// Call invokes a method on the server and waits for the response.
func (c *HTTP2Client) Call(method string, params any, opts ...CallOption) (Value, error) {
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

	// Send request
	select {
	case c.writeChan <- req:
	case <-c.closeChan:
		return Value{}, fmt.Errorf("connection closed")
	}

	// Wait for response
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	select {
	case resp := <-respChan:
		if resp.Error != nil {
			return Value{}, resp.Error
		}
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
func (c *HTTP2Client) Notify(method string, params any, opts ...CallOption) error {
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

	// Send notification
	select {
	case c.writeChan <- req:
		return nil
	case <-c.closeChan:
		return fmt.Errorf("connection closed")
	}
}

// CallBatch sends a batch of requests and returns the results.
func (c *HTTP2Client) CallBatch(batchReqs []BatchRequest) (*BatchResults, error) {
	// Check if connection is closed
	if err := c.getError(); err != nil {
		return nil, err
	}

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

	// Send batch
	select {
	case c.writeChan <- msgSet:
	case <-c.closeChan:
		return nil, fmt.Errorf("connection closed")
	}

	// Wait for all responses
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	responses := make([]*Response, len(requests))

	for i, req := range requests {
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
func (c *HTTP2Client) RegisterObject(ref string, obj Object) Reference {
	if ref == "" {
		counter := c.refCounter.Add(1)
		ref = fmt.Sprintf("%s-%d", c.refPrefix, counter)
	}
	c.handler.session.AddLocalRef(ref, obj)
	return NewReference(ref)
}

// UnregisterObject removes a registered local object.
func (c *HTTP2Client) UnregisterObject(ref Reference) {
	c.handler.session.RemoveLocalRef(ref.Ref)
}

// GetSession returns the client's session.
func (c *HTTP2Client) GetSession() *Session {
	return c.session
}

// Close closes the HTTP/2 connection.
func (c *HTTP2Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.cancel()
		close(c.closeChan)

		if c.stream != nil {
			c.stream.Close()
		}

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

// readLoop continuously reads messages from the stream.
func (c *HTTP2Client) readLoop() {
	defer c.Close()

	codec := GetCodec(c.contentType)
	decoder := codec.NewMessageDecoder(c.stream)

	for {
		select {
		case <-c.closeChan:
			return
		default:
		}

		// Decode message set
		msgSet, err := decoder.Decode()
		if err != nil {
			if err != io.EOF {
				c.setError(fmt.Errorf("read error: %w", err))
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

		// Handle batch requests as a unit
		if allRequests && msgSet.IsBatch {
			go c.handleIncomingBatch(&msgSet)
		} else if allRequests {
			msg := &msgSet.Messages[0]
			go c.handleIncomingRequest(msg)
		} else if allResponses {
			for i := range msgSet.Messages {
				msg := &msgSet.Messages[i]
				c.handleIncomingResponse(msg)
			}
		} else {
			go c.handleInvalidBatch(&msgSet)
		}
	}
}

// writeLoop continuously sends queued messages to the stream.
func (c *HTTP2Client) writeLoop() {
	codec := GetCodec(c.contentType)

	for {
		select {
		case item := <-c.writeChan:
			// Check if item implements MessageSetConvertible
			convertible, ok := item.(MessageSetConvertible)
			if !ok {
				c.setError(fmt.Errorf("unexpected message type: %T", item))
				return
			}

			// Convert to MessageSet
			msgSet := convertible.ToMessageSet()

			// Encode the message set
			data, err := codec.MarshalMessages(msgSet)
			if err != nil {
				c.setError(fmt.Errorf("marshal error: %w", err))
				return
			}

			// Write to stream
			if _, err := c.stream.Write(data); err != nil {
				c.setError(fmt.Errorf("write error: %w", err))
				return
			}

		case <-c.closeChan:
			return
		}
	}
}

// handleIncomingRequest processes an incoming request from the server.
func (c *HTTP2Client) handleIncomingRequest(msg *Message) {
	req := msg.ToRequest()
	if req == nil {
		return
	}

	// Handle request via handler
	resp := c.handler.HandleRequest(req)

	// Send response if not a notification
	if resp != nil {
		select {
		case c.writeChan <- resp:
		case <-c.closeChan:
		}
	}
}

// handleIncomingBatch processes a batch of incoming requests from the server.
func (c *HTTP2Client) handleIncomingBatch(msgSet *MessageSet) {
	batch, err := msgSet.ToBatch()
	if err != nil {
		c.sendErrorForBatch(msgSet, NewInternalError("failed to parse batch: "+err.Error()))
		return
	}

	responses := c.handler.HandleBatch(batch)

	if len(responses) > 0 {
		select {
		case c.writeChan <- responses:
		case <-c.closeChan:
		}
	}
}

// handleInvalidBatch handles a batch with mixed requests and responses.
func (c *HTTP2Client) handleInvalidBatch(msgSet *MessageSet) {
	c.sendErrorForBatch(msgSet, NewInvalidRequestError("mixed requests and responses in batch"))
}

// sendErrorForBatch sends error responses for all non-notification requests in a MessageSet.
func (c *HTTP2Client) sendErrorForBatch(msgSet *MessageSet, rpcErr *Error) {
	var responses BatchResponse

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

	if len(responses) > 0 {
		select {
		case c.writeChan <- responses:
		case <-c.closeChan:
		}
	}
}

// handleIncomingResponse processes an incoming response to our request.
func (c *HTTP2Client) handleIncomingResponse(msg *Message) {
	resp := msg.ToResponse()
	if resp == nil {
		return
	}

	if value, ok := c.pendingReqs.Load(resp.ID); ok {
		if ch, ok := value.(chan *Response); ok {
			select {
			case ch <- resp:
			default:
			}
		}
	}
}

// setError stores a connection error.
func (c *HTTP2Client) setError(err error) {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.connErr == nil {
		c.connErr = err
	}
}

// getError retrieves any connection error.
func (c *HTTP2Client) getError() error {
	c.errMu.RLock()
	defer c.errMu.RUnlock()
	return c.connErr
}

// http2ClientStream wraps a request body writer and response body reader.
type http2ClientStream struct {
	reader io.ReadCloser
	writer io.WriteCloser
	mu     sync.Mutex
}

func (s *http2ClientStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

func (s *http2ClientStream) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writer.Write(p)
}

func (s *http2ClientStream) Close() error {
	s.writer.Close()
	return s.reader.Close()
}
