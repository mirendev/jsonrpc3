package jsonrpc3

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// Enhanced type constants
const (
	TypeDateTime = "datetime"
	TypeBytes    = "bytes"
	TypeBigInt   = "bigint"
	TypeRegExp   = "regexp"
)

// EnhancedType represents the base structure for all enhanced types
type EnhancedType struct {
	Type string `json:"$type" cbor:"$type"`
}

// DateTime represents a JSON-RPC 3.0 datetime enhanced type
type DateTime struct {
	Type  string    `json:"$type" cbor:"$type"`
	Value time.Time `json:"value" cbor:"value"`
}

// NewDateTime creates a new DateTime enhanced type
func NewDateTime(t time.Time) *DateTime {
	return &DateTime{
		Type:  TypeDateTime,
		Value: t,
	}
}

// MarshalJSON implements json.Marshaler for DateTime
func (dt *DateTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"$type": TypeDateTime,
		"value": dt.Value.Format(time.RFC3339Nano),
	})
}

// UnmarshalJSON implements json.Unmarshaler for DateTime
func (dt *DateTime) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type  string `json:"$type"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type != TypeDateTime {
		return fmt.Errorf("expected $type=%s, got %s", TypeDateTime, raw.Type)
	}
	t, err := time.Parse(time.RFC3339Nano, raw.Value)
	if err != nil {
		// Try RFC3339 without nanoseconds
		t, err = time.Parse(time.RFC3339, raw.Value)
		if err != nil {
			return fmt.Errorf("invalid datetime value: %w", err)
		}
	}
	dt.Type = TypeDateTime
	dt.Value = t
	return nil
}

// Bytes represents a JSON-RPC 3.0 bytes enhanced type
type Bytes struct {
	Type  string `json:"$type" cbor:"$type"`
	Value []byte `json:"value" cbor:"value"`
}

// NewBytes creates a new Bytes enhanced type
func NewBytes(data []byte) *Bytes {
	return &Bytes{
		Type:  TypeBytes,
		Value: data,
	}
}

// MarshalJSON implements json.Marshaler for Bytes
func (b *Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"$type": TypeBytes,
		"value": base64.StdEncoding.EncodeToString(b.Value),
	})
}

// UnmarshalJSON implements json.Unmarshaler for Bytes
func (b *Bytes) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type  string `json:"$type"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type != TypeBytes {
		return fmt.Errorf("expected $type=%s, got %s", TypeBytes, raw.Type)
	}
	decoded, err := base64.StdEncoding.DecodeString(raw.Value)
	if err != nil {
		return fmt.Errorf("invalid base64 value: %w", err)
	}
	b.Type = TypeBytes
	b.Value = decoded
	return nil
}

// MarshalCBOR implements cbor.Marshaler for Bytes
// In CBOR, we use native byte strings instead of base64
func (b *Bytes) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(map[string]any{
		"$type": TypeBytes,
		"value": b.Value, // Native byte string in CBOR
	})
}

// UnmarshalCBOR implements cbor.Unmarshaler for Bytes
func (b *Bytes) UnmarshalCBOR(data []byte) error {
	var raw struct {
		Type  string `cbor:"$type"`
		Value []byte `cbor:"value"`
	}
	if err := cbor.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type != TypeBytes {
		return fmt.Errorf("expected $type=%s, got %s", TypeBytes, raw.Type)
	}
	b.Type = TypeBytes
	b.Value = raw.Value
	return nil
}

// BigInt represents a JSON-RPC 3.0 bigint enhanced type
type BigInt struct {
	Type  string   `json:"$type" cbor:"$type"`
	Value *big.Int `json:"value" cbor:"value"`
}

// NewBigInt creates a new BigInt enhanced type
func NewBigInt(n *big.Int) *BigInt {
	return &BigInt{
		Type:  TypeBigInt,
		Value: n,
	}
}

// NewBigIntFromString creates a new BigInt from a decimal string
func NewBigIntFromString(s string) (*BigInt, error) {
	n := new(big.Int)
	if _, ok := n.SetString(s, 10); !ok {
		return nil, fmt.Errorf("invalid bigint string: %s", s)
	}
	return &BigInt{
		Type:  TypeBigInt,
		Value: n,
	}, nil
}

// MarshalJSON implements json.Marshaler for BigInt
func (bi *BigInt) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"$type": TypeBigInt,
		"value": bi.Value.String(),
	})
}

// UnmarshalJSON implements json.Unmarshaler for BigInt
func (bi *BigInt) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type  string `json:"$type"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type != TypeBigInt {
		return fmt.Errorf("expected $type=%s, got %s", TypeBigInt, raw.Type)
	}
	n := new(big.Int)
	if _, ok := n.SetString(raw.Value, 10); !ok {
		return fmt.Errorf("invalid bigint value: %s", raw.Value)
	}
	bi.Type = TypeBigInt
	bi.Value = n
	return nil
}

// RegExp represents a JSON-RPC 3.0 regexp enhanced type
type RegExp struct {
	Type    string         `json:"$type" cbor:"$type"`
	Pattern string         `json:"pattern" cbor:"pattern"`
	Flags   string         `json:"flags,omitempty" cbor:"flags,omitempty"`
	regexp  *regexp.Regexp // compiled regexp
}

// NewRegExp creates a new RegExp enhanced type from a compiled regexp
func NewRegExp(re *regexp.Regexp) *RegExp {
	return &RegExp{
		Type:    TypeRegExp,
		Pattern: re.String(),
		regexp:  re,
	}
}

// NewRegExpFromPattern creates a new RegExp from a pattern string
func NewRegExpFromPattern(pattern, flags string) (*RegExp, error) {
	// Go regexp doesn't have separate flags like JavaScript
	// We incorporate common flags into the pattern
	modifiedPattern := pattern
	if flags != "" {
		// Handle common flags
		prefix := ""
		for _, flag := range flags {
			switch flag {
			case 'i': // case-insensitive
				prefix += "(?i)"
			case 'm': // multiline
				prefix += "(?m)"
			case 's': // dotall
				prefix += "(?s)"
			}
		}
		modifiedPattern = prefix + pattern
	}

	re, err := regexp.Compile(modifiedPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regexp pattern: %w", err)
	}

	return &RegExp{
		Type:    TypeRegExp,
		Pattern: pattern,
		Flags:   flags,
		regexp:  re,
	}, nil
}

// Regexp returns the compiled regexp
func (re *RegExp) Regexp() *regexp.Regexp {
	if re.regexp == nil {
		// Lazy compile
		compiled, _ := NewRegExpFromPattern(re.Pattern, re.Flags)
		if compiled != nil {
			re.regexp = compiled.regexp
		}
	}
	return re.regexp
}

// MarshalJSON implements json.Marshaler for RegExp
func (re *RegExp) MarshalJSON() ([]byte, error) {
	result := map[string]any{
		"$type":   TypeRegExp,
		"pattern": re.Pattern,
	}
	if re.Flags != "" {
		result["flags"] = re.Flags
	}
	return json.Marshal(result)
}

// UnmarshalJSON implements json.Unmarshaler for RegExp
func (re *RegExp) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type    string `json:"$type"`
		Pattern string `json:"pattern"`
		Flags   string `json:"flags,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type != TypeRegExp {
		return fmt.Errorf("expected $type=%s, got %s", TypeRegExp, raw.Type)
	}

	compiled, err := NewRegExpFromPattern(raw.Pattern, raw.Flags)
	if err != nil {
		return err
	}

	*re = *compiled
	return nil
}

// IsEnhancedType checks if the given data contains an enhanced type marker
func IsEnhancedType(data any) (string, bool) {
	if m, ok := data.(map[string]any); ok {
		if typeVal, exists := m["$type"]; exists {
			if typeStr, ok := typeVal.(string); ok {
				return typeStr, true
			}
		}
	}
	return "", false
}

// DecodeEnhancedType attempts to decode data as an enhanced type
// Returns the decoded enhanced type or the original data if not an enhanced type
func DecodeEnhancedType(data any, format string) (any, error) {
	typeStr, isEnhanced := IsEnhancedType(data)
	if !isEnhanced {
		return data, nil
	}

	// Get the codec for the format
	codec := GetCodec(format)

	// Re-encode and decode as specific type
	encoded, err := codec.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to encode enhanced type: %w", err)
	}

	switch typeStr {
	case TypeDateTime:
		var dt DateTime
		if err := codec.Unmarshal(encoded, &dt); err != nil {
			return nil, fmt.Errorf("failed to decode datetime: %w", err)
		}
		return dt.Value, nil

	case TypeBytes:
		var b Bytes
		if err := codec.Unmarshal(encoded, &b); err != nil {
			return nil, fmt.Errorf("failed to decode bytes: %w", err)
		}
		return b.Value, nil

	case TypeBigInt:
		var bi BigInt
		if err := codec.Unmarshal(encoded, &bi); err != nil {
			return nil, fmt.Errorf("failed to decode bigint: %w", err)
		}
		return bi.Value, nil

	case TypeRegExp:
		var re RegExp
		if err := codec.Unmarshal(encoded, &re); err != nil {
			return nil, fmt.Errorf("failed to decode regexp: %w", err)
		}
		return re.Regexp(), nil

	default:
		// Unknown enhanced type - return as-is
		return data, nil
	}
}
