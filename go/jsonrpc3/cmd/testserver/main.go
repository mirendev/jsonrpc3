package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/evanphx/jsonrpc3"
)

func main() {
	httpPort := flag.Int("http", 8080, "HTTP server port")
	wsPort := flag.Int("ws", 8081, "WebSocket server port")
	peerPort := flag.Int("peer", 8082, "Peer TCP server port")
	useStdio := flag.Bool("stdio", false, "Use stdin/stdout for peer (disables HTTP/WS servers)")
	flag.Parse()

	// Create root object with test methods
	root := jsonrpc3.NewMethodMap()

	// Add method with introspection metadata
	root.Register("add", func(params jsonrpc3.Params, caller jsonrpc3.Caller) (any, error) {
		var nums []int
		if err := params.Decode(&nums); err != nil {
			return nil, jsonrpc3.NewInvalidParamsError(err.Error())
		}
		sum := 0
		for _, n := range nums {
			sum += n
		}
		return sum, nil
	},
		jsonrpc3.WithDescription("Adds a list of numbers"),
		jsonrpc3.WithPositionalParams([]string{"number"}))

	// Echo method with introspection metadata
	root.Register("echo", func(params jsonrpc3.Params, caller jsonrpc3.Caller) (any, error) {
		var data any
		if err := params.Decode(&data); err != nil {
			return nil, jsonrpc3.NewInvalidParamsError(err.Error())
		}
		return data, nil
	},
		jsonrpc3.WithDescription("Echoes back the input"))

	// Create counter method with introspection metadata
	root.Register("createCounter", func(params jsonrpc3.Params, caller jsonrpc3.Caller) (any, error) {
		counter := jsonrpc3.NewMethodMap()
		counter.Type = "Counter"
		count := 0
		var mu sync.Mutex

		counter.Register("increment", func(params jsonrpc3.Params, caller jsonrpc3.Caller) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			count++
			return count, nil
		},
			jsonrpc3.WithDescription("Increments the counter by 1"))

		counter.Register("getValue", func(params jsonrpc3.Params, caller jsonrpc3.Caller) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			return count, nil
		},
			jsonrpc3.WithDescription("Gets the current counter value"))

		return counter, nil
	},
		jsonrpc3.WithDescription("Creates a new counter object"))

	// If stdio mode, create peer on stdin/stdout and wait
	if *useStdio {
		// Use JSON encoding for stdio peer (TypeScript expects JSON)
		peer, err := jsonrpc3.NewPeer(os.Stdin, os.Stdout, root, jsonrpc3.WithJSON())
		if err != nil {
			log.Fatalf("Failed to create stdio peer: %v", err)
		}
		defer peer.Close()

		// Wait for interrupt signal
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh

		return
	}

	// Start HTTP server
	httpHandler := jsonrpc3.NewHTTPHandler(root)
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *httpPort),
		Handler: httpHandler,
	}
	go func() {
		log.Printf("HTTP server listening on :%d", *httpPort)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Start WebSocket server
	wsHandler := jsonrpc3.NewWebSocketHandler(root)
	wsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *wsPort),
		Handler: wsHandler,
	}
	go func() {
		log.Printf("WebSocket server listening on :%d", *wsPort)
		if err := wsServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("WebSocket server error: %v", err)
		}
	}()

	// Start Peer TCP server
	peerListener, err := net.Listen("tcp", fmt.Sprintf(":%d", *peerPort))
	if err != nil {
		log.Fatalf("Failed to start Peer server: %v", err)
	}
	defer peerListener.Close()

	go func() {
		log.Printf("Peer TCP server listening on :%d", *peerPort)
		for {
			conn, err := peerListener.Accept()
			if err != nil {
				log.Printf("Peer accept error: %v", err)
				continue
			}

			// Handle peer connection
			go func(c net.Conn) {
				defer c.Close()
				peer, err := jsonrpc3.NewPeer(c, c, root)
				if err != nil {
					log.Printf("Failed to create peer: %v", err)
					return
				}
				defer peer.Close()

				// Keep connection alive - wait for connection to close
				select {}
			}(conn)
		}
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down servers...")
	httpServer.Close()
	wsServer.Close()
	peerListener.Close()
}
