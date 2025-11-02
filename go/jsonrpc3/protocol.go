package jsonrpc3

import (
	"context"
	"fmt"
)

// ProtocolHandler handles protocol methods invoked on the magic "$rpc" reference.
// It implements the Object interface.
type ProtocolHandler struct {
	session   *Session
	mimeTypes []string // Supported MIME types for this implementation
}

// NewProtocolHandler creates a new protocol handler for a session.
func NewProtocolHandler(session *Session, mimeTypes []string) *ProtocolHandler {
	if mimeTypes == nil {
		mimeTypes = []string{MimeTypeJSON}
	}
	return &ProtocolHandler{
		session:   session,
		mimeTypes: mimeTypes,
	}
}

// DisposeParams represents parameters for the dispose method.
type DisposeParams struct {
	Ref string `json:"ref"`
}

// SessionIDResult represents the result of session_id method.
type SessionIDResult struct {
	SessionID string `json:"session_id"`
}

// RefInfoParams represents parameters for the ref_info method.
type RefInfoParams struct {
	Ref string `json:"ref"`
}

// RefInfoResult represents the result of ref_info and list_refs methods.
type RefInfoResult struct {
	Ref          string `json:"ref"`
	Type         string `json:"type,omitempty"`
	Direction    string `json:"direction"` // "local" or "remote"
	Created      string `json:"created"`   // ISO 8601 timestamp
	LastAccessed string `json:"last_accessed,omitempty"`
	Metadata     any    `json:"metadata,omitempty"`
}

// DisposeAllResult represents the result of dispose_all method.
type DisposeAllResult struct {
	LocalCount  int `json:"local_count"`
	RemoteCount int `json:"remote_count"`
}

// MimeTypesResult represents the result of mimetypes method.
type MimeTypesResult struct {
	MimeTypes []string `json:"mimetypes"`
}

// CallMethod implements the Object interface for protocol methods.
func (h *ProtocolHandler) CallMethod(ctx context.Context, method string, params Params, caller Caller) (any, error) {
	switch method {
	case "dispose":
		return h.handleDispose(params)
	case "session_id":
		return h.handleSessionID(params)
	case "list_refs":
		return h.handleListRefs(params)
	case "ref_info":
		return h.handleRefInfo(params)
	case "dispose_all":
		return h.handleDisposeAll(params)
	case "mimetypes":
		return h.handleMimeTypes(params)
	case "capabilities":
		return h.handleCapabilities(params)
	default:
		return nil, NewMethodNotFoundError(method)
	}
}

// handleDispose implements the dispose(ref) method.
func (h *ProtocolHandler) handleDispose(params Params) (any, error) {
	var p DisposeParams
	if err := params.Decode(&p); err != nil {
		return nil, NewInvalidParamsError(fmt.Sprintf("invalid params: %v", err))
	}

	if p.Ref == "" {
		return nil, NewInvalidParamsError("ref parameter is required")
	}

	// Try to remove as local ref first, then remote
	removed := h.session.RemoveLocalRef(p.Ref)
	if !removed {
		removed = h.session.RemoveRemoteRef(p.Ref)
	}

	if !removed {
		return nil, NewReferenceNotFoundError(p.Ref)
	}

	return true, nil
}

// handleSessionID implements the session_id() method.
func (h *ProtocolHandler) handleSessionID(params Params) (any, error) {
	return &SessionIDResult{
		SessionID: h.session.ID(),
	}, nil
}

// handleListRefs implements the list_refs() method.
func (h *ProtocolHandler) handleListRefs(params Params) (any, error) {
	var results []RefInfoResult

	// Get local refs
	for _, ref := range h.session.ListLocalRefs() {
		info := h.session.GetLocalRefInfo(ref)
		if info != nil {
			results = append(results, convertRefInfo(info))
		}
	}

	// Get remote refs
	for _, info := range h.session.ListRemoteRefs() {
		results = append(results, convertRefInfo(info))
	}

	return results, nil
}

// handleRefInfo implements the ref_info(ref) method.
func (h *ProtocolHandler) handleRefInfo(params Params) (any, error) {
	var p RefInfoParams
	if err := params.Decode(&p); err != nil {
		return nil, NewInvalidParamsError(fmt.Sprintf("invalid params: %v", err))
	}

	if p.Ref == "" {
		return nil, NewInvalidParamsError("ref parameter is required")
	}

	// Try to get local ref info first, then remote
	info := h.session.GetLocalRefInfo(p.Ref)
	if info == nil {
		info = h.session.GetRemoteRefInfo(p.Ref)
	}

	if info == nil {
		return nil, NewReferenceNotFoundError(p.Ref)
	}

	return convertRefInfo(info), nil
}

// handleDisposeAll implements the dispose_all() method.
func (h *ProtocolHandler) handleDisposeAll(params Params) (any, error) {
	localCount, remoteCount := h.session.DisposeAll()
	return &DisposeAllResult{
		LocalCount:  localCount,
		RemoteCount: remoteCount,
	}, nil
}

// handleMimeTypes implements the mimetypes() method.
func (h *ProtocolHandler) handleMimeTypes(params Params) (any, error) {
	return &MimeTypesResult{
		MimeTypes: h.mimeTypes,
	}, nil
}

// handleCapabilities implements the capabilities() method.
func (h *ProtocolHandler) handleCapabilities(params Params) (any, error) {
	capabilities := []string{
		"references",
		"batch-local-references",
		"bidirectional-calls",
		"introspection",
	}

	// Add encoding capabilities based on supported MIME types
	for _, mimeType := range h.mimeTypes {
		switch mimeType {
		case MimeTypeCBOR:
			capabilities = append(capabilities, "cbor-encoding")
		case MimeTypeCBORCompact:
			capabilities = append(capabilities, "cbor-compact-encoding")
		}
	}

	return capabilities, nil
}

// convertRefInfo converts internal RefInfo to protocol RefInfoResult.
func convertRefInfo(info *RefInfo) RefInfoResult {
	result := RefInfoResult{
		Ref:       info.Ref,
		Type:      info.Type,
		Direction: info.Direction,
		Created:   info.Created.Format("2006-01-02T15:04:05.999Z07:00"),
		Metadata:  info.Metadata,
	}

	if !info.LastAccessed.IsZero() {
		result.LastAccessed = info.LastAccessed.Format("2006-01-02T15:04:05.999Z07:00")
	}

	return result
}
