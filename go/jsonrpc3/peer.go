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

// NewPeer creates a new bidirectional JSON-RPC 3.0 peer over a stream-based transport.
// The rootObject handles incoming method calls from the remote peer.
// Default encoding is "application/cbor".
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
		contentType: "application/cbor",
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

	peer := &Peer{
		reader:      reader,
		writer:      writer,
		session:     session,
		handler:     handler,
		rootObject:  rootObject,
		contentType: options.contentType,
		writeChan:   make(chan any, 100),
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
// Default encoding is "application/json".
func Stdio(rootObject Object, opts ...ClientOption) (*Peer, error) {
	opts = append([]ClientOption{WithJSON()}, opts...)
	return NewPeer(os.Stdin, os.Stdout, rootObject, opts...)
}

// Call invokes a method on the remote peer and waits for the response.
func (p *Peer) Call(method string, params any, result any) error {
	return p.CallRef("", method, params, result)
}

// CallRef invokes a method on a remote reference and waits for the response.
func (p *Peer) CallRef(ref string, method string, params any, result any) error {
	// Check if connection is closed
	if err := p.getError(); err != nil {
		return err
	}

	// Generate request ID
	// Use float64 because JSON unmarshaling converts numbers to float64
	id := float64(p.nextID.Add(1))

	// Create request
	req, err := NewRequestWithFormat(method, params, id, p.contentType)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if ref != "" {
		req.Ref = ref
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
		return fmt.Errorf("request timeout")

	case <-p.closeChan:
		return fmt.Errorf("connection closed")
	}
}

// Notify sends a notification (no response expected).
func (p *Peer) Notify(method string, params any) error {
	return p.NotifyRef("", method, params)
}

// NotifyRef sends a notification to a remote reference.
func (p *Peer) NotifyRef(ref string, method string, params any) error {
	// Check if connection is closed
	if err := p.getError(); err != nil {
		return err
	}

	// Create notification (nil ID)
	req, err := NewRequestWithFormat(method, params, nil, p.contentType)
	if err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}

	if ref != "" {
		req.Ref = ref
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
func (p *Peer) RegisterObject(ref string, obj Object) {
	p.handler.session.AddLocalRef(ref, obj)
}

// UnregisterObject removes a registered local object.
func (p *Peer) UnregisterObject(ref string) {
	p.handler.session.RemoveLocalRef(ref)
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
	decoder := codec.NewDecoder(p.reader)

	for {
		select {
		case <-p.closeChan:
			return
		default:
		}

		// Decode message from stream
		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				p.setError(fmt.Errorf("read error: %w", err))
			}
			return
		}

		// Dispatch based on message type
		if msg.IsRequest() {
			// Incoming request from remote peer
			go p.handleIncomingRequest(&msg)
		} else if msg.IsResponse() {
			// Response to our request
			p.handleIncomingResponse(&msg)
		}
	}
}

// handleIncomingRequest processes an incoming request from the remote peer.
func (p *Peer) handleIncomingRequest(msg *Message) {
	req := msg.ToRequest()
	if req == nil {
		return
	}

	req.SetFormat(p.contentType)

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
	encoder := codec.NewEncoder(p.writer)

	for {
		select {
		case msg := <-p.writeChan:
			if err := encoder.Encode(msg); err != nil {
				p.setError(fmt.Errorf("write error: %w", err))
				return
			}

		case <-p.closeChan:
			return
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
