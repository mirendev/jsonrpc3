# JSON-RPC 3.0 Python Implementation

Complete Python implementation of JSON-RPC 3.0 with support for object references, batch requests, and bidirectional communication.

## Features

- **Object References**: Call methods on remote objects
- **Batch Requests**: Send multiple requests in a single call
- **Batch-Local References**: Reference results within the same batch
- **Bidirectional Communication**: Server can call methods on client (Peer transport)
- **HTTP Transport**: Simple HTTP client/server
- **Async Peer Transport**: Asyncio-based bidirectional streams
- **Protocol Methods**: Built-in `$rpc` methods for introspection and session management

## Installation

```bash
# Using uv
uv add jsonrpc3

# Using pip
pip install jsonrpc3
```

## Quick Start

```python
from jsonrpc3 import Session, MethodMap, Handler, HttpServer, HttpClient

# Create server
server_session = Session()
root = MethodMap()

def add_handler(params):
    data = params.decode()
    return data["a"] + data["b"]

root.register("add", add_handler)

handler = Handler(server_session, root)
server = HttpServer(handler, port=3000)
server.start()

# Create client
client = HttpClient("http://localhost:3000")
result = client.call("add", {"a": 5, "b": 3})
print(result)  # 8

server.stop()
```

## Requirements

- Python >= 3.8

## License

MIT
