package shell

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chzyer/readline"
	"miren.dev/jsonrpc3/go/jsonrpc3"
)

// Shell provides an interactive shell for JSON-RPC 3.0 servers.
type Shell struct {
	client    jsonrpc3.Caller
	variables map[string]jsonrpc3.Reference
	verbose   bool
}

// New creates a new Shell with the given client connection.
func New(client jsonrpc3.Caller) *Shell {
	return &Shell{
		client:    client,
		variables: make(map[string]jsonrpc3.Reference),
		verbose:   false,
	}
}

// SetVerbose enables or disables verbose output.
func (s *Shell) SetVerbose(verbose bool) {
	s.verbose = verbose
}

// Run starts the interactive REPL.
func (s *Shell) Run() error {
	// Setup history file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	historyFile := filepath.Join(homeDir, ".rpcshell_history")

	// Create readline instance with tab completion
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "> ",
		HistoryFile:     historyFile,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		AutoComplete:    s.createCompleter(),
	})
	if err != nil {
		return err
	}
	defer rl.Close()

	fmt.Println("JSON-RPC 3.0 Shell")
	fmt.Println("Type 'help' for available commands, 'exit' to quit")
	fmt.Println()

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				if len(line) == 0 {
					return nil
				}
				continue
			} else if err == io.EOF {
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if err := s.ExecuteLine(line); err != nil {
			if err == io.EOF {
				return nil
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
	}
}

// RunFile executes commands from a file, one per line.
func (s *Shell) RunFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if s.verbose {
			fmt.Printf("[%d] > %s\n", lineNum, line)
		}

		if err := s.ExecuteLine(line); err != nil {
			return fmt.Errorf("line %d: %w", lineNum, err)
		}
	}

	return scanner.Err()
}

// ExecuteLine executes a single command line.
func (s *Shell) ExecuteLine(line string) error {
	// Handle special commands
	switch {
	case line == "exit" || line == "quit":
		return io.EOF
	case line == "help":
		s.printHelp()
		return nil
	case line == "methods" || line == "list":
		return s.listMethods()
	case line == "vars":
		s.listVariables()
		return nil
	case strings.HasPrefix(line, "let "):
		return s.executeAssignment(line)
	default:
		return s.executeCall(line)
	}
}

// executeAssignment handles "let var = method args..." syntax.
func (s *Shell) executeAssignment(line string) error {
	// Parse: let var = method args...
	parts := strings.SplitN(line[4:], "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid let syntax: expected 'let var = method args...'")
	}

	varName := strings.TrimSpace(parts[0])
	callPart := strings.TrimSpace(parts[1])

	if varName == "" {
		return fmt.Errorf("variable name cannot be empty")
	}

	// Execute the call
	result, err := s.executeCallPart(callPart)
	if err != nil {
		return err
	}

	// Check if result is a reference
	ref, err := result.AsReference()
	if err != nil {
		// Not a reference, just print the value
		var data any
		result.Decode(&data)
		jsonData := toJSONCompatible(data)
		output, _ := json.MarshalIndent(jsonData, "", "  ")
		fmt.Printf("%s = %s\n", varName, string(output))
		return fmt.Errorf("result is not a reference, cannot store in variable")
	}

	// Store the reference
	s.variables[varName] = ref
	fmt.Printf("%s = %s (stored)\n", varName, ref.Ref)

	return nil
}

// executeCall handles method calls.
func (s *Shell) executeCall(line string) error {
	result, err := s.executeCallPart(line)
	if err != nil {
		return err
	}

	// Decode and print result
	var resultData any
	if err := result.Decode(&resultData); err != nil {
		return fmt.Errorf("failed to decode result: %w", err)
	}

	jsonData := toJSONCompatible(resultData)
	output, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to format output: %w", err)
	}

	fmt.Println(string(output))
	return nil
}

// executeCallPart executes a method call and returns the raw result.
func (s *Shell) executeCallPart(line string) (jsonrpc3.Value, error) {
	// Parse the command
	tokens := tokenize(line)
	if len(tokens) == 0 {
		return jsonrpc3.Value{}, fmt.Errorf("empty command")
	}

	// Check if calling method on a reference: $var.method or var.method
	var caller jsonrpc3.Caller = s.client
	methodStart := 0

	if strings.Contains(tokens[0], ".") {
		parts := strings.SplitN(tokens[0], ".", 2)
		varName := strings.TrimPrefix(parts[0], "$")
		method := parts[1]

		// Get the reference from variables
		ref, ok := s.variables[varName]
		if !ok {
			return jsonrpc3.Value{}, fmt.Errorf("undefined variable: %s", varName)
		}

		// Call method on the reference
		params := parsePositionalArgs(tokens[1:])
		return s.client.Call(method, params, jsonrpc3.ToRef(ref))
	}

	// Regular method call on root object
	method := tokens[methodStart]
	args := tokens[methodStart+1:]

	params := parsePositionalArgs(args)
	return caller.Call(method, params)
}

// listMethods calls $methods and prints available methods.
func (s *Shell) listMethods() error {
	result, err := s.client.Call("$methods", nil)
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

		fmt.Println()
	}

	return nil
}

// listVariables prints all stored variables.
func (s *Shell) listVariables() {
	if len(s.variables) == 0 {
		fmt.Println("No variables stored")
		return
	}

	fmt.Println("Stored variables:")
	for name, ref := range s.variables {
		fmt.Printf("  %s = %s\n", name, ref.Ref)
	}
}

// printHelp prints help information.
func (s *Shell) printHelp() {
	fmt.Println("Available commands:")
	fmt.Println()
	fmt.Println("  <method> [args...]           Call method on root object")
	fmt.Println("  let <var> = <method> [args]  Call method and store reference in variable")
	fmt.Println("  $<var>.<method> [args...]    Call method on stored reference")
	fmt.Println("  methods, list                List available methods")
	fmt.Println("  vars                         List stored variables")
	fmt.Println("  help                         Show this help")
	fmt.Println("  exit, quit                   Exit the shell")
	fmt.Println()
	fmt.Println("Parameter formats:")
	fmt.Println("  Array:  method 1 2 3         → [1, 2, 3]")
	fmt.Println("  Map:    method a:1 b:2       → {\"a\": 1, \"b\": 2}")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  add 1 2 3")
	fmt.Println("  echo message:hello count:42")
	fmt.Println("  let counter = createCounter")
	fmt.Println("  $counter.increment")
	fmt.Println("  $counter.getValue")
}

// createCompleter creates a tab completion handler.
func (s *Shell) createCompleter() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("help"),
		readline.PcItem("exit"),
		readline.PcItem("quit"),
		readline.PcItem("methods"),
		readline.PcItem("list"),
		readline.PcItem("vars"),
		readline.PcItem("let"),
	)
}

// getMethodNames fetches available method names from the server.
func (s *Shell) getMethodNames() []string {
	result, err := s.client.Call("$methods", nil)
	if err != nil {
		return nil
	}

	var methods []map[string]any
	if err := result.Decode(&methods); err != nil {
		return nil
	}

	names := make([]string, 0, len(methods))
	for _, method := range methods {
		if name, ok := method["name"].(string); ok {
			names = append(names, name)
		}
	}

	return names
}
