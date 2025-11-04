package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"miren.dev/jsonrpc3/go/jsonrpc3"
	"miren.dev/jsonrpc3/go/jsonrpc3/pkg/shell"
	"miren.dev/mflags"
)

func main() {
	fs := mflags.NewFlagSet("rpcurl")
	paramsFlag := fs.String("params", 'p', "", "JSON parameters (overrides positional args)")
	shellFlag := fs.Bool("shell", 'i', false, "Interactive shell mode")
	sessionFile := fs.String("session", 's', "", "Session file to execute in shell mode")
	verbose := fs.Bool("verbose", 'v', false, "Verbose output (shows commands when running session file)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	args := fs.Args()

	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: rpcurl <url> [method] [args...] [flags]\n")
		fmt.Fprintf(os.Stderr, "\nModes:\n")
		fmt.Fprintf(os.Stderr, "  rpcurl <url> [method] [args...]         # One-shot command\n")
		fmt.Fprintf(os.Stderr, "  rpcurl <url> --shell                    # Interactive shell\n")
		fmt.Fprintf(os.Stderr, "  rpcurl <url> --session <file>           # Run session file\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  rpcurl https://example.com              # List methods\n")
		fmt.Fprintf(os.Stderr, "  rpcurl https://example.com add a:1 b:2  # Call with map params\n")
		fmt.Fprintf(os.Stderr, "  rpcurl https://example.com sum 1 2 3    # Call with array params\n")
		fmt.Fprintf(os.Stderr, "  rpcurl https://example.com echo --params '{\"text\":\"hello\"}'\n")
		fmt.Fprintf(os.Stderr, "  rpcurl https://example.com --shell      # Start interactive shell\n")
		os.Exit(1)
	}

	url := args[0]
	var method string
	var positionalArgs []string

	if len(args) > 1 {
		method = args[1]
		if len(args) > 2 {
			positionalArgs = args[2:]
		}
	}

	// Probe the server
	endpoint, err := jsonrpc3.Probe(url, jsonrpc3.WithTLSConfig(&tls.Config{
		InsecureSkipVerify: true, // For development/testing
	}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error probing server: %v\n", err)
		os.Exit(1)
	}

	// Connect to the server
	client, err := endpoint.Connect(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to server: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// Handle shell mode
	if *shellFlag || *sessionFile != "" {
		sh := shell.New(client)
		sh.SetVerbose(*verbose)

		if *sessionFile != "" {
			if err := sh.RunFile(*sessionFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error running session file: %v\n", err)
				os.Exit(1)
			}
		} else {
			if err := sh.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}
		return
	}

	// If no method provided, call $methods
	if method == "" {
		if err := listMethods(client); err != nil {
			fmt.Fprintf(os.Stderr, "Error listing methods: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Parse parameters
	var params any
	if *paramsFlag != "" {
		// Use literal JSON from --params flag
		if err := json.Unmarshal([]byte(*paramsFlag), &params); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing --params JSON: %v\n", err)
			os.Exit(1)
		}
	} else if len(positionalArgs) > 0 {
		// Smart positional parameter parsing
		params = parsePositionalArgs(positionalArgs)
	}

	// Call the method
	result, err := client.Call(method, params)
	if err != nil {
		if rpcErr, ok := err.(*jsonrpc3.Error); ok {
			fmt.Fprintf(os.Stderr, "RPC Error %d: %s\n", rpcErr.Code, rpcErr.Message)
			if rpcErr.Data != nil {
				data, _ := json.MarshalIndent(rpcErr.Data, "", "  ")
				fmt.Fprintf(os.Stderr, "Data: %s\n", string(data))
			}
		} else {
			fmt.Fprintf(os.Stderr, "Error calling method: %v\n", err)
		}
		os.Exit(1)
	}

	// Decode and print result
	var resultData any
	if err := result.Decode(&resultData); err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding result: %v\n", err)
		os.Exit(1)
	}

	// Convert to JSON-compatible format (handles CBOR map[interface{}]interface{})
	jsonData := toJSONCompatible(resultData)

	output, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting output: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(output))
}

// listMethods calls $methods introspection and pretty-prints the results
func listMethods(client jsonrpc3.Caller) error {
	result, err := client.Call("$methods", nil)
	if err != nil {
		return err
	}

	var methods []map[string]any
	if err := result.Decode(&methods); err != nil {
		return err
	}

	fmt.Println("Available methods:")
	fmt.Println()

	for _, method := range methods {
		name, _ := method["name"].(string)
		description, _ := method["description"].(string)

		fmt.Printf("  %s\n", name)

		if description != "" {
			fmt.Printf("    %s\n", description)
		}

		if params, ok := method["params"]; ok && params != nil {
			paramsJSON, _ := json.MarshalIndent(params, "    ", "  ")
			fmt.Printf("    Params: %s\n", string(paramsJSON))
		}

		if category, ok := method["category"].(string); ok && category != "" {
			fmt.Printf("    Category: %s\n", category)
		}

		fmt.Println()
	}

	return nil
}

// parsePositionalArgs parses positional arguments into params
// If any arg contains ":", it's treated as key:value pairs for a map
// Otherwise, it's treated as an array
func parsePositionalArgs(args []string) any {
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

// coerceValue attempts to convert a string to the most appropriate type
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

// toJSONCompatible converts CBOR map[interface{}]interface{} to JSON-compatible map[string]interface{}
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
