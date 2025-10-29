package jsonrpc3_test

import (
	"encoding/json"
	"fmt"

	"github.com/evanphx/jsonrpc3"
)

// Counter is a simple object that implements Object
type Counter struct {
	Name  string
	Value int
}

// CallMethod implements the jsonrpc3.Object interface
func (c *Counter) CallMethod(method string, params jsonrpc3.Params) (any, error) {
	switch method {
	case "increment":
		c.Value++
		return c.Value, nil
	case "decrement":
		c.Value--
		return c.Value, nil
	case "getValue":
		return c.Value, nil
	case "reset":
		c.Value = 0
		return nil, nil
	default:
		return nil, jsonrpc3.NewMethodNotFoundError(method)
	}
}

// Example_remoteObject demonstrates automatic reference management
// with objects that implement the Object interface.
func Example_remoteObject() {
	session := jsonrpc3.NewSession()
	root := jsonrpc3.NewMethodMap()
	handler := jsonrpc3.NewHandler(session, root, nil)

	// Register a method that returns an Object
	// The handler will automatically register it and return a reference
	root.Register("create_counter", func(params jsonrpc3.Params) (any, error) {
		var name string
		params.Decode(&name)
		return &Counter{Name: name, Value: 0}, nil
	})

	// Create a counter - it gets automatically registered
	req1, _ := jsonrpc3.NewRequest("create_counter", "MyCounter", 1)
	resp1 := handler.HandleRequest(req1)

	// The result is a LocalReference
	var counterRef jsonrpc3.LocalReference
	json.Unmarshal(resp1.Result, &counterRef)
	fmt.Printf("Got reference: %s\n", counterRef.Ref)

	// Call methods on the counter using the reference
	req2, _ := jsonrpc3.NewRequestWithRef(counterRef.Ref, "increment", nil, 2)
	resp2 := handler.HandleRequest(req2)

	var value int
	json.Unmarshal(resp2.Result, &value)
	fmt.Printf("After increment: %d\n", value)

	// Output:
	// Got reference: ref-1
	// After increment: 1
}

// Database simulates a database connection that implements Object
type Database struct {
	Name   string
	Tables map[string][]string
}

func (db *Database) CallMethod(method string, params jsonrpc3.Params) (any, error) {
	switch method {
	case "createTable":
		var tableName string
		params.Decode(&tableName)
		db.Tables[tableName] = []string{}
		return true, nil

	case "insert":
		var data struct {
			Table string `json:"table"`
			Value string `json:"value"`
		}
		params.Decode(&data)
		db.Tables[data.Table] = append(db.Tables[data.Table], data.Value)
		return true, nil

	case "query":
		var tableName string
		params.Decode(&tableName)
		return db.Tables[tableName], nil

	default:
		return nil, jsonrpc3.NewMethodNotFoundError(method)
	}
}

// Example_remoteObjectComplex demonstrates a more complex scenario
// where a method returns an Object embedded in a structure.
func Example_remoteObjectComplex() {
	session := jsonrpc3.NewSession()
	root := jsonrpc3.NewMethodMap()
	handler := jsonrpc3.NewHandler(session, root, nil)

	// Method that returns a structure containing an Object
	root.Register("connect_db", func(params jsonrpc3.Params) (any, error) {
		var dbName string
		params.Decode(&dbName)

		// Return a map with the database object
		// The framework will automatically find the Object and register it
		return map[string]any{
			"status": "connected",
			"db":     &Database{Name: dbName, Tables: make(map[string][]string)},
		}, nil
	})

	// Connect to database
	req1, _ := jsonrpc3.NewRequest("connect_db", "mydb", 1)
	resp1 := handler.HandleRequest(req1)

	// Parse the result - db field will be a LocalReference
	var result struct {
		Status string                   `json:"status"`
		DB     jsonrpc3.LocalReference `json:"db"`
	}
	json.Unmarshal(resp1.Result, &result)

	fmt.Printf("Status: %s\n", result.Status)
	fmt.Printf("DB Reference: %s\n", result.DB.Ref)

	// Now use the database reference
	req2, _ := jsonrpc3.NewRequestWithRef(result.DB.Ref, "createTable", "users", 2)
	handler.HandleRequest(req2)

	insertParams := map[string]string{"table": "users", "value": "alice"}
	req3, _ := jsonrpc3.NewRequestWithRef(result.DB.Ref, "insert", insertParams, 3)
	handler.HandleRequest(req3)

	req4, _ := jsonrpc3.NewRequestWithRef(result.DB.Ref, "query", "users", 4)
	resp4 := handler.HandleRequest(req4)

	var users []string
	json.Unmarshal(resp4.Result, &users)
	fmt.Printf("Users: %v\n", users)

	// Output:
	// Status: connected
	// DB Reference: ref-1
	// Users: [alice]
}

// Example_remoteObjectMultiple demonstrates returning multiple Objects
// which all get automatically registered.
func Example_remoteObjectMultiple() {
	session := jsonrpc3.NewSession()
	root := jsonrpc3.NewMethodMap()
	handler := jsonrpc3.NewHandler(session, root, nil)

	// Method that returns multiple Objects in a slice
	root.Register("create_counters", func(params jsonrpc3.Params) (any, error) {
		var count int
		params.Decode(&count)

		counters := make([]any, count)
		for i := 0; i < count; i++ {
			counters[i] = &Counter{
				Name:  fmt.Sprintf("counter-%d", i),
				Value: i * 10,
			}
		}
		return counters, nil
	})

	// Create multiple counters
	req1, _ := jsonrpc3.NewRequest("create_counters", 3, 1)
	resp1 := handler.HandleRequest(req1)

	// All counters are automatically registered and returned as references
	var refs []jsonrpc3.LocalReference
	json.Unmarshal(resp1.Result, &refs)

	fmt.Printf("Created %d counters\n", len(refs))

	// Use the first counter
	req2, _ := jsonrpc3.NewRequestWithRef(refs[0].Ref, "getValue", nil, 2)
	resp2 := handler.HandleRequest(req2)

	var value int
	json.Unmarshal(resp2.Result, &value)
	fmt.Printf("First counter value: %d\n", value)

	// Output:
	// Created 3 counters
	// First counter value: 0
}
