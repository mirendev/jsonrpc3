"""Tests for HTTP client and server."""

import time
import pytest

from jsonrpc3 import (
    Session,
    MethodMap,
    Handler,
    HttpServer,
    HttpClient,
    RpcError,
    CODE_METHOD_NOT_FOUND,
)


@pytest.fixture
def server_and_client():
    """Create HTTP server and client for testing."""
    # Create server
    server_session = Session()
    root = MethodMap()

    def add_handler(params, caller):
        data = params.decode()
        return data["a"] + data["b"]

    root.register("add", add_handler)

    def echo_handler(params, caller):
        return params.decode()

    root.register("echo", echo_handler)

    from jsonrpc3 import NoOpCaller
    handler = Handler(server_session, root, NoOpCaller())
    server = HttpServer(handler, port=0)  # Random port
    server.start()

    # Wait for server to be ready
    time.sleep(0.5)

    # Create client
    client_session = Session()
    client = HttpClient(server.url, client_session)

    yield server, client

    # Cleanup
    server.stop()


def test_simple_call(server_and_client):
    """Test simple RPC call."""
    server, client = server_and_client
    result = client.call("add", {"a": 10, "b": 20})
    assert result == 30


def test_echo(server_and_client):
    """Test echo method."""
    server, client = server_and_client
    data = {"foo": "bar", "num": 42}
    result = client.call("echo", data)
    assert result == data


def test_object_reference(server_and_client):
    """Test object references."""
    server, client = server_and_client

    # Add counter factory to server
    server_session = server.handler.session
    root = server.handler.root_object

    def create_counter_handler(params, caller):
        counter = MethodMap()
        count = [0]  # Use list for mutability

        def increment_handler(params, caller):
            count[0] += 1
            return count[0]

        def get_value_handler(params, caller):
            return count[0]

        counter.register("increment", increment_handler)
        counter.register("getValue", get_value_handler)

        return counter

    root.register("createCounter", create_counter_handler)

    # Create counter
    counter_ref = client.call("createCounter")
    assert isinstance(counter_ref, dict)
    assert "$ref" in counter_ref

    # Increment
    val1 = client.call("increment", None, counter_ref)
    assert val1 == 1

    val2 = client.call("increment", None, counter_ref)
    assert val2 == 2

    # Get value
    final = client.call("getValue", None, counter_ref)
    assert final == 2


def test_protocol_methods(server_and_client):
    """Test $rpc protocol methods."""
    server, client = server_and_client

    # Session ID
    session_info = client.call("session_id", None, "$rpc")
    assert "session_id" in session_info
    assert session_info["session_id"] == server.handler.session.id

    # Capabilities
    caps = client.call("capabilities", None, "$rpc")
    assert "references" in caps
    assert "batch-local-references" in caps
    assert "bidirectional-calls" in caps
    assert "introspection" in caps


def test_method_not_found(server_and_client):
    """Test method not found error."""
    server, client = server_and_client

    with pytest.raises(RpcError) as exc_info:
        client.call("nonexistent")

    assert exc_info.value.code == CODE_METHOD_NOT_FOUND


def test_notification(server_and_client):
    """Test notification (no response expected)."""
    server, client = server_and_client

    # Should not raise error and return None
    result = client.notify("add", {"a": 1, "b": 2})
    assert result is None


def test_batch_requests(server_and_client):
    """Test batch requests."""
    from jsonrpc3 import new_request

    server, client = server_and_client

    req1 = new_request("add", {"a": 1, "b": 2}, 1, None)
    req2 = new_request("add", {"a": 10, "b": 20}, 2, None)

    responses = client.batch([req1, req2])
    assert len(responses) == 2

    # Find responses by ID
    resp1 = next(r for r in responses if r.id == 1)
    resp2 = next(r for r in responses if r.id == 2)

    assert resp1.result == 3
    assert resp2.result == 30
