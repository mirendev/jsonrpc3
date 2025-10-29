package jsonrpc3

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	// supportedFormats in order of preference (most efficient first)
	supportedFormats = []string{
		"application/cbor",
		"application/json",
	}
)

// HTTPClient is a JSON-RPC 3.0 client that communicates over HTTP.
// It automatically tracks session IDs via the RPC-Session-Id header.
// Defaults to CBOR format with automatic fallback to JSON.
// Supports local callback objects for server-to-client notifications via SSE.
type HTTPClient struct {
	url         string
	httpClient  *http.Client
	contentType string
	nextID      atomic.Int64
	sessionID   atomic.Value // stores string

	// Callback support (lazily initialized)
	sessionMu sync.Mutex
	session   *Session
	handler   *Handler
}

// NewHTTPClient creates a new HTTP client for JSON-RPC 3.0.
// If httpClient is nil, http.DefaultClient is used.
// Defaults to CBOR format with automatic fallback to JSON.
func NewHTTPClient(url string, httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	client := &HTTPClient{
		url:         url,
		httpClient:  httpClient,
		contentType: "application/cbor",
	}
	client.sessionID.Store("")

	return client
}

// SessionID returns the current session ID, or empty string if none.
func (c *HTTPClient) SessionID() string {
	if val := c.sessionID.Load(); val != nil {
		return val.(string)
	}
	return ""
}

// SetSessionID sets the session ID to use for subsequent requests.
// Use empty string to clear the session ID.
func (c *HTTPClient) SetSessionID(sessionID string) {
	c.sessionID.Store(sessionID)
}

// DeleteSession deletes the current session on the server.
// After calling this, the client's session ID is cleared.
// Returns an error if the HTTP request fails.
func (c *HTTPClient) DeleteSession() error {
	sessionID := c.SessionID()
	if sessionID == "" {
		// No session to delete
		return nil
	}

	httpReq, err := http.NewRequest(http.MethodDelete, c.url, nil)
	if err != nil {
		return fmt.Errorf("failed to create DELETE request: %w", err)
	}

	httpReq.Header.Set("RPC-Session-Id", sessionID)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("DELETE request failed: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("DELETE failed with status %d: %s", httpResp.StatusCode, string(body))
	}

	// Clear local session ID
	c.SetSessionID("")

	return nil
}

// SetContentType sets the content type for requests and responses.
// Supported types: "application/json", "application/cbor", "application/cbor; format=compact"
func (c *HTTPClient) SetContentType(contentType string) {
	c.contentType = contentType
}

// GetSession returns the client's session for registering local callback objects.
// The session is lazily created on first access.
// Use this to register callback objects that the server can invoke via notifications.
func (c *HTTPClient) GetSession() *Session {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	if c.session == nil {
		c.session = NewSession()
		c.handler = NewHandler(c.session, nil, nil)
	}

	return c.session
}

// RegisterCallback registers a local callback object that can be invoked by the server.
// The server can send notifications to this reference, which will be dispatched to the object.
// Use this when passing client references to the server in request parameters.
//
// Example:
//
//	callback := &MyCallback{}
//	client.RegisterCallback("client-callback-1", callback)
//	client.Call("subscribe", map[string]any{
//	    "callback": LocalReference{Ref: "client-callback-1"},
//	}, &result)
func (c *HTTPClient) RegisterCallback(ref string, obj Object) {
	session := c.GetSession()
	session.AddLocalRef(ref, obj)
}

// UnregisterCallback removes a previously registered callback object.
func (c *HTTPClient) UnregisterCallback(ref string) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	if c.session != nil {
		c.session.RemoveLocalRef(ref)
	}
}

// generateID generates a unique request ID.
func (c *HTTPClient) generateID() int64 {
	return c.nextID.Add(1)
}

// getAcceptHeader returns the Accept header value with all supported formats.
func (c *HTTPClient) getAcceptHeader() string {
	return strings.Join(supportedFormats, ", ")
}

// doRequest sends an HTTP request with the given data and content type.
// Includes Accept header and session tracking.
func (c *HTTPClient) doRequest(reqData []byte, contentType string) (*http.Response, error) {
	httpReq, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Accept", c.getAcceptHeader())

	// Add session ID header if we have one
	if sessionID := c.SessionID(); sessionID != "" {
		httpReq.Header.Set("RPC-Session-Id", sessionID)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	// Update session ID from response
	if sessionID := httpResp.Header.Get("RPC-Session-Id"); sessionID != "" {
		c.SetSessionID(sessionID)
	}

	return httpResp, nil
}

// shouldFallback checks if the HTTP status indicates we should try a different format.
func (c *HTTPClient) shouldFallback(statusCode int) bool {
	return statusCode == http.StatusUnsupportedMediaType || statusCode == http.StatusNotAcceptable
}

// Call invokes a JSON-RPC method and decodes the result into the provided value.
// Returns an error if the call fails or if the server returns an error.
func (c *HTTPClient) Call(method string, params any, result any) error {
	return c.CallRef("", method, params, result)
}

// CallRef invokes a JSON-RPC method on a remote reference.
// If ref is empty, calls the method at the root level.
// Automatically falls back to other formats if the server rejects the current format.
func (c *HTTPClient) CallRef(ref string, method string, params any, result any) error {
	// Prepare list of formats to try (current format first, then fallbacks)
	formats := []string{c.contentType}
	for _, format := range supportedFormats {
		if format != c.contentType {
			formats = append(formats, format)
		}
	}

	var lastErr error
	for _, format := range formats {
		// Create request with the current format
		req, err := NewRequestWithFormat(method, params, c.generateID(), format)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		if ref != "" {
			req.Ref = ref
		}

		// Encode request
		codec := GetCodec(format)
		reqData, err := codec.Marshal(req)
		if err != nil {
			return fmt.Errorf("failed to encode request: %w", err)
		}

		// Send HTTP request
		httpResp, err := c.doRequest(reqData, format)
		if err != nil {
			lastErr = err
			continue
		}

		// Check if we should fallback to a different format
		if c.shouldFallback(httpResp.StatusCode) {
			httpResp.Body.Close()
			lastErr = fmt.Errorf("server rejected format %s (HTTP %d)", format, httpResp.StatusCode)
			continue
		}

		// Check HTTP status
		if httpResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			return fmt.Errorf("HTTP error %d: %s", httpResp.StatusCode, string(body))
		}

		// Check if response is SSE stream
		responseContentType := httpResp.Header.Get("Content-Type")
		if strings.HasPrefix(responseContentType, "text/event-stream") {
			// Handle SSE response with notifications
			return c.handleSSEResponse(httpResp, format, result)
		}

		// Read response body
		respData, err := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		// Decode response
		var resp Response
		if err := codec.Unmarshal(respData, &resp); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}

		// Success - update client's format preference for future requests
		if format != c.contentType {
			c.SetContentType(format)
		}

		// Check for RPC error
		if resp.Error != nil {
			return resp.Error
		}

		// Decode result if provided
		if result != nil && resp.Result != nil {
			params := NewParamsWithFormat(resp.Result, format)
			if err := params.Decode(result); err != nil {
				return fmt.Errorf("failed to decode result: %w", err)
			}
		}

		return nil
	}

	// All formats failed
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("all formats rejected by server")
}

// handleSSEResponse processes an SSE (Server-Sent Events) response stream.
// It reads notifications and the final result from the stream, dispatching
// notifications to local callback objects registered via RegisterCallback.
func (c *HTTPClient) handleSSEResponse(httpResp *http.Response, format string, result any) error {
	defer httpResp.Body.Close()

	// Update session ID from response header
	if sessionID := httpResp.Header.Get("RPC-Session-Id"); sessionID != "" {
		c.SetSessionID(sessionID)
	}

	codec := GetCodec(format)
	scanner := bufio.NewScanner(httpResp.Body)

	var finalResult *Response
	var dataLine string

	// Get handler for dispatching notifications
	c.sessionMu.Lock()
	handler := c.handler
	c.sessionMu.Unlock()

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "data: <json>\n\n"
		if strings.HasPrefix(line, "data: ") {
			dataLine = line[6:] // Skip "data: " prefix
		} else if line == "" && dataLine != "" {
			// Blank line indicates end of event
			// Decode as Message to determine if it's a request or response
			var msg Message
			if err := codec.Unmarshal([]byte(dataLine), &msg); err == nil {
				if msg.IsNotification() {
					// It's a notification - dispatch to local callback
					if handler != nil {
						req := msg.ToRequest()
						req.format = format
						resp := handler.HandleRequest(req)
						// resp will be nil for notifications, which is expected
						_ = resp
					}
				} else if msg.IsResponse() {
					// It's the final response
					finalResult = msg.ToResponse()
					break
				}
			}
			dataLine = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading SSE stream: %w", err)
	}

	if finalResult == nil {
		return fmt.Errorf("no final result received in SSE stream")
	}

	// Check for RPC error
	if finalResult.Error != nil {
		return finalResult.Error
	}

	// Decode result if provided
	if result != nil && finalResult.Result != nil {
		params := NewParamsWithFormat(finalResult.Result, format)
		if err := params.Decode(result); err != nil {
			return fmt.Errorf("failed to decode result: %w", err)
		}
	}

	return nil
}

// Notify sends a notification (no response expected).
// Notifications do not have an ID and the server will not send a response.
func (c *HTTPClient) Notify(method string, params any) error {
	return c.NotifyRef("", method, params)
}

// NotifyRef sends a notification to a remote reference.
// Automatically falls back to other formats if the server rejects the current format.
func (c *HTTPClient) NotifyRef(ref string, method string, params any) error {
	// Prepare list of formats to try
	formats := []string{c.contentType}
	for _, format := range supportedFormats {
		if format != c.contentType {
			formats = append(formats, format)
		}
	}

	var lastErr error
	for _, format := range formats {
		// Create notification (nil ID)
		req, err := NewRequestWithFormat(method, params, nil, format)
		if err != nil {
			return fmt.Errorf("failed to create notification: %w", err)
		}

		if ref != "" {
			req.Ref = ref
		}

		// Encode request
		codec := GetCodec(format)
		reqData, err := codec.Marshal(req)
		if err != nil {
			return fmt.Errorf("failed to encode notification: %w", err)
		}

		// Send HTTP request
		httpResp, err := c.doRequest(reqData, format)
		if err != nil {
			lastErr = err
			continue
		}

		// Check if we should fallback to a different format
		if c.shouldFallback(httpResp.StatusCode) {
			httpResp.Body.Close()
			lastErr = fmt.Errorf("server rejected format %s (HTTP %d)", format, httpResp.StatusCode)
			continue
		}

		// For notifications, expect 204 No Content or 200 OK
		if httpResp.StatusCode != http.StatusNoContent && httpResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			return fmt.Errorf("HTTP error %d: %s", httpResp.StatusCode, string(body))
		}

		httpResp.Body.Close()

		// Success - update client's format preference for future requests
		if format != c.contentType {
			c.SetContentType(format)
		}

		return nil
	}

	// All formats failed
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("all formats rejected by server")
}

// CallBatch sends multiple requests in a single batch.
// Returns a slice of responses in the same order as the requests.
// Note: Notifications in the batch will not have responses.
// Automatically falls back to other formats if the server rejects the current format.
func (c *HTTPClient) CallBatch(requests []BatchRequest) ([]BatchResult, error) {
	if len(requests) == 0 {
		return nil, fmt.Errorf("batch cannot be empty")
	}

	// Prepare list of formats to try
	formats := []string{c.contentType}
	for _, format := range supportedFormats {
		if format != c.contentType {
			formats = append(formats, format)
		}
	}

	var lastErr error
	for _, format := range formats {
		// Create batch of requests
		batch := make(Batch, len(requests))
		for i, breq := range requests {
			var id any
			if breq.IsNotification {
				id = nil
			} else {
				id = c.generateID()
			}

			req, err := NewRequestWithFormat(breq.Method, breq.Params, id, format)
			if err != nil {
				return nil, fmt.Errorf("failed to create request %d: %w", i, err)
			}

			if breq.Ref != "" {
				req.Ref = breq.Ref
			}

			batch[i] = *req
		}

		// Encode batch
		codec := GetCodec(format)
		reqData, err := codec.Marshal(batch)
		if err != nil {
			return nil, fmt.Errorf("failed to encode batch: %w", err)
		}

		// Send HTTP request
		httpResp, err := c.doRequest(reqData, format)
		if err != nil {
			lastErr = err
			continue
		}

		// Check if we should fallback to a different format
		if c.shouldFallback(httpResp.StatusCode) {
			httpResp.Body.Close()
			lastErr = fmt.Errorf("server rejected format %s (HTTP %d)", format, httpResp.StatusCode)
			continue
		}

		// Check for 204 No Content (all notifications)
		if httpResp.StatusCode == http.StatusNoContent {
			httpResp.Body.Close()
			// Success - update client's format preference
			if format != c.contentType {
				c.SetContentType(format)
			}
			return nil, nil
		}

		// Check HTTP status
		if httpResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			return nil, fmt.Errorf("HTTP error %d: %s", httpResp.StatusCode, string(body))
		}

		// Read response body
		respData, err := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		// Decode batch response
		var batchResp BatchResponse
		if err := codec.Unmarshal(respData, &batchResp); err != nil {
			return nil, fmt.Errorf("failed to decode batch response: %w", err)
		}

		// Success - update client's format preference
		if format != c.contentType {
			c.SetContentType(format)
		}

		// Convert to results
		results := make([]BatchResult, len(batchResp))
		for i, resp := range batchResp {
			results[i].ID = resp.ID
			results[i].Error = resp.Error
			results[i].format = format // Track the format for decoding

			if resp.Result != nil {
				results[i].Result = resp.Result
			}
		}

		return results, nil
	}

	// All formats failed
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("all formats rejected by server")
}

// BatchRequest represents a request in a batch call.
type BatchRequest struct {
	Ref            string // Optional reference
	Method         string
	Params         any
	IsNotification bool // If true, no response will be returned
}

// BatchResult represents a response from a batch call.
type BatchResult struct {
	ID     any
	Result RawMessage
	Error  *Error
	format string // internal: tracks the format of the result
}

// Decode decodes the result into the provided value.
func (r *BatchResult) Decode(v any) error {
	if r.Error != nil {
		return r.Error
	}

	// Use the format that was used for the batch call
	params := NewParamsWithFormat(r.Result, r.format)
	return params.Decode(v)
}
