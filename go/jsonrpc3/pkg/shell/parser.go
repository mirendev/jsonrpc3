package shell

import (
	"fmt"
	"strconv"
	"strings"
)

// tokenize splits a command line into tokens, respecting quoted strings.
func tokenize(line string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, ch := range line {
		switch {
		case (ch == '"' || ch == '\'') && !inQuote:
			// Start quoted string
			inQuote = true
			quoteChar = ch
		case ch == quoteChar && inQuote:
			// End quoted string
			inQuote = false
			quoteChar = 0
		case ch == ' ' && !inQuote:
			// End of token
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			// Add character to current token
			current.WriteRune(ch)
		}
	}

	// Add final token
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// parsePositionalArgs parses positional arguments into params.
// If any arg contains ":", it's treated as key:value pairs for a map.
// Otherwise, it's treated as an array.
func parsePositionalArgs(args []string) any {
	if len(args) == 0 {
		return nil
	}

	// Check if any arg has "key:" prefix (map mode)
	hasKeyValue := false
	for _, arg := range args {
		if strings.Contains(arg, ":") {
			hasKeyValue = true
			break
		}
	}

	if hasKeyValue {
		// Map mode: parse as key:value pairs
		params := make(map[string]any)
		for _, arg := range args {
			parts := strings.SplitN(arg, ":", 2)
			if len(parts) == 2 {
				key := parts[0]
				value := coerceValue(parts[1])
				params[key] = value
			}
		}
		return params
	}

	// Array mode: parse as array of values
	params := make([]any, 0, len(args))
	for _, arg := range args {
		params = append(params, coerceValue(arg))
	}
	return params
}

// coerceValue attempts to convert a string to the most appropriate type.
// Tries: int -> float -> bool -> string
func coerceValue(s string) any {
	// Try int
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	// Try float
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	// Try bool
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}

	// Default to string
	return s
}

// toJSONCompatible converts CBOR map[interface{}]interface{} to JSON-compatible map[string]interface{}.
func toJSONCompatible(v any) any {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		// Convert to map[string]interface{}
		result := make(map[string]interface{})
		for k, v := range val {
			keyStr := fmt.Sprint(k)
			result[keyStr] = toJSONCompatible(v)
		}
		return result
	case map[string]interface{}:
		// Already JSON-compatible, but recursively convert values
		result := make(map[string]interface{})
		for k, v := range val {
			result[k] = toJSONCompatible(v)
		}
		return result
	case []interface{}:
		// Recursively convert array elements
		result := make([]interface{}, len(val))
		for i, v := range val {
			result[i] = toJSONCompatible(v)
		}
		return result
	default:
		// Primitive types are already JSON-compatible
		return val
	}
}
