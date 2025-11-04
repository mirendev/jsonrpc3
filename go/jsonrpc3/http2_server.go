package jsonrpc3

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
)

// HTTP2Server represents an HTTP/2 server for JSON-RPC 3.0.
// It uses HTTP/2 streams for efficient bidirectional communication.
type HTTP2Server struct {
	rootObject Object
	mimeTypes  []string
	server     *http.Server
	addr       string
}

// HTTP2Conn represents a single HTTP/2 connection handling JSON-RPC requests.
type HTTP2Conn struct {
	session     *Session
	handler     *Handler
	stream      io.ReadWriteCloser
	contentType string

	// Request tracking for concurrent requests
	pendingReqs sync.Map // id (any) -> chan *Response
	nextID      atomic.Int64

	// Reference ID generation
	refPrefix  string // Random connection-specific prefix for refs
	refCounter atomic.Int64

	// Message channels
	writeChan chan MessageSetConvertible
	closeChan chan struct{}
	closeOnce sync.Once

	// Context for cancelling operations
	ctx    context.Context
	cancel context.CancelFunc
}

// NewHTTP2Server creates a new HTTP/2 server.
// If mimeTypes is nil or empty, defaults to supporting JSON, CBOR, and CBOR-Compact.
func NewHTTP2Server(rootObject Object, mimeTypes []string) *HTTP2Server {
	if len(mimeTypes) == 0 {
		mimeTypes = []string{
			MimeTypeJSON,
			MimeTypeCBOR,
			MimeTypeCBORCompact,
		}
	}

	return &HTTP2Server{
		rootObject: rootObject,
		mimeTypes:  mimeTypes,
	}
}

// ListenAndServe starts the HTTP/2 server on the given address with TLS.
// It generates a self-signed certificate for testing purposes.
func (s *HTTP2Server) ListenAndServe(addr string) error {
	// Generate self-signed certificate
	cert, err := generateSelfSignedCert()
	if err != nil {
		return fmt.Errorf("failed to generate certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	return s.ListenAndServeTLS(addr, tlsConfig)
}

// ListenAndServeTLS starts the HTTP/2 server on the given address with the provided TLS config.
func (s *HTTP2Server) ListenAndServeTLS(addr string, tlsConfig *tls.Config) error {
	s.addr = addr

	// Create HTTP server with HTTP/2 support
	s.server = &http.Server{
		Addr:      addr,
		Handler:   s,
		TLSConfig: tlsConfig,
	}

	// Configure HTTP/2
	http2Server := &http2.Server{}
	if err := http2.ConfigureServer(s.server, http2Server); err != nil {
		return fmt.Errorf("failed to configure HTTP/2: %w", err)
	}

	// Start server with TLS
	return s.server.ListenAndServeTLS("", "")
}

// Close stops the HTTP/2 server.
func (s *HTTP2Server) Close() error {
	if s.server != nil {
		return s.server.Shutdown(context.Background())
	}
	return nil
}

// ServeHTTP handles an incoming HTTP/2 stream connection.
func (s *HTTP2Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if this is HTTP/2
	if r.ProtoMajor != 2 {
		http.Error(w, "HTTP/2 required", http.StatusHTTPVersionNotSupported)
		return
	}

	// Determine content type (default to CBOR)
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = MimeTypeCBOR
	}

	// Validate content type is supported
	supported := false
	for _, mt := range s.mimeTypes {
		if mt == contentType {
			supported = true
			break
		}
	}
	if !supported {
		http.Error(w, "Unsupported content type", http.StatusUnsupportedMediaType)
		return
	}

	// Set response headers for streaming
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Transfer-Encoding", "chunked")
	w.WriteHeader(http.StatusOK)

	// Flush headers to establish stream
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Create bidirectional stream wrapper
	stream := &http2Stream{
		reader: r.Body,
		writer: w,
	}

	// Create connection handler
	ctx, cancel := context.WithCancel(r.Context())
	conn := &HTTP2Conn{
		session:     NewSession(),
		handler:     nil, // Will be set below
		stream:      stream,
		contentType: contentType,
		refPrefix:   generateConnID(),
		writeChan:   make(chan MessageSetConvertible, 100),
		closeChan:   make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Create handler with this connection as caller (for bidirectional RPC)
	conn.handler = NewHandler(conn.session, s.rootObject, conn, s.mimeTypes)

	// Handle the connection
	conn.handle()
}

// handle processes the HTTP/2 stream connection.
func (c *HTTP2Conn) handle() {
	defer c.Close()

	// Start read and write loops
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		c.readLoop()
	}()

	go func() {
		defer wg.Done()
		c.writeLoop()
	}()

	// Wait for both loops to finish
	wg.Wait()
}

// readLoop continuously reads messages from the stream.
func (c *HTTP2Conn) readLoop() {
	defer c.Close()

	codec := GetCodec(c.contentType)
	decoder := codec.NewMessageDecoder(c.stream)

	for {
		select {
		case <-c.closeChan:
			return
		case <-c.ctx.Done():
			return
		default:
		}

		// Decode message set from stream
		msgSet, err := decoder.Decode()
		if err != nil {
			if err == io.EOF {
				return
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
			go c.handleInvalidBatch(&msgSet)
		}
	}
}

// writeLoop continuously sends queued messages to the stream.
func (c *HTTP2Conn) writeLoop() {
	codec := GetCodec(c.contentType)

	for {
		select {
		case item := <-c.writeChan:
			// Convert to MessageSet
			msgSet := item.ToMessageSet()

			// Encode the message set
			data, err := codec.MarshalMessages(msgSet)
			if err != nil {
				return
			}

			// Write to stream
			if _, err := c.stream.Write(data); err != nil {
				return
			}

			// Flush if possible
			if flusher, ok := c.stream.(*http2Stream); ok {
				if w, ok := flusher.writer.(http.Flusher); ok {
					w.Flush()
				}
			}

		case <-c.closeChan:
			return
		case <-c.ctx.Done():
			return
		}
	}
}

// handleIncomingRequest processes an incoming request.
func (c *HTTP2Conn) handleIncomingRequest(msg *Message) {
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

// handleIncomingBatch processes a batch of incoming requests.
func (c *HTTP2Conn) handleIncomingBatch(msgSet *MessageSet) {
	// Convert MessageSet to Batch
	batch, err := msgSet.ToBatch()
	if err != nil {
		c.sendErrorForBatch(msgSet, NewInternalError("failed to parse batch: "+err.Error()))
		return
	}

	// Handle batch via handler
	responses := c.handler.HandleBatch(batch)

	// Send batch response
	if len(responses) > 0 {
		select {
		case c.writeChan <- responses:
		case <-c.closeChan:
		}
	}
}

// handleInvalidBatch handles a batch with mixed requests and responses.
func (c *HTTP2Conn) handleInvalidBatch(msgSet *MessageSet) {
	c.sendErrorForBatch(msgSet, NewInvalidRequestError("mixed requests and responses in batch"))
}

// sendErrorForBatch sends error responses for all non-notification requests in a MessageSet.
func (c *HTTP2Conn) sendErrorForBatch(msgSet *MessageSet, rpcErr *Error) {
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
func (c *HTTP2Conn) handleIncomingResponse(msg *Message) {
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

// Call invokes a method on the remote peer (for bidirectional RPC).
func (c *HTTP2Conn) Call(method string, params any, opts ...CallOption) (Value, error) {
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
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
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
func (c *HTTP2Conn) Notify(method string, params any, opts ...CallOption) error {
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

// CallBatch sends a batch of requests (for bidirectional RPC).
func (c *HTTP2Conn) CallBatch(batchReqs []BatchRequest) (*BatchResults, error) {
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
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
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

// RegisterObject registers a local object that the remote peer can call.
func (c *HTTP2Conn) RegisterObject(ref string, obj Object) Reference {
	if ref == "" {
		counter := c.refCounter.Add(1)
		ref = fmt.Sprintf("%s-%d", c.refPrefix, counter)
	}
	c.handler.session.AddLocalRef(ref, obj)
	return NewReference(ref)
}

// UnregisterObject removes a registered local object.
func (c *HTTP2Conn) UnregisterObject(ref Reference) {
	c.handler.session.RemoveLocalRef(ref.Ref)
}

// GetSession returns the connection's session (implements Caller interface).
func (c *HTTP2Conn) GetSession() *Session {
	return c.session
}

// Close closes the connection.
func (c *HTTP2Conn) Close() error {
	c.closeOnce.Do(func() {
		c.cancel()
		close(c.closeChan)
		c.stream.Close()
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
	return nil
}

// http2Stream wraps an HTTP/2 request/response pair to provide io.ReadWriteCloser.
type http2Stream struct {
	reader io.ReadCloser
	writer io.Writer
	mu     sync.Mutex
}

func (s *http2Stream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

func (s *http2Stream) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writer.Write(p)
}

func (s *http2Stream) Close() error {
	return s.reader.Close()
}
