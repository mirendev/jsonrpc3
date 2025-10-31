package jsonrpc3

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultSessionTTL is the default time-to-live for HTTP sessions with refs
	DefaultSessionTTL = 5 * time.Minute

	// StatelessSessionTTL is the time-to-live for sessions without object references
	// These are cleaned up more aggressively to optimize memory usage
	StatelessSessionTTL = 30 * time.Second

	// SessionCleanupInterval is how often we check for expired sessions
	SessionCleanupInterval = 10 * time.Second
)

// HTTPHandler is an HTTP handler that serves JSON-RPC 3.0 requests.
// It reads requests from HTTP POST bodies, processes them using a Handler,
// and writes responses back to the HTTP response.
// Sessions are tracked via RPC-Session-Id header and automatically expire.
type HTTPHandler struct {
	rootObject   Object
	mimeTypes    []string
	version      string
	sessions     map[string]*sessionEntry
	sessionMutex sync.RWMutex
	sessionTTL   time.Duration
	stopCleanup  chan struct{}
}

// sessionEntry tracks a session, its handler, and last access time
type sessionEntry struct {
	session      *Session
	handler      *Handler
	lastAccessed time.Time
	mutex        sync.Mutex
}

// NewHTTPHandler creates a new HTTP handler for JSON-RPC 3.0.
// The rootObject handles top-level method calls.
// Sessions can span multiple requests via the RPC-Session-Id header.
// Supports all three encoding formats: JSON, CBOR, and compact CBOR.
func NewHTTPHandler(rootObject Object) *HTTPHandler {
	h := &HTTPHandler{
		rootObject: rootObject,
		mimeTypes: []string{
			MimeTypeJSON,
			MimeTypeCBOR,
			MimeTypeCBORCompact,
		},
		version:     Version30,
		sessions:    make(map[string]*sessionEntry),
		sessionTTL:  DefaultSessionTTL,
		stopCleanup: make(chan struct{}),
	}

	// Start background cleanup goroutine
	go h.cleanupLoop()

	return h
}

// SetSessionTTL sets the session time-to-live duration.
// Sessions that haven't been accessed for this duration will be expired.
func (h *HTTPHandler) SetSessionTTL(ttl time.Duration) {
	h.sessionMutex.Lock()
	defer h.sessionMutex.Unlock()
	h.sessionTTL = ttl
}

// Close stops the background cleanup goroutine.
// Call this when shutting down the handler.
func (h *HTTPHandler) Close() {
	close(h.stopCleanup)
}

// ServeHTTP implements http.Handler.
// It processes JSON-RPC requests and returns responses.
// Sessions are reused if RPC-Session-Id header is present, otherwise a new session is created and stored.
// Sessions without object references are cleaned up more aggressively to optimize memory usage.
// DELETE method with RPC-Session-Id header deletes the session and returns 204.
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle DELETE method for session deletion
	if r.Method == http.MethodDelete {
		sessionID := r.Header.Get("RPC-Session-Id")
		if sessionID != "" {
			h.sessionMutex.Lock()
			entry, exists := h.sessions[sessionID]
			if exists {
				// Dispose all refs before deleting session
				entry.session.DisposeAll()
				delete(h.sessions, sessionID)
			}
			h.sessionMutex.Unlock()
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Only accept POST requests for RPC
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get content type from request
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = MimeTypeJSON
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Get or create session based on RPC-Session-Id header
	clientSessionID := r.Header.Get("RPC-Session-Id")
	session, handler, sessionID, wasExisting := h.getOrCreateSession(clientSessionID)

	// Decode request
	codec := GetCodec(contentType)
	msgSet, err := codec.UnmarshalMessages(body)
	if err != nil {
		// Send parse error response
		errResp := NewErrorResponse(nil, NewParseError(err.Error()), h.version)
		h.writeResponse(w, errResp, contentType, codec)
		return
	}

	// Check if this was originally a batch (JSON/CBOR array)
	var req *Request
	var batch Batch
	var isBatch bool
	if !msgSet.IsBatch {
		req, err = msgSet.ToRequest()
		if err != nil {
			errResp := NewErrorResponse(nil, NewParseError(err.Error()), h.version)
			h.writeResponse(w, errResp, contentType, codec)
			return
		}
	} else {
		batch, err = msgSet.ToBatch()
		isBatch = true
		if err != nil {
			errResp := NewErrorResponse(nil, NewParseError(err.Error()), h.version)
			h.writeResponse(w, errResp, contentType, codec)
			return
		}
	}

	// Check if request contains client references for SSE mode
	// Only support SSE for single requests (not batch) for now
	useSSE := false
	if !isBatch && req != nil && scanForClientRefs(req.Params) {
		useSSE = true
	}

	// Handle SSE mode if client refs detected
	if useSSE {
		h.handleSSERequest(w, r, codec, req, handler, contentType, sessionID, wasExisting, session)
		return
	}

	// Process request (non-SSE mode)
	var batchResponses BatchResponse
	var resp *Response
	var batchData []byte
	var hasResponse bool

	if isBatch {
		// Handle batch request
		batchResponses = handler.HandleBatch(batch)
		hasResponse = len(batchResponses) > 0

		if hasResponse {
			var err error
			msgSet := batchResponses.ToMessageSet()
			batchData, err = codec.MarshalMessages(msgSet)
			if err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}
		}
	} else {
		// Handle single request
		resp = handler.HandleRequest(req)
		hasResponse = resp != nil
	}

	// Determine if session should be stored
	// Store if: 1) existing session, 2) protocol method call ($rpc), 3) has local refs
	shouldStore := wasExisting
	isProtocolMethod := !isBatch && req != nil && (req.Ref == "$rpc" || strings.HasPrefix(req.Method, "$rpc."))
	hasRefs := false

	if !wasExisting {
		// Check if session has refs after processing
		hasRefs = len(session.ListLocalRefs()) > 0

		// Store if it's a protocol method call or has refs
		if isProtocolMethod || hasRefs {
			h.storeSession(sessionID, session, handler)
			shouldStore = true
		}
	}

	// Set session ID header only if session is stored
	if shouldStore {
		w.Header().Set("RPC-Session-Id", sessionID)
	}

	// Write response
	if isBatch {
		if hasResponse {
			h.writeData(w, batchData, contentType)
		} else {
			// All notifications - return 204 No Content
			w.WriteHeader(http.StatusNoContent)
		}
	} else {
		if hasResponse {
			h.writeResponse(w, resp, contentType, codec)
		} else {
			// Notification - return 204 No Content
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// writeResponse encodes and writes a single response.
func (h *HTTPHandler) writeResponse(w http.ResponseWriter, resp *Response, contentType string, codec Codec) {
	msgSet := resp.ToMessageSet()
	data, err := codec.MarshalMessages(msgSet)
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
	h.writeData(w, data, contentType)
}

// writeData writes encoded data with appropriate headers.
func (h *HTTPHandler) writeData(w http.ResponseWriter, data []byte, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// getOrCreateSession retrieves an existing session or creates a new one.
// Returns the session, handler, session ID, and whether it was an existing session.
// New sessions are NOT automatically stored - call storeSession after processing.
func (h *HTTPHandler) getOrCreateSession(sessionID string) (*Session, *Handler, string, bool) {
	now := time.Now()

	// Try to get existing session
	if sessionID != "" {
		h.sessionMutex.RLock()
		entry, exists := h.sessions[sessionID]
		h.sessionMutex.RUnlock()

		if exists {
			// Lock the session entry to update last accessed time
			entry.mutex.Lock()
			entry.lastAccessed = now
			entry.mutex.Unlock()
			return entry.session, entry.handler, sessionID, true
		}
	}

	// Create new session and handler (but don't store yet)
	session := NewSession()
	newSessionID := session.ID()
	handler := NewHandler(session, h.rootObject, h.mimeTypes)
	handler.SetVersion(h.version)

	return session, handler, newSessionID, false
}

// storeSession stores a session for reuse across requests.
// Sessions without local refs will be cleaned up more aggressively by the cleanup loop.
func (h *HTTPHandler) storeSession(sessionID string, session *Session, handler *Handler) {
	now := time.Now()
	entry := &sessionEntry{
		session:      session,
		handler:      handler,
		lastAccessed: now,
	}

	h.sessionMutex.Lock()
	h.sessions[sessionID] = entry
	h.sessionMutex.Unlock()
}

// cleanupLoop periodically removes expired sessions
func (h *HTTPHandler) cleanupLoop() {
	ticker := time.NewTicker(SessionCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.cleanupExpiredSessions()
		case <-h.stopCleanup:
			return
		}
	}
}

// cleanupExpiredSessions removes sessions that haven't been accessed recently.
// Sessions without object references are cleaned up more aggressively.
func (h *HTTPHandler) cleanupExpiredSessions() {
	now := time.Now()

	h.sessionMutex.Lock()
	defer h.sessionMutex.Unlock()

	// Find expired sessions
	var toDelete []string
	for id, entry := range h.sessions {
		entry.mutex.Lock()
		lastAccessed := entry.lastAccessed
		entry.mutex.Unlock()

		// Determine TTL based on whether session has refs
		refs := entry.session.ListLocalRefs()
		ttl := h.sessionTTL
		if len(refs) == 0 {
			// Sessions without refs expire faster - use the minimum of configured TTL or StatelessSessionTTL
			if StatelessSessionTTL < ttl {
				ttl = StatelessSessionTTL
			}
		}

		if now.Sub(lastAccessed) > ttl {
			toDelete = append(toDelete, id)
		}
	}

	// Delete expired sessions and dispose their refs
	for _, id := range toDelete {
		if entry, exists := h.sessions[id]; exists {
			entry.session.DisposeAll()
		}
		delete(h.sessions, id)
	}
}

// scanForClientRefs recursively scans params for Reference structures.
// Returns true if any client references are found ({"$ref": "..."}).
func scanForClientRefs(params RawMessage) bool {
	if len(params) == 0 {
		return false
	}

	// Try to decode as generic structure
	var data any
	if err := json.Unmarshal(params, &data); err != nil {
		return false
	}

	return hasClientRefsInValue(data)
}

// hasClientRefsInValue recursively checks if a value contains Reference structures.
func hasClientRefsInValue(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		// Check if this is a Reference
		if refStr, ok := v["$ref"].(string); ok && refStr != "" && len(v) == 1 {
			return true
		}
		// Recurse into map values
		for _, val := range v {
			if hasClientRefsInValue(val) {
				return true
			}
		}
	case []any:
		// Recurse into array elements
		for _, val := range v {
			if hasClientRefsInValue(val) {
				return true
			}
		}
	}
	return false
}

// writeSSEEvent writes a single SSE event in the format:
// data: <json>\n\n
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, data []byte) error {
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

// handleSSERequest handles a request that contains client references using SSE.
// It sends the response as an SSE stream, allowing for notifications to be sent
// to client callback objects during request processing.
func (h *HTTPHandler) handleSSERequest(
	w http.ResponseWriter,
	r *http.Request,
	codec Codec,
	req *Request,
	handler *Handler,
	contentType string,
	sessionID string,
	wasExisting bool,
	session *Session,
) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Always store session for SSE mode (client refs detected)
	if !wasExisting {
		h.sessionMutex.Lock()
		h.sessions[sessionID] = &sessionEntry{
			session:      session,
			handler:      handler,
			lastAccessed: time.Now(),
		}
		h.sessionMutex.Unlock()
	}

	// Set session ID header
	w.Header().Set("RPC-Session-Id", sessionID)

	// Get flusher for streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Write headers immediately
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Process request
	resp := handler.HandleRequest(req)

	// Encode and send result as SSE event
	if resp != nil {
		msgSet := resp.ToMessageSet()
		respData, err := codec.MarshalMessages(msgSet)
		if err != nil {
			// Can't send error response at this point, connection is already open
			return
		}

		if err := writeSSEEvent(w, flusher, respData); err != nil {
			// Connection error, nothing we can do
			return
		}
	}

	// Connection closes when function returns
}
