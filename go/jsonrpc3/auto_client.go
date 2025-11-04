package jsonrpc3

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
)

// Endpoint represents a probed server endpoint with known protocol capabilities.
// Use Probe() to create an Endpoint, then call Connect() to establish connections.
type Endpoint struct {
	addr        string
	protocol    string // "http/3", "http/2", "http/1.1", "peer", or ""
	altSvcURL   string // HTTP/3 URL from Alt-Svc header, if available
	http3Works  bool   // Whether direct HTTP/3 probe succeeded
	tlsConfig   *tls.Config
	contentType string
}

// Probe discovers the best available protocol for connecting to a server.
// It returns an Endpoint that can be used to establish multiple connections
// without re-probing.
//
// For HTTP/HTTPS URLs, it probes in parallel:
// - HTTP/1.1/2 (checks for Alt-Svc header)
// - HTTP/3 (direct probe)
//
// For http3:// URLs, it directly uses WebTransport (HTTP/3) without probing.
//
// For non-HTTP URLs (no scheme or non-http/https/http3 scheme), it uses peer transport over TCP.
func Probe(addr string, opts ...ClientOption) (*Endpoint, error) {
	// Apply options with defaults
	options := &clientOptions{
		contentType: MimeTypeJSON,
		tlsConfig: &tls.Config{
			InsecureSkipVerify: true, // For development/testing
		},
	}
	for _, opt := range opts {
		opt(options)
	}

	endpoint := &Endpoint{
		addr:        addr,
		tlsConfig:   options.tlsConfig,
		contentType: options.contentType,
	}

	// Handle http3:// scheme - use WebTransport directly
	if strings.HasPrefix(addr, "http3://") {
		endpoint.protocol = "http/3"
		return endpoint, nil
	}

	// Detect if this is an HTTP URL
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		// Use peer transport over TCP
		endpoint.protocol = "peer"
		return endpoint, nil
	}

	// Probe server capabilities in parallel
	protocol, altSvcURL, http3Works := probeServerParallel(addr, options.tlsConfig)

	endpoint.protocol = protocol
	endpoint.altSvcURL = altSvcURL
	endpoint.http3Works = http3Works

	return endpoint, nil
}

// Connect establishes a connection to the probed endpoint.
// Multiple connections can be established from the same Endpoint without re-probing.
func (e *Endpoint) Connect(rootObject Object, opts ...ClientOption) (Caller, error) {
	// Apply options with defaults, but prefer endpoint's settings
	options := &clientOptions{
		contentType: e.contentType,
		tlsConfig:   e.tlsConfig,
	}
	for _, opt := range opts {
		opt(options)
	}

	addr := e.addr

	// Handle peer transport
	if e.protocol == "peer" {
		return dialPeer(addr, rootObject, options)
	}

	// Handle http3:// scheme or probed HTTP/3
	if e.protocol == "http/3" || e.http3Works {
		var h3URL string

		// For http3:// scheme, convert to https://
		if strings.HasPrefix(addr, "http3://") {
			h3URL = "https://" + strings.TrimPrefix(addr, "http3://")
		} else {
			h3URL = addr
		}

		// Ensure URL has trailing slash for WebTransport
		if !strings.HasSuffix(h3URL, "/") {
			h3URL += "/"
		}

		// Clone TLS config to avoid modification by webtransport library
		h3TLSConfig := options.tlsConfig.Clone()

		http3Client, err := NewWebTransportClient(
			h3URL,
			rootObject,
			WithContentType(options.contentType),
			WithTLSConfig(h3TLSConfig),
		)
		if err == nil {
			return http3Client, nil
		}
		// If HTTP/3 client creation fails, fall through to other strategies
	}

	// Try HTTP/3 via Alt-Svc if available
	if e.altSvcURL != "" {
		// Clone TLS config to avoid modification by webtransport library
		h3TLSConfig := options.tlsConfig.Clone()

		http3Client, err := NewWebTransportClient(
			e.altSvcURL,
			rootObject,
			WithContentType(options.contentType),
			WithTLSConfig(h3TLSConfig),
		)
		if err == nil {
			return http3Client, nil
		}
		// If HTTP/3 fails, fall through to use the observed protocol
	}

	// Use the protocol we observed from probing
	switch e.protocol {
	case "HTTP/2.0", "HTTP/2", "http/2":
		return tryHTTP2(addr, rootObject, options)
	case "HTTP/1.1", "HTTP/1.0", "http/1.1":
		// HTTP/1.1 - try WebSocket upgrade first
		if client, err := tryWebSocket(addr, rootObject, options); err == nil {
			return client, nil
		}
		// Fallback to stateless HTTP
		return tryHTTP(addr, rootObject, options)
	case "":
		// Probes failed, try protocols in order: HTTP/2, WebSocket, HTTP
		if client, err := tryHTTP2(addr, rootObject, options); err == nil {
			return client, nil
		}
		if client, err := tryWebSocket(addr, rootObject, options); err == nil {
			return client, nil
		}
		if client, err := tryHTTP(addr, rootObject, options); err == nil {
			return client, nil
		}
		return nil, fmt.Errorf("failed to connect using any protocol")
	default:
		// Unknown protocol, try HTTP/2 then fallback
		if client, err := tryHTTP2(addr, rootObject, options); err == nil {
			return client, nil
		}
		return tryHTTP(addr, rootObject, options)
	}
}

// probeServerParallel probes server capabilities using both HTTP/1.1/2 and HTTP/3 in parallel.
// Returns the HTTP/1.1/2 protocol version, Alt-Svc URL, and whether HTTP/3 HEAD succeeded.
func probeServerParallel(url string, tlsConfig *tls.Config) (protocol string, altSvcURL string, http3Works bool) {
	type probeResult struct {
		protocol   string
		altSvcURL  string
		http3Works bool
	}

	resultChan := make(chan probeResult, 2)

	// Probe HTTP/1.1/2 with Alt-Svc check
	go func() {
		proto, altSvc, err := probeServer(url, tlsConfig)
		if err != nil {
			resultChan <- probeResult{protocol: "", altSvcURL: "", http3Works: false}
		} else {
			resultChan <- probeResult{protocol: proto, altSvcURL: altSvc, http3Works: false}
		}
	}()

	// Probe HTTP/3 directly
	go func() {
		works := probeHTTP3(url, tlsConfig)
		resultChan <- probeResult{protocol: "", altSvcURL: "", http3Works: works}
	}()

	// Collect results from both probes
	var httpResult, http3Result probeResult
	for i := 0; i < 2; i++ {
		result := <-resultChan
		if result.protocol != "" || result.altSvcURL != "" {
			httpResult = result
		}
		if result.http3Works {
			http3Result = result
		}
	}

	return httpResult.protocol, httpResult.altSvcURL, http3Result.http3Works
}

// probeHTTP3 attempts a HEAD request over HTTP/3 to check if the server supports it.
func probeHTTP3(url string, tlsConfig *tls.Config) bool {
	// Clone TLS config to avoid modification
	h3TLSConfig := tlsConfig.Clone()

	// Create HTTP/3 transport
	transport := &http3.Transport{
		TLSClientConfig: h3TLSConfig,
	}
	defer transport.Close()

	// Create HTTP client with HTTP/3 transport
	client := &http.Client{
		Transport: transport,
		Timeout:   1 * time.Second,
	}

	// Make HEAD request over HTTP/3
	resp, err := client.Head(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// If we got a response, HTTP/3 is supported
	return true
}

// probeServer makes a HEAD request to probe the server capabilities.
// Returns the protocol version and Alt-Svc URL (if available).
func probeServer(url string, tlsConfig *tls.Config) (protocol string, altSvcURL string, err error) {
	// Clone TLS config to avoid modification by http2 library
	probeTLSConfig := tlsConfig.Clone()

	// Create transport that supports both HTTP/1.1 and HTTP/2
	transport := &http.Transport{
		TLSClientConfig: probeTLSConfig,
	}

	// Enable HTTP/2 support
	if err := http2.ConfigureTransport(transport); err != nil {
		// If HTTP/2 config fails, continue with HTTP/1.1 only
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	// Make HEAD request to probe the server
	resp, err := client.Head(url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	// Get the protocol version from the response
	protocol = resp.Proto

	// Check for Alt-Svc header
	altSvc := resp.Header.Get("Alt-Svc")
	if altSvc != "" && strings.Contains(altSvc, "h3=") {
		// Parse Alt-Svc header to get HTTP/3 URL
		altSvcURL = parseAltSvc(url, altSvc)
	}

	return protocol, altSvcURL, nil
}

// parseAltSvc parses the Alt-Svc header and returns the HTTP/3 URL.
func parseAltSvc(originalURL string, altSvc string) string {
	// Parse Alt-Svc header (format: h3=":port"; ma=2592000)
	parts := strings.Split(altSvc, ";")
	if len(parts) == 0 {
		return ""
	}

	h3Part := strings.TrimSpace(parts[0])
	if !strings.HasPrefix(h3Part, "h3=") {
		return ""
	}

	portSpec := strings.Trim(h3Part[3:], `"`)

	// Parse original URL to get scheme and host
	scheme := "https"
	urlWithoutScheme := strings.TrimPrefix(originalURL, "http://")
	urlWithoutScheme = strings.TrimPrefix(urlWithoutScheme, "https://")

	// Parse the original URL to extract any path component
	var path string
	if idx := strings.Index(urlWithoutScheme, "/"); idx >= 0 {
		path = urlWithoutScheme[idx:]
		urlWithoutScheme = urlWithoutScheme[:idx]
	}
	if path == "" {
		path = "/" // WebTransport requires a path
	}

	// If portSpec is just ":port", use the original host
	var h3URL string
	if strings.HasPrefix(portSpec, ":") {
		host, _, err := net.SplitHostPort(urlWithoutScheme)
		if err != nil {
			// No port in original URL, use the whole thing as host
			host = urlWithoutScheme
		}
		h3URL = fmt.Sprintf("%s://%s%s%s", scheme, host, portSpec, path)
	} else {
		h3URL = fmt.Sprintf("%s://%s%s", scheme, portSpec, path)
	}

	return h3URL
}

// tryHTTP2 attempts to establish an HTTP/2 connection.
func tryHTTP2(url string, rootObject Object, options *clientOptions) (Caller, error) {
	http2Client, err := NewHTTP2Client(
		url,
		rootObject,
		WithContentType(options.contentType),
		WithTLSConfig(options.tlsConfig),
	)
	if err != nil {
		return nil, fmt.Errorf("HTTP/2 connection failed: %w", err)
	}

	return http2Client, nil
}

// tryWebSocket attempts to connect via WebSocket.
func tryWebSocket(url string, rootObject Object, options *clientOptions) (Caller, error) {
	// Convert https:// to wss:// or http:// to ws://
	wsURL := strings.Replace(url, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)

	wsClient, err := NewWebSocketClient(wsURL, rootObject)
	if err != nil {
		return nil, fmt.Errorf("WebSocket connection failed: %w", err)
	}

	return wsClient, nil
}

// tryHTTP attempts to connect via stateless HTTP.
func tryHTTP(url string, rootObject Object, options *clientOptions) (Caller, error) {
	httpClient := NewHTTPClient(url, nil)
	return httpClient, nil
}

// dialPeer establishes a peer connection over TCP.
func dialPeer(addr string, rootObject Object, options *clientOptions) (Caller, error) {
	// Dial TCP connection
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", addr, err)
	}

	// Create peer with the connection
	peer, err := NewPeer(conn, conn, rootObject, WithContentType(options.contentType))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create peer: %w", err)
	}

	return peer, nil
}
