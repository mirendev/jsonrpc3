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
		return ctx.Err()

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
		}
		// Mixed request/response batches are invalid, ignore
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

// handleIncomingBatch processes a batch of incoming requests from the remote peer.
// Batch requests must be processed together to support batch-local references (\0, \1, etc.)
func (p *Peer) handleIncomingBatch(msgSet *MessageSet) {
	// Convert MessageSet to Batch
	batch, err := msgSet.ToBatch()
	if err != nil {
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

			// Write to stream
			if _, err := p.writer.Write(data); err != nil {
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
