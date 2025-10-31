"""Tests for batch request builder API."""

import pytest
from jsonrpc3.batch import (
    BatchBuilder,
    BatchBuilderPromise,
    BatchResults,
    execute_batch,
)
from jsonrpc3.types import Request, Response


class MockCaller:
    """Mock caller for testing."""

    def __init__(self):
        self.responses = []

    def batch(self, requests):
        """Return pre-configured responses."""
        return self.responses


class TestBatchBuilder:
    """Test BatchBuilder class."""

    def test_simple_call(self):
        """Test creating a simple batch with one call."""
        id_counter = 0

        def id_gen():
            nonlocal id_counter
            id_counter += 1
            return id_counter

        builder = BatchBuilder(id_gen)
        builder.call("testMethod", {"value": 42})

        requests = builder.get_requests()
        assert len(requests) == 1
        assert requests[0].method == "testMethod"
        assert requests[0].params == {"value": 42}
        assert requests[0].id == 1

    def test_multiple_calls(self):
        """Test creating a batch with multiple independent calls."""
        id_counter = 0

        def id_gen():
            nonlocal id_counter
            id_counter += 1
            return id_counter

        builder = BatchBuilder(id_gen)
        builder.call("method1", {"a": 1})
        builder.call("method2", {"b": 2})
        builder.call("method3", {"c": 3})

        requests = builder.get_requests()
        assert len(requests) == 3
        assert requests[0].method == "method1"
        assert requests[1].method == "method2"
        assert requests[2].method == "method3"
        assert requests[0].id == 1
        assert requests[1].id == 2
        assert requests[2].id == 3

    def test_chained_calls(self):
        """Test creating chained calls with batch-local references."""
        id_counter = 0

        def id_gen():
            nonlocal id_counter
            id_counter += 1
            return id_counter

        builder = BatchBuilder(id_gen)
        promise1 = builder.call("createCounter", None)
        promise1.call("increment", None)
        promise1.call("getValue", None)

        requests = builder.get_requests()
        assert len(requests) == 3

        # First request has no ref
        assert requests[0].method == "createCounter"
        assert requests[0].ref is None

        # Second request references first (\0)
        assert requests[1].method == "increment"
        assert requests[1].ref == "\\0"

        # Third request also references first (\0)
        assert requests[2].method == "getValue"
        assert requests[2].ref == "\\0"

    def test_nested_chained_calls(self):
        """Test creating nested chained calls."""
        id_counter = 0

        def id_gen():
            nonlocal id_counter
            id_counter += 1
            return id_counter

        builder = BatchBuilder(id_gen)
        p1 = builder.call("createCounter", None)
        p2 = p1.call("increment", None)
        p2.call("increment", None)

        requests = builder.get_requests()
        assert len(requests) == 3

        # First: createCounter
        assert requests[0].method == "createCounter"
        assert requests[0].ref is None

        # Second: increment on \0 (createCounter result)
        assert requests[1].method == "increment"
        assert requests[1].ref == "\\0"

        # Third: increment on \1 (first increment result)
        assert requests[2].method == "increment"
        assert requests[2].ref == "\\1"


class TestBatchResults:
    """Test BatchResults class."""

    def test_get_response(self):
        """Test getting response by index."""
        mock_response1 = Response(jsonrpc="3.0", result=42, id=1)
        mock_response2 = Response(jsonrpc="3.0", result="hello", id=2)

        results = BatchResults([mock_response1, mock_response2])

        assert results.get_response(0) == mock_response1
        assert results.get_response(1) == mock_response2

    def test_get_response_out_of_bounds(self):
        """Test that out of bounds index raises error."""
        mock_response = Response(jsonrpc="3.0", result=42, id=1)
        results = BatchResults([mock_response])

        with pytest.raises(IndexError, match="out of bounds"):
            results.get_response(1)

        with pytest.raises(IndexError, match="out of bounds"):
            results.get_response(-1)

    def test_get_result(self):
        """Test getting result by index."""
        mock_response1 = Response(jsonrpc="3.0", result=42, id=1)
        mock_response2 = Response(jsonrpc="3.0", result="hello", id=2)

        results = BatchResults([mock_response1, mock_response2])

        assert results.get_result(0) == 42
        assert results.get_result(1) == "hello"

    def test_get_result_with_error(self):
        """Test getting result when response has error."""
        mock_error_response = Response(
            jsonrpc="3.0",
            error={"code": -32600, "message": "Invalid Request"},
            id=1,
        )

        results = BatchResults([mock_error_response])

        with pytest.raises(ValueError, match="Invalid Request"):
            results.get_result(0)

    def test_get_result_from_notification(self):
        """Test getting result from notification (None response)."""
        results = BatchResults([None])

        with pytest.raises(ValueError, match="No response at index 0"):
            results.get_result(0)

    def test_length(self):
        """Test getting length of results."""
        mock_response1 = Response(jsonrpc="3.0", result=42, id=1)
        mock_response2 = Response(jsonrpc="3.0", result="hello", id=2)

        results = BatchResults([mock_response1, mock_response2])
        assert len(results) == 2

        empty = BatchResults([])
        assert len(empty) == 0

    def test_has_error(self):
        """Test detecting errors in responses."""
        mock_response1 = Response(jsonrpc="3.0", result=42, id=1)
        mock_response2 = Response(jsonrpc="3.0", result="hello", id=2)
        mock_error = Response(
            jsonrpc="3.0",
            error={"code": -32600, "message": "Invalid Request"},
            id=3,
        )

        no_errors = BatchResults([mock_response1, mock_response2])
        assert no_errors.has_error() is False

        with_error = BatchResults([mock_response1, mock_error])
        assert with_error.has_error() is True

        with_none = BatchResults([None, mock_response1])
        assert with_none.has_error() is False

    def test_all_responses(self):
        """Test getting all responses including None."""
        responses = [None, Response(jsonrpc="3.0", result=42, id=1)]
        results = BatchResults(responses)

        assert results.all_responses() == responses


class TestExecuteBatch:
    """Test execute_batch function."""

    def test_simple_batch(self):
        """Test executing a simple batch."""
        mock_caller = MockCaller()
        mock_caller.responses = [
            Response(jsonrpc="3.0", result=10, id=1),
            Response(jsonrpc="3.0", result=20, id=2),
        ]

        def build_batch(b):
            b.call("add", [1, 2, 3, 4])
            b.call("multiply", [2, 10])

        results = execute_batch(mock_caller, build_batch)

        assert len(results) == 2
        assert results.get_result(0) == 10
        assert results.get_result(1) == 20

    def test_batch_with_chained_calls(self):
        """Test executing batch with chained calls."""
        mock_caller = MockCaller()
        mock_caller.responses = [
            Response(jsonrpc="3.0", result={"$ref": "counter1"}, id=1),
            Response(jsonrpc="3.0", result=1, id=2),
            Response(jsonrpc="3.0", result=2, id=3),
            Response(jsonrpc="3.0", result=2, id=4),
        ]

        def build_batch(b):
            counter = b.call("createCounter", None)
            counter.call("increment", None)
            counter.call("increment", None)
            counter.call("getValue", None)

        results = execute_batch(mock_caller, build_batch)

        assert len(results) == 4
        assert results.get_result(0) == {"$ref": "counter1"}
        assert results.get_result(1) == 1
        assert results.get_result(2) == 2
        assert results.get_result(3) == 2

    def test_empty_batch(self):
        """Test executing empty batch."""
        mock_caller = MockCaller()

        def build_batch(b):
            pass  # Don't add any calls

        results = execute_batch(mock_caller, build_batch)

        assert len(results) == 0
