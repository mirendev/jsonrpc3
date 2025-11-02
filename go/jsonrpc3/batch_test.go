package jsonrpc3

import (
	"context"
	"io"
	"sync"
	"testing"
)

// TestBatch_SimpleChain tests a simple batch with chained calls
func TestBatch_SimpleChain(t *testing.T) {
	// Create test server
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	serverRoot := NewMethodMap()
	serverRoot.Register("createCounter", func(ctx context.Context, params Params, caller Caller) (any, error) {
		counter := NewMethodMap()
		count := 0
		var mu sync.Mutex

		counter.Register("increment", func(ctx context.Context, params Params, caller Caller) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			count++
			return count, nil
		})

		counter.Register("getValue", func(ctx context.Context, params Params, caller Caller) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			return count, nil
		})

		return counter, nil
	})

	serverPeer, err := NewPeer(r1, w2, serverRoot)
	if err != nil {
		t.Fatalf("Failed to create server peer: %v", err)
	}
	defer serverPeer.Close()

	// Create client peer
	clientRoot := NewMethodMap()
	clientPeer, err := NewPeer(r2, w1, clientRoot)
	if err != nil {
		t.Fatalf("Failed to create client peer: %v", err)
	}
	defer clientPeer.Close()

	// Execute batch with chained calls
	resp, err := ExecuteBatch(clientPeer, func(b *BatchBuilder) error {
		counter := b.Call("createCounter", nil)
		counter.Call("increment", nil)
		counter.Call("increment", nil)
		counter.Call("getValue", nil)
		return nil
	})

	if err != nil {
		t.Fatalf("Batch failed: %v", err)
	}

	// Verify number of responses
	if resp.Len() != 4 {
		t.Fatalf("Expected 4 responses, got %d", resp.Len())
	}

	// Check counter creation (should return a reference)
	counterResult, err := resp.GetResult(0)
	if err != nil {
		t.Fatalf("Failed to get counter result: %v", err)
	}
	if counterResult == nil {
		t.Fatal("Counter result should not be nil")
	}

	// Check first increment
	val1, err := resp.GetResult(1)
	if err != nil {
		t.Fatalf("Failed to get first increment result: %v", err)
	}
	if val1 != float64(1) {
		t.Errorf("Expected first increment to return 1, got %v", val1)
	}

	// Check second increment
	val2, err := resp.GetResult(2)
	if err != nil {
		t.Fatalf("Failed to get second increment result: %v", err)
	}
	if val2 != float64(2) {
		t.Errorf("Expected second increment to return 2, got %v", val2)
	}

	// Check getValue
	val3, err := resp.GetResult(3)
	if err != nil {
		t.Fatalf("Failed to get getValue result: %v", err)
	}
	if val3 != float64(2) {
		t.Errorf("Expected getValue to return 2, got %v", val3)
	}
}

// TestBatch_MultipleChains tests multiple independent chains in the same batch
func TestBatch_MultipleChains(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	serverRoot := NewMethodMap()
	serverRoot.Register("add", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var nums []int
		if err := params.Decode(&nums); err != nil {
			return nil, err
		}
		sum := 0
		for _, n := range nums {
			sum += n
		}
		return sum, nil
	})

	serverRoot.Register("multiply", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var nums []int
		if err := params.Decode(&nums); err != nil {
			return nil, err
		}
		product := 1
		for _, n := range nums {
			product *= n
		}
		return product, nil
	})

	serverPeer, err := NewPeer(r1, w2, serverRoot)
	if err != nil {
		t.Fatalf("Failed to create server peer: %v", err)
	}
	defer serverPeer.Close()

	clientRoot := NewMethodMap()
	clientPeer, err := NewPeer(r2, w1, clientRoot)
	if err != nil {
		t.Fatalf("Failed to create client peer: %v", err)
	}
	defer clientPeer.Close()

	// Execute batch with multiple independent calls
	resp, err := ExecuteBatch(clientPeer, func(b *BatchBuilder) error {
		b.Call("add", []int{1, 2})
		b.Call("add", []int{3, 4})
		b.Call("multiply", []int{2, 3})
		b.Call("multiply", []int{4, 5})
		return nil
	})

	if err != nil {
		t.Fatalf("Batch failed: %v", err)
	}

	if resp.Len() != 4 {
		t.Fatalf("Expected 4 responses, got %d", resp.Len())
	}

	// Check results
	results := []float64{3, 7, 6, 20}
	for i, expected := range results {
		val, err := resp.GetResult(i)
		if err != nil {
			t.Fatalf("Failed to get result %d: %v", i, err)
		}
		if val != expected {
			t.Errorf("Result %d: expected %v, got %v", i, expected, val)
		}
	}
}

// TestBatch_EmptyBatch tests that an empty batch returns an empty response
func TestBatch_EmptyBatch(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	serverRoot := NewMethodMap()
	serverPeer, err := NewPeer(r1, w2, serverRoot)
	if err != nil {
		t.Fatalf("Failed to create server peer: %v", err)
	}
	defer serverPeer.Close()

	clientRoot := NewMethodMap()
	clientPeer, err := NewPeer(r2, w1, clientRoot)
	if err != nil {
		t.Fatalf("Failed to create client peer: %v", err)
	}
	defer clientPeer.Close()

	// Execute empty batch
	resp, err := ExecuteBatch(clientPeer, func(b *BatchBuilder) error {
		// Don't add any calls
		return nil
	})

	if err != nil {
		t.Fatalf("Empty batch failed: %v", err)
	}

	if resp.Len() != 0 {
		t.Errorf("Expected 0 responses for empty batch, got %d", resp.Len())
	}
}

// TestBatch_ErrorResponse tests handling of error responses in batch
func TestBatch_ErrorResponse(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	serverRoot := NewMethodMap()
	serverRoot.Register("divide", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var nums []int
		if err := params.Decode(&nums); err != nil {
			return nil, err
		}
		if len(nums) != 2 {
			return nil, NewInvalidParamsError("expected 2 numbers")
		}
		if nums[1] == 0 {
			return nil, NewError(-32000, "division by zero", nil)
		}
		return nums[0] / nums[1], nil
	})

	serverPeer, err := NewPeer(r1, w2, serverRoot)
	if err != nil {
		t.Fatalf("Failed to create server peer: %v", err)
	}
	defer serverPeer.Close()

	clientRoot := NewMethodMap()
	clientPeer, err := NewPeer(r2, w1, clientRoot)
	if err != nil {
		t.Fatalf("Failed to create client peer: %v", err)
	}
	defer clientPeer.Close()

	// Execute batch with one error
	resp, err := ExecuteBatch(clientPeer, func(b *BatchBuilder) error {
		b.Call("divide", []int{10, 2})
		b.Call("divide", []int{10, 0}) // Division by zero
		b.Call("divide", []int{20, 4})
		return nil
	})

	if err != nil {
		t.Fatalf("Batch failed: %v", err)
	}

	if resp.Len() != 3 {
		t.Fatalf("Expected 3 responses, got %d", resp.Len())
	}

	// First result should succeed
	val1, err := resp.GetResult(0)
	if err != nil {
		t.Errorf("First result should succeed: %v", err)
	}
	if val1 != float64(5) {
		t.Errorf("Expected 5, got %v", val1)
	}

	// Second result should error
	_, err = resp.GetResult(1)
	if err == nil {
		t.Error("Second result should have error")
	}

	// Third result should succeed
	val3, err := resp.GetResult(2)
	if err != nil {
		t.Errorf("Third result should succeed: %v", err)
	}
	if val3 != float64(5) {
		t.Errorf("Expected 5, got %v", val3)
	}

	// Test HasError
	if !resp.HasError() {
		t.Error("BatchResponse.HasError() should return true")
	}
}

// TestBatch_GetResults tests the GetResults convenience method
func TestBatch_GetResults(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	serverRoot := NewMethodMap()
	serverRoot.Register("double", func(ctx context.Context, params Params, caller Caller) (any, error) {
		var num int
		if err := params.Decode(&num); err != nil {
			return nil, err
		}
		return num * 2, nil
	})

	serverPeer, err := NewPeer(r1, w2, serverRoot)
	if err != nil {
		t.Fatalf("Failed to create server peer: %v", err)
	}
	defer serverPeer.Close()

	clientRoot := NewMethodMap()
	clientPeer, err := NewPeer(r2, w1, clientRoot)
	if err != nil {
		t.Fatalf("Failed to create client peer: %v", err)
	}
	defer clientPeer.Close()

	// Execute batch
	resp, err := ExecuteBatch(clientPeer, func(b *BatchBuilder) error {
		b.Call("double", 5)
		b.Call("double", 10)
		b.Call("double", 15)
		return nil
	})

	if err != nil {
		t.Fatalf("Batch failed: %v", err)
	}

	// Get all results at once
	results, err := resp.GetResults()
	if err != nil {
		t.Fatalf("GetResults failed: %v", err)
	}

	expected := []float64{10, 20, 30}
	if len(results) != len(expected) {
		t.Fatalf("Expected %d results, got %d", len(expected), len(results))
	}

	for i, exp := range expected {
		if results[i] != exp {
			t.Errorf("Result %d: expected %v, got %v", i, exp, results[i])
		}
	}
}

// TestBatch_BuilderError tests that errors in the builder function are returned
func TestBatch_BuilderError(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	serverRoot := NewMethodMap()
	serverPeer, err := NewPeer(r1, w2, serverRoot)
	if err != nil {
		t.Fatalf("Failed to create server peer: %v", err)
	}
	defer serverPeer.Close()

	clientRoot := NewMethodMap()
	clientPeer, err := NewPeer(r2, w1, clientRoot)
	if err != nil {
		t.Fatalf("Failed to create client peer: %v", err)
	}
	defer clientPeer.Close()

	// Execute batch that returns an error
	_, err = ExecuteBatch(clientPeer, func(b *BatchBuilder) error {
		b.Call("someMethod", nil)
		return NewError(-32000, "builder error", nil)
	})

	if err == nil {
		t.Fatal("Expected error from builder function")
	}
}
