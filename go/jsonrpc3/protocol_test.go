package jsonrpc3

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProtocolHandler(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, []string{"application/json"})

	if h == nil {
		t.Fatal("NewProtocolHandler() returned nil")
	}
	if h.session != s {
		t.Error("session not set correctly")
	}
	if len(h.mimeTypes) != 1 || h.mimeTypes[0] != "application/json" {
		t.Errorf("mimeTypes = %v, want [application/json]", h.mimeTypes)
	}
}

func TestProtocolHandler_SessionID(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	result, err := h.CallMethod(context.Background(), "session_id", NewParams(nil), nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	sessionResult, ok := result.(*SessionIDResult)
	if !ok {
		t.Fatalf("result type = %T, want *SessionIDResult", result)
	}

	if sessionResult.SessionID != s.ID() {
		t.Errorf("SessionID = %v, want %v", sessionResult.SessionID, s.ID())
	}
}

func TestProtocolHandler_Dispose(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	// Add a local ref
	s.AddLocalRef("obj-1", &dummyObject{name: "test"})

	// Dispose it
	params, _ := json.Marshal(DisposeParams{Ref: "obj-1"})
	result, err := h.CallMethod(context.Background(), "dispose", NewParams(params), nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if result != true {
		t.Errorf("result = %v, want true", result)
	}

	// Verify it's gone
	if s.HasLocalRef("obj-1") {
		t.Error("ref should be removed")
	}
}

func TestProtocolHandler_DisposeRemoteRef(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	// Add a remote ref
	s.AddRemoteRef("remote-1", nil)

	// Dispose it
	params, _ := json.Marshal(DisposeParams{Ref: "remote-1"})
	result, err := h.CallMethod(context.Background(), "dispose", NewParams(params), nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if result != true {
		t.Errorf("result = %v, want true", result)
	}

	// Verify it's gone
	if s.HasRemoteRef("remote-1") {
		t.Error("remote ref should be removed")
	}
}

func TestProtocolHandler_DisposeNotFound(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	params, _ := json.Marshal(DisposeParams{Ref: "non-existent"})
	_, err := h.CallMethod(context.Background(), "dispose", NewParams(params), nil)
	if err == nil {
		t.Fatal("expected error for non-existent ref")
	}
	rpcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if rpcErr.Code != CodeReferenceNotFound {
		t.Errorf("error code = %v, want %v", rpcErr.Code, CodeReferenceNotFound)
	}
}

func TestProtocolHandler_DisposeInvalidParams(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	tests := []struct {
		name   string
		params RawMessage
	}{
		{
			name:   "empty ref",
			params: RawMessage(`{"ref":""}`),
		},
		{
			name:   "invalid json",
			params: RawMessage(`{invalid}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.CallMethod(context.Background(), "dispose", NewParams(tt.params), nil)
			if err == nil {
				t.Fatal("expected error for invalid params")
			}
			rpcErr, ok := err.(*Error)
			if !ok {
				t.Fatalf("expected *Error, got %T", err)
			}
			if rpcErr.Code != CodeInvalidParams {
				t.Errorf("error code = %v, want %v", rpcErr.Code, CodeInvalidParams)
			}
		})
	}
}

func TestProtocolHandler_ListRefs(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	// Add some refs
	s.AddLocalRef("local-1", &dummyObject{name: "obj1"})
	s.AddLocalRef("local-2", &dummyObject{name: "obj2"})
	s.AddRemoteRef("remote-1", nil)

	result, err := h.CallMethod(context.Background(), "list_refs", NewParams(nil), nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	refs, ok := result.([]RefInfoResult)
	if !ok {
		t.Fatalf("result type = %T, want []RefInfoResult", result)
	}

	if len(refs) != 3 {
		t.Errorf("len(refs) = %v, want 3", len(refs))
	}

	// Check that we have the expected refs
	refMap := make(map[string]RefInfoResult)
	for _, ref := range refs {
		refMap[ref.Ref] = ref
	}

	if _, ok := refMap["local-1"]; !ok {
		t.Error("local-1 not found in list")
	}
	if _, ok := refMap["local-2"]; !ok {
		t.Error("local-2 not found in list")
	}
	if _, ok := refMap["remote-1"]; !ok {
		t.Error("remote-1 not found in list")
	}

	// Check directions
	if refMap["local-1"].Direction != "local" {
		t.Errorf("local-1 direction = %v, want local", refMap["local-1"].Direction)
	}
	if refMap["remote-1"].Direction != "remote" {
		t.Errorf("remote-1 direction = %v, want remote", refMap["remote-1"].Direction)
	}
}

func TestProtocolHandler_RefInfo(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	// Add a local ref
	s.AddLocalRef("obj-1", &dummyObject{name: "test-string"})

	params, _ := json.Marshal(RefInfoParams{Ref: "obj-1"})
	result, err := h.CallMethod(context.Background(), "ref_info", NewParams(params), nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	info, ok := result.(RefInfoResult)
	if !ok {
		t.Fatalf("result type = %T, want RefInfoResult", result)
	}

	if info.Ref != "obj-1" {
		t.Errorf("Ref = %v, want obj-1", info.Ref)
	}
	if info.Direction != "local" {
		t.Errorf("Direction = %v, want local", info.Direction)
	}
	if info.Type != "*jsonrpc3.dummyObject" {
		t.Errorf("Type = %v, want *jsonrpc3.dummyObject", info.Type)
	}
	if info.Created == "" {
		t.Error("Created should not be empty")
	}
}

func TestProtocolHandler_RefInfoRemote(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	// Add a remote ref with metadata
	metadata := map[string]string{"db": "users"}
	refInfo := &RefInfo{
		Type:     "database",
		Metadata: metadata,
	}
	s.AddRemoteRef("remote-1", refInfo)

	// Wait a bit to ensure LastAccessed is set
	time.Sleep(10 * time.Millisecond)

	params, _ := json.Marshal(RefInfoParams{Ref: "remote-1"})
	result, err := h.CallMethod(context.Background(), "ref_info", NewParams(params), nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	info, ok := result.(RefInfoResult)
	if !ok {
		t.Fatalf("result type = %T, want RefInfoResult", result)
	}

	if info.Ref != "remote-1" {
		t.Errorf("Ref = %v, want remote-1", info.Ref)
	}
	if info.Direction != "remote" {
		t.Errorf("Direction = %v, want remote", info.Direction)
	}
	if info.Type != "database" {
		t.Errorf("Type = %v, want database", info.Type)
	}
	if info.LastAccessed == "" {
		t.Error("LastAccessed should not be empty for remote refs")
	}
}

func TestProtocolHandler_RefInfoNotFound(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	params, _ := json.Marshal(RefInfoParams{Ref: "non-existent"})
	_, err := h.CallMethod(context.Background(), "ref_info", NewParams(params), nil)
	if err == nil {
		t.Fatal("expected error for non-existent ref")
	}
	rpcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if rpcErr.Code != CodeReferenceNotFound {
		t.Errorf("error code = %v, want %v", rpcErr.Code, CodeReferenceNotFound)
	}
}

func TestProtocolHandler_RefInfoInvalidParams(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	tests := []struct {
		name   string
		params RawMessage
	}{
		{
			name:   "empty ref",
			params: RawMessage(`{"ref":""}`),
		},
		{
			name:   "invalid json",
			params: RawMessage(`{invalid}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.CallMethod(context.Background(), "ref_info", NewParams(tt.params), nil)
			if err == nil {
				t.Fatal("expected error for invalid params")
			}
			rpcErr, ok := err.(*Error)
			if !ok {
				t.Fatalf("expected *Error, got %T", err)
			}
			if rpcErr.Code != CodeInvalidParams {
				t.Errorf("error code = %v, want %v", rpcErr.Code, CodeInvalidParams)
			}
		})
	}
}

func TestProtocolHandler_DisposeAll(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	// Add multiple refs
	s.AddLocalRef("local-1", &dummyObject{name: "obj1"})
	s.AddLocalRef("local-2", &dummyObject{name: "obj2"})
	s.AddLocalRef("local-3", &dummyObject{name: "obj3"})
	s.AddRemoteRef("remote-1", nil)
	s.AddRemoteRef("remote-2", nil)

	result, err := h.CallMethod(context.Background(), "dispose_all", NewParams(nil), nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	disposeResult, ok := result.(*DisposeAllResult)
	if !ok {
		t.Fatalf("result type = %T, want *DisposeAllResult", result)
	}

	if disposeResult.LocalCount != 3 {
		t.Errorf("LocalCount = %v, want 3", disposeResult.LocalCount)
	}
	if disposeResult.RemoteCount != 2 {
		t.Errorf("RemoteCount = %v, want 2", disposeResult.RemoteCount)
	}

	// Verify all refs are gone
	if len(s.ListLocalRefs()) != 0 {
		t.Error("ListLocalRefs() should be empty")
	}
	if len(s.ListRemoteRefs()) != 0 {
		t.Error("ListRemoteRefs() should be empty")
	}
}

func TestProtocolHandler_MimeTypes(t *testing.T) {
	s := NewSession()
	mimeTypes := []string{"application/json", "application/cbor", "application/cbor-compact"}
	h := NewProtocolHandler(s, mimeTypes)

	result, err := h.CallMethod(context.Background(), "mimetypes", NewParams(nil), nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	mtResult, ok := result.(*MimeTypesResult)
	if !ok {
		t.Fatalf("result type = %T, want *MimeTypesResult", result)
	}

	if len(mtResult.MimeTypes) != 3 {
		t.Errorf("len(MimeTypes) = %v, want 3", len(mtResult.MimeTypes))
	}

	// Check that all MIME types are present
	found := make(map[string]bool)
	for _, mt := range mtResult.MimeTypes {
		found[mt] = true
	}

	for _, expected := range mimeTypes {
		if !found[expected] {
			t.Errorf("MIME type %s not found in result", expected)
		}
	}
}

func TestProtocolHandler_MimeTypesDefault(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	result, err := h.CallMethod(context.Background(), "mimetypes", NewParams(nil), nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	mtResult, ok := result.(*MimeTypesResult)
	if !ok {
		t.Fatalf("result type = %T, want *MimeTypesResult", result)
	}

	if len(mtResult.MimeTypes) != 1 {
		t.Errorf("len(MimeTypes) = %v, want 1", len(mtResult.MimeTypes))
	}
	if mtResult.MimeTypes[0] != "application/json" {
		t.Errorf("MimeTypes[0] = %v, want application/json", mtResult.MimeTypes[0])
	}
}

func TestProtocolHandler_MethodNotFound(t *testing.T) {
	s := NewSession()
	h := NewProtocolHandler(s, nil)

	_, err := h.CallMethod(context.Background(), "non_existent_method", NewParams(nil), nil)
	if err == nil {
		t.Fatal("expected error for non-existent method")
	}
	rpcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if rpcErr.Code != CodeMethodNotFound {
		t.Errorf("error code = %v, want %v", rpcErr.Code, CodeMethodNotFound)
	}
}

func TestConvertRefInfo(t *testing.T) {
	created := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	accessed := time.Date(2024, 1, 1, 12, 30, 0, 0, time.UTC)

	info := &RefInfo{
		Ref:          "test-ref",
		Type:         "test-type",
		Direction:    "local",
		Created:      created,
		LastAccessed: accessed,
		Metadata:     map[string]string{"key": "value"},
	}

	result := convertRefInfo(info)

	if result.Ref != "test-ref" {
		t.Errorf("Ref = %v, want test-ref", result.Ref)
	}
	if result.Type != "test-type" {
		t.Errorf("Type = %v, want test-type", result.Type)
	}
	if result.Direction != "local" {
		t.Errorf("Direction = %v, want local", result.Direction)
	}
	if result.Created == "" {
		t.Error("Created should not be empty")
	}
	if result.LastAccessed == "" {
		t.Error("LastAccessed should not be empty")
	}
}

func TestConvertRefInfo_ZeroLastAccessed(t *testing.T) {
	created := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	info := &RefInfo{
		Ref:          "test-ref",
		Type:         "test-type",
		Direction:    "local",
		Created:      created,
		LastAccessed: time.Time{}, // Zero value
	}

	result := convertRefInfo(info)

	if result.LastAccessed != "" {
		t.Errorf("LastAccessed should be empty for zero time, got %v", result.LastAccessed)
	}
}

func TestProtocolHandler_Capabilities(t *testing.T) {
	session := NewSession()
	mimeTypes := []string{MimeTypeJSON, MimeTypeCBOR, MimeTypeCBORCompact}
	handler := NewProtocolHandler(session, mimeTypes)

	// Call capabilities method
	result, err := handler.CallMethod(context.Background(), "capabilities", nil, nil)
	require.NoError(t, err)

	capabilities, ok := result.([]string)
	require.True(t, ok, "capabilities should return []string")

	// Check for expected capabilities
	assert.Contains(t, capabilities, "references")
	assert.Contains(t, capabilities, "batch-local-references")
	assert.Contains(t, capabilities, "bidirectional-calls")
	assert.Contains(t, capabilities, "introspection")
	assert.Contains(t, capabilities, "cbor-encoding")
	assert.Contains(t, capabilities, "cbor-compact-encoding")
}
