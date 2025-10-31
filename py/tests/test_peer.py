"""Tests for async Peer transport."""

import asyncio
import pytest

from jsonrpc3 import (
    Session,
    MethodMap,
    Peer,
    RpcError,
    CODE_METHOD_NOT_FOUND,
)


@pytest.fixture
async def peer_pair():
    """Create a pair of connected peers for testing."""
    # Create a simple server
    async def handle_client(reader, writer):
        # This will be our "server" peer
        server_root = MethodMap()

        def add_handler(params):
            data = params.decode()
            return data["a"] + data["b"]

        server_root.register("add", add_handler)

        def echo_handler(params):
            return params.decode()

        server_root.register("echo", echo_handler)

        server_session = Session()
        server_peer = Peer(reader, writer, server_root, server_session)
        await server_peer.start()
        await server_peer.wait()

    server = await asyncio.start_server(handle_client, "127.0.0.1", 0)
    server_addr = server.sockets[0].getsockname()

    # Wait a bit for server to be ready
    await asyncio.sleep(0.1)

    # Create client peer
    reader, writer = await asyncio.open_connection(server_addr[0], server_addr[1])

    client_root = MethodMap()
    client_session = Session()
    client_peer = Peer(reader, writer, client_root, client_session)
    await client_peer.start()

    yield client_peer, server

    # Cleanup
    await client_peer.close()
    server.close()
    await server.wait_closed()


async def test_peer_simple_call(peer_pair):
    """Test simple RPC call over peer connection."""
    client_peer, server = peer_pair

    result = await client_peer.call("add", {"a": 5, "b": 3})
    assert result == 8


async def test_peer_echo(peer_pair):
    """Test echo method over peer."""
    client_peer, server = peer_pair

    data = {"foo": "bar", "num": 42}
    result = await client_peer.call("echo", data)
    assert result == data


async def test_peer_notification(peer_pair):
    """Test notification (no response expected)."""
    client_peer, server = peer_pair

    # Should not raise error and return None
    result = await client_peer.notify("add", {"a": 1, "b": 2})
    assert result is None


async def test_peer_method_not_found(peer_pair):
    """Test method not found error."""
    client_peer, server = peer_pair

    with pytest.raises(RpcError) as exc_info:
        await client_peer.call("nonexistent")

    assert exc_info.value.code == CODE_METHOD_NOT_FOUND


async def test_peer_object_reference(peer_pair):
    """Test object reference registration and unregistration."""
    client_peer, server = peer_pair

    # Register object directly on client side
    counter = MethodMap()
    count = [0]

    def increment_handler(params):
        count[0] += 1
        return count[0]

    def get_value_handler(params):
        return count[0]

    counter.register("increment", increment_handler)
    counter.register("getValue", get_value_handler)

    # Register the counter object
    ref = client_peer.register_object(counter)
    assert ref is not None
    assert client_peer.session.has_local_ref(ref)

    # Unregister it
    result = client_peer.unregister_object(ref)
    assert result is True
    assert not client_peer.session.has_local_ref(ref)


async def test_peer_close(peer_pair):
    """Test peer close and cleanup."""
    client_peer, server = peer_pair

    # Close the peer
    await client_peer.close()

    # Should raise error after close
    with pytest.raises(RuntimeError, match="Connection closed"):
        await client_peer.call("test")


async def test_peer_concurrent_calls(peer_pair):
    """Test concurrent RPC calls."""
    client_peer, server = peer_pair

    # Make multiple concurrent calls
    tasks = [
        client_peer.call("add", {"a": i, "b": i})
        for i in range(5)
    ]

    results = await asyncio.gather(*tasks)

    assert results == [0, 2, 4, 6, 8]
