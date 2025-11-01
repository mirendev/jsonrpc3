package jsonrpc3

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

// WebTransportServer is an HTTP/3 server that supports WebTransport
// for bidirectional JSON-RPC 3.0 communication.
type WebTransportServer struct {
	server          *webtransport.Server
	rootObject      Object
	mimeTypes       []string
	addr            string
	onNewConnection func(*WebTransportConn) // Optional callback for new connections
}

// NewWebTransportServer creates a new WebTransport server.
// The rootObject handles incoming method calls from clients.
// If tlsConfig is nil, a self-signed certificate will be generated for testing.
func NewWebTransportServer(addr string, rootObject Object, tlsConfig *tls.Config) (*WebTransportServer, error) {
	mimeTypes := []string{
		MimeTypeJSON,
		MimeTypeCBOR,
		MimeTypeCBORCompact,
	}

	// Generate self-signed cert if not provided
	if tlsConfig == nil {
		cert, err := generateSelfSignedCert()
		if err != nil {
			return nil, fmt.Errorf("failed to generate certificate: %w", err)
		}
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	}

	ws := &WebTransportServer{
		rootObject: rootObject,
		mimeTypes:  mimeTypes,
		addr:       addr,
	}

	// Create WebTransport server
	server := &webtransport.Server{
		H3: http3.Server{
			Addr:      addr,
			TLSConfig: tlsConfig,
			Handler:   ws,
		},
	}

	ws.server = server

	return ws, nil
}

// ListenAndServe starts the WebTransport server.
func (s *WebTransportServer) ListenAndServe() error {
	return s.server.ListenAndServe()
}

// Close closes the WebTransport server.
func (s *WebTransportServer) Close() error {
	return s.server.Close()
}

// ServeHTTP handles HTTP/3 requests and upgrades to WebTransport.
func (s *WebTransportServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Determine content type from request
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = MimeTypeJSON
	}

	// Set response content type BEFORE upgrading
	w.Header().Set("Content-Type", contentType)

	// Upgrade to WebTransport
	session, err := s.server.Upgrade(w, r)
	if err != nil {
		http.Error(w, "Failed to upgrade to WebTransport", http.StatusInternalServerError)
		return
	}

	// Create connection handler
	wtConn := newWebTransportConn(session, s.rootObject, contentType, s.mimeTypes)

	// Handle the connection - this blocks until the connection closes
	// ServeHTTP must NOT return early or HTTP/3 will cancel the request
	wtConn.handleWithCallback(s.onNewConnection)
}

// WebTransportConn represents a single WebTransport session.
// It manages bidirectional JSON-RPC 3.0 communication for one client.
type WebTransportConn struct {
	session       *webtransport.Session
	wtSession     *Session
	handler       *Handler
	contentType   string
	controlStream *webtransport.Stream

	// Request tracking for server-initiated requests
	pendingReqs sync.Map // id (any) -> chan *Response
	nextID      atomic.Int64

	// Reference ID generation
	refPrefix  string // Random connection-specific prefix for refs
	refCounter atomic.Int64

	// Message channels
	writeChan chan MessageSetConvertible // Messages to encode and send
	closeChan chan struct{}
	closeOnce sync.Once

	// Error tracking
	connErr error
	errMu   sync.RWMutex
}

// newWebTransportConn creates a new WebTransport connection handler.
func newWebTransportConn(session *webtransport.Session, rootObject Object, contentType string, mimeTypes []string) *WebTransportConn {
	wtSession := NewSession()
	handler := NewHandler(wtSession, rootObject, mimeTypes)

	return &WebTransportConn{
		session:     session,
		wtSession:   wtSession,
		handler:     handler,
		contentType: contentType,
		refPrefix:   generateConnID(),
		writeChan:   make(chan MessageSetConvertible, 100),
		closeChan:   make(chan struct{}),
	}
}

// handle starts the read and write loops for this connection.
func (c *WebTransportConn) handle() {
	c.handleWithCallback(nil)
}

// handleWithCallback starts the read and write loops, calling the callback after setup.
func (c *WebTransportConn) handleWithCallback(onReady func(*WebTransportConn)) {
	// Accept control stream (first bidirectional stream from client)
	ctx := context.Background()
	stream, err := c.session.AcceptStream(ctx)
	if err != nil {
		c.Close()
		return
	}

	c.controlStream = stream

	// Start read and write loops
	go c.readLoop()
	go c.writeLoop()

	// Call callback after connection is ready
	if onReady != nil {
		onReady(c)
	}

	// Wait for connection to close
	<-c.closeChan
}

// Call invokes a method on the client and waits for the response.
// Use the ToRef option to call a method on a remote object reference.
func (c *WebTransportConn) Call(method string, params any, result any, opts ...CallOption) error {
	// Check if connection is closed
	if err := c.getError(); err != nil {
		return err
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
		return fmt.Errorf("failed to create request: %w", err)
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
// Use the ToRef option to send a notification to a remote object reference.
func (c *WebTransportConn) Notify(method string, params any, opts ...CallOption) error {
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
func (c *WebTransportConn) CallBatch(batchReqs []BatchRequest) (*BatchResults, error) {
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

// RegisterObject registers a server object that the client can call.
// If ref is empty, a reference ID is auto-generated using the connection's prefix and counter.
// Returns a Reference instance that can be used to identify the object.
func (c *WebTransportConn) RegisterObject(ref string, obj Object) Reference {
	if ref == "" {
		counter := c.refCounter.Add(1)
		ref = fmt.Sprintf("%s-%d", c.refPrefix, counter)
	}
	c.handler.session.AddLocalRef(ref, obj)
	return NewReference(ref)
}

// UnregisterObject removes a registered server object.
func (c *WebTransportConn) UnregisterObject(ref Reference) {
	c.handler.session.RemoveLocalRef(ref.Ref)
}

// GetSession returns the connection's session.
func (c *WebTransportConn) GetSession() *Session {
	return c.wtSession
}

// Close closes the WebTransport connection gracefully.
func (c *WebTransportConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closeChan)

		// Close control stream
		if c.controlStream != nil {
			c.controlStream.Close()
		}

		// Close session
		if c.session != nil {
			err = c.session.CloseWithError(0, "server closed")
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
func (c *WebTransportConn) readLoop() {
	defer c.Close()

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

// handleIncomingRequest processes an incoming request from the client.
func (c *WebTransportConn) handleIncomingRequest(msg *Message) {
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
func (c *WebTransportConn) sendErrorForBatch(msgSet *MessageSet, rpcErr *Error) {
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

// handleIncomingBatch processes a batch of incoming requests from the client.
// Batch requests must be processed together to support batch-local references (\0, \1, etc.)
func (c *WebTransportConn) handleIncomingBatch(msgSet *MessageSet) {
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
func (c *WebTransportConn) handleInvalidBatch(msgSet *MessageSet) {
	c.sendErrorForBatch(msgSet, NewInvalidRequestError("mixed requests and responses in batch"))
}

// handleIncomingResponse processes an incoming response to our request.
func (c *WebTransportConn) handleIncomingResponse(msg *Message) {
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
func (c *WebTransportConn) writeLoop() {
	codec := GetCodec(c.contentType)

	for {
		select {
		case item := <-c.writeChan:
			// Convert to MessageSet
			msgSet := item.ToMessageSet()

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
func (c *WebTransportConn) notifyPendingRequest(item any, err error) {
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
func (c *WebTransportConn) setError(err error) {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.connErr == nil {
		c.connErr = err
	}
}

// getError retrieves any connection error.
func (c *WebTransportConn) getError() error {
	c.errMu.RLock()
	defer c.errMu.RUnlock()
	return c.connErr
}

// generateSelfSignedCert creates a self-signed TLS certificate for testing.
// In production, use a proper certificate from a trusted CA.
func generateSelfSignedCert() (tls.Certificate, error) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"JSON-RPC 3.0 Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate and private key to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	// Create TLS certificate
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create X509 key pair: %w", err)
	}

	return cert, nil
}
