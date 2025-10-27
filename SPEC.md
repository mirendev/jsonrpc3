# JSON-RPC 3.0 Specification

## 1. Introduction

JSON-RPC 3.0 is a backward-compatible superset of JSON-RPC 2.0 that extends the protocol with two key capabilities:

1. **Object References**: Servers can return references to remote objects, which clients can then invoke methods on
2. **Bidirectional Method Calls**: Clients can pass references to themselves, enabling servers to invoke methods back on the client

Every JSON-RPC 3.0 server is also a compliant JSON-RPC 2.0 server. All JSON-RPC 2.0 clients can communicate with JSON-RPC 3.0 servers using standard 2.0 semantics.

## 2. JSON-RPC 2.0 Foundation

JSON-RPC 3.0 is built upon JSON-RPC 2.0. This section summarizes the core 2.0 specification.

### 2.1. Protocol Version

In JSON-RPC 2.0, all requests and responses MUST include a `jsonrpc` member with the string value `"2.0"`.

JSON-RPC 3.0 introduces version negotiation: clients that support JSON-RPC 3.0 features SHOULD send requests with `"jsonrpc": "3.0"`. Servers that support JSON-RPC 3.0 will respond with `"jsonrpc": "3.0"`. If a server does not support JSON-RPC 3.0, it MUST return an error response, allowing the client to fall back to JSON-RPC 2.0.

### 2.2. Request Format

A request object contains the following members:

- **jsonrpc**: A string specifying the version of the JSON-RPC protocol. MUST be exactly "2.0" for JSON-RPC 2.0 requests, or "3.0" for JSON-RPC 3.0 requests (see section 2.7 for version negotiation).
- **method**: A string containing the name of the method to be invoked.
- **params**: (Optional) A structured value (Array or Object) that holds the parameter values to be used during the invocation of the method.
- **id**: An identifier established by the client. This member is REQUIRED for requests expecting a response. It MUST be a String, Number, or NULL. If omitted, the request is a notification.

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "subtract",
  "params": [42, 23],
  "id": 1
}
```

**Example Request with Named Parameters:**
```json
{
  "jsonrpc": "2.0",
  "method": "subtract",
  "params": {"minuend": 42, "subtrahend": 23},
  "id": 2
}
```

### 2.3. Response Format

When a request is completed, the server MUST reply with a response object.

#### 2.3.1. Success Response

A successful response object contains:

- **jsonrpc**: MUST match the version from the request ("2.0" or "3.0")
- **result**: The result of the method invocation. This member is REQUIRED on success.
- **id**: MUST be the same as the value of the id member in the request object.

**Example:**
```json
{
  "jsonrpc": "2.0",
  "result": 19,
  "id": 1
}
```

#### 2.3.2. Error Response

An error response object contains:

- **jsonrpc**: MUST match the version from the request ("2.0" or "3.0"), except when returning an error for an unsupported version (see section 2.7)
- **error**: An object describing the error. This member is REQUIRED on error.
- **id**: MUST be the same as the value of the id member in the request object. If there was an error detecting the id in the request object, it MUST be NULL.

The error object contains:

- **code**: An integer indicating the error type
- **message**: A string providing a short description of the error
- **data**: (Optional) A primitive or structured value containing additional information about the error

**Example:**
```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32601,
    "message": "Method not found",
    "data": "The method 'foobar' does not exist"
  },
  "id": 1
}
```

A response object MUST contain either a `result` member or an `error` member, but not both.

### 2.4. Standard Error Codes

The following error codes are defined:

| Code | Message | Meaning |
|------|---------|---------|
| -32700 | Parse error | Invalid JSON was received |
| -32600 | Invalid Request | The JSON sent is not a valid request object |
| -32601 | Method not found | The method does not exist or is not available |
| -32602 | Invalid params | Invalid method parameter(s) |
| -32603 | Internal error | Internal JSON-RPC error |
| -32000 to -32099 | Server error | Reserved for implementation-defined server errors |

### 2.5. Notification

A notification is a request without an `id` member. The server MUST NOT reply to a notification, including error responses.

**Example:**
```json
{
  "jsonrpc": "2.0",
  "method": "update",
  "params": [1, 2, 3, 4, 5]
}
```

### 2.6. Batch Requests

Multiple request objects can be sent as an array. The server SHOULD process them concurrently when possible. The server MUST return an array of response objects, in any order.

**Example Batch Request:**
```json
[
  {"jsonrpc": "2.0", "method": "sum", "params": [1, 2, 4], "id": "1"},
  {"jsonrpc": "2.0", "method": "notify_hello", "params": [7]},
  {"jsonrpc": "2.0", "method": "subtract", "params": [42, 23], "id": "2"}
]
```

**Example Batch Response:**
```json
[
  {"jsonrpc": "2.0", "result": 7, "id": "1"},
  {"jsonrpc": "2.0", "result": 19, "id": "2"}
]
```

If the batch contains only notifications, the server returns nothing. If the batch is empty or not a valid array, the server returns a single error response.

### 2.7. Version Negotiation

JSON-RPC 3.0 uses the `jsonrpc` field for version negotiation between clients and servers.

#### 2.7.1. Client Behavior

- A client that supports **only** JSON-RPC 2.0 MUST send `"jsonrpc": "2.0"`
- A client that supports JSON-RPC 3.0 SHOULD send `"jsonrpc": "3.0"` when it intends to use 3.0 features (object references or bidirectional calls)
- A client MAY send `"jsonrpc": "2.0"` even if it supports 3.0, to communicate using only 2.0 features

#### 2.7.2. Server Behavior

- A server that receives a request with `"jsonrpc": "2.0"` MUST respond according to JSON-RPC 2.0 semantics
- A server that supports JSON-RPC 3.0 and receives a request with `"jsonrpc": "3.0"` MUST respond with `"jsonrpc": "3.0"` and MAY return object references or expect bidirectional calls
- A server that does **not** support JSON-RPC 3.0 but receives a request with `"jsonrpc": "3.0"` MUST return an error response with code `-32600` (Invalid Request) and SHOULD include information about the unsupported version in the error data

#### 2.7.3. Fallback Mechanism

When a client sends a request with `"jsonrpc": "3.0"` and receives an error indicating the version is not supported:

1. The client SHOULD retry the request with `"jsonrpc": "2.0"`
2. The client MUST NOT use JSON-RPC 3.0 features (object references, bidirectional calls) for the remainder of the session
3. The client MAY periodically retry with `"jsonrpc": "3.0"` to detect if server capabilities have changed (e.g., after a server upgrade)

**Example - Version negotiation failure:**

**Request (3.0 client to 2.0 server):**
```json
{
  "jsonrpc": "3.0",
  "method": "getServerInfo",
  "id": 1
}
```

**Response (2.0 server):**
```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32600,
    "message": "Invalid Request",
    "data": "JSON-RPC version '3.0' is not supported. This server supports version '2.0'."
  },
  "id": 1
}
```

**Retry (client falls back to 2.0):**
```json
{
  "jsonrpc": "2.0",
  "method": "getServerInfo",
  "id": 2
}
```

**Success:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "name": "Example Server",
    "version": "1.0.0"
  },
  "id": 2
}
```

#### 2.7.4. Response Version Matching

The server's response MUST use the same `jsonrpc` version as the request:

- If a request has `"jsonrpc": "2.0"`, the response MUST have `"jsonrpc": "2.0"`
- If a request has `"jsonrpc": "3.0"`, the response MUST have `"jsonrpc": "3.0"` (or an error with `"jsonrpc": "2.0"` if 3.0 is not supported)

This ensures clear communication about which protocol version is being used for each request/response pair.

## 3. JSON-RPC 3.0 Extensions

### 3.1. Object References

JSON-RPC 3.0 introduces the ability for servers to return references to remote objects. Clients can then invoke methods on these object references.

#### 3.1.1. Reference Format

JSON-RPC 3.0 uses two distinct mechanisms for working with object references:

1. **Remote Reference Invocation** (top-level `ref` field): When invoking a method on a remote object
   - Format: `"ref": "unique-identifier"` at the message top level
   - Used to call methods on objects controlled by the OTHER party
   - This is the ONLY way to specify which remote object to invoke a method on

2. **Local Reference Passing** (`{"$ref": "..."}` objects): When passing references as parameters or returning them
   - Format: `{"$ref": "unique-identifier"}` in `params` or `result`
   - Used to pass references to objects YOU control to the other party
   - Allows the other party to call back to your objects

The `unique-identifier` is a string chosen by the party that controls the object. It uniquely identifies the object within the session. The format of this identifier is implementation-defined, but it MUST be unique within the session.

**Examples of valid reference identifiers:**
- UUID: `"550e8400-e29b-41d4-a716-446655440000"`
- Sequential: `"obj-1"`
- Hierarchical: `"db/connection/42"`

#### 3.1.2. Returning Object References

A server can return an object reference in the `result` member of a response:

**Request:**
```json
{
  "jsonrpc": "3.0",
  "method": "openDatabase",
  "params": {"name": "mydb"},
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {"$ref": "db-connection-1"},
  "id": 1
}
```

Object references can also be nested within complex structures:

```json
{
  "jsonrpc": "3.0",
  "result": {
    "database": {"$ref": "db-1"},
    "tables": [
      {"$ref": "table-users"},
      {"$ref": "table-products"}
    ]
  },
  "id": 2
}
```

#### 3.1.3. Using Object References

Once a client receives an object reference, it can invoke methods on that reference by including a top-level `ref` member in the request. The `ref` member specifies which remote object the method should be invoked on.

**Important**: The top-level `ref` field is the ONLY place where a remote reference (a reference to an object controlled by the other party) can be indicated. This distinguishes it from references passed as parameters, which are references to objects controlled by the caller.

**Example - Invoking Method on Remote Reference:**
```json
{
  "jsonrpc": "3.0",
  "ref": "db-connection-1",
  "method": "query",
  "params": ["SELECT * FROM users"],
  "id": 2
}
```

**Example - With Named Parameters:**
```json
{
  "jsonrpc": "3.0",
  "ref": "db-connection-1",
  "method": "executeQuery",
  "params": {
    "sql": "SELECT * FROM users WHERE id = ?",
    "args": [42]
  },
  "id": 2
}
```

The server MUST accept the reference and perform the method invocation on the referenced object. If the reference is invalid or not found, the server MUST return an error response (see section 3.3).

#### 3.1.4. Reference Lifecycle

Object references in JSON-RPC 3.0 are **session-based**:

1. **Creation**: References are created when a server returns them in a response
2. **Validity**: References remain valid for the duration of the connection/session
3. **Invalidation**: References are automatically invalidated when the connection is closed
4. **Cleanup**: Servers MUST clean up all resources associated with references when the connection closes

A server MAY also explicitly invalidate references before connection closure (e.g., when an object is explicitly closed). In such cases, subsequent attempts to use the reference MUST return an error.

#### 3.1.5. Reference Scope

References are scoped to the session in which they were created. A reference obtained in one connection MUST NOT be valid in a different connection, even if the same client reconnects.

#### 3.1.6. Reference Direction: `ref` vs `{"$ref": "..."}`

JSON-RPC 3.0 distinguishes between two types of references based on their direction:

1. **Remote References (top-level `ref`)**: When you want to invoke a method on an object controlled by the OTHER party
   - Used at the message top-level: `"ref": "object-id"`
   - Indicates "call this method on YOUR object (that you gave me a reference to)"
   - This is the ONLY place remote references can be specified

2. **Local References (in `params` as `{"$ref": "..."}`)**: When you want to pass a reference to an object YOU control
   - Used in `params`: `{"$ref": "object-id"}`
   - Indicates "here's a reference to MY object (that you can call back to)"
   - These are references being passed from caller to callee

**Example showing both:**

Client calls a method on a server object (remote reference) and passes a callback reference (local reference):

```json
{
  "jsonrpc": "3.0",
  "ref": "db-connection-1",
  "method": "subscribe",
  "params": {
    "table": "users",
    "callback": {"$ref": "client-callback-1"}
  },
  "id": 1
}
```

In this example:
- `"ref": "db-connection-1"` - remote reference to server's database connection object
- `{"$ref": "client-callback-1"}` - local reference to client's callback object being passed to the server

### 3.2. Bidirectional Method Calls

JSON-RPC 3.0 enables servers to invoke methods on the client by allowing clients to pass references to themselves (or objects they control).

#### 3.2.1. Connection Requirements

Bidirectional method calls require a persistent, bidirectional connection between client and server (e.g., WebSocket, TCP socket). Both parties MUST be able to send and receive messages at any time.

#### 3.2.2. Client References

A client can create a reference to itself (or an object it controls) and pass it to the server. The format is identical to server-created references:

**Client passes reference to itself:**
```json
{
  "jsonrpc": "3.0",
  "method": "subscribeToEvents",
  "params": {
    "eventType": "database-update",
    "callback": {"$ref": "client-callback-1"}
  },
  "id": 1
}
```

The reference identifier is chosen by the client and MUST be unique within the client's scope.

#### 3.2.3. Server-to-Client Invocation

When the server needs to invoke a method on a client reference, it sends a standard JSON-RPC request over the same connection with the client's reference in the top-level `ref` field.

**Server invokes client callback:**
```json
{
  "jsonrpc": "3.0",
  "ref": "client-callback-1",
  "method": "notify",
  "params": {
    "event": "database-update",
    "table": "users",
    "change": "insert"
  },
  "id": "server-req-1"
}
```

**Client responds:**
```json
{
  "jsonrpc": "3.0",
  "result": "acknowledged",
  "id": "server-req-1"
}
```

#### 3.2.4. Message Flow

In a bidirectional JSON-RPC 3.0 connection:

1. Either party can send requests at any time
2. The recipient MUST respond to requests (unless they are notifications)
3. Each party maintains its own `id` space for requests it originates
4. Responses MUST match the `id` from the corresponding request
5. Both parties manage the lifecycle of references they create

**Example Full Flow:**

1. Client requests subscription, passes reference to itself:
   ```json
   {
     "jsonrpc": "3.0",
     "method": "subscribe",
     "params": {"callback": {"$ref": "client-1"}},
     "id": 1
   }
   ```

2. Server acknowledges:
   ```json
   {
     "jsonrpc": "3.0",
     "result": "subscribed",
     "id": 1
   }
   ```

3. Later, server invokes client callback:
   ```json
   {
     "jsonrpc": "3.0",
     "ref": "client-1",
     "method": "onEvent",
     "params": {"data": "update"},
     "id": "s1"
   }
   ```

4. Client responds:
   ```json
   {
     "jsonrpc": "3.0",
     "result": null,
     "id": "s1"
   }
   ```

### 3.3. Extended Error Codes

JSON-RPC 3.0 defines additional error codes for reference-related errors:

| Code | Message | Meaning |
|------|---------|---------|
| -32001 | Invalid reference | The reference object is malformed |
| -32002 | Reference not found | The reference does not exist or has expired |
| -32003 | Reference type error | The reference is not the expected type |

**Example - Reference not found:**
```json
{
  "jsonrpc": "3.0",
  "error": {
    "code": -32002,
    "message": "Reference not found",
    "data": "Reference 'db-connection-1' does not exist or has expired"
  },
  "id": 5
}
```

## 4. Complete Examples

### 4.1. Server Returns Object Reference

**Scenario**: Client calls server method, receives object reference, then invokes methods on that object.

**Step 1 - Client requests database connection:**
```json
{
  "jsonrpc": "3.0",
  "method": "connect",
  "params": {"database": "myapp"},
  "id": 1
}
```

**Step 2 - Server returns reference:**
```json
{
  "jsonrpc": "3.0",
  "result": {"$ref": "conn-abc123"},
  "id": 1
}
```

**Step 3 - Client executes query on reference:**
```json
{
  "jsonrpc": "3.0",
  "ref": "conn-abc123",
  "method": "execute",
  "params": {
    "query": "SELECT * FROM users WHERE id = ?",
    "args": [42]
  },
  "id": 2
}
```

**Step 4 - Server returns query results:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "rows": [
      {"id": 42, "name": "Alice", "email": "alice@example.com"}
    ]
  },
  "id": 2
}
```

**Step 5 - Client closes connection:**
```json
{
  "jsonrpc": "3.0",
  "ref": "conn-abc123",
  "method": "close",
  "id": 3
}
```

**Step 6 - Server confirms closure:**
```json
{
  "jsonrpc": "3.0",
  "result": "closed",
  "id": 3
}
```

### 4.2. Client Passes Reference, Server Calls Back

**Scenario**: Client subscribes to events by passing a reference to itself. Server later invokes callback on that reference.

**Step 1 - Client subscribes with callback reference:**
```json
{
  "jsonrpc": "3.0",
  "method": "subscribe",
  "params": {
    "topic": "price-updates",
    "callback": {"$ref": "client-handler-1"}
  },
  "id": 1
}
```

**Step 2 - Server acknowledges subscription:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "subscriptionId": "sub-xyz789",
    "status": "active"
  },
  "id": 1
}
```

**Step 3 - Server invokes callback when event occurs:**
```json
{
  "jsonrpc": "3.0",
  "ref": "client-handler-1",
  "method": "handleEvent",
  "params": {
    "topic": "price-updates",
    "item": "AAPL",
    "price": 150.25,
    "timestamp": "2025-10-27T10:30:00Z"
  },
  "id": "srv-100"
}
```

**Step 4 - Client processes event and responds:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "processed": true,
    "action": "updated-display"
  },
  "id": "srv-100"
}
```

### 4.3. Error Scenarios

**Scenario 1 - Using expired reference:**

**Request:**
```json
{
  "jsonrpc": "3.0",
  "ref": "conn-old123",
  "method": "query",
  "params": ["SELECT 1"],
  "id": 10
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "error": {
    "code": -32002,
    "message": "Reference not found",
    "data": "Connection reference 'conn-old123' has expired or was closed"
  },
  "id": 10
}
```

**Scenario 2 - Invalid reference format:**

**Request:**
```json
{
  "jsonrpc": "3.0",
  "ref": "",
  "method": "query",
  "params": ["SELECT 1"],
  "id": 11
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "error": {
    "code": -32001,
    "message": "Invalid reference",
    "data": "Reference identifier must be a non-empty string"
  },
  "id": 11
}
```

**Scenario 3 - Wrong reference type:**

**Request:**
```json
{
  "jsonrpc": "3.0",
  "ref": "result-set-5",
  "method": "executeTransaction",
  "id": 12
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "error": {
    "code": -32003,
    "message": "Reference type error",
    "data": "Expected connection reference, got result-set reference"
  },
  "id": 12
}
```

### 4.4. Complex Example - Nested References and Bidirectional Calls

**Scenario**: Client creates a transaction, server returns transaction reference, client sets up callbacks, server notifies client of transaction events.

**Step 1 - Client begins transaction on database reference:**
```json
{
  "jsonrpc": "3.0",
  "ref": "db-1",
  "method": "beginTransaction",
  "params": {
    "isolation": "serializable",
    "observer": {"$ref": "client-observer-1"}
  },
  "id": 1
}
```

**Step 2 - Server returns transaction reference:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "transaction": {"$ref": "txn-999"},
    "startedAt": "2025-10-27T10:35:00Z"
  },
  "id": 1
}
```

**Step 3 - Client executes operations in transaction:**
```json
{
  "jsonrpc": "3.0",
  "ref": "txn-999",
  "method": "execute",
  "params": {
    "operations": [
      {"type": "update", "table": "accounts", "set": {"balance": 1000}, "where": {"id": 1}},
      {"type": "update", "table": "accounts", "set": {"balance": 2000}, "where": {"id": 2}}
    ]
  },
  "id": 2
}
```

**Step 4 - Server notifies observer (client) of each operation:**

Note: This demonstrates both reference mechanisms:
- `"ref": "client-observer-1"` - server invoking method on remote client object
- `{"$ref": "txn-999"}` - server passing back its own transaction reference to the client

```json
{
  "jsonrpc": "3.0",
  "ref": "client-observer-1",
  "method": "onTransactionEvent",
  "params": {
    "transaction": {"$ref": "txn-999"},
    "event": "operation-completed",
    "operation": 1,
    "rowsAffected": 1
  },
  "id": "srv-200"
}
```

**Step 5 - Client acknowledges:**
```json
{
  "jsonrpc": "3.0",
  "result": null,
  "id": "srv-200"
}
```

**Step 6 - Client commits transaction:**
```json
{
  "jsonrpc": "3.0",
  "ref": "txn-999",
  "method": "commit",
  "id": 3
}
```

**Step 7 - Server commits and notifies observer:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "status": "committed",
    "committedAt": "2025-10-27T10:35:05Z"
  },
  "id": 3
}
```

```json
{
  "jsonrpc": "3.0",
  "ref": "client-observer-1",
  "method": "onTransactionEvent",
  "params": {
    "transaction": {"$ref": "txn-999"},
    "event": "committed",
    "committedAt": "2025-10-27T10:35:05Z"
  },
  "id": "srv-201"
}
```

## 5. Compatibility and Implementation Notes

### 5.1. Backward Compatibility

JSON-RPC 3.0 is designed to be backward compatible with JSON-RPC 2.0:

- **2.0 clients with 3.0 servers**: A JSON-RPC 2.0 client (sending `"jsonrpc": "2.0"`) can communicate with a JSON-RPC 3.0 server without modification. The server MUST respond using JSON-RPC 2.0 semantics and MUST NOT send object references or initiate bidirectional calls to these clients.

- **3.0 clients with 2.0 servers**: A JSON-RPC 3.0 client uses the version negotiation mechanism described in section 2.7. The client sends `"jsonrpc": "3.0"` in its initial request. If the server does not support JSON-RPC 3.0, it will return an error (code -32600), and the client should fall back to `"jsonrpc": "2.0"` for all subsequent requests.

- **3.0 clients with 3.0 servers**: When both client and server support JSON-RPC 3.0, the client sends `"jsonrpc": "3.0"` and the server responds with `"jsonrpc": "3.0"`. Both parties can then use object references and bidirectional calls.

- **Mixed version within a session**: Once a version is negotiated for a connection, that version SHOULD be used consistently for the duration of the session. A client MAY attempt to renegotiate by sending a request with a different version number, but this behavior is implementation-specific.

### 5.2. Connection Requirements

JSON-RPC 2.0 is transport-agnostic and can work over stateless transports (like HTTP). JSON-RPC 3.0 retains this flexibility for basic requests, but object references and bidirectional calls impose additional requirements:

- **Object references**: Require session tracking, which typically implies a persistent connection or session management (e.g., cookies, session tokens over HTTP)
- **Bidirectional calls**: Require a bidirectional transport (e.g., WebSocket, TCP socket, HTTP/2 with server push) where both parties can initiate messages

Implementations SHOULD document which features require which connection types.

### 5.3. Security Considerations

#### 5.3.1. Reference Security

- **Reference guessing**: Reference identifiers SHOULD be difficult to guess (e.g., use UUIDs or cryptographically random values) to prevent unauthorized access to objects
- **Authorization**: Servers MUST verify that the client has permission to access an object each time a reference is used, not just when the reference is created
- **Reference leakage**: Servers MUST ensure references cannot be extracted from error messages or logs in a way that compromises security

#### 5.3.2. Bidirectional Call Security

- **Authentication**: Both client and server MUST authenticate each other before establishing bidirectional communication
- **Input validation**: Both parties MUST validate all inputs from bidirectional calls, as either side could be compromised
- **Rate limiting**: Implementations SHOULD implement rate limiting to prevent one party from overwhelming the other with requests
- **Callback validation**: Clients MUST validate that callback invocations are expected and authorized (e.g., only allowing callbacks on references they explicitly provided)

#### 5.3.3. Resource Management

- **Reference limits**: Servers SHOULD impose limits on the number of active references per client to prevent resource exhaustion
- **Connection timeouts**: Both parties SHOULD implement timeouts for connections to prevent resource leaks from abandoned connections
- **Graceful cleanup**: Both parties MUST properly clean up resources when connections are closed, whether gracefully or abruptly

### 5.4. Implementation Recommendations

#### 5.4.1. Reference Management

- Use weak references or reference counting internally to allow garbage collection when references are no longer accessible
- Maintain a registry mapping reference identifiers to objects
- Implement a cleanup mechanism that runs when connections close
- Consider providing explicit reference deletion methods for clients to signal when they're done with a reference

#### 5.4.2. Bidirectional Communication

- Implement separate message queues for incoming and outgoing messages
- Use asynchronous I/O to handle concurrent requests from both directions
- Maintain separate `id` counters for client-originated and server-originated requests to avoid collisions
- Consider using prefixes for `id` values (e.g., "c-1" for client, "s-1" for server) to make debugging easier

#### 5.4.3. Error Handling

- Provide detailed error messages in the `data` field to help with debugging
- Log errors appropriately on both sides
- Handle connection failures gracefully and clean up references automatically
- Implement retry logic for transient failures in bidirectional calls

#### 5.4.4. Testing

- Test reference lifecycle thoroughly, including edge cases like using references after connection closure
- Test bidirectional call scenarios with varying message timing
- Test error conditions, including network failures, invalid references, and malformed messages
- Perform load testing to ensure the system handles many concurrent references and bidirectional calls

### 5.5. Optional Extensions

Implementations MAY provide additional features beyond this specification:

- **Reference transfer**: Allowing references to be passed between different clients (with appropriate security measures)
- **Reference persistence**: Allowing references to survive connection closures (with explicit session management)
- **Typed references**: Including type information in references to enable better validation and error messages
- **Reference introspection**: Methods to query available operations on a reference
- **Weak references**: References that don't prevent object cleanup and may become invalid even during a session

These extensions SHOULD be clearly documented and SHOULD NOT break compatibility with implementations that only support the core specification.

## 6. Formal Grammar

### 6.1. JSON-RPC 2.0 Objects

```
Request       := {jsonrpc: "2.0", method: string, params?: array | object, id?: string | number | null}
Response      := SuccessResponse | ErrorResponse
SuccessResponse := {jsonrpc: "2.0", result: any, id: string | number | null}
ErrorResponse := {jsonrpc: "2.0", error: Error, id: string | number | null}
Error         := {code: number, message: string, data?: any}
Notification  := {jsonrpc: "2.0", method: string, params?: array | object}
Batch         := Array<Request | Notification>
```

### 6.2. JSON-RPC 3.0 Objects

JSON-RPC 3.0 extends the grammar to support version "3.0", adds the `ref` field for remote object invocation, and adds reference objects for passing local references:

```
Version       := "2.0" | "3.0"
Request       := {jsonrpc: Version, ref?: string, method: string, params?: array | object, id?: string | number | null}
Response      := SuccessResponse | ErrorResponse
SuccessResponse := {jsonrpc: Version, result: any, id: string | number | null}
ErrorResponse := {jsonrpc: Version, error: Error, id: string | number | null}
Error         := {code: number, message: string, data?: any}
Notification  := {jsonrpc: Version, ref?: string, method: string, params?: array | object}
Batch         := Array<Request | Notification>
LocalReference := {$ref: string}
```

Where:
- `Version` in a response MUST match the version in the corresponding request (except when returning an error for unsupported version)
- `ref` (optional, top-level): Specifies a remote reference to invoke the method on. This is the ONLY place where remote references can be specified. If present, the method is invoked on the referenced object controlled by the other party.
- `LocalReference` (as `{$ref: string}`): Used in `params` or `result` to pass references to objects controlled by the caller. These are references being passed from caller to callee.
- Reference identifiers (both in `ref` and `LocalReference`) MUST be non-empty strings and MUST be unique within the session for references created by the same party
- `LocalReference` objects MUST only have a single `$ref` member (no additional properties)

## 7. Conclusion

JSON-RPC 3.0 extends JSON-RPC 2.0 with powerful capabilities for building distributed, object-oriented systems while maintaining backward compatibility. Object references enable remote object manipulation without exposing implementation details, and bidirectional calls enable reactive, event-driven architectures.

Implementations should carefully consider the security implications of these features and implement appropriate safeguards. The session-based lifecycle of references provides a simple and predictable model for resource management.

For further information, examples, and implementation guidance, refer to the JSON-RPC 3.0 implementation guide (if available) or the JSON-RPC 2.0 specification at https://www.jsonrpc.org/specification.

---

**Document Version**: 1.0
**Date**: 2025-10-27
**Status**: Draft Specification
