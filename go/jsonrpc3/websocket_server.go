package jsonrpc3

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketHandler is an HTTP handler that upgrades connections to WebSocket
// for bidirectional JSON-RPC 3.0 communication.
type WebSocketHandler struct {
	rootObject Object
	mimeTypes  []string
	upgrader   websocket.Upgrader
}

// NewWebSocketHandler creates a new WebSocket handler.
// The rootObject handles incoming method calls from clients.
func NewWebSocketHandler(rootObject Object) *WebSocketHandler {
	return &WebSocketHandler{
		rootObject: rootObject,
		mimeTypes: []string{
			MimeTypeJSON,
			MimeTypeCBOR,
			MimeTypeCBORCompact,
		},
		upgrader: websocket.Upgrader{
			Subprotocols: []string{
				"jsonrpc3.json",
				"jsonrpc3.cbor",
				"jsonrpc3.cbor-compact",
			},
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins by default
				// In production, this should be more restrictive
				return true
			},
		},
	}
}

// ServeHTTP implements http.Handler, upgrading HTTP connections to WebSocket.
func (h *WebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Upgrade to WebSocket
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "Failed to upgrade to WebSocket", http.StatusInternalServerError)
		return
	}

	// Determine content type from accepted subprotocol
	contentType := MimeTypeJSON
	switch conn.Subprotocol() {
	case "jsonrpc3.cbor":
		contentType = MimeTypeCBOR
	case "jsonrpc3.cbor-compact":
		contentType = MimeTypeCBORCompact
	}

	// Create connection handler
	wsConn := newWebSocketConn(conn, h.rootObject, contentType, h.mimeTypes)

	// Start handling the connection
	wsConn.handle()
}

// WebSocketConn represents a single WebSocket connection.
// It manages bidirectional JSON-RPC 3.0 communication for one client.
type WebSocketConn struct {
	conn        *websocket.Conn
	session     *Session
	handler     *Handler
	contentType string

	// Request tracking for server-initiated requests
	pendingReqs sync.Map // id (any) -> chan *Response
	nextID      atomic.Int64

	// Reference ID generation
	refPrefix  string // Random connection-specific prefix for refs
	refCounter atomic.Int64

	// Message channels
	writeChan chan []byte
	closeChan chan struct{}
	closeOnce sync.Once

	// Error tracking
	connErr error
	errMu   sync.RWMutex
}

// newWebSocketConn creates a new WebSocket connection handler.
func newWebSocketConn(conn *websocket.Conn, rootObject Object, contentType string, mimeTypes []string) *WebSocketConn {
	session := NewSession()

	// Create connection first (without handler)
	wsConn := &WebSocketConn{
		conn:        conn,
		session:     session,
		handler:     nil, // Will be set below
		contentType: contentType,
		refPrefix:   generateConnID(),
		writeChan:   make(chan []byte, 100),
		closeChan:   make(chan struct{}),
	}

	// Now create handler with connection as caller
	wsConn.handler = NewHandler(session, rootObject, wsConn, mimeTypes)

	return wsConn
}

// handle starts the read and write loops for this connection.
func (c *WebSocketConn) handle() {
	// Start read and write loops
	go c.readLoop()
	go c.writeLoop()

	// Wait for connection to close
	<-c.closeChan
}

// Call invokes a method on the client and waits for the response.
// This allows the server to initiate calls to the client.
func (c *WebSocketConn) Call(method string, params any, opts ...CallOption) (Value, error) {
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
	case <-c.closeChan:
		return Value{}, fmt.Errorf("connection closed")
	}

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		if resp.Error != nil {
			return Value{}, resp.Error
		}

		// Return Value with result
		if resp.Result != nil {
			return NewValueWithCodec(resp.Result, codec), nil
		}
		return NilValue, nil

	case <-time.After(30 * time.Second):
		return Value{}, fmt.Errorf("request timeout")

	case <-c.closeChan:
		return Value{}, fmt.Errorf("connection closed")
	}
}

// Notify sends a notification to the client (no response expected).
func (c *WebSocketConn) Notify(method string, params any, opts ...CallOption) error {
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

// CallBatch sends a batch of requests and waits for all responses.
func (c *WebSocketConn) CallBatch(batchReqs []BatchRequest) (*BatchResults, error) {
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
	reqData, err := codec.MarshalMessages(*msgSet)
	if err != nil {
		return nil, fmt.Errorf("failed to encode batch: %w", err)
	}

	// Send via write channel
	select {
	case c.writeChan <- reqData:
		// Successfully queued for sending
	case <-c.closeChan:
		return nil, fmt.Errorf("connection closed")
	}

	// Wait for all responses with timeout
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

		case <-time.After(30 * time.Second):
			return nil, fmt.Errorf("timeout waiting for response %d", i)

		case <-c.closeChan:
			return nil, fmt.Errorf("connection closed while waiting for responses")
		}
	}

	return &BatchResults{responses: responses}, nil
}

// RegisterObject registers a server object that the client can call.
// If ref is empty, a reference ID is auto-generated using the connection's prefix and counter.
// Returns a Reference instance that can be used to identify the object.
func (c *WebSocketConn) RegisterObject(ref string, obj Object) Reference {
	if ref == "" {
		counter := c.refCounter.Add(1)
		ref = fmt.Sprintf("%s-%d", c.refPrefix, counter)
	}
	c.handler.session.AddLocalRef(ref, obj)
	return NewReference(ref)
}

// UnregisterObject removes a registered server object.
func (c *WebSocketConn) UnregisterObject(ref Reference) {
	c.handler.session.RemoveLocalRef(ref.Ref)
}

// GetSession returns the connection's session.
func (c *WebSocketConn) GetSession() *Session {
	return c.session
}

// Close closes the WebSocket connection gracefully.
func (c *WebSocketConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
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
func (c *WebSocketConn) readLoop() {
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
			go c.handleInvalidBatch(codec, &msgSet)
		}
	}
}

// handleIncomingRequest processes an incoming request from the client.
func (c *WebSocketConn) handleIncomingRequest(codec Codec, msg *Message) {
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
func (c *WebSocketConn) sendErrorForBatch(codec Codec, msgSet *MessageSet, rpcErr *Error) {
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

// handleIncomingBatch processes a batch of incoming requests from the client.
// Batch requests must be processed together to support batch-local references (\0, \1, etc.)
func (c *WebSocketConn) handleIncomingBatch(codec Codec, msgSet *MessageSet) {
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

// handleInvalidBatch handles a batch with mixed requests and responses.
// Sends error responses for all requests in the batch.
func (c *WebSocketConn) handleInvalidBatch(codec Codec, msgSet *MessageSet) {
	c.sendErrorForBatch(codec, msgSet, NewInvalidRequestError("mixed requests and responses in batch"))
}

// handleIncomingResponse processes an incoming response to our request.
func (c *WebSocketConn) handleIncomingResponse(msg *Message) {
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
func (c *WebSocketConn) writeLoop() {
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
func (c *WebSocketConn) setError(err error) {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.connErr == nil {
		c.connErr = err
	}
}

// getError retrieves any connection error.
func (c *WebSocketConn) getError() error {
	c.errMu.RLock()
	defer c.errMu.RUnlock()
	return c.connErr
}
