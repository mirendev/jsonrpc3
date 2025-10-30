package jsonrpc3

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Session represents a JSON-RPC 3.0 session that manages references.
// Sessions are transport-agnostic and must be manually managed by both sides.
type Session struct {
	id        string
	createdAt time.Time

	mu sync.RWMutex

	// Local references are objects we control that the other party can call
	localRefs map[string]Object

	// Remote references are objects the other party controls that we received
	remoteRefs map[string]*RefInfo

	// Reference counter for generating sequential IDs
	refCounter atomic.Uint64
}

// RefInfo contains metadata about a reference.
type RefInfo struct {
	Ref          string
	Type         string // Optional type information
	Direction    string // "local" or "remote"
	Created      time.Time
	LastAccessed time.Time
	Metadata     any // Additional implementation-specific data
}

// NewSession creates a new session with a generated UUID.
func NewSession() *Session {
	return &Session{
		id:         uuid.New().String(),
		createdAt:  time.Now(),
		localRefs:  make(map[string]Object),
		remoteRefs: make(map[string]*RefInfo),
	}
}

// NewSessionWithID creates a new session with a specific ID.
func NewSessionWithID(id string) *Session {
	return &Session{
		id:         id,
		createdAt:  time.Now(),
		localRefs:  make(map[string]Object),
		remoteRefs: make(map[string]*RefInfo),
	}
}

// ID returns the session ID.
func (s *Session) ID() string {
	return s.id
}

// CreatedAt returns when the session was created.
func (s *Session) CreatedAt() time.Time {
	return s.createdAt
}

// GenerateRefID generates a new unique reference ID.
func (s *Session) GenerateRefID() string {
	count := s.refCounter.Add(1)
	return fmt.Sprintf("ref-%d", count)
}

// AddLocalRef adds a local reference (object we control).
// Returns the reference ID.
func (s *Session) AddLocalRef(ref string, obj Object) string {
	if ref == "" {
		ref = s.GenerateRefID()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.localRefs[ref] = obj
	return ref
}

// AddLocal adds a local reference with an auto-generated ID.
func (s *Session) AddLocal(obj Object) string {
	return s.AddLocalRef("", obj)
}

// GetLocalRef retrieves a local reference.
// Returns nil if not found.
func (s *Session) GetLocalRef(ref string) Object {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.localRefs[ref]
}

// RemoveLocalRef removes a local reference.
// Calls Dispose() or Close() on the object if it implements those methods.
// Returns true if the reference existed.
func (s *Session) RemoveLocalRef(ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if obj, exists := s.localRefs[ref]; exists {
		// Call lifecycle methods before removing
		disposeObject(obj)
		delete(s.localRefs, ref)
		return true
	}
	return false
}

// RemoveObject removes a local reference by object instance.
func (s *Session) RemoveObject(obj Object) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ref, storedObj := range s.localRefs {
		if storedObj == obj {
			// Call lifecycle methods before removing
			disposeObject(storedObj)
			delete(s.localRefs, ref)
			return true
		}
	}
	return false
}

// HasLocalRef checks if a local reference exists.
func (s *Session) HasLocalRef(ref string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.localRefs[ref]
	return exists
}

// ListLocalRefs returns a list of all local reference IDs.
func (s *Session) ListLocalRefs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	refs := make([]string, 0, len(s.localRefs))
	for ref := range s.localRefs {
		refs = append(refs, ref)
	}
	return refs
}

// AddRemoteRef adds a remote reference (object the other party controls).
func (s *Session) AddRemoteRef(ref string, info *RefInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if info == nil {
		info = &RefInfo{
			Ref:       ref,
			Direction: "remote",
			Created:   time.Now(),
		}
	}
	info.Ref = ref
	info.Direction = "remote"
	if info.Created.IsZero() {
		info.Created = time.Now()
	}
	info.LastAccessed = time.Now()
	s.remoteRefs[ref] = info
}

// GetRemoteRefInfo retrieves info about a remote reference.
// Returns nil if not found.
func (s *Session) GetRemoteRefInfo(ref string) *RefInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if info, exists := s.remoteRefs[ref]; exists {
		// Update last accessed time
		info.LastAccessed = time.Now()
		return info
	}
	return nil
}

// RemoveRemoteRef removes a remote reference.
// Returns true if the reference existed.
func (s *Session) RemoveRemoteRef(ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.remoteRefs[ref]; exists {
		delete(s.remoteRefs, ref)
		return true
	}
	return false
}

// HasRemoteRef checks if a remote reference exists.
func (s *Session) HasRemoteRef(ref string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.remoteRefs[ref]
	return exists
}

// ListRemoteRefs returns a list of all remote reference infos.
func (s *Session) ListRemoteRefs() []*RefInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	refs := make([]*RefInfo, 0, len(s.remoteRefs))
	for _, info := range s.remoteRefs {
		refs = append(refs, info)
	}
	return refs
}

// DisposeAll removes all references (both local and remote).
// Calls Dispose() or Close() on all local objects that implement those methods.
// Returns counts of disposed references.
func (s *Session) DisposeAll() (localCount, remoteCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	localCount = len(s.localRefs)
	remoteCount = len(s.remoteRefs)

	// Call lifecycle methods on all local objects
	for _, obj := range s.localRefs {
		disposeObject(obj)
	}

	s.localRefs = make(map[string]Object)
	s.remoteRefs = make(map[string]*RefInfo)

	return localCount, remoteCount
}

// GetLocalRefInfo returns info about a local reference.
// This is used for protocol methods like ref_info and list_refs.
func (s *Session) GetLocalRefInfo(ref string) *RefInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if obj, exists := s.localRefs[ref]; exists {
		// Try to determine type
		typeName := fmt.Sprintf("%T", obj)

		return &RefInfo{
			Ref:          ref,
			Type:         typeName,
			Direction:    "local",
			Created:      s.createdAt, // Approximate - we don't track per-ref creation
			LastAccessed: time.Now(),
		}
	}
	return nil
}

// Disposable is an interface for objects that need cleanup when disposed.
type Disposable interface {
	Dispose() error
}

// Closeable is an interface for objects that need cleanup when closed.
type Closeable interface {
	Close() error
}

// disposeObject calls lifecycle methods on an object if it implements them.
// Tries Dispose() first, then Close() if Dispose() is not available.
func disposeObject(obj any) {
	// Try Dispose() first
	if disposable, ok := obj.(Disposable); ok {
		_ = disposable.Dispose() // Ignore errors during disposal
		return
	}

	// Try Close() as fallback
	if closeable, ok := obj.(Closeable); ok {
		_ = closeable.Close() // Ignore errors during disposal
	}
}

// generateConnID generates a random 2-byte connection ID prefix encoded as hex.
// This is used to create unique reference IDs for each connection.
// Returns a 4-character hex string (e.g., "a3f2").
func generateConnID() string {
	var connIDBytes [2]byte
	if _, err := rand.Read(connIDBytes[:]); err != nil {
		// Fallback to zero if random generation fails
		connIDBytes = [2]byte{0, 0}
	}
	return hex.EncodeToString(connIDBytes[:])
}
