package jsonrpc3

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPeer_BasicCommunication tests basic peer-to-peer communication
func TestPeer_BasicCommunication(t *testing.T) {
	// Create pipe for bidirectional communication
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	defer r1.Close()
	defer w1.Close()
	defer r2.Close()
	defer w2.Close()

	// Create server root with echo method
	serverRoot := NewMethodMap()
	serverRoot.Register("echo", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var msg string
		if err := params.Decode(&msg); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		return msg, nil
	})

	// Create peers
	peer1, err := NewPeer(r1, w2, serverRoot)
	require.NoError(t, err)
	defer peer1.Close()

	peer2, err := NewPeer(r2, w1, nil)
	require.NoError(t, err)
	defer peer2.Close()

	// Give peers time to start
	time.Sleep(50 * time.Millisecond)

	// Test echo call from peer2 to peer1
	val, err := peer2.Call("echo", "hello")
	require.NoError(t, err)
	var result string
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "hello", result)
}

// TestPeer_BidirectionalCalls tests both peers can call each other
func TestPeer_BidirectionalCalls(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	defer r1.Close()
	defer w1.Close()
	defer r2.Close()
	defer w2.Close()

	// Peer 1 methods
	peer1Root := NewMethodMap()
	peer1Root.Register("greet", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var name string
		params.Decode(&name)
		return "Hello, " + name, nil
	})

	// Peer 2 methods
	peer2Root := NewMethodMap()
	peer2Root.Register("add", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var nums []int
		params.Decode(&nums)
		sum := 0
		for _, n := range nums {
			sum += n
		}
		return sum, nil
	})

	peer1, err := NewPeer(r1, w2, peer1Root)
	require.NoError(t, err)
	defer peer1.Close()

	peer2, err := NewPeer(r2, w1, peer2Root)
	require.NoError(t, err)
	defer peer2.Close()

	time.Sleep(50 * time.Millisecond)

	// Peer 2 calls peer 1
	val1, err := peer2.Call("greet", "Alice")
	require.NoError(t, err)
	var greeting string
	err = val1.Decode(&greeting)
	require.NoError(t, err)
	assert.Equal(t, "Hello, Alice", greeting)

	// Peer 1 calls peer 2
	val2, err := peer1.Call("add", []int{1, 2, 3, 4, 5})
	require.NoError(t, err)
	var sum int
	err = val2.Decode(&sum)
	require.NoError(t, err)
	assert.Equal(t, 15, sum)
}

// TestPeer_Notifications tests one-way notifications
func TestPeer_Notifications(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	defer r1.Close()
	defer w1.Close()
	defer r2.Close()
	defer w2.Close()

	var notifCount atomic.Int32
	serverRoot := NewMethodMap()
	serverRoot.Register("log", func(ctx context.Context, params Params, caller Caller) (any, error) {
		notifCount.Add(1)
		return nil, nil
	})

	peer1, err := NewPeer(r1, w2, serverRoot)
	require.NoError(t, err)
	defer peer1.Close()

	peer2, err := NewPeer(r2, w1, nil)
	require.NoError(t, err)
	defer peer2.Close()

	time.Sleep(50 * time.Millisecond)

	// Send notifications
	err = peer2.Notify("log", "message 1")
	require.NoError(t, err)

	err = peer2.Notify("log", "message 2")
	require.NoError(t, err)

	err = peer2.Notify("log", "message 3")
	require.NoError(t, err)

	// Wait for notifications to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify notifications were received
	assert.Equal(t, int32(3), notifCount.Load())
}

// TestPeer_ConcurrentRequests tests multiple concurrent requests
func TestPeer_ConcurrentRequests(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	defer r1.Close()
	defer w1.Close()
	defer r2.Close()
	defer w2.Close()

	var callCount atomic.Int32
	serverRoot := NewMethodMap()
	serverRoot.Register("increment", func(ctx context.Context, params Params, caller Caller) (any, error) {
		count := callCount.Add(1)
		time.Sleep(10 * time.Millisecond) // Simulate work
		return count, nil
	})

	peer1, err := NewPeer(r1, w2, serverRoot)
	require.NoError(t, err)
	defer peer1.Close()

	peer2, err := NewPeer(r2, w1, nil)
	require.NoError(t, err)
	defer peer2.Close()

	time.Sleep(50 * time.Millisecond)

	// Make concurrent calls
	const numCalls = 10
	var wg sync.WaitGroup
	results := make([]int, numCalls)
	var resultsMu sync.Mutex

	wg.Add(numCalls)
	for i := 0; i < numCalls; i++ {
		go func(idx int) {
			defer wg.Done()
			val, err := peer2.Call("increment", nil)
			assert.NoError(t, err)
			var result int
			err = val.Decode(&result)
			assert.NoError(t, err)
			resultsMu.Lock()
			results[idx] = result
			resultsMu.Unlock()
		}(i)
	}

	wg.Wait()

	// Verify all calls completed
	resultsMu.Lock()
	assert.Equal(t, int32(numCalls), callCount.Load())
	assert.Len(t, results, numCalls)
	resultsMu.Unlock()
}

// TestPeer_ObjectRegistration tests registering and calling remote objects
func TestPeer_ObjectRegistration(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	defer r1.Close()
	defer w1.Close()
	defer r2.Close()
	defer w2.Close()

	// Create counter object
	counterObj := NewMethodMap()
	var count int
	var countMu sync.Mutex

	counterObj.Register("increment", func(ctx context.Context, params Params, caller Caller) (any, error) {
		countMu.Lock()
		defer countMu.Unlock()
		count++
		return count, nil
	})

	counterObj.Register("getCount", func(ctx context.Context, params Params, caller Caller) (any, error) {
		countMu.Lock()
		defer countMu.Unlock()
		return count, nil
	})

	serverRoot := NewMethodMap()
	serverRoot.Register("getCounter", func(ctx context.Context, params Params, caller Caller) (any, error) {
		return NewReference("counter-1"), nil
	})

	peer1, err := NewPeer(r1, w2, serverRoot)
	require.NoError(t, err)
	defer peer1.Close()
	peer1.RegisterObject("counter-1", counterObj)

	peer2, err := NewPeer(r2, w1, nil)
	require.NoError(t, err)
	defer peer2.Close()

	time.Sleep(50 * time.Millisecond)

	// Get counter reference
	val, err := peer2.Call("getCounter", nil)
	require.NoError(t, err)
	var counterRef Reference
	err = val.Decode(&counterRef)
	require.NoError(t, err)

	// Call methods on the counter object
	val2, err := peer2.Call("increment", nil, ToRef(counterRef))
	require.NoError(t, err)
	var result int
	err = val2.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 1, result)

	val3, err := peer2.Call("increment", nil, ToRef(counterRef))
	require.NoError(t, err)
	err = val3.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 2, result)

	val4, err := peer2.Call("getCount", nil, ToRef(counterRef))
	require.NoError(t, err)
	err = val4.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 2, result)
}

// TestPeer_ErrorHandling tests error propagation
func TestPeer_ErrorHandling(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	defer r1.Close()
	defer w1.Close()
	defer r2.Close()
	defer w2.Close()

	serverRoot := NewMethodMap()
	serverRoot.Register("divide", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var nums struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := params.Decode(&nums); err != nil {
			return nil, NewInvalidParamsError(err.Error())
		}
		if nums.B == 0 {
			return nil, &Error{
				Code:    -32000,
				Message: "Division by zero",
			}
		}
		return nums.A / nums.B, nil
	})

	peer1, err := NewPeer(r1, w2, serverRoot)
	require.NoError(t, err)
	defer peer1.Close()

	peer2, err := NewPeer(r2, w1, nil)
	require.NoError(t, err)
	defer peer2.Close()

	time.Sleep(50 * time.Millisecond)

	// Test valid division
	val, err := peer2.Call("divide", map[string]float64{"a": 10, "b": 2})
	require.NoError(t, err)
	var result float64
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 5.0, result)

	// Test division by zero
	_, err = peer2.Call("divide", map[string]float64{"a": 10, "b": 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Division by zero")

	// Test method not found
	_, err = peer2.Call("nonexistent", nil)
	require.Error(t, err)
}

// TestPeer_CBOR tests CBOR encoding
func TestPeer_CBOR(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	defer r1.Close()
	defer w1.Close()
	defer r2.Close()
	defer w2.Close()

	serverRoot := NewMethodMap()
	serverRoot.Register("echo", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var data map[string]any
		params.Decode(&data)
		return data, nil
	})

	// Create peers with CBOR encoding
	peer1, err := NewPeer(r1, w2, serverRoot, WithContentType("application/cbor"))
	require.NoError(t, err)
	defer peer1.Close()
	assert.Equal(t, "application/cbor", peer1.contentType)

	peer2, err := NewPeer(r2, w1, nil, WithContentType("application/cbor"))
	require.NoError(t, err)
	defer peer2.Close()
	assert.Equal(t, "application/cbor", peer2.contentType)

	time.Sleep(50 * time.Millisecond)

	// Test with CBOR
	input := map[string]any{
		"name":  "test",
		"value": 42,
	}
	val, err := peer2.Call("echo", input)
	require.NoError(t, err)
	var result map[string]any
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "test", result["name"])
	// CBOR may decode integers as uint64 or int64
	switch v := result["value"].(type) {
	case uint64:
		assert.Equal(t, uint64(42), v)
	case int64:
		assert.Equal(t, int64(42), v)
	case float64:
		assert.Equal(t, float64(42), v)
	default:
		t.Fatalf("unexpected type for value: %T", v)
	}
}

// TestPeer_JSON tests JSON encoding
func TestPeer_JSON(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	defer r1.Close()
	defer w1.Close()
	defer r2.Close()
	defer w2.Close()

	serverRoot := NewMethodMap()
	serverRoot.Register("echo", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var data map[string]any
		params.Decode(&data)
		return data, nil
	})

	// Create peers with JSON encoding
	peer1, err := NewPeer(r1, w2, serverRoot, WithContentType("application/json"))
	require.NoError(t, err)
	defer peer1.Close()
	assert.Equal(t, "application/json", peer1.contentType)

	peer2, err := NewPeer(r2, w1, nil, WithContentType("application/json"))
	require.NoError(t, err)
	defer peer2.Close()
	assert.Equal(t, "application/json", peer2.contentType)

	time.Sleep(50 * time.Millisecond)

	// Test with JSON
	input := map[string]any{
		"name":  "test",
		"value": float64(42), // JSON always uses float64 for numbers
	}
	val, err := peer2.Call("echo", input)
	require.NoError(t, err)
	var result map[string]any
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "test", result["name"])
	assert.Equal(t, float64(42), result["value"])
}

// TestPeer_CloseHandling tests graceful close handling
func TestPeer_CloseHandling(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	serverRoot := NewMethodMap()
	serverRoot.Register("echo", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var msg string
		params.Decode(&msg)
		return msg, nil
	})

	peer1, err := NewPeer(r1, w2, serverRoot)
	require.NoError(t, err)

	peer2, err := NewPeer(r2, w1, nil)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Make a successful call
	val, err := peer2.Call("echo", "test")
	require.NoError(t, err)
	var result string
	err = val.Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "test", result)

	// Close peer1
	peer1.Close()
	r1.Close()
	w1.Close()

	// Wait a moment for close to propagate
	time.Sleep(100 * time.Millisecond)

	// Try to call - should fail
	_, err = peer2.Call("echo", "test")
	assert.Error(t, err)

	// Close peer2
	peer2.Close()
	r2.Close()
	w2.Close()
}
