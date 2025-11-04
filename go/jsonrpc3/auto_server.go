package jsonrpc3

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
)

// AutoServer is a high-level server that automatically runs multiple transport protocols
// and negotiates the best one with clients. It runs:
// - HTTP/3 (WebTransport) for the best performance
// - HTTP/2 with Alt-Svc header directing clients to HTTP/3
// - HTTP/1.1 with WebSocket upgrade support
// - HTTP/1.1 fallback for stateless requests
type AutoServer struct {
	rootObject Object
	addr       string
	tlsConfig  *tls.Config

	// Server instances
	http3Server *WebTransportServer
	http2Server *HTTP2Server
	http1Server *http.Server
	httpHandler *HTTPHandler

	// For managing different ports
	http3Addr string
	http2Addr string

	closeOnce sync.Once
}

// NewAutoServer creates a new auto-negotiating server that runs multiple transport protocols.
// It will bind to the specified address and automatically start HTTP/2 and HTTP/3 servers.
// If tlsConfig is nil, a self-signed certificate will be generated.
func NewAutoServer(addr string, rootObject Object, tlsConfig *tls.Config) (*AutoServer, error) {
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

	// Parse address to get host and port
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	// Use same port for HTTP/2 and HTTP/3 (standard QUIC behavior)
	http2Addr := net.JoinHostPort(host, strconv.Itoa(port))
	http3Addr := net.JoinHostPort(host, strconv.Itoa(port))

	// Create HTTP/3 WebTransport server
	http3Server, err := NewWebTransportServer(http3Addr, rootObject, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP/3 server: %w", err)
	}

	// Create HTTP/2 server
	http2Server := NewHTTP2Server(rootObject, nil)

	// Create HTTP/1.1 handler (supports WebSocket upgrade)
	httpHandler := NewHTTPHandler(rootObject)

	return &AutoServer{
		rootObject:  rootObject,
		addr:        addr,
		tlsConfig:   tlsConfig,
		http3Server: http3Server,
		http2Server: http2Server,
		httpHandler: httpHandler,
		http3Addr:   http3Addr,
		http2Addr:   http2Addr,
	}, nil
}

// ListenAndServe starts all transport servers.
// It runs HTTP/3, HTTP/2, and HTTP/1.1 servers concurrently.
func (s *AutoServer) ListenAndServe() error {
	errChan := make(chan error, 2)

	// Start HTTP/3 WebTransport server
	go func() {
		if err := s.http3Server.ListenAndServe(); err != nil {
			errChan <- fmt.Errorf("HTTP/3 server error: %w", err)
		}
	}()

	// Start HTTP/2 server with HTTP/1.1 fallback
	// This server handles both HTTP/2 and HTTP/1.1 on the same port
	go func() {
		// Create a wrapper handler that adds Alt-Svc and falls back to HTTP/1.1
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Add Alt-Svc header to direct clients to HTTP/3
			// Format: Alt-Svc: h3=":port"; ma=2592000
			_, port, _ := net.SplitHostPort(s.http3Addr)
			w.Header().Set("Alt-Svc", fmt.Sprintf(`h3=":%s"; ma=2592000`, port))

			// Handle HEAD requests for protocol probing
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}

			// Route based on protocol version
			if r.ProtoMajor == 2 {
				// HTTP/2 - use HTTP2Server
				s.http2Server.ServeHTTP(w, r)
			} else {
				// HTTP/1.1 - use HTTPHandler (supports WebSocket upgrade)
				s.httpHandler.ServeHTTP(w, r)
			}
		})

		s.http1Server = &http.Server{
			Addr:      s.http2Addr,
			Handler:   handler,
			TLSConfig: s.tlsConfig,
		}

		if err := s.http1Server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("HTTP/2 server error: %w", err)
		}
	}()

	// Wait for any server to error
	return <-errChan
}

// Close stops all servers gracefully.
func (s *AutoServer) Close() error {
	var firstErr error

	s.closeOnce.Do(func() {
		// Close HTTP/3 server
		if err := s.http3Server.Close(); err != nil && firstErr == nil {
			firstErr = err
		}

		// Close HTTP/2 server
		if err := s.http2Server.Close(); err != nil && firstErr == nil {
			firstErr = err
		}

		// Close HTTP/1.1 server
		if s.http1Server != nil {
			if err := s.http1Server.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}

		// Close HTTP handler
		s.httpHandler.Close()
	})

	return firstErr
}

// GetHTTP3Addr returns the address of the HTTP/3 server.
func (s *AutoServer) GetHTTP3Addr() string {
	return s.http3Addr
}

// GetHTTP2Addr returns the address of the HTTP/2 server.
func (s *AutoServer) GetHTTP2Addr() string {
	return s.http2Addr
}
