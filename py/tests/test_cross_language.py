"""Cross-language integration tests.

Tests Python clients against Go servers.
"""

import asyncio
import socket
import subprocess
import time
from pathlib import Path

import pytest
from jsonrpc3.http_client import HttpClient
from jsonrpc3.websocket_client import WebSocketClient
from jsonrpc3.batch import execute_batch
from jsonrpc3.types import Request


HTTP_PORT = 18085
WS_PORT = 18086
PEER_PORT = 18087


def wait_for_port(port: int, max_attempts: int = 30, delay_ms: int = 100) -> None:
    """Wait for a port to be ready."""
    for i in range(max_attempts):
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(0.1)
            sock.connect(("localhost", port))
            sock.close()
            return
        except (socket.error, OSError):
            if i == max_attempts - 1:
                raise TimeoutError(f"Port {port} not ready after {max_attempts} attempts")
            time.sleep(delay_ms / 1000)


@pytest.fixture(scope="session", autouse=True)
def go_server():
    """Start Go test server for session."""
    # Find Go test server
    test_dir = Path(__file__).parent
    go_server_path = test_dir.parent.parent / "go" / "jsonrpc3" / "testserver"

    if not go_server_path.exists():
        pytest.skip(f"Go test server not found at {go_server_path}")

    # Start server
    process = subprocess.Popen(
        [str(go_server_path), "-http", str(HTTP_PORT), "-ws", str(WS_PORT), "-peer", str(PEER_PORT)],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )

    # Wait for servers to be ready
    try:
        wait_for_port(HTTP_PORT)
        time.sleep(0.5)  # Wait for WebSocket server
        print(f"\nGo test server started on HTTP port {HTTP_PORT}, WS port {WS_PORT}")
    except TimeoutError as e:
        process.terminate()
        process.wait()
        pytest.fail(str(e))

    yield

    # Cleanup
    process.terminate()
    process.wait()
    print("\nGo test server stopped")


class TestCrossLanguageHTTP:
    """Test Python HTTP client against Go HTTP server."""

    def test_python_http_client_can_call_go_server_add(self):
        """Test basic add method."""
        client = HttpClient(f"http://localhost:{HTTP_PORT}")

        result = client.call("add", [2, 3])
        assert result == 5

    def test_python_http_client_can_call_go_server_echo(self):
        """Test echo method."""
        client = HttpClient(f"http://localhost:{HTTP_PORT}")

        data = {"message": "hello", "value": 42}
        result = client.call("echo", data)
        assert result == data

    def test_python_http_client_can_use_batch_with_references(self):
        """Test batch requests with object references."""
        client = HttpClient(f"http://localhost:{HTTP_PORT}")

        # Use batch to keep everything in same request
        def build_batch(b):
            counter = b.call("createCounter", None)
            counter.call("increment", None)
            counter.call("increment", None)
            counter.call("getValue", None)

        results = execute_batch(client, build_batch)

        assert len(results) == 4

        # First result should be a reference
        counter_ref = results.get_result(0)
        assert isinstance(counter_ref, dict)
        assert "$ref" in counter_ref

        # Subsequent results should be increment values
        assert results.get_result(1) == 1
        assert results.get_result(2) == 2
        assert results.get_result(3) == 2

    def test_python_http_client_can_send_notifications(self):
        """Test sending notifications."""
        client = HttpClient(f"http://localhost:{HTTP_PORT}")

        # Should not raise
        client.notify("add", [1, 2])

    def test_python_http_client_can_send_batch_requests(self):
        """Test sending batch requests."""
        client = HttpClient(f"http://localhost:{HTTP_PORT}")

        requests = [
            Request(jsonrpc="3.0", method="add", params=[1, 1], id=1),
            Request(jsonrpc="3.0", method="add", params=[2, 2], id=2),
            Request(jsonrpc="3.0", method="add", params=[3, 3], id=3),
        ]

        responses = client.batch(requests)
        assert len(responses) == 3

        results = [r.result for r in responses]
        assert results == [2, 4, 6]

    def test_python_http_client_can_use_batch_builder(self):
        """Test using batch builder."""
        client = HttpClient(f"http://localhost:{HTTP_PORT}")

        def build_batch(b):
            b.call("add", [1, 2])
            b.call("add", [3, 4])
            b.call("add", [5, 6])

        results = execute_batch(client, build_batch)

        assert len(results) == 3
        assert results.get_result(0) == 3
        assert results.get_result(1) == 7
        assert results.get_result(2) == 11


class TestCrossLanguageWebSocket:
    """Test Python WebSocket client against Go WebSocket server."""

    @pytest.mark.asyncio
    async def test_python_ws_client_can_call_go_server_add(self):
        """Test basic add method via WebSocket."""
        client = WebSocketClient(f"ws://localhost:{WS_PORT}")
        await client.connect()

        try:
            result = await client.call("add", [2, 3])
            assert result == 5
        finally:
            await client.close()

    @pytest.mark.asyncio
    async def test_python_ws_client_can_call_go_server_echo(self):
        """Test echo method via WebSocket."""
        client = WebSocketClient(f"ws://localhost:{WS_PORT}")
        await client.connect()

        try:
            data = {"message": "hello", "value": 42}
            result = await client.call("echo", data)
            assert result == data
        finally:
            await client.close()

    @pytest.mark.asyncio
    async def test_python_ws_client_can_create_and_use_counter(self):
        """Test creating and using counter object via WebSocket."""
        client = WebSocketClient(f"ws://localhost:{WS_PORT}")
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
    async def test_python_ws_client_can_send_notifications(self):
        """Test sending notifications via WebSocket."""
        client = WebSocketClient(f"ws://localhost:{WS_PORT}")
        await client.connect()

        try:
            # Should not raise
            await client.notify("add", [1, 2])
        finally:
            await client.close()

    @pytest.mark.asyncio
    async def test_python_ws_client_can_query_go_server_methods(self):
        """Test querying server methods via WebSocket."""
        client = WebSocketClient(f"ws://localhost:{WS_PORT}")
        await client.connect()

        try:
            # Query available methods
            methods = await client.call("$methods")
            assert isinstance(methods, list)

            # Verify methods is an array of objects
            method_names = [m["name"] for m in methods]
            assert "add" in method_names
            assert "echo" in method_names
            assert "createCounter" in method_names
            assert "$methods" in method_names
            assert "$type" in method_names

            # Find add method and check it has metadata
            add_method = next((m for m in methods if m["name"] == "add"), None)
            assert add_method is not None
            assert add_method["description"] == "Adds a list of numbers"
            assert add_method["params"] == ["number"]
        finally:
            await client.close()

    @pytest.mark.asyncio
    async def test_python_ws_client_can_query_go_counter_introspection(self):
        """Test querying counter introspection via WebSocket."""
        client = WebSocketClient(f"ws://localhost:{WS_PORT}")
        await client.connect()

        try:
            # Create a counter
            counter_ref = await client.call("createCounter")
            assert isinstance(counter_ref, dict)
            assert "$ref" in counter_ref

            # Query counter's type
            type_info = await client.call("$type", None, counter_ref)
            assert type_info == "Counter"

            # Query counter's methods
            methods = await client.call("$methods", None, counter_ref)
            method_names = [m["name"] for m in methods]
            assert "increment" in method_names
            assert "getValue" in method_names

            # Verify increment method has metadata
            increment_method = next((m for m in methods if m["name"] == "increment"), None)
            assert increment_method is not None
            assert increment_method["description"] == "Increments the counter by 1"
        finally:
            await client.close()

    @pytest.mark.asyncio
    async def test_python_ws_client_concurrent_requests(self):
        """Test concurrent requests via WebSocket."""
        client = WebSocketClient(f"ws://localhost:{WS_PORT}")
        await client.connect()

        try:
            # Make multiple concurrent requests
            async def make_call(i):
                result = await client.call("add", [i, i])
                assert result == i * 2
                return result

            tasks = [make_call(i) for i in range(5)]
            results = await asyncio.gather(*tasks)

            assert results == [0, 2, 4, 6, 8]
        finally:
            await client.close()
