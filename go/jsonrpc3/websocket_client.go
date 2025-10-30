package jsonrpc3

import (
	"context"
	"crypto/tls"
	"fmt"
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
	return WithContentType("application/json")
}

func WithCBOR() ClientOption {
	return WithContentType("application/cbor")
}

func WithCompactCBOR() ClientOption {
	return WithContentType("application/cbor; format=compact")
}

// WithTLSConfig sets a custom TLS configuration for the WebTransport connection.
func WithTLSConfig(tlsConfig *tls.Config) ClientOption {
	return func(o *clientOptions) {
		o.tlsConfig = tlsConfig
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
		contentType: "application/cbor",
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
	case "application/cbor":
		protocol = "jsonrpc3.cbor"
	case "application/cbor; format=compact":
		protocol = "jsonrpc3.cbor-compact"
	default:
		protocol = "jsonrpc3.json"
	}

	// Connect to WebSocket using context
	dialer := websocket.Dialer{
		Subprotocols: []string{protocol},
	}

	conn, resp, err := dialer.DialContext(ctx, url, nil)
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
		acceptedContentType = "application/json"
	case "jsonrpc3.cbor":
		acceptedContentType = "application/cbor"
	case "jsonrpc3.cbor-compact":
		acceptedContentType = "application/cbor; format=compact"
	}

	// Create client
	session := NewSession()
	mimeTypes := []string{acceptedContentType}
	handler := NewHandler(session, rootObject, mimeTypes)

	// Generate random connection ID prefix
	refPrefix := generateConnID()

	client := &WebSocketClient{
		url:         url,
		conn:        conn,
		session:     session,
		handler:     handler,
		rootObject:  rootObject,
		contentType: acceptedContentType,
		refPrefix:   refPrefix,
		writeChan:   make(chan []byte, 100),
		closeChan:   make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Start read and write loops
	go client.readLoop()
	go client.writeLoop()

	return client, nil
}

// Call invokes a method on the server and waits for the response.
func (c *WebSocketClient) Call(method string, params any, result any) error {
	return c.CallRef("", method, params, result)
}

// CallRef invokes a method on a remote reference and waits for the response.
func (c *WebSocketClient) CallRef(ref string, method string, params any, result any) error {
	// Check if connection is closed
	if err := c.getError(); err != nil {
		return err
	}

	// Generate request ID
	// Use float64 because JSON unmarshaling converts numbers to float64
	id := float64(c.nextID.Add(1))

	// Create request
	req, err := NewRequestWithFormat(method, params, id, c.contentType)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if ref != "" {
		req.Ref = ref
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
		return fmt.Errorf("failed to encode request: %w", err)
	}

	// Send via write channel
	select {
	case c.writeChan <- reqData:
		// Successfully queued for sending
	case <-c.closeChan:
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
			params := NewParamsWithFormat(resp.Result, c.contentType)
			if err := params.Decode(result); err != nil {
				return fmt.Errorf("failed to decode result: %w", err)
			}
		}
		return nil

	case <-ctx.Done():
		return fmt.Errorf("request timeout")

	case <-c.closeChan:
		return fmt.Errorf("connection closed")
	}
}

// Notify sends a notification (no response expected).
func (c *WebSocketClient) Notify(method string, params any) error {
	return c.NotifyRef("", method, params)
}

// NotifyRef sends a notification to a remote reference.
func (c *WebSocketClient) NotifyRef(ref string, method string, params any) error {
	// Check if connection is closed
	if err := c.getError(); err != nil {
		return err
	}

	// Create notification (nil ID)
	req, err := NewRequestWithFormat(method, params, nil, c.contentType)
	if err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}

	if ref != "" {
		req.Ref = ref
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

// RegisterObject registers a local object that the server can call.
// If ref is empty, a reference ID is auto-generated using the connection's prefix and counter.
// Returns the reference ID that was used (either the provided one or the generated one).
func (c *WebSocketClient) RegisterObject(ref string, obj Object) string {
	if ref == "" {
		counter := c.refCounter.Add(1)
		ref = fmt.Sprintf("%s-%d", c.refPrefix, counter)
	}
	c.handler.session.AddLocalRef(ref, obj)
	return ref
}

// UnregisterObject removes a registered local object.
func (c *WebSocketClient) UnregisterObject(ref string) {
	c.handler.session.RemoveLocalRef(ref)
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

		// Process single message (WebSocket doesn't support batch)
		if len(msgSet.Messages) != 1 {
			continue
		}

		msg := &msgSet.Messages[0]
		msg.SetFormat(c.contentType)

		// Dispatch based on message type
		if msg.IsRequest() {
			// Incoming request from server
			go c.handleIncomingRequest(msg)
		} else if msg.IsResponse() {
			// Response to our request
			c.handleIncomingResponse(msg)
		}
	}
}

// handleIncomingRequest processes an incoming request from the server.
func (c *WebSocketClient) handleIncomingRequest(msg *Message) {
	req := msg.ToRequest()
	if req == nil {
		return
	}

	req.SetFormat(c.contentType)

	// Handle request via handler
	resp := c.handler.HandleRequest(req)

	// Send response if not a notification
	if resp != nil {
		codec := GetCodec(c.contentType)
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
