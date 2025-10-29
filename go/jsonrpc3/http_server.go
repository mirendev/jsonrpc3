package jsonrpc3

import (
	"io"
	"net/http"
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
			"application/json",
			"application/cbor",
			"application/cbor; format=compact",
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
// Sessions are reused if RPC-Session-Id header is present, otherwise a new session is created.
// Sessions are only stored if they contain object references (optimization for stateless requests).
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
		contentType = "application/json"
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
	req, batch, isBatch, err := DecodeRequest(body, contentType)
	if err != nil {
		// Send parse error response
		errResp := NewErrorResponse(nil, NewParseError(err.Error()), h.version)
		h.writeResponse(w, errResp, contentType)
		return
	}

	// Process request
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
			batchData, err = EncodeBatchResponseWithFormat(batchResponses, contentType)
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

	// Determine if session should be stored (has refs or was existing)
	sessionStored := wasExisting
	if !wasExisting {
		h.storeSessionIfNeeded(sessionID, session, handler)
		// Check if session was actually stored
		sessionStored = len(session.ListLocalRefs()) > 0
	}

	// Set session ID header only if session is stored
	if sessionStored {
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
			h.writeResponse(w, resp, contentType)
		} else {
			// Notification - return 204 No Content
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// writeResponse encodes and writes a single response.
func (h *HTTPHandler) writeResponse(w http.ResponseWriter, resp *Response, contentType string) {
	data, err := EncodeResponseWithFormat(resp, contentType)
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
// New sessions are NOT automatically stored - call storeSessionIfNeeded after processing.
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

// storeSessionIfNeeded stores a session only if it has local object references.
// This optimizes memory usage by not storing sessions for truly stateless requests.
func (h *HTTPHandler) storeSessionIfNeeded(sessionID string, session *Session, handler *Handler) {
	// Only store if session has local refs
	refs := session.ListLocalRefs()
	if len(refs) == 0 {
		return
	}

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
