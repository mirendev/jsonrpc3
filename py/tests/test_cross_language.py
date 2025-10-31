"""Cross-language integration tests.

Tests Python clients against Go servers.
"""

import socket
import subprocess
import time
from pathlib import Path

import pytest
from jsonrpc3.http_client import HttpClient
from jsonrpc3.batch import execute_batch
from jsonrpc3.types import Request


HTTP_PORT = 18084


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
        pytest.fail(f"Go test server not found at {go_server_path}")

    # Start server
    process = subprocess.Popen(
        [str(go_server_path), "-http", str(HTTP_PORT)],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )

    # Wait for server to be ready
    try:
        wait_for_port(HTTP_PORT)
        print(f"\nGo test server started on HTTP port {HTTP_PORT}")
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
