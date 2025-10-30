package jsonrpc3

import "strings"

// MimeType represents a parsed MIME type with optional format parameter.
type MimeType struct {
	Type   string // e.g., "application/json" or "application/cbor"
	Format string // e.g., "compact" (empty for standard format)
}

// ParseMimeType parses a MIME type string and extracts the format parameter if present.
// Examples:
//   - "application/json" -> MimeType{Type: "application/json", Format: ""}
//   - "application/cbor" -> MimeType{Type: "application/cbor", Format: ""}
//   - "application/cbor; format=compact" -> MimeType{Type: "application/cbor", Format: "compact"}
func ParseMimeType(mimetype string) MimeType {
	// Split on semicolon to separate type from parameters
	parts := strings.SplitN(mimetype, ";", 2)
	mt := MimeType{
		Type: strings.TrimSpace(parts[0]),
	}

	// Parse parameters if present
	if len(parts) == 2 {
		params := strings.TrimSpace(parts[1])
		// Look for format= parameter
		if strings.HasPrefix(params, "format=") {
			mt.Format = strings.TrimSpace(strings.TrimPrefix(params, "format="))
		}
	}

	return mt
}

// IsCBOR returns true if this is a CBOR mime type.
func (mt MimeType) IsCBOR() bool {
	return mt.Type == MimeTypeCBOR
}

// IsJSON returns true if this is a JSON mime type.
func (mt MimeType) IsJSON() bool {
	return mt.Type == MimeTypeJSON
}

// IsCompact returns true if the format is compact.
func (mt MimeType) IsCompact() bool {
	return mt.Format == "compact"
}
