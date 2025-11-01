package jsonrpc3

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebTransport_BasicConnection tests that WebTransport connection can be established
func TestWebTransport_BasicConnection(t *testing.T) {
	root := NewMethodMap()
	server, err := NewWebTransportServer("localhost:0", root, nil)
	require.NoError(t, err)

	// Start server in background
	go func() {
		server.ListenAndServe()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// For now, just verify server starts
	// Full client connection test will be in integration tests
}

// TestWebTransport_FramingUtilities tests message framing
func TestWebTransport_FramingUtilities(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("hello")},
		{"json", []byte(`{"jsonrpc":"3.0","method":"test","id":1}`)},
		{"large", make([]byte, 10000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a pipe for testing
			r, w := newTestPipe()

			// Write framed message
			err := writeFramedMessage(w, tt.data)
			require.NoError(t, err)

			// Read framed message
			result, err := readFramedMessage(r)
			require.NoError(t, err)

			// Verify data matches
			assert.Equal(t, tt.data, result)
		})
	}
}

// TestWebTransport_FramingMaxSize tests message size limit
func TestWebTransport_FramingMaxSize(t *testing.T) {
	r, w := newTestPipe()

	// Create message larger than max size
	largeData := make([]byte, 101*1024*1024) // 101 MB

	// Write should succeed
	err := writeFramedMessage(w, largeData)
	require.NoError(t, err)

	// Read should fail due to size limit
	_, err = readFramedMessage(r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "message too large")
}

// TestWebTransportConn_ObjectRegistration tests registering and calling objects
func TestWebTransportConn_ObjectRegistration(t *testing.T) {
	// Create server connection
	root := NewMethodMap()
	server, err := NewWebTransportServer("localhost:0", root, nil)
	require.NoError(t, err)
	defer server.Close()

	// Test object registration (without actual network connection)
	session := NewSession()
	handler := NewHandler(session, root, NewNoOpCaller(), []string{"application/json"})

	// Register a method
	root.Register("test", func(params Params, caller Caller) (any, error) {
		return "ok", nil
	})

	// Create a test request
	req, err := NewRequest("test", nil, 1)
	require.NoError(t, err)

	// Handle the request
	resp := handler.HandleRequest(req)
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
}

// testPipe implements a simple in-memory pipe for testing
type testPipe struct {
	buf    []byte
	offset int
	mu     sync.Mutex
	cond   *sync.Cond
}

func newTestPipe() (*testPipe, *testPipe) {
	p := &testPipe{}
	p.cond = sync.NewCond(&p.mu)
	return p, p
}

func (p *testPipe) Read(b []byte) (n int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Wait for data
	for len(p.buf)-p.offset == 0 {
		p.cond.Wait()
	}

	n = copy(b, p.buf[p.offset:])
	p.offset += n
	return n, nil
}

func (p *testPipe) Write(b []byte) (n int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.buf = append(p.buf, b...)
	p.cond.Signal()
	return len(b), nil
}

func (p *testPipe) Close() error {
	return nil
}
