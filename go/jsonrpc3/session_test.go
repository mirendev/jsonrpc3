package jsonrpc3

import (
	"context"
	"sync"
	"testing"
	"time"
)

// simpleObject is a simple Object implementation for session testing.
type simpleObject struct {
	value any
}

func (s *simpleObject) CallMethod(ctx context.Context, method string, params Params, caller Caller) (any, error) {
	return nil, NewMethodNotFoundError(method)
}

func TestNewSession(t *testing.T) {
	s := NewSession()
	if s == nil {
		t.Fatal("NewSession() returned nil")
	}
	if s.ID() == "" {
		t.Error("Session ID should not be empty")
	}
	if s.CreatedAt().IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestNewSessionWithID(t *testing.T) {
	id := "test-session-123"
	s := NewSessionWithID(id)
	if s.ID() != id {
		t.Errorf("ID() = %v, want %v", s.ID(), id)
	}
}

func TestSession_GenerateRefID(t *testing.T) {
	s := NewSession()

	// Generate multiple IDs and ensure they're unique
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := s.GenerateRefID()
		if ids[id] {
			t.Errorf("Duplicate ref ID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestSession_LocalRefs(t *testing.T) {
	s := NewSession()

	// Test adding local ref
	obj := &simpleObject{value: "test-object"}
	s.AddLocalRef("obj-1", obj)

	// Test getting local ref
	got := s.GetLocalRef("obj-1")
	if got != obj {
		t.Errorf("GetLocalRef() = %v, want %v", got, obj)
	}

	// Test has local ref
	if !s.HasLocalRef("obj-1") {
		t.Error("HasLocalRef() should return true")
	}

	// Test list local refs
	refs := s.ListLocalRefs()
	if len(refs) != 1 {
		t.Errorf("ListLocalRefs() length = %v, want 1", len(refs))
	}
	if refs[0] != "obj-1" {
		t.Errorf("ListLocalRefs()[0] = %v, want obj-1", refs[0])
	}

	// Test removing local ref
	if !s.RemoveLocalRef("obj-1") {
		t.Error("RemoveLocalRef() should return true")
	}
	if s.HasLocalRef("obj-1") {
		t.Error("HasLocalRef() should return false after removal")
	}

	// Test removing non-existent ref
	if s.RemoveLocalRef("obj-1") {
		t.Error("RemoveLocalRef() should return false for non-existent ref")
	}
}

func TestSession_RemoteRefs(t *testing.T) {
	s := NewSession()

	// Test adding remote ref with nil info
	s.AddRemoteRef("remote-1", nil)

	// Test getting remote ref info
	info := s.GetRemoteRefInfo("remote-1")
	if info == nil {
		t.Fatal("GetRemoteRefInfo() returned nil")
	}
	if info.Ref != "remote-1" {
		t.Errorf("Ref = %v, want remote-1", info.Ref)
	}
	if info.Direction != "remote" {
		t.Errorf("Direction = %v, want remote", info.Direction)
	}

	// Test has remote ref
	if !s.HasRemoteRef("remote-1") {
		t.Error("HasRemoteRef() should return true")
	}

	// Test list remote refs
	refs := s.ListRemoteRefs()
	if len(refs) != 1 {
		t.Errorf("ListRemoteRefs() length = %v, want 1", len(refs))
	}

	// Test removing remote ref
	if !s.RemoveRemoteRef("remote-1") {
		t.Error("RemoveRemoteRef() should return true")
	}
	if s.HasRemoteRef("remote-1") {
		t.Error("HasRemoteRef() should return false after removal")
	}
}

func TestSession_AddRemoteRefWithInfo(t *testing.T) {
	s := NewSession()

	created := time.Now().Add(-1 * time.Hour)
	info := &RefInfo{
		Type:     "database",
		Created:  created,
		Metadata: map[string]string{"db": "users"},
	}

	s.AddRemoteRef("remote-1", info)

	got := s.GetRemoteRefInfo("remote-1")
	if got == nil {
		t.Fatal("GetRemoteRefInfo() returned nil")
	}
	if got.Type != "database" {
		t.Errorf("Type = %v, want database", got.Type)
	}
	if got.Direction != "remote" {
		t.Errorf("Direction = %v, want remote", got.Direction)
	}
	// LastAccessed should be updated
	if got.LastAccessed.IsZero() {
		t.Error("LastAccessed should not be zero")
	}
}

func TestSession_GetLocalRefInfo(t *testing.T) {
	s := NewSession()

	obj := &simpleObject{value: "test-string"}
	s.AddLocalRef("obj-1", obj)

	info := s.GetLocalRefInfo("obj-1")
	if info == nil {
		t.Fatal("GetLocalRefInfo() returned nil")
	}
	if info.Ref != "obj-1" {
		t.Errorf("Ref = %v, want obj-1", info.Ref)
	}
	if info.Direction != "local" {
		t.Errorf("Direction = %v, want local", info.Direction)
	}
	if info.Type != "*jsonrpc3.simpleObject" {
		t.Errorf("Type = %v, want *jsonrpc3.simpleObject", info.Type)
	}

	// Test non-existent ref
	info = s.GetLocalRefInfo("non-existent")
	if info != nil {
		t.Error("GetLocalRefInfo() should return nil for non-existent ref")
	}
}

func TestSession_DisposeAll(t *testing.T) {
	s := NewSession()

	// Add some local refs
	s.AddLocalRef("local-1", &simpleObject{value: "obj1"})
	s.AddLocalRef("local-2", &simpleObject{value: "obj2"})
	s.AddLocalRef("local-3", &simpleObject{value: "obj3"})

	// Add some remote refs
	s.AddRemoteRef("remote-1", nil)
	s.AddRemoteRef("remote-2", nil)

	// Dispose all
	localCount, remoteCount := s.DisposeAll()

	if localCount != 3 {
		t.Errorf("localCount = %v, want 3", localCount)
	}
	if remoteCount != 2 {
		t.Errorf("remoteCount = %v, want 2", remoteCount)
	}

	// Verify all refs are gone
	if len(s.ListLocalRefs()) != 0 {
		t.Error("ListLocalRefs() should be empty after DisposeAll()")
	}
	if len(s.ListRemoteRefs()) != 0 {
		t.Error("ListRemoteRefs() should be empty after DisposeAll()")
	}
}

func TestSession_ConcurrentAccess(t *testing.T) {
	s := NewSession()

	// Test concurrent access to ensure thread safety
	var wg sync.WaitGroup

	// Add refs concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ref := s.GenerateRefID()
			s.AddLocalRef(ref, &simpleObject{value: i})
		}(i)
	}

	// Read refs concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.ListLocalRefs()
		}()
	}

	wg.Wait()

	// Should have 100 refs
	refs := s.ListLocalRefs()
	if len(refs) != 100 {
		t.Errorf("ListLocalRefs() length = %v, want 100", len(refs))
	}
}

func TestSession_RemoteRefLastAccessed(t *testing.T) {
	s := NewSession()

	s.AddRemoteRef("remote-1", nil)

	// Get first time
	info1 := s.GetRemoteRefInfo("remote-1")
	if info1 == nil {
		t.Fatal("GetRemoteRefInfo() returned nil")
	}
	firstAccessed := info1.LastAccessed

	// Wait a bit to ensure time difference
	time.Sleep(50 * time.Millisecond)

	// Get second time - last accessed should be updated
	info2 := s.GetRemoteRefInfo("remote-1")
	if info2 == nil {
		t.Fatal("GetRemoteRefInfo() returned nil")
	}

	if !info2.LastAccessed.After(firstAccessed) {
		t.Errorf("LastAccessed should be updated on each access: first=%v, second=%v",
			firstAccessed, info2.LastAccessed)
	}
}

func TestSession_NonExistentRefs(t *testing.T) {
	s := NewSession()

	// Test getting non-existent local ref
	if got := s.GetLocalRef("non-existent"); got != nil {
		t.Error("GetLocalRef() should return nil for non-existent ref")
	}

	// Test getting non-existent remote ref
	if got := s.GetRemoteRefInfo("non-existent"); got != nil {
		t.Error("GetRemoteRefInfo() should return nil for non-existent ref")
	}

	// Test has methods with non-existent refs
	if s.HasLocalRef("non-existent") {
		t.Error("HasLocalRef() should return false for non-existent ref")
	}
	if s.HasRemoteRef("non-existent") {
		t.Error("HasRemoteRef() should return false for non-existent ref")
	}
}
