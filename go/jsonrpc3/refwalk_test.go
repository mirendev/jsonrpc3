package jsonrpc3

import (
	"encoding/json"
	"testing"
)

// TestCounter is a simple Object implementation for testing
type TestCounter struct {
	Value int
}

func (c *TestCounter) CallMethod(method string, params Params) (any, error) {
	switch method {
	case "increment":
		c.Value++
		return c.Value, nil
	case "getValue":
		return c.Value, nil
	case "reset":
		c.Value = 0
		return nil, nil
	default:
		return nil, NewMethodNotFoundError(method)
	}
}

func TestProcessResult_Primitive(t *testing.T) {
	s := NewSession()
	root := NewMethodMap()
	h := NewHandler(s, root, nil)

	tests := []struct {
		name   string
		result any
	}{
		{"string", "hello"},
		{"int", 42},
		{"float", 3.14},
		{"bool", true},
		{"nil", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processed := processResult(h, tt.result)
			if processed != tt.result {
				t.Errorf("primitive value changed: got %v, want %v", processed, tt.result)
			}
		})
	}
}

func TestProcessResult_Object(t *testing.T) {
	s := NewSession()
	root := NewMethodMap()
	h := NewHandler(s, root, nil)

	counter := &TestCounter{Value: 10}
	processed := processResult(h, counter)

	// Should be replaced with LocalReference
	ref, ok := processed.(LocalReference)
	if !ok {
		t.Fatalf("expected LocalReference, got %T", processed)
	}

	if ref.Ref == "" {
		t.Error("LocalReference should have non-empty Ref")
	}

	// Verify it was registered in session
	obj := h.session.GetLocalRef(ref.Ref)
	if obj == nil {
		t.Error("Object should be registered in session")
	}

	// Verify it's the same object
	if obj != counter {
		t.Error("registered object should be the same as original")
	}
}

func TestProcessResult_SliceWithObject(t *testing.T) {
	s := NewSession()
	root := NewMethodMap()
	h := NewHandler(s, root, nil)

	counter1 := &TestCounter{Value: 1}
	counter2 := &TestCounter{Value: 2}

	result := []any{"hello", counter1, 42, counter2}
	processed := processResult(h, result)

	processedSlice, ok := processed.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", processed)
	}

	if len(processedSlice) != 4 {
		t.Fatalf("expected 4 elements, got %d", len(processedSlice))
	}

	// First element should be unchanged
	if processedSlice[0] != "hello" {
		t.Errorf("element 0: got %v, want hello", processedSlice[0])
	}

	// Second element should be LocalReference
	ref1, ok := processedSlice[1].(LocalReference)
	if !ok {
		t.Errorf("element 1 should be LocalReference, got %T", processedSlice[1])
	} else if h.session.GetLocalRef(ref1.Ref) == nil {
		t.Error("counter1 should be registered")
	}

	// Third element should be unchanged
	if processedSlice[2] != 42 {
		t.Errorf("element 2: got %v, want 42", processedSlice[2])
	}

	// Fourth element should be LocalReference
	ref2, ok := processedSlice[3].(LocalReference)
	if !ok {
		t.Errorf("element 3 should be LocalReference, got %T", processedSlice[3])
	} else if h.session.GetLocalRef(ref2.Ref) == nil {
		t.Error("counter2 should be registered")
	}

	// References should be different
	if ref1.Ref == ref2.Ref {
		t.Error("different objects should have different references")
	}
}

func TestProcessResult_MapWithObject(t *testing.T) {
	s := NewSession()
	root := NewMethodMap()
	h := NewHandler(s, root, nil)

	counter := &TestCounter{Value: 5}

	result := map[string]any{
		"name":    "test",
		"counter": counter,
		"value":   100,
	}

	processed := processResult(h, result)

	processedMap, ok := processed.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", processed)
	}

	// Check name field
	if processedMap["name"] != "test" {
		t.Errorf("name: got %v, want test", processedMap["name"])
	}

	// Check counter field - should be LocalReference
	ref, ok := processedMap["counter"].(LocalReference)
	if !ok {
		t.Errorf("counter should be LocalReference, got %T", processedMap["counter"])
	} else if h.session.GetLocalRef(ref.Ref) == nil {
		t.Error("counter should be registered in session")
	}

	// Check value field
	if processedMap["value"] != 100 {
		t.Errorf("value: got %v, want 100", processedMap["value"])
	}
}

func TestProcessResult_NestedStructures(t *testing.T) {
	s := NewSession()
	root := NewMethodMap()
	h := NewHandler(s, root, nil)

	counter1 := &TestCounter{Value: 1}
	counter2 := &TestCounter{Value: 2}

	result := map[string]any{
		"counters": []any{counter1, counter2},
		"data": map[string]any{
			"primary": counter1,
		},
	}

	processed := processResult(h, result)

	processedMap, ok := processed.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", processed)
	}

	// Check counters slice
	counters, ok := processedMap["counters"].([]any)
	if !ok {
		t.Fatalf("counters should be []any, got %T", processedMap["counters"])
	}

	ref1, ok := counters[0].(LocalReference)
	if !ok {
		t.Errorf("counters[0] should be LocalReference, got %T", counters[0])
	}

	ref2, ok := counters[1].(LocalReference)
	if !ok {
		t.Errorf("counters[1] should be LocalReference, got %T", counters[1])
	}

	// Check nested map
	data, ok := processedMap["data"].(map[string]any)
	if !ok {
		t.Fatalf("data should be map[string]any, got %T", processedMap["data"])
	}

	_, ok = data["primary"].(LocalReference)
	if !ok {
		t.Errorf("data.primary should be LocalReference, got %T", data["primary"])
	}

	// Note: Currently, the same object appearing in different places
	// gets registered multiple times with different references.
	// This is acceptable behavior as each occurrence is independent.

	// Verify all registered
	if h.session.GetLocalRef(ref1.Ref) == nil {
		t.Error("counter1 should be registered")
	}
	if h.session.GetLocalRef(ref2.Ref) == nil {
		t.Error("counter2 should be registered")
	}
}

func TestProcessResult_ExistingLocalReference(t *testing.T) {
	s := NewSession()
	root := NewMethodMap()
	h := NewHandler(s, root, nil)

	// Create an existing LocalReference
	existingRef := NewLocalReference("existing-ref")

	result := map[string]any{
		"ref": existingRef,
	}

	processed := processResult(h, result)

	processedMap, ok := processed.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", processed)
	}

	// LocalReference should pass through unchanged
	ref, ok := processedMap["ref"].(LocalReference)
	if !ok {
		t.Errorf("ref should be LocalReference, got %T", processedMap["ref"])
	}

	if ref.Ref != "existing-ref" {
		t.Errorf("ref should be unchanged, got %s", ref.Ref)
	}
}

func TestHandler_ObjectIntegration(t *testing.T) {
	s := NewSession()
	root := NewMethodMap()
	h := NewHandler(s, root, nil)

	// Register a method that returns an Object
	root.Register("create_counter", func(params Params) (any, error) {
		var initial int
		if params != nil {
			params.Decode(&initial)
		}
		return &TestCounter{Value: initial}, nil
	})

	// Call the method
	req, _ := NewRequest("create_counter", 5, 1)
	resp := h.HandleRequest(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	// Result should be a LocalReference
	var ref LocalReference
	if err := json.Unmarshal(resp.Result, &ref); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if ref.Ref == "" {
		t.Error("expected non-empty reference")
	}

	// Verify object is registered in session
	if h.session.GetLocalRef(ref.Ref) == nil {
		t.Error("counter should be registered in session")
	}

	// Now call a method on the reference
	req2, _ := NewRequestWithRef(ref.Ref, "increment", nil, 2)
	resp2 := h.HandleRequest(req2)

	if resp2.Error != nil {
		t.Fatalf("increment error: %v", resp2.Error)
	}

	var value int
	if err := json.Unmarshal(resp2.Result, &value); err != nil {
		t.Fatalf("failed to unmarshal value: %v", err)
	}

	if value != 6 {
		t.Errorf("value = %d, want 6", value)
	}
}

func TestHandler_ObjectReturningObject(t *testing.T) {
	s := NewSession()
	root := NewMethodMap()
	h := NewHandler(s, root, nil)

	// Register a method that returns multiple Objects
	root.Register("create_pair", func(params Params) (any, error) {
		return map[string]any{
			"counter1": &TestCounter{Value: 1},
			"counter2": &TestCounter{Value: 2},
		}, nil
	})

	req, _ := NewRequest("create_pair", nil, 1)
	resp := h.HandleRequest(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result map[string]LocalReference
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if _, ok := result["counter1"]; !ok {
		t.Error("expected counter1 in result")
	}
	if _, ok := result["counter2"]; !ok {
		t.Error("expected counter2 in result")
	}

	// Both should be registered
	if h.session.GetLocalRef(result["counter1"].Ref) == nil {
		t.Error("counter1 should be registered")
	}
	if h.session.GetLocalRef(result["counter2"].Ref) == nil {
		t.Error("counter2 should be registered")
	}
}
