# JSON-RPC 3.0 TypeScript Implementation

A complete TypeScript implementation of JSON-RPC 3.0 with support for object references, batch requests, and bidirectional communication.

## Features

- ✅ **JSON-RPC 3.0 Protocol**: Full support for the JSON-RPC 3.0 specification
- ✅ **Object References**: Local and remote object reference management
- ✅ **Batch Requests**: Send multiple requests in a single batch
- ✅ **Batch-Local References**: Reference results from previous requests in a batch (`\0`, `\1`, etc.)
- ✅ **Bidirectional Calls**: Both client and server can initiate RPC calls
- ✅ **Introspection**: Built-in `$methods` and `$type` introspection
- ✅ **Protocol Methods**: Standard `$rpc` methods (session_id, capabilities, etc.)
- ✅ **HTTP Transport**: Client and server using Bun's native HTTP
- ✅ **Type-Safe**: Full TypeScript with strict mode enabled
- ✅ **Automatic Reference Registration**: Objects returned from methods are automatically registered

## Installation

```bash
bun install
```

## Quick Start

### Server

```typescript
import { Session, Handler, MethodMap, HttpServer } from "@jsonrpc3/core";

const session = new Session();
const root = new MethodMap();

// Register methods
root.register("add", (params) => {
  const { a, b } = params.decode<{ a: number; b: number }>();
  return a + b;
});

// Create handler and server
const handler = new Handler(session, root);
const server = new HttpServer(handler, { port: 3000 });
await server.start();
```

### Client

```typescript
import { Session, HttpClient } from "@jsonrpc3/core";

const session = new Session();
const client = new HttpClient("http://localhost:3000", session);

// Make RPC call
const result = await client.call("add", { a: 5, b: 3 });
console.log(result); // 8
```

## Object References

Methods can return objects that implement the `RpcObject` interface. These objects are automatically registered and returned as references:

```typescript
root.register("createCounter", () => {
  const counter = new MethodMap();
  let count = 0;

  counter.register("increment", () => ++count);
  counter.register("getValue", () => count);

  return counter; // Auto-registered, returns { $ref: "ref-..." }
});

// Client side
const counterRef = await client.call("createCounter");
const ref = counterRef.$ref;

await client.call("increment", undefined, ref); // Call on the object
const value = await client.call("getValue", undefined, ref);
```

## Introspection

Objects can implement introspection methods:

```typescript
const obj = new MethodMap();
obj.type = "Calculator";
obj.register("add", ...);

// Introspection methods are built-in
await client.call("$methods", undefined, ref); // ["add", "$methods", "$type"]
await client.call("$type", undefined, ref);    // "Calculator"
```

## Protocol Methods

The `$rpc` reference provides protocol-level methods:

```typescript
// Get session ID
const info = await client.call("session_id", undefined, "$rpc");

// Get capabilities
const caps = await client.call("capabilities", undefined, "$rpc");
// ["references", "batch-local-references", "bidirectional-calls", "introspection"]

// List all references
const refs = await client.call("list_refs", undefined, "$rpc");

// Dispose a reference
await client.call("dispose", { ref: "some-ref" }, "$rpc");
```

## Batch Requests

Send multiple requests at once:

```typescript
const responses = await client.batch([
  newRequest("add", { a: 1, b: 2 }, 1),
  newRequest("add", { a: 3, b: 4 }, 2),
]);
```

### Batch-Local References

Reference results from earlier requests in the same batch:

```typescript
const responses = await client.batch([
  newRequest("createCounter", undefined, 0),
  newRequest("increment", undefined, 1, "\\0"), // Ref to first result
]);
```

## Running Tests

```bash
bun test
```

## Peer-to-Peer Communication

The `Peer` class enables bidirectional JSON-RPC 3.0 communication over streams:

```typescript
import { Peer, MethodMap } from "@jsonrpc3/core";

// Create stream pairs
const transform1 = new TransformStream<Uint8Array, Uint8Array>();
const transform2 = new TransformStream<Uint8Array, Uint8Array>();

// Create peers with methods
const peer1Root = new MethodMap();
peer1Root.register("greet", (params) => {
  const { name } = params.decode<{ name: string }>();
  return `Hello, ${name}!`;
});

const peer2Root = new MethodMap();
peer2Root.register("add", (params) => {
  const { a, b } = params.decode<{ a: number; b: number }>();
  return a + b;
});

// Connect peers
const peer1 = new Peer(transform1.readable, transform2.writable, peer1Root);
const peer2 = new Peer(transform2.readable, transform1.writable, peer2Root);

// Both peers can initiate calls
const greeting = await peer2.call("greet", { name: "Alice" });
const sum = await peer1.call("add", { a: 5, b: 3 });
```

Features:
- **Bidirectional**: Both peers can initiate calls
- **Stream-based**: Works with any ReadableStream/WritableStream pair
- **Object References**: Full support for remote object registration
- **Concurrent**: Multiple requests can be in-flight simultaneously
- **Notifications**: One-way messages with no response

## Examples

See examples for complete working demonstrations:

```bash
# HTTP client/server
bun examples/basic.ts

# Peer-to-peer communication
bun examples/peer.ts
```

## Architecture

The implementation follows a layered architecture:

1. **Core Types**: `Request`, `Response`, `Error`, `Message` types
2. **Encoding**: `Codec` interface with JSON implementation
3. **Session**: Reference lifecycle management
4. **Handler**: Request routing and batch processing
5. **Transport**: HTTP client/server (more transports coming)

## API Reference

### Session

Manages object references for a connection:

- `addLocalRef(obj)`: Register a local object
- `getLocalRef(ref)`: Get a local object
- `addRemoteRef(ref)`: Track a remote reference
- `disposeAll()`: Clear all references

### Handler

Routes requests to objects:

- `handleRequest(req)`: Handle a single request
- `handleBatch(batch)`: Handle a batch of requests

### MethodMap

Simple RPC object implementation:

- `register(method, handler)`: Register a method
- Implements `RpcObject` interface automatically
- Built-in `$methods` and `$type` introspection

### HttpServer

HTTP server using Bun.serve:

- `start()`: Start listening
- `stop()`: Stop server
- `url()`: Get server URL

### HttpClient

HTTP client using fetch:

- `call(method, params?, ref?)`: Make RPC call
- `notify(method, params?, ref?)`: Send notification
- `batch(requests)`: Send batch request

### Peer

Bidirectional peer over streams:

- `call(method, params?, ref?)`: Make RPC call to remote peer
- `notify(method, params?, ref?)`: Send notification to remote peer
- `batch(requests)`: Send batch request to remote peer
- `registerObject(obj, ref?)`: Register local object for remote access
- `unregisterObject(ref)`: Remove registered object
- `close()`: Close the peer connection
- `wait()`: Wait for peer to close

## License

MIT
