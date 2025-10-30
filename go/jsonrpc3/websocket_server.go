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
			"application/json",
			"application/cbor",
			"application/cbor; format=compact",
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
	contentType := "application/json"
	switch conn.Subprotocol() {
	case "jsonrpc3.cbor":
		contentType = "application/cbor"
	case "jsonrpc3.cbor-compact":
		contentType = "application/cbor; format=compact"
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
	handler := NewHandler(session, rootObject, mimeTypes)

	return &WebSocketConn{
		conn:        conn,
		session:     session,
		handler:     handler,
		contentType: contentType,
		writeChan:   make(chan []byte, 100),
		closeChan:   make(chan struct{}),
	}
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
func (c *WebSocketConn) Call(ref string, method string, params any, result any) error {
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
	reqData, err := codec.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	// Send via write channel
	select {
	case c.writeChan <- reqData:
	case <-c.closeChan:
		return fmt.Errorf("connection closed")
	}

	// Wait for response with timeout
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

	case <-time.After(30 * time.Second):
		return fmt.Errorf("request timeout")

	case <-c.closeChan:
		return fmt.Errorf("connection closed")
	}
}

// Notify sends a notification to the client (no response expected).
func (c *WebSocketConn) Notify(ref string, method string, params any) error {
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
	reqData, err := codec.Marshal(req)
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

// RegisterObject registers a server object that the client can call.
func (c *WebSocketConn) RegisterObject(ref string, obj Object) {
	c.handler.session.AddLocalRef(ref, obj)
}

// UnregisterObject removes a registered server object.
func (c *WebSocketConn) UnregisterObject(ref string) {
	c.handler.session.RemoveLocalRef(ref)
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

		// Decode as Message
		var msg Message
		if err := codec.Unmarshal(data, &msg); err != nil {
			// Invalid message, ignore
			continue
		}

		// Dispatch based on message type
		if msg.IsRequest() {
			// Incoming request from client
			go c.handleIncomingRequest(&msg, codec)
		} else if msg.IsResponse() {
			// Response to our request
			c.handleIncomingResponse(&msg)
		}
	}
}

// handleIncomingRequest processes an incoming request from the client.
func (c *WebSocketConn) handleIncomingRequest(msg *Message, codec Codec) {
	req := msg.ToRequest()
	if req == nil {
		return
	}

	req.SetFormat(c.contentType)

	// Handle request via handler
	resp := c.handler.HandleRequest(req)

	// Send response if not a notification
	if resp != nil {
		respData, err := codec.Marshal(resp)
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
