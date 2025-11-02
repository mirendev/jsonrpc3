"""Tests for WebSocket client and server."""

import asyncio
import pytest

import jsonrpc3
from jsonrpc3 import (
    WebSocketClient,
    WebSocketHandler,
    MethodMap,
    RpcError,
    CODE_METHOD_NOT_FOUND,
    CODE_CONNECTION_CLOSED,
    Reference,
)


# Test server root object
server_root = MethodMap()


def add(params, caller):
    data = params.decode()
    return data["a"] + data["b"]


def echo(params, caller):
    return params.decode()


def ping(params, caller):
    return "pong"


def create_counter(params, caller):
    """Create a counter object with increment and getValue methods."""
    counter = MethodMap()
    count = {"value": 0}  # Use dict to avoid closure issues

    def increment(p, caller):
        count["value"] += 1
        return count["value"]

    def get_value(p, caller):
        return count["value"]

    counter.register("increment", increment)
    counter.register("getValue", get_value)

    return counter


# Register methods
server_root.register("add", add)
server_root.register("echo", echo)
server_root.register("ping", ping)
server_root.register("createCounter", create_counter)


@pytest.fixture
async def websocket_server():
    """Start WebSocket server on available port."""
    handler = WebSocketHandler(server_root)

    # Find available port
    import socket
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]

    # Start server
    import websockets
    server = await websockets.serve(
        handler.handle_connection,
        "127.0.0.1",
        port,
        subprotocols=handler._subprotocols,
    )

    # Wait for server to start
    await asyncio.sleep(0.1)

    yield f"ws://127.0.0.1:{port}", handler

    # Cleanup
    server.close()
    await server.wait_closed()
    await handler.close_all()


@pytest.mark.asyncio
async def test_simple_call(websocket_server):
    """Test simple method call."""
    url, _ = websocket_server

    client = WebSocketClient(url)
    await client.connect()

    try:
        result = await client.call("add", {"a": 10, "b": 20})
        assert result == 30
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_echo(websocket_server):
    """Test echo method."""
    url, _ = websocket_server

    client = WebSocketClient(url)
    await client.connect()

    try:
        data = {"foo": "bar", "num": 42}
        result = await client.call("echo", data)
        assert result == data
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_ping(websocket_server):
    """Test ping method."""
    url, _ = websocket_server

    client = WebSocketClient(url)
    await client.connect()

    try:
        result = await client.call("ping")
        assert result == "pong"
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_notification(websocket_server):
    """Test notification (no response expected)."""
    url, _ = websocket_server

    client = WebSocketClient(url)
    await client.connect()

    try:
        # Should not raise error and return None
        result = await client.notify("ping")
        assert result is None
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_method_not_found(websocket_server):
    """Test method not found error."""
    url, _ = websocket_server

    client = WebSocketClient(url)
    await client.connect()

    try:
        with pytest.raises(RpcError) as exc_info:
            await client.call("nonexistent")
        assert exc_info.value.code == CODE_METHOD_NOT_FOUND
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_object_registration(websocket_server):
    """Test object registration and method calls."""
    url, _ = websocket_server

    client = WebSocketClient(url)
    await client.connect()

    try:
        # Create counter
        counter_ref = await client.call("createCounter")
        assert isinstance(counter_ref, dict)
        assert "$ref" in counter_ref

        # Increment
        val1 = await client.call("increment", None, counter_ref)
        assert val1 == 1

        val2 = await client.call("increment", None, counter_ref)
        assert val2 == 2

        # Get value
        final = await client.call("getValue", None, counter_ref)
        assert final == 2
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_bidirectional_server_to_client_call(websocket_server):
    """Test bidirectional communication setup."""
    url, _ = websocket_server

    # Register method on client
    client_object = MethodMap()

    def client_echo(params, caller):
        data = params.decode()
        return f"client received: {data}"

    client_object.register("clientEcho", client_echo)

    client = WebSocketClient(url, root_object=client_object)
    await client.connect()

    try:
        # For now, just test that bidirectional setup works
        result = await client.call("ping")
        assert result == "pong"
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_concurrent_requests(websocket_server):
    """Test concurrent requests."""
    url, _ = websocket_server

    client = WebSocketClient(url)
    await client.connect()

    try:
        # Make multiple concurrent requests
        async def make_call(i):
            result = await client.call("add", {"a": i, "b": i})
            assert result == i * 2
            return result

        tasks = [make_call(i) for i in range(5)]
        results = await asyncio.gather(*tasks)

        assert results == [0, 2, 4, 6, 8]
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_client_register_object(websocket_server):
    """Test registering objects on client."""
    url, _ = websocket_server

    client_object = MethodMap()
    received_messages = []

    def receive_message(params, caller):
        msg = params.decode()
        received_messages.append(msg)
        return "received"

    client_object.register("receiveMessage", receive_message)

    client = WebSocketClient(url, root_object=client_object)
    await client.connect()

    try:
        # Register the client object and get a reference
        ref = client.register_object(client_object)
        assert isinstance(ref, Reference)
        assert ref.ref is not None
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_client_unregister_object(websocket_server):
    """Test unregistering objects on client."""
    url, _ = websocket_server

    client_object = MethodMap()

    def test_method(params, caller):
        return "ok"

    client_object.register("test", test_method)

    client = WebSocketClient(url, root_object=client_object)
    await client.connect()

    try:
        # Register and then unregister
        ref = client.register_object(client_object)
        assert client.session.has_local_ref(ref.ref)

        client.unregister_object(ref)
        assert not client.session.has_local_ref(ref.ref)
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_connection_close(websocket_server):
    """Test connection close."""
    url, _ = websocket_server

    client = WebSocketClient(url)
    await client.connect()

    # Make a call to ensure connection works
    result = await client.call("ping")
    assert result == "pong"

    # Close connection
    await client.close()
    assert client.closed()

    # Further calls should raise error
    with pytest.raises(RpcError) as exc_info:
        await client.call("ping")
    assert exc_info.value.code == CODE_CONNECTION_CLOSED


@pytest.mark.asyncio
async def test_reference_convenience_methods(websocket_server):
    """Test Reference convenience methods."""
    url, _ = websocket_server

    client = WebSocketClient(url)
    await client.connect()

    try:
        # Create counter and get reference
        counter_ref_dict = await client.call("createCounter")

        # Convert to Reference object
        counter_ref = Reference.from_dict(counter_ref_dict)

        # Use convenience call method
        val1 = await counter_ref.async_call(client, "increment")
        assert val1 == 1

        val2 = await counter_ref.async_call(client, "increment")
        assert val2 == 2

        # Get value
        final = await counter_ref.async_call(client, "getValue")
        assert final == 2
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_error_handling(websocket_server):
    """Test error handling."""
    url, _ = websocket_server

    # Register divide method with error
    def divide(params, caller):
        data = params.decode()
        a = data["a"]
        b = data["b"]

        if b == 0:
            raise RpcError("Division by zero", code=-32000)

        return a / b

    server_root.register("divide", divide)

    client = WebSocketClient(url)
    await client.connect()

    try:
        # Test successful division
        result = await client.call("divide", {"a": 10, "b": 2})
        assert result == 5

        # Test division by zero error
        with pytest.raises(RpcError) as exc_info:
            await client.call("divide", {"a": 10, "b": 0})
        assert exc_info.value.code == -32000
        assert str(exc_info.value) == "Division by zero"
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_session_persistence(websocket_server):
    """Test session persistence across calls."""
    url, _ = websocket_server

    client = WebSocketClient(url)
    await client.connect()

    try:
        # Session should be maintained across multiple calls
        session_id1 = client.session.id
        await client.call("ping")

        session_id2 = client.session.id
        await client.call("ping")

        assert session_id1 == session_id2
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_multiple_clients(websocket_server):
    """Test multiple independent clients."""
    url, _ = websocket_server

    # Create two independent clients
    client1 = WebSocketClient(url)
    client2 = WebSocketClient(url)

    await client1.connect()
    await client2.connect()

    try:
        # Both clients should work independently
        result1 = await client1.call("ping")
        result2 = await client2.call("ping")

        assert result1 == "pong"
        assert result2 == "pong"

        # They should have different sessions
        assert client1.session.id != client2.session.id
    finally:
        await client1.close()
        await client2.close()
