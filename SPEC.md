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

### 2.7. Request Context

JSON-RPC 3.0 introduces an optional top-level `context` field that can be included in both requests and responses to provide contextual information about the message. This field enables features like distributed tracing, authentication metadata, request correlation, and custom application-specific context.

#### 2.7.1. Context Format

The `context` field is an OPTIONAL top-level member that can appear in both Request and Response objects:

```json
{
  "jsonrpc": "3.0",
  "method": "getData",
  "params": {"id": 123},
  "id": 1,
  "context": {
    "rpc.trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
    "rpc.span_id": "00f067aa0ba902b7"
  }
}
```

**Properties:**
- **context** (object, optional): An object containing contextual information
- The context object can contain any key-value pairs
- Both parties SHOULD preserve and pass through unknown context fields

#### 2.7.2. Standard Context Keys

The following context keys are RECOMMENDED for common use cases:

| Key | Type | Purpose | Example |
|-----|------|---------|---------|
| `rpc.trace_id` | string | Distributed tracing identifier | `"4bf92f3577b34da6a3ce929d0e0e4736"` |
| `rpc.span_id` | string | Current span identifier for tracing | `"00f067aa0ba902b7"` |
| `rpc.parent_span_id` | string | Parent span identifier | `"00f067aa0ba902b6"` |
| `rpc.correlation_id` | string | Request correlation identifier | `"req-12345"` |
| `rpc.auth_token` | string | Authentication token | `"Bearer eyJhbG..."` |
| `rpc.user_id` | string | User identifier | `"user-789"` |
| `rpc.session_id` | string | Application session identifier | `"session-abc123"` |
| `rpc.locale` | string | Preferred locale for responses | `"en-US"` |
| `rpc.timezone` | string | Client timezone | `"America/Los_Angeles"` |

Applications MAY define custom context keys. Custom keys SHOULD use a namespace prefix (e.g., `"myapp.custom_field"`).

#### 2.7.3. Context in Requests

When a client includes a `context` field in a request:

**Example Request with Context:**
```json
{
  "jsonrpc": "3.0",
  "method": "processOrder",
  "params": {
    "orderId": "12345",
    "items": ["item1", "item2"]
  },
  "id": 1,
  "context": {
    "rpc.trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
    "rpc.user_id": "user-789",
    "myapp.region": "us-west"
  }
}
```

**Server Behavior:**
- Servers SHOULD accept and process the context field
- Servers SHOULD use context information for tracing, logging, and authentication
- Servers MAY propagate context to downstream services
- Servers that do not support context SHOULD ignore it without error

#### 2.7.4. Context in Responses

Servers MAY include a `context` field in responses to provide additional contextual information back to the client:

**Example Response with Context:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "status": "completed",
    "orderId": "12345"
  },
  "id": 1,
  "context": {
    "rpc.trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
    "rpc.span_id": "00f067aa0ba902b8",
    "rpc.server_time": "2025-10-30T12:00:00Z"
  }
}
```

**Response Context Uses:**
- Return the same trace ID for correlation
- Include server timing information
- Provide additional metadata about the response
- Propagate context from downstream services

#### 2.7.5. Context in Batch Requests

When using batch requests, the `context` field can appear:

1. **At the top level of each request** in the batch (most common)
2. **Shared across the batch** - implementations MAY support a batch-level context, but this is not standardized

**Example Batch with Context:**
```json
[
  {
    "jsonrpc": "3.0",
    "method": "getUser",
    "params": {"id": 1},
    "id": 1,
    "context": {
      "rpc.trace_id": "abc123",
      "rpc.span_id": "span-1"
    }
  },
  {
    "jsonrpc": "3.0",
    "method": "getOrders",
    "params": {"userId": 1},
    "id": 2,
    "context": {
      "rpc.trace_id": "abc123",
      "rpc.span_id": "span-2"
    }
  }
]
```

#### 2.7.6. Context Propagation

When a server receives a request with context and makes downstream RPC calls, it SHOULD:

1. **Preserve trace information**: Pass through `rpc.trace_id` and create new `rpc.span_id` values
2. **Update span hierarchy**: Set `rpc.parent_span_id` to the current span
3. **Forward relevant fields**: Pass authentication and correlation fields as appropriate
4. **Add server context**: Include server-specific context when relevant

**Example Context Propagation:**

Client → Server A:
```json
{
  "jsonrpc": "3.0",
  "method": "processPayment",
  "params": {"amount": 100},
  "id": 1,
  "context": {
    "rpc.trace_id": "trace-xyz",
    "rpc.span_id": "span-1"
  }
}
```

Server A → Server B:
```json
{
  "jsonrpc": "3.0",
  "method": "chargeCard",
  "params": {"amount": 100},
  "id": "a-1",
  "context": {
    "rpc.trace_id": "trace-xyz",
    "rpc.span_id": "span-2",
    "rpc.parent_span_id": "span-1"
  }
}
```

#### 2.7.7. Implementation Guidelines

**Context Support:**
- Support for the `context` field is OPTIONAL
- Implementations that do not support context MUST ignore it without error
- Implementations SHOULD document which context keys they recognize and use

**Context Size:**
- Context objects SHOULD be kept small (typically < 1KB)
- Servers MAY reject requests with excessively large context objects
- Large binary data SHOULD NOT be included in context; use params instead

**Security Considerations:**
- Context fields MAY contain sensitive information (tokens, user IDs)
- Implementations MUST ensure context is not logged or exposed inappropriately
- Authentication tokens in context SHOULD use standard security practices
- Context SHOULD be sanitized before logging or error messages

**Performance:**
- Context processing SHOULD have minimal performance impact
- Implementations MAY cache context parsing results
- Trace IDs SHOULD be generated efficiently (avoid UUIDv1 with system calls)

#### 2.7.8. Compatibility

The `context` field is fully backward compatible:

- **3.0 clients with 2.0 servers**: The context field will be ignored by 2.0 servers
- **2.0 clients with 3.0 servers**: 2.0 clients won't send context, which is fine since it's optional
- **Mixed versions**: Context can be used with both `"jsonrpc": "2.0"` and `"jsonrpc": "3.0"` requests

**Example with JSON-RPC 2.0:**
```json
{
  "jsonrpc": "2.0",
  "method": "getData",
  "params": [1, 2, 3],
  "id": 1,
  "context": {
    "rpc.trace_id": "abc123"
  }
}
```

This is valid and will be processed by 3.0 servers, ignored by 2.0 servers.

### 2.8. Version Negotiation

JSON-RPC 3.0 uses the `jsonrpc` field for version negotiation between clients and servers.

#### 2.8.1. Client Behavior

- A client that supports **only** JSON-RPC 2.0 MUST send `"jsonrpc": "2.0"`
- A client that supports JSON-RPC 3.0 SHOULD send `"jsonrpc": "3.0"` when it intends to use 3.0 features (object references or bidirectional calls)
- A client MAY send `"jsonrpc": "2.0"` even if it supports 3.0, to communicate using only 2.0 features

#### 2.8.2. Server Behavior

- A server that receives a request with `"jsonrpc": "2.0"` MUST respond according to JSON-RPC 2.0 semantics
- A server that supports JSON-RPC 3.0 and receives a request with `"jsonrpc": "3.0"` MUST respond with `"jsonrpc": "3.0"` and MAY return object references or expect bidirectional calls
- A server that does **not** support JSON-RPC 3.0 but receives a request with `"jsonrpc": "3.0"` MUST return an error response with code `-32600` (Invalid Request) and SHOULD include information about the unsupported version in the error data

#### 2.8.3. Fallback Mechanism

When a client sends a request with `"jsonrpc": "3.0"` and receives an error indicating the version is not supported:

1. The client SHOULD retry the request with `"jsonrpc": "2.0"`
2. The client MUST NOT use JSON-RPC 3.0 features (object references, bidirectional calls) for the remainder of the session
3. The client MAY periodically retry with `"jsonrpc": "3.0"` to detect if server capabilities have changed (e.g., after a server upgrade)

**Example - Version negotiation failure:**

#### 2.8.4. Example Negotiation Flow

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

#### 2.8.5. Response Version Matching

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

#### 3.2.5. Transport Considerations

The ability for servers to invoke methods on client references depends on the underlying transport mechanism:

**Bidirectional Transports** (Server-to-Client Calls Supported):
- **WebSocket**: Full bidirectional support; both client and server can initiate requests at any time
- **TCP/Unix Domain Sockets**: Full bidirectional support over persistent connections
- **stdio (Process Communication)**: Full bidirectional support; parent and child processes can exchange messages in both directions

**Unidirectional/Request-Response Transports** (Server-to-Client Calls NOT Supported):
- **HTTP**: Standard HTTP is request-response only; servers cannot initiate requests to clients
  - The server can only respond to client requests
  - Client callbacks passed to the server via `{"$ref": "..."}` cannot be invoked unless:
    - The client implements a separate HTTP endpoint that the server can call (out-of-band)
    - A bidirectional transport like WebSocket is established
    - Long-polling or similar techniques are used

**Implementation Requirements:**

1. **API Documentation**: APIs using JSON-RPC 3.0 MUST document whether they support bidirectional method calls and which transports enable this feature

2. **Transport Capabilities**: Implementations SHOULD clearly indicate transport capabilities:
   - Whether server-to-client calls are supported
   - Which transports enable bidirectional communication
   - Any limitations or requirements for using client callbacks

3. **Error Handling**: When a client passes a callback reference over a unidirectional transport:
   - The server MAY accept the reference but should document that callbacks cannot be invoked
   - The server MAY return an error indicating bidirectional calls are not supported
   - The API documentation MUST explain the expected behavior

**Example API Documentation:**

*"This API supports client callbacks only over WebSocket connections. When using HTTP, client references will be accepted but cannot be invoked by the server. To receive server-initiated notifications, clients must establish a WebSocket connection."*

#### 3.2.6. Server Notifications to Client References

Even over unidirectional transports like HTTP, servers can still interact with client references using **notifications** (requests without an `id` field). Since notifications do not require a response, they can be included in server responses without violating the request-response model.

**Key Concept**: When a server needs to invoke a client reference but cannot send a request (because the transport doesn't support server-to-client requests), the server can send a **notification** that references the client's callback. The client processes this notification locally without sending a response.

**How It Works:**

1. **Client passes reference to server** via `{"$ref": "client-callback-1"}`
2. **Server sends notification in response** that references the client callback
3. **Client processes notification locally** by invoking the method on its own reference
4. **No response is sent** because it's a notification (no `id` field)

**Example - HTTP with Notifications:**

**Client Request:**
```json
{
  "jsonrpc": "3.0",
  "method": "subscribe",
  "params": {
    "topic": "updates",
    "callback": {"$ref": "client-cb-1"}
  },
  "id": 1
}
```

**Server Response (includes notification to client ref):**
```json
[
  {
    "jsonrpc": "3.0",
    "result": "subscribed",
    "id": 1
  },
  {
    "jsonrpc": "3.0",
    "ref": "client-cb-1",
    "method": "onUpdate",
    "params": {"message": "Initial update"}
  }
]
```

The response is a batch containing:
1. The normal response to the client's request (with `id: 1`)
2. A notification to the client's callback (no `id` field)

The client processes both messages:
- Resolves the response for request ID 1
- Invokes the `onUpdate` method on its local `client-cb-1` reference

**Advantages of Notifications:**

1. **Works over HTTP**: No bidirectional connection required
2. **No response needed**: Notifications are fire-and-forget
3. **Efficient**: Multiple notifications can be batched in a single response
4. **Simple**: Client processes its own references locally

**Limitations:**

1. **No acknowledgment**: Server doesn't know if notification was processed
2. **No return value**: Client cannot return a result to the server
3. **One-way only**: Server can send notifications but cannot receive responses to them

**When to Use Notifications vs. Requests:**

| Scenario | Use Notification | Use Request |
|----------|------------------|-------------|
| Fire-and-forget callback | ✓ | |
| Need acknowledgment | | ✓ |
| Need return value | | ✓ |
| HTTP transport | ✓ | |
| WebSocket/bidirectional transport | Either | Either |

**Polling Pattern with Notifications:**

For HTTP APIs, a common pattern is for clients to poll periodically, and the server includes any pending notifications in the poll response:

```json
// Client polls
{
  "jsonrpc": "3.0",
  "method": "poll",
  "params": {"session": "abc123"},
  "id": 42
}

// Server response with notifications
[
  {
    "jsonrpc": "3.0",
    "result": {"pending": 2},
    "id": 42
  },
  {
    "jsonrpc": "3.0",
    "ref": "client-cb-1",
    "method": "onEvent",
    "params": {"event": "update-1"}
  },
  {
    "jsonrpc": "3.0",
    "ref": "client-cb-1",
    "method": "onEvent",
    "params": {"event": "update-2"}
  }
]
```

**Server-Sent Events (SSE) Alternative:**

APIs can also utilize **HTTP Server-Sent Events (SSE)** to stream notifications to client references without requiring polling or batching. With SSE, the server maintains an open HTTP connection and sends notifications as individual events.

**How SSE Works with Client References:**

1. Client establishes initial HTTP request with callback reference
2. Server opens SSE stream in response
3. Server sends notifications as separate SSE events whenever needed
4. Each event contains a JSON-RPC notification to the client reference

**Example SSE Stream:**

```
// Client opens SSE connection
GET /notifications?session=abc123 HTTP/1.1

// Server sends notifications as they occur
data: {"jsonrpc":"3.0","ref":"client-cb-1","method":"onEvent","params":{"event":"update-1"}}

data: {"jsonrpc":"3.0","ref":"client-cb-1","method":"onEvent","params":{"event":"update-2"}}

data: {"jsonrpc":"3.0","ref":"client-cb-1","method":"onEvent","params":{"event":"update-3"}}
```

**Advantages of SSE for Notifications:**

- **Real-time**: Notifications sent immediately without polling
- **Efficient**: Single long-lived connection, no repeated HTTP overhead
- **Simple**: Standard HTTP, widely supported by browsers and clients
- **No batching needed**: Each notification sent independently
- **Automatic reconnection**: SSE clients handle reconnection automatically

**Comparison of HTTP Patterns:**

| Pattern | Latency | Overhead | Complexity | Browser Support |
|---------|---------|----------|------------|-----------------|
| Batch Response | None | Low | Low | Universal |
| Polling | High | High | Low | Universal |
| SSE | Low | Low | Medium | Good |
| WebSocket | Low | Low | High | Good |

APIs using SSE for notifications SHOULD document the SSE endpoint and expected event format.

**Implementation Notes:**

- Notifications to client references SHOULD only be sent when the client has explicitly passed that reference to the server
- Servers SHOULD document their use of notifications for client callbacks
- Clients MUST be prepared to handle batch responses that include notifications
- Notifications to client references are processed the same way as any notification: invoke the method locally without sending a response

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

### 3.4. Protocol Methods

JSON-RPC 3.0 defines a special reserved reference `"$rpc"` that provides access to protocol-level operations. These methods allow explicit management of references and session information.

#### 3.4.1. The `$rpc` Magic Reference

The reference identifier `"$rpc"` is reserved for protocol operations. It MUST NOT be used as a user-defined reference identifier. When a request includes `"ref": "$rpc"`, the method is invoked on the protocol handler rather than on a user-defined object.

**Example:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "session_id",
  "id": 1
}
```

#### 3.4.2. Optional Protocol Methods

All protocol methods are OPTIONAL to implement. If a server or client does not support a protocol method, it MUST return a Method not found error (code -32601).

##### 3.4.2.1. `dispose`

Explicitly disposes of a reference, freeing associated resources immediately rather than waiting for session cleanup.

**Parameters:**
- `ref` (string, required): The reference identifier to dispose

**Returns:** `null` on success

**Example - Client disposing server reference:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "dispose",
  "params": {"ref": "db-connection-1"},
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": null,
  "id": 1
}
```

**Example - Server disposing client reference:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "dispose",
  "params": {"ref": "client-callback-1"},
  "id": "srv-100"
}
```

If the reference has already been disposed or never existed, the server SHOULD return a Reference not found error (-32002).

##### 3.4.2.2. `session_id`

Retrieves the current session identifier. Useful for debugging, logging, and correlation across multiple connections.

**Parameters:** None

**Returns:** An object containing session information

**Example:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "session_id",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "sessionId": "550e8400-e29b-41d4-a716-446655440000",
    "createdAt": "2025-10-27T10:30:00Z"
  },
  "id": 1
}
```

The format of the session identifier and additional fields in the result are implementation-defined.

##### 3.4.2.3. `list_refs`

Lists all active references in the current session. Useful for debugging, monitoring, and resource tracking.

**Parameters:** None

**Returns:** An object containing arrays of references by direction

**Example:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "list_refs",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "local": [
      {"ref": "db-1", "type": "database", "created": "2025-10-27T10:30:00Z"},
      {"ref": "conn-123", "type": "connection", "created": "2025-10-27T10:30:05Z"}
    ],
    "remote": [
      {"ref": "client-callback-1", "created": "2025-10-27T10:30:02Z"}
    ]
  },
  "id": 1
}
```

The structure and fields in the result are implementation-defined. Implementations MAY include:
- `local`: References to objects controlled by the responding party
- `remote`: References to objects controlled by the requesting party
- Additional metadata like type, creation time, last accessed, etc.

##### 3.4.2.4. `dispose_all`

Disposes all references in the current session. Useful for cleanup scenarios, testing, or when a client wants to reset its state without closing the connection.

**Parameters:** None

**Returns:** An object containing disposal statistics

**Example:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "dispose_all",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "disposed": 5,
    "localDisposed": 3,
    "remoteDisposed": 2
  },
  "id": 1
}
```

**Important:** `dispose_all` disposes references controlled by BOTH parties:
- Local references (objects you control)
- Remote references (objects the other party passed to you)

After calling `dispose_all`, the session continues but with no active references.

##### 3.4.2.5. `ref_info`

Retrieves information about a specific reference. Returns the same type of information as `list_refs`, but for a single reference.

**Parameters:**
- `ref` (string, required): The reference identifier to query

**Returns:** An object containing reference information

**Example:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "ref_info",
  "params": {"ref": "db-connection-1"},
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "ref": "db-connection-1",
    "type": "database-connection",
    "direction": "local",
    "created": "2025-10-27T10:30:00Z",
    "lastAccessed": "2025-10-27T10:35:12Z"
  },
  "id": 1
}
```

The structure and fields in the result are implementation-defined. Implementations MAY include:
- `ref`: The reference identifier
- `type`: The type or class of the referenced object
- `direction`: Whether this is a "local" (controlled by responder) or "remote" (controlled by requester) reference
- `created`: Timestamp when the reference was created
- `lastAccessed`: Timestamp of last use
- Additional metadata specific to the referenced object

If the reference does not exist or has been disposed, the server MUST return a Reference not found error (-32002):

**Example - Reference not found:**
```json
{
  "jsonrpc": "3.0",
  "error": {
    "code": -32002,
    "message": "Reference not found",
    "data": "Reference 'db-connection-1' does not exist or has been disposed"
  },
  "id": 1
}
```

##### 3.4.2.6. `mimetypes`

Retrieves the list of MIME types (encodings) supported by the server. This is useful for content negotiation in transport-agnostic scenarios where HTTP headers are not available.

**Parameters:** None

**Returns:** An array of supported MIME type strings, ordered by server preference

**Example:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "mimetypes",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": [
    "application/cbor; format=compact",
    "application/cbor",
    "application/json"
  ],
  "id": 1
}
```

**Interpretation:**
- The array is ordered by server preference (most preferred first)
- `application/json` SHOULD always be included for maximum compatibility
- Clients can use this information to select the optimal encoding for subsequent requests
- The first supported encoding in the list that the client also supports is recommended

**Example with limited support:**
```json
{
  "jsonrpc": "3.0",
  "result": [
    "application/json"
  ],
  "id": 1
}
```

This response indicates the server only supports JSON encoding.

##### 3.4.2.7. `capabilities`

**Parameters:** None

**Returns:** An array of capability strings indicating optional features supported by the implementation

**Purpose:** Allows peers to discover which optional protocol features are supported. This is purely informational—peers will attempt to function regardless of whether capabilities are checked.

**Example:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "capabilities",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": [
    "references",
    "batch-local-references",
    "bidirectional-calls",
    "introspection",
    "cbor-encoding",
    "cbor-compact-encoding"
  ],
  "id": 1
}
```

**Standard Capabilities:**

The following capability strings are defined by this specification:

- **`references`**: Supports JSON-RPC 3.0 object references (local and remote references)
- **`batch-local-references`**: Supports batch-local references (`\0`, `\1`, etc.) as defined in section 3.6
- **`bidirectional-calls`**: Supports both parties initiating method calls (client and server can both call methods on each other)
- **`introspection`**: Supports object introspection methods (`$methods` and `$type`)
- **`cbor-encoding`**: Supports CBOR encoding (`application/cbor`)
- **`cbor-compact-encoding`**: Supports compact CBOR encoding (`application/cbor; format=compact`)

**Implementation Notes:**
- Implementations MAY return custom capability strings prefixed with an organization or implementation identifier (e.g., `"myorg:custom-feature"`)
- Clients SHOULD gracefully handle unknown capability strings
- The absence of a capability in the list does not necessarily mean the feature is unsupported—implementations may support features without advertising them
- This method is purely informational and best-effort; peers should attempt functionality regardless of capabilities

#### 3.4.3. Security Considerations

Implementations SHOULD consider the following security implications of protocol methods:

1. **Authorization**: Should all clients be allowed to call protocol methods? Consider restricting `list_refs`, `ref_info`, and `mimetypes` in production environments.

2. **Disposal permissions**: The `dispose` method allows either party to dispose references. Implementations MAY restrict who can dispose which references, but the default behavior allows both parties to explicitly release resources.

3. **Information disclosure**: `list_refs`, `ref_info`, `session_id`, and `mimetypes` may expose implementation details useful to attackers. Implementations MAY require special permissions or disable these methods in production.

4. **Denial of service**: `dispose_all` could be used to disrupt service by clearing all references. Implementations MAY rate-limit or require authentication for this method.

#### 3.4.4. Implementation Notes

- Protocol methods are independent of the JSON-RPC version negotiation. They can be used with both `"jsonrpc": "2.0"` and `"jsonrpc": "3.0"` requests (though references are only meaningful in 3.0).

- Implementations MAY provide additional protocol methods beyond those defined here. Custom protocol methods SHOULD be documented clearly.

- The `$rpc` reference does not appear in `list_refs` results—it is always implicitly available.

- Protocol methods MUST be safe to call from either party (client or server) in bidirectional connections.

### 3.5. Object Introspection Methods

JSON-RPC 3.0 defines optional introspection methods that objects can implement to provide information about themselves. These methods are invoked on object references (including the root object when no `ref` is specified) rather than on the `$rpc` protocol handler.

#### 3.5.1. The `$methods` Method

Objects MAY implement a `$methods` method that returns a list of method names the object supports.

**Parameters:** None

**Returns:** An array of strings representing method names supported by the object

**Example:**
```json
{
  "jsonrpc": "3.0",
  "ref": "calculator-1",
  "method": "$methods",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": [
    "add",
    "subtract",
    "multiply",
    "divide",
    "clear"
  ],
  "id": 1
}
```

**Implementation Notes:**
- The `$methods` method itself SHOULD be included in the returned list
- If the object also implements `$type`, it SHOULD be included in the list
- Implementations MAY choose to exclude internal or private methods from the list
- The absence of `$methods` support means the object does not provide this introspection capability

#### 3.5.2. The `$type` Method

Objects MAY implement a `$type` method that returns a string describing the object's type or class.

**Parameters:** None

**Returns:** A string describing the object's type

**Purpose:** Provides type information for objects, enabling clients to understand what kind of object they're working with. The format and content of the type string is implementation-defined.

**Example:**
```json
{
  "jsonrpc": "3.0",
  "ref": "calculator-1",
  "method": "$type",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": "Calculator",
  "id": 1
}
```

**Alternative type formats:**
```json
{
  "jsonrpc": "3.0",
  "result": "com.example.Calculator",
  "id": 1
}
```

```json
{
  "jsonrpc": "3.0",
  "result": "database:connection",
  "id": 1
}
```

**Implementation Notes:**
- The type string format is implementation-defined—it can be a simple name, a fully-qualified class name, a URI, or any other identifying string
- Clients SHOULD NOT assume any particular format for type strings
- Type strings are purely informational and do not imply any behavioral contract
- The absence of `$type` support means the object does not provide type information

#### 3.5.3. Introspection and Capabilities

If an implementation supports introspection methods (`$methods` and/or `$type`), it SHOULD advertise the `"introspection"` capability in its `capabilities` method response (see section 3.4.2.7).

Objects are not required to support introspection even if the implementation advertises the capability—the capability indicates that some objects in the system support introspection, not that all objects do.

### 3.6. Batch-Local References

JSON-RPC 3.0 provides a special mechanism for batch requests that allows operations to be pipelined without requiring round-trip communication. This is accomplished using **batch-local references** that automatically resolve to the results of previous requests within the same batch.

#### 3.6.1. Batch-Local Reference Format

Batch-local references use a special syntax: a backslash followed by a number (e.g., `\0`, `\1`, `\2`). The number indicates the zero-based index of a request within the same batch.

**Format**: `\<index>` where `<index>` is the zero-based position of the request in the batch array.

**Examples**:
- `\0` - Refers to the result of the first request (index 0) in the batch
- `\1` - Refers to the result of the second request (index 1) in the batch
- `\2` - Refers to the result of the third request (index 2) in the batch

#### 3.6.2. Resolution Semantics

When a batch-local reference appears in the `ref` field of a request within a batch:

1. **Sequential Processing**: The batch MUST be processed sequentially in array order when batch-local references are present
2. **Forward References Only**: A request can only reference earlier requests (indices less than the current request's index)
3. **Result Resolution**: The batch-local reference `\N` resolves to the `result` field of the response for request at index N
4. **Reference Type**: If the result at index N is a LocalReference (`{"$ref": "..."}`), the batch-local reference resolves to that reference identifier
5. **Error Propagation**: If the referenced request (index N) results in an error, the current request MUST fail with a reference error

#### 3.6.3. Example: Database Query Pipeline

This example shows opening a database and immediately querying it in a single batch request:

**Batch Request**:
```json
[
  {
    "jsonrpc": "3.0",
    "method": "openDatabase",
    "params": {"name": "mydb"},
    "id": 0
  },
  {
    "jsonrpc": "3.0",
    "ref": "\\0",
    "method": "query",
    "params": {"sql": "SELECT * FROM users"},
    "id": 1
  }
]
```

**Batch Response**:
```json
[
  {
    "jsonrpc": "3.0",
    "result": {"$ref": "db-conn-123"},
    "id": 0
  },
  {
    "jsonrpc": "3.0",
    "result": {
      "rows": [
        {"id": 1, "name": "Alice"},
        {"id": 2, "name": "Bob"}
      ]
    },
    "id": 1
  }
]
```

**Explanation**:
1. Request 0 calls `openDatabase`, which returns a reference `{"$ref": "db-conn-123"}`
2. Request 1 uses `"ref": "\\0"`, which resolves to `"db-conn-123"` (the reference from request 0's result)
3. The server invokes `query` on the database connection
4. Both results are returned in a single response

#### 3.6.4. Example: Chaining Multiple Operations

Batch-local references can chain multiple operations together:

**Batch Request**:
```json
[
  {
    "jsonrpc": "3.0",
    "method": "createWorkspace",
    "params": {"name": "project-a"},
    "id": 0
  },
  {
    "jsonrpc": "3.0",
    "ref": "\\0",
    "method": "createDocument",
    "params": {"title": "README"},
    "id": 1
  },
  {
    "jsonrpc": "3.0",
    "ref": "\\1",
    "method": "write",
    "params": {"content": "# Hello World"},
    "id": 2
  }
]
```

This creates a workspace, creates a document in that workspace, and writes content to the document—all in a single batch request.

#### 3.6.5. Error Handling

If a referenced request fails, the dependent request MUST also fail:

**Batch Request**:
```json
[
  {
    "jsonrpc": "3.0",
    "method": "openDatabase",
    "params": {"name": "invalid-db"},
    "id": 0
  },
  {
    "jsonrpc": "3.0",
    "ref": "\\0",
    "method": "query",
    "params": {"sql": "SELECT * FROM users"},
    "id": 1
  }
]
```

**Batch Response** (when request 0 fails):
```json
[
  {
    "jsonrpc": "3.0",
    "error": {
      "code": -32000,
      "message": "Database not found",
      "data": "invalid-db"
    },
    "id": 0
  },
  {
    "jsonrpc": "3.0",
    "error": {
      "code": -32001,
      "message": "Invalid reference",
      "data": "Referenced request \\0 failed"
    },
    "id": 1
  }
]
```

#### 3.6.6. Implementation Requirements

Implementations that support batch-local references MUST:

1. **Validate Reference Indices**: Ensure that batch-local reference indices are within valid range (0 to current index - 1)
2. **Process Sequentially**: Process batch requests in order when batch-local references are detected (cannot process in parallel)
3. **Check Result Type**: Verify that the referenced result is a LocalReference or can be used as a reference
4. **Error on Forward References**: Return an error if a request references a later request in the batch (index >= current)
5. **Error on Failed References**: If a referenced request fails, the dependent request MUST fail with code -32001 (Invalid reference)

#### 3.6.7. Non-Reference Results

If a batch-local reference `\N` points to a request whose result is NOT a LocalReference (e.g., a plain value like a number or string), the implementation SHOULD return an error with code -32003 (Reference type error).

**Example of Invalid Usage**:
```json
[
  {
    "jsonrpc": "3.0",
    "method": "add",
    "params": [2, 3],
    "id": 0
  },
  {
    "jsonrpc": "3.0",
    "ref": "\\0",
    "method": "someMethod",
    "id": 1
  }
]
```

If request 0 returns `{"result": 5, "id": 0}`, request 1 should fail because `5` is not a reference.

#### 3.5.8. Benefits

Batch-local references provide several advantages:

1. **Reduced Latency**: Multiple dependent operations can be executed without round-trips
2. **Atomic Operations**: A sequence of operations can be sent as a single unit
3. **Simplified Client Code**: Clients don't need to wait for intermediate responses
4. **Bandwidth Efficiency**: Reduces the number of HTTP requests or messages sent

#### 3.5.9. Compatibility

- Batch-local references are a JSON-RPC 3.0 feature (they use the `ref` field which is 3.0-specific)
- Servers that do not support batch-local references MAY treat `\0` as a regular reference string and return a "Reference not found" error
- Clients SHOULD only use batch-local references when they know the server supports JSON-RPC 3.0

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

### 4.5. Protocol Methods Example

**Scenario**: Client uses protocol methods for debugging and resource management.

**Step 1 - Client requests session ID:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "session_id",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "sessionId": "a3f2c891-5b44-4d2e-9a12-8c7f3e6d1b4a",
    "createdAt": "2025-10-27T15:00:00Z"
  },
  "id": 1
}
```

**Step 2 - Client creates multiple resources:**
```json
{
  "jsonrpc": "3.0",
  "method": "openDatabase",
  "params": {"name": "users"},
  "id": 2
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {"$ref": "db-1"},
  "id": 2
}
```

```json
{
  "jsonrpc": "3.0",
  "method": "openDatabase",
  "params": {"name": "products"},
  "id": 3
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {"$ref": "db-2"},
  "id": 3
}
```

**Step 3 - Client lists all active references:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "list_refs",
  "id": 4
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "local": [
      {"ref": "db-1", "type": "database", "created": "2025-10-27T15:00:05Z"},
      {"ref": "db-2", "type": "database", "created": "2025-10-27T15:00:08Z"}
    ],
    "remote": []
  },
  "id": 4
}
```

**Step 4 - Client gets detailed info about a specific reference:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "ref_info",
  "params": {"ref": "db-1"},
  "id": 5
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "ref": "db-1",
    "type": "database",
    "direction": "local",
    "created": "2025-10-27T15:00:05Z",
    "lastAccessed": "2025-10-27T15:00:10Z",
    "metadata": {
      "databaseName": "users",
      "connectionCount": 1
    }
  },
  "id": 5
}
```

**Step 5 - Client explicitly disposes one reference:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "dispose",
  "params": {"ref": "db-1"},
  "id": 6
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": null,
  "id": 6
}
```

**Step 6 - Client verifies reference was disposed:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "list_refs",
  "id": 7
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "local": [
      {"ref": "db-2", "type": "database", "created": "2025-10-27T15:00:08Z"}
    ],
    "remote": []
  },
  "id": 7
}
```

**Step 7 - Client disposes all remaining references:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "dispose_all",
  "id": 8
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "disposed": 1,
    "localDisposed": 1,
    "remoteDisposed": 0
  },
  "id": 8
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

### 5.6. CBOR Encoding

JSON-RPC 3.0 supports CBOR (Concise Binary Object Representation, RFC 8949) as an alternative encoding to JSON. CBOR provides more compact representation and efficient binary encoding while maintaining the same semantic structure.

#### 5.6.1. Encoding Selection

The encoding format is determined by the MIME type (Content-Type) used for the message:

- **`application/json`** (default): Standard JSON encoding with string keys
- **`application/cbor`**: CBOR encoding with string keys (same structure as JSON)
- **`application/cbor; format=compact`**: CBOR encoding with integer keys for top-level message fields

When no Content-Type is specified, `application/json` MUST be assumed. Implementations MAY support one, two, or all three encoding formats.

#### 5.6.2. Standard CBOR Mode (`application/cbor`)

In standard CBOR mode, messages are encoded exactly as they would be in JSON, but using CBOR binary format. All top-level keys remain as strings. This mode provides a drop-in replacement for JSON with better performance and smaller size while maintaining readability in debugging tools.

**Example Request (conceptual):**
```
Request with string keys in CBOR:
{
  "jsonrpc": "3.0",
  "method": "add",
  "params": [1, 2],
  "id": 1
}
```

#### 5.6.3. Compact CBOR Mode (`application/cbor; format=compact`)

In compact CBOR mode, top-level message fields use integer keys instead of strings for maximum efficiency. This mode is suitable for bandwidth-constrained or high-performance scenarios.

**Integer Key Mapping:**

| Field | Integer Key | Used In | Notes |
|-------|-------------|---------|-------|
| `jsonrpc` | 0 | Request, Response | Protocol version |
| `id` | 1 | Request, Response | Request/response correlation |
| `method` | 2 | Request | Method name |
| `params` | 3 | Request | Method parameters |
| `ref` | 4 | Request | Remote reference to invoke on |
| `result` | 5 | Response | Success result |
| `error` | 6 | Response | Error object |
| `code` | 7 | Error | Error code |
| `message` | 8 | Error | Error message |
| `data` | 9 | Error | Additional error data |
| `$ref` | 10 | LocalReference | Reference identifier |

**Example Request in Compact Mode (conceptual):**
```
Compact CBOR request:
{
  0: "3.0",        // jsonrpc
  2: "add",        // method
  3: [1, 2],       // params
  1: 1             // id
}
```

**Example Response in Compact Mode (conceptual):**
```
Compact CBOR response:
{
  0: "3.0",        // jsonrpc
  5: 3,            // result
  1: 1             // id
}
```

**Example Error Response in Compact Mode (conceptual):**
```
Compact CBOR error response:
{
  0: "3.0",                    // jsonrpc
  6: {                         // error
    7: -32601,                 // code
    8: "Method not found",     // message
    9: "add method not found"  // data
  },
  1: 1                         // id
}
```

**Example LocalReference in Compact Mode (conceptual):**
```
Compact CBOR local reference:
{
  10: "db-connection-1"        // $ref
}
```

#### 5.6.4. Nested Objects

**Important**: The integer key mapping applies ONLY to the top-level fields of Request, Response, Error, and LocalReference objects. All nested objects (such as `params`, `result`, or custom data structures) continue to use their natural string keys, even in compact mode.

For example, in compact mode:
```
{
  0: "3.0",               // jsonrpc (integer key)
  2: "openDatabase",      // method (integer key)
  3: {                    // params (integer key)
    "name": "mydb",       // nested: string key
    "readonly": false     // nested: string key
  },
  1: 1                    // id (integer key)
}
```

#### 5.6.5. Implementation Requirements

- Implementations that support CBOR MUST support `application/cbor` mode
- Support for `application/cbor; format=compact` is OPTIONAL
- Implementations MUST correctly handle the Content-Type header (or equivalent mechanism) to determine encoding
- When responding, implementations MUST use the same encoding as the request
- If an implementation receives a Content-Type it does not support, it SHOULD return an error (code -32700, Parse error)

#### 5.6.6. Batch Requests in CBOR

Batch requests in CBOR mode follow the same structure as JSON: an array of Request/Notification objects. In compact mode, each request in the array uses integer keys, while the array itself remains a standard CBOR array.

#### 5.6.7. Advantages of CBOR

- **Size**: CBOR typically produces 20-50% smaller messages than JSON
- **Speed**: Faster parsing and serialization than JSON
- **Binary data**: Native support for binary data without base64 encoding
- **Extensibility**: Supports additional data types (arbitrary precision numbers, tags, etc.)
- **Compact mode**: Integer keys provide additional 10-20% size reduction for protocol overhead

#### 5.6.8. Encoding Negotiation and Fallback

Clients and servers must negotiate which encoding to use. The mechanism varies by transport.

##### 5.6.8.1. HTTP Content Negotiation

For HTTP-based transports, standard HTTP content negotiation mechanisms SHOULD be used:

**Client Request Headers:**
```
Content-Type: application/cbor; format=compact
Accept: application/cbor; format=compact, application/cbor;q=0.8, application/json;q=0.5
```

- **`Content-Type`**: Specifies the encoding of the request body
- **`Accept`**: Specifies acceptable encodings for the response, with optional quality values (q)

**Server Response Headers:**
```
Content-Type: application/cbor; format=compact
```

- The server MUST respond using an encoding specified in the client's `Accept` header
- The server SHOULD use the highest quality encoding it supports

**Using `Expect: 100-continue`:**

For large request bodies, clients can use the `Expect` header to verify encoding support before sending the body:

```
POST /jsonrpc HTTP/1.1
Content-Type: application/cbor; format=compact
Content-Length: 50000
Expect: 100-continue
Accept: application/cbor; format=compact, application/cbor;q=0.8, application/json;q=0.5
```

**Server responses:**
- `100 Continue`: Encoding is acceptable, client may send the request body
- `417 Expectation Failed`: Encoding is not supported, client should retry with a different encoding
- `415 Unsupported Media Type`: Content-Type is not supported

##### 5.6.8.2. Transport-Agnostic Discovery

For non-HTTP transports or when HTTP headers are not available, use the `$rpc.mimetypes` protocol method:

**Step 1 - Discover supported encodings:**
```json
{
  "jsonrpc": "3.0",
  "ref": "$rpc",
  "method": "mimetypes",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "3.0",
  "result": [
    "application/cbor; format=compact",
    "application/cbor",
    "application/json"
  ],
  "id": 1
}
```

**Step 2 - Use preferred encoding for subsequent requests**

The client can now select the best encoding from the intersection of what it supports and what the server supports.

##### 5.6.8.3. Fallback Strategy

When a server rejects an encoding, the client MUST implement the following fallback strategy:

1. **First attempt**: Try `application/cbor; format=compact` (if client supports it)
2. **Second attempt**: Fall back to `application/cbor` (if client supports it)
3. **Final attempt**: Fall back to `application/json` (MUST be supported by all implementations)

**Rejection Indicators:**
- HTTP: `415 Unsupported Media Type` or `417 Expectation Failed`
- JSON-RPC: Error code `-32700` (Parse error) with data indicating encoding issues
- Connection closed or parse failure

**Example Fallback Flow:**

```
Client → Server: Request with Content-Type: application/cbor; format=compact
Server → Client: HTTP 415 Unsupported Media Type

Client → Server: Request with Content-Type: application/cbor
Server → Client: HTTP 415 Unsupported Media Type

Client → Server: Request with Content-Type: application/json
Server → Client: HTTP 200 OK
```

**Caching Encoding:**

- Clients SHOULD cache the successfully negotiated encoding for the session
- Clients MAY periodically retry higher-preference encodings to detect capability changes
- Cache invalidation SHOULD occur on connection close or explicit renegotiation

##### 5.6.8.4. Error Handling

**Unsupported Encoding Error:**

When a server cannot parse a message due to unsupported encoding, it SHOULD return:

```json
{
  "jsonrpc": "3.0",
  "error": {
    "code": -32700,
    "message": "Parse error",
    "data": "Unsupported encoding: application/cbor; format=compact. Supported: application/json"
  },
  "id": null
}
```

**Note**: The error response MUST use an encoding the client can understand. If unknown, the server SHOULD respond with `application/json`.

##### 5.6.8.5. Implementation Requirements

- All implementations MUST support `application/json`
- Implementations that support CBOR MUST support `application/cbor`
- Support for `application/cbor; format=compact` is OPTIONAL
- Servers MUST correctly identify encoding from Content-Type header or transport mechanism
- Servers MUST respond using the same encoding as the request (or fall back to JSON for error cases)
- Clients MUST implement fallback strategy when encoding negotiation fails

### 5.7. Enhanced Types

JSON-RPC 3.0 defines optional enhanced types that extend the basic JSON type system. Enhanced types use a special `$type` field to indicate how to interpret the object, similar to how `$ref` indicates a reference.

Enhanced types are OPTIONAL. Implementations that do not support enhanced types SHOULD treat them as regular objects. Implementations that support enhanced types MUST recognize the `$type` field and interpret the value accordingly.

#### 5.7.1. Enhanced Type Format

An enhanced type object has the following structure:

```json
{
  "$type": "type-name",
  ...type-specific fields...
}
```

The `$type` field MUST be a string identifying the type. Additional fields are type-specific.

**Reserved `$type` values:**
- `datetime`: Date and time values
- `bytes`: Binary data
- `bigint`: Arbitrary-precision integers
- `regexp`: Regular expressions

Implementations MAY define custom enhanced types using type names that do not conflict with reserved values. Custom type names SHOULD use a namespace prefix (e.g., `"myapp.customtype"`).

#### 5.7.2. DateTime Type

Represents a date and time value with optional timezone information.

**Format:**
```json
{
  "$type": "datetime",
  "value": "ISO-8601-string"
}
```

**Fields:**
- `value` (string, required): ISO 8601 formatted datetime string

**Examples:**

UTC datetime:
```json
{
  "$type": "datetime",
  "value": "2025-10-27T10:30:00Z"
}
```

Datetime with timezone offset:
```json
{
  "$type": "datetime",
  "value": "2025-10-27T10:30:00-07:00"
}
```

Datetime with milliseconds:
```json
{
  "$type": "datetime",
  "value": "2025-10-27T10:30:00.123Z"
}
```

**CBOR Encoding:**

In CBOR, datetime values MAY use CBOR tag 0 (Standard date/time string) or tag 1 (Epoch-based date/time) as an alternative to the enhanced type format. When using enhanced types in CBOR, the same JSON structure applies.

#### 5.7.3. Bytes Type

Represents binary data.

**Format:**
```json
{
  "$type": "bytes",
  "value": "base64-encoded-string"
}
```

**Fields:**
- `value` (string, required): Base64-encoded binary data (in JSON encoding)

**Example in JSON:**
```json
{
  "$type": "bytes",
  "value": "SGVsbG8gV29ybGQ="
}
```

This represents the byte sequence for "Hello World".

**CBOR Encoding:**

In CBOR, the `value` field SHOULD be encoded as a native CBOR byte string (major type 2) rather than base64:

```
{
  "$type": "bytes",
  "value": h'48656c6c6f20576f726c64'
}
```

Alternatively, CBOR implementations MAY use CBOR tag 24 (Encoded CBOR data item) for nested CBOR data.

#### 5.7.4. BigInt Type

Represents arbitrary-precision integers that exceed the range of standard JSON numbers.

**Format:**
```json
{
  "$type": "bigint",
  "value": "decimal-string"
}
```

**Fields:**
- `value` (string, required): Decimal string representation of the integer

**Examples:**

Large positive integer:
```json
{
  "$type": "bigint",
  "value": "12345678901234567890123456789"
}
```

Large negative integer:
```json
{
  "$type": "bigint",
  "value": "-98765432109876543210987654321"
}
```

**CBOR Encoding:**

In CBOR, bigint values MAY use CBOR tag 2 (Unsigned bignum) or tag 3 (Negative bignum) as defined in RFC 8949, section 3.4.3. When using enhanced types in CBOR, the string representation is still valid.

**Note:** The `value` MUST be a valid decimal integer (no decimal point, no scientific notation). Leading zeros are allowed but discouraged.

#### 5.7.5. RegExp Type

Represents a regular expression pattern with optional flags.

**Format:**
```json
{
  "$type": "regexp",
  "pattern": "regex-pattern-string",
  "flags": "flag-string"
}
```

**Fields:**
- `pattern` (string, required): The regular expression pattern
- `flags` (string, optional): Regular expression flags (e.g., "i", "g", "m")

**Examples:**

Simple pattern:
```json
{
  "$type": "regexp",
  "pattern": "\\d+"
}
```

Pattern with flags:
```json
{
  "$type": "regexp",
  "pattern": "[a-z]+",
  "flags": "gi"
}
```

Email validation pattern:
```json
{
  "$type": "regexp",
  "pattern": "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
}
```

**Flag Interpretation:**

Common flags (interpretation is implementation-defined):
- `i`: Case-insensitive matching
- `g`: Global matching (find all matches)
- `m`: Multi-line mode
- `s`: Dot matches newline
- `u`: Unicode mode
- `x`: Extended/verbose mode (ignore whitespace)

Implementations SHOULD document which flags they support and how they interpret them.

**CBOR Encoding:**

CBOR may use tag 35 (Regular expression) as defined in RFC 7049. When using enhanced types in CBOR, the same JSON structure applies.

#### 5.7.6. Using Enhanced Types

Enhanced types can appear anywhere a value is expected: in `params`, `result`, or nested within other structures.

**Example - Method with enhanced type parameters:**
```json
{
  "jsonrpc": "3.0",
  "method": "scheduleEvent",
  "params": {
    "name": "Meeting",
    "startTime": {
      "$type": "datetime",
      "value": "2025-10-28T14:00:00Z"
    },
    "duration": 3600,
    "attachment": {
      "$type": "bytes",
      "value": "UERGLTEuNA0KJeLjz9MNCjEgMCBvYmoNPDwvVHlwZS9DYXRh"
    }
  },
  "id": 1
}
```

**Example - Result with enhanced types:**
```json
{
  "jsonrpc": "3.0",
  "result": {
    "id": {
      "$type": "bigint",
      "value": "999999999999999999999"
    },
    "createdAt": {
      "$type": "datetime",
      "value": "2025-10-27T10:30:00Z"
    },
    "pattern": {
      "$type": "regexp",
      "pattern": "^[A-Z]{3}-\\d{4}$",
      "flags": "i"
    }
  },
  "id": 1
}
```

#### 5.7.7. Compatibility and Fallback

**Non-Supporting Implementations:**

Implementations that do not support enhanced types will treat them as regular objects. For example, a `datetime` enhanced type will be seen as:

```json
{
  "$type": "datetime",
  "value": "2025-10-27T10:30:00Z"
}
```

The application code would need to manually check for the `$type` field and interpret it.

**Partial Support:**

Implementations MAY support some enhanced types but not others. When encountering an unknown `$type`, implementations SHOULD either:
1. Treat it as a regular object and pass it through to the application
2. Return an error if the method requires support for that type

**Recommendations:**

- Use enhanced types when precision or type safety is important
- Provide fallback behavior for clients that don't support enhanced types
- Document which enhanced types your implementation supports
- Consider using enhanced types in combination with schema validation

#### 5.7.8. Implementation Requirements

- Enhanced type support is OPTIONAL
- Implementations that support enhanced types MUST correctly interpret the `$type` field
- The `$type` field MUST NOT be used for any purpose other than indicating enhanced types
- Implementations SHOULD validate enhanced type values (e.g., ensure `datetime` values are valid ISO 8601)
- Invalid enhanced type values SHOULD result in an Invalid params error (-32602)
- Implementations MAY define custom enhanced types but MUST NOT use reserved type names

### 5.8. HTTP Session Management

When JSON-RPC 3.0 is used over HTTP, servers MAY implement session persistence using the `RPC-Session-Id` header. This allows clients to maintain stateful sessions across multiple HTTP requests, enabling features like object references to persist between calls.

#### 5.8.1. RPC-Session-Id Header

The `RPC-Session-Id` header is an OPTIONAL mechanism for HTTP-based transports to maintain session state between the client and server.

**Server Behavior:**

When a server supports session persistence:

1. The server SHOULD include an `RPC-Session-Id` header in the response with a unique session identifier
2. The session identifier MUST be unique per session
3. The server MUST maintain session state (e.g., object references) associated with this identifier
4. The server SHOULD accept the same session identifier in subsequent requests to resume the session

**Client Behavior:**

When a client receives an `RPC-Session-Id` header:

1. The client SHOULD include the same `RPC-Session-Id` header value in subsequent requests to resume the session
2. The client MAY omit the header to start a new session
3. The client MAY clear the session by omitting the header or using a different session ID

**Example:**

Initial request (no session):
```http
POST /rpc HTTP/1.1
Content-Type: application/json

{"jsonrpc":"3.0","method":"createCounter","id":1}
```

Server response with session:
```http
HTTP/1.1 200 OK
Content-Type: application/json
RPC-Session-Id: 550e8400-e29b-41d4-a716-446655440000

{"jsonrpc":"3.0","result":{"$ref":"ref-1"},"id":1}
```

Subsequent request using the same session:
```http
POST /rpc HTTP/1.1
Content-Type: application/json
RPC-Session-Id: 550e8400-e29b-41d4-a716-446655440000

{"jsonrpc":"3.0","ref":"ref-1","method":"increment","id":2}
```

#### 5.8.2. Session Lifecycle

**Session Creation:**

- Servers MAY create a new session for any request that does not include a valid `RPC-Session-Id` header
- Servers are NOT required to create sessions for all requests (sessions may only be created when needed)
- Servers that create sessions MUST include the `RPC-Session-Id` header in the response

**Session Resumption:**

- When a client sends a request with an `RPC-Session-Id` header, the server SHOULD resume the corresponding session
- If the session ID is not found or has expired, the server SHOULD either:
  1. Create a new session and return a new `RPC-Session-Id`, OR
  2. Return an error indicating the session is invalid

**Session Termination:**

- Servers MAY expire sessions after a period of inactivity
- Servers MAY provide a method for clients to explicitly terminate sessions (e.g., via HTTP DELETE request or a special RPC method)
- Object references within a session become invalid when the session terminates

#### 5.8.3. Session Optimization

Servers MAY optimize session storage by:

- Only creating sessions when object references are returned
- Not storing sessions for purely stateless request/response interactions
- Expiring sessions without active object references more aggressively than sessions with references
- Providing different time-to-live (TTL) values for different types of sessions

**Example optimization strategy:**

- Sessions with object references: 5 minute TTL
- Sessions without references: 30 second TTL or no persistence

#### 5.8.4. Implementation Requirements

- Session support via `RPC-Session-Id` is OPTIONAL
- If implemented, servers MUST use the `RPC-Session-Id` header name
- Session identifiers SHOULD be globally unique (e.g., UUIDs)
- Session identifiers MUST be treated as opaque strings by clients
- Servers MUST handle missing or invalid session IDs gracefully
- Implementations SHOULD document their session behavior (TTL, cleanup policy, etc.)
- Object references (`$ref`) REQUIRE session support to function properly across multiple requests

#### 5.8.5. Security Considerations

- Session identifiers SHOULD be cryptographically random and unpredictable
- Servers SHOULD implement rate limiting to prevent session exhaustion attacks
- Servers SHOULD expire inactive sessions to prevent resource exhaustion
- Session identifiers MUST NOT contain sensitive information
- HTTPS SHOULD be used when sessions contain sensitive data or object references
- Servers MAY implement additional authentication/authorization for session access

### 5.9. Persistent Stream-Based Sessions

When JSON-RPC 3.0 is used over persistent bidirectional streams (such as WebSockets, TCP connections, stdio, or other streaming transports), session management works differently than with HTTP:

**Stream-Session Binding:**

- Both client and server SHOULD bind a session to each persistent stream automatically
- The session SHOULD be implicitly created when the stream is established
- No explicit session identifiers or headers are needed since the stream itself identifies the session
- All messages exchanged over the stream are part of the same session

**Session Lifecycle:**

- **Creation**: A session is created automatically when the stream connection is established
- **Active**: The session remains active for the entire duration of the stream
- **Termination**: The session MUST be automatically disposed when the stream terminates (closes, disconnects, or errors)

**Session Disposal:**

When a stream terminates:

1. Both client and server MUST dispose of the session associated with that stream
2. All object references (both local and remote) in that session become invalid
3. Any lifecycle methods (such as `Dispose()` or `Close()`) on referenced objects SHOULD be called
4. Implementations MUST clean up all resources associated with the session

**Example Scenarios:**

- **WebSocket**: Each WebSocket connection has one session; when the WebSocket closes, the session is disposed
- **TCP Connection**: Each TCP connection has one session; when the connection terminates, the session is disposed
- **Process stdio**: A parent-child process communication over stdin/stdout has one session; when either process terminates, the session is disposed
- **Unix Domain Socket**: Each socket connection has one session; when the socket closes, the session is disposed

**Benefits:**

- Simpler session management (no need for session identifiers or TTL)
- Automatic cleanup (session disposal is tied to connection lifecycle)
- Natural request/response correlation within a single stream
- Efficient for long-lived connections with multiple RPC calls

**Comparison with HTTP Sessions:**

| Aspect | HTTP Sessions | Stream Sessions |
|--------|--------------|-----------------|
| Session ID | Required (RPC-Session-Id header) | Not needed (stream identifies session) |
| Lifetime | TTL-based, expires after inactivity | Connection-based, lasts as long as stream is open |
| Cleanup | Timeout or explicit DELETE | Automatic on stream close |
| Use Case | Stateless HTTP request/response | Long-lived bidirectional communication |

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

JSON-RPC 3.0 extends the grammar to support version "3.0", adds the `ref` field for remote object invocation, adds reference objects for passing local references, and adds the `context` field for contextual information:

```
Version       := "2.0" | "3.0"
Request       := {jsonrpc: Version, ref?: string, method: string, params?: array | object, id?: string | number | null, context?: object}
Response      := SuccessResponse | ErrorResponse
SuccessResponse := {jsonrpc: Version, result: any, id: string | number | null, context?: object}
ErrorResponse := {jsonrpc: Version, error: Error, id: string | number | null, context?: object}
Error         := {code: number, message: string, data?: any}
Notification  := {jsonrpc: Version, ref?: string, method: string, params?: array | object, context?: object}
Batch         := Array<Request | Notification>
LocalReference := {$ref: string}
```

Where:
- `Version` in a response MUST match the version in the corresponding request (except when returning an error for unsupported version)
- `ref` (optional, top-level): Specifies a remote reference to invoke the method on. This is the ONLY place where remote references can be specified. If present, the method is invoked on the referenced object controlled by the other party.
- `context` (optional, top-level): An object containing contextual information such as trace IDs, authentication tokens, or application-specific metadata. Both requests and responses can include context.
- `LocalReference` (as `{$ref: string}`): Used in `params` or `result` to pass references to objects controlled by the caller. These are references being passed from caller to callee.
- Reference identifiers (both in `ref` and `LocalReference`) MUST be non-empty strings and MUST be unique within the session for references created by the same party
- The reference identifier `"$rpc"` is reserved for protocol methods (see section 3.4) and MUST NOT be used as a user-defined reference
- `LocalReference` objects MUST only have a single `$ref` member (no additional properties)

## 7. Conclusion

JSON-RPC 3.0 extends JSON-RPC 2.0 with powerful capabilities for building distributed, object-oriented systems while maintaining backward compatibility. Object references enable remote object manipulation without exposing implementation details, and bidirectional calls enable reactive, event-driven architectures.

Implementations should carefully consider the security implications of these features and implement appropriate safeguards. The session-based lifecycle of references provides a simple and predictable model for resource management.

For further information, examples, and implementation guidance, refer to the JSON-RPC 3.0 implementation guide (if available) or the JSON-RPC 2.0 specification at https://www.jsonrpc.org/specification.

---

**Document Version**: 1.0
**Date**: 2025-10-27
**Status**: Draft Specification
