package main

import (
	"crypto/tls"
	"fmt"
	"os"

	"miren.dev/jsonrpc3/go/jsonrpc3"
	"miren.dev/jsonrpc3/go/jsonrpc3/pkg/shell"
	"miren.dev/mflags"
)

func main() {
	fs := mflags.NewFlagSet("rpcshell")
	sessionFile := fs.String("session", 's', "", "Session file to execute commands from")
	verbose := fs.Bool("verbose", 'v', false, "Verbose output (shows commands when running session file)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	args := fs.Args()

	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: rpcshell <url> [--session <file>] [--verbose]\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  rpcshell https://example.com                  # Interactive shell\n")
		fmt.Fprintf(os.Stderr, "  rpcshell https://example.com --session test.session\n")
		os.Exit(1)
	}

	url := args[0]

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

	// Create shell
	sh := shell.New(client)
	sh.SetVerbose(*verbose)

	// Run in session file mode or interactive mode
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
}
