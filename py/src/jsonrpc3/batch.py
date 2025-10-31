"""Batch request builder with fluent API and automatic batch-local reference management."""

from typing import List, Optional, Callable, Any, Protocol
from .types import Request, Response


class Caller(Protocol):
    """Protocol for objects that support batch operations."""

    def batch(self, requests: List[Request]) -> List[Response]:
        """Send a batch of requests and return responses."""
        ...


class BatchBuilder:
    """BatchBuilder collects requests to be sent as a single batch.

    It automatically manages batch-local references when chaining method calls.
    """

    def __init__(self, id_gen: Callable[[], Any]):
        """Initialize batch builder with ID generator function."""
        self._requests: List[Request] = []
        self._id_gen = id_gen

    def call(self, method: str, params: Any = None) -> "BatchBuilderPromise":
        """Add a method call to the batch and return a promise that can be chained.

        Args:
            method: Method name to call
            params: Method parameters

        Returns:
            BatchBuilderPromise for chaining
        """
        request_id = self._id_gen()

        request = Request(
            jsonrpc="3.0",
            method=method,
            params=params,
            id=request_id,
        )

        self._requests.append(request)

        return BatchBuilderPromise(self, len(self._requests) - 1)

    def get_requests(self) -> List[Request]:
        """Get all collected requests."""
        return self._requests


class BatchBuilderPromise:
    """BatchBuilderPromise represents a pending result in a batch request.

    It can be used to chain method calls that reference the result of a previous call
    using batch-local references (\\0, \\1, \\2, etc.).
    """

    def __init__(self, builder: BatchBuilder, index: int):
        """Initialize promise with builder and index.

        Args:
            builder: The BatchBuilder instance
            index: Index of this request in the batch
        """
        self._builder = builder
        self._index = index

    def call(self, method: str, params: Any = None) -> "BatchBuilderPromise":
        """Chain a method call on the result of the previous call.

        This automatically uses a batch-local reference (e.g., \\0, \\1) to reference
        the result from the promise's position in the batch.

        Args:
            method: Method name to call
            params: Method parameters

        Returns:
            New BatchBuilderPromise for further chaining
        """
        request_id = self._builder._id_gen()

        # Create batch-local reference: \0, \1, \2, etc.
        ref = f"\\{self._index}"

        request = Request(
            jsonrpc="3.0",
            ref=ref,
            method=method,
            params=params,
            id=request_id,
        )

        requests = self._builder.get_requests()
        requests.append(request)

        return BatchBuilderPromise(self._builder, len(requests) - 1)


class BatchResults:
    """BatchResults contains the responses from a batch request."""

    def __init__(self, responses: List[Optional[Response]]):
        """Initialize batch results.

        Args:
            responses: List of responses (None for notifications)
        """
        self._responses = responses

    def get_response(self, index: int) -> Optional[Response]:
        """Get the raw response at the specified index.

        Args:
            index: Index of the response

        Returns:
            Response or None for notifications

        Raises:
            IndexError: If index is out of bounds
        """
        if index < 0 or index >= len(self._responses):
            raise IndexError(f"Index {index} out of bounds (batch size: {len(self._responses)})")

        return self._responses[index]

    def get_result(self, index: int) -> Any:
        """Get and decode the result at the specified index.

        Args:
            index: Index of the response

        Returns:
            Decoded result

        Raises:
            ValueError: If response is None, has an error, or index is out of bounds
        """
        response = self.get_response(index)

        if response is None:
            raise ValueError(f"No response at index {index} (notification)")

        if response.error:
            raise ValueError(
                f"Error at index {index}: {response.error['message']} "
                f"(code: {response.error['code']})"
            )

        return response.result

    def __len__(self) -> int:
        """Get the number of responses in the batch."""
        return len(self._responses)

    def has_error(self) -> bool:
        """Check if any response in the batch contains an error."""
        return any(resp is not None and resp.error is not None for resp in self._responses)

    def all_responses(self) -> List[Optional[Response]]:
        """Get all responses (including None for notifications)."""
        return self._responses


def execute_batch(caller: Caller, fn: Callable[[BatchBuilder], None]) -> BatchResults:
    """Execute a batch of requests using the builder pattern with automatic
    batch-local reference management.

    The provided function receives a BatchBuilder that collects requests. When the
    function returns, the batch is sent via the provided Caller and responses are returned.

    Example:
        >>> results = execute_batch(client, lambda b: (
        ...     counter := b.call("createCounter", None),
        ...     counter.call("increment", None),  # Uses \\0 reference
        ...     counter.call("increment", None),  # Uses \\0 reference
        ...     counter.call("getValue", None)    # Uses \\0 reference
        ... ))
        >>> final_value = results.get_result(3)

    Args:
        caller: Caller implementation that supports batch operations
        fn: Function that builds the batch using BatchBuilder

    Returns:
        BatchResults containing the responses
    """
    # Create ID generator
    id_counter = 0

    def id_gen():
        nonlocal id_counter
        id_counter += 1
        return id_counter

    # Create builder
    builder = BatchBuilder(id_gen)

    # Execute user's batch building function
    fn(builder)

    # Get collected requests
    requests = builder.get_requests()

    if len(requests) == 0:
        return BatchResults([])

    # Send batch via caller
    responses = caller.batch(requests)

    # Build response map by ID for alignment
    response_map = {}
    for response in responses:
        if response.id is not None:
            response_map[response.id] = response

    # Align responses with requests (notifications get None)
    aligned_responses: List[Optional[Response]] = []
    for request in requests:
        if request.id is None:
            # Notification - no response
            aligned_responses.append(None)
        else:
            # Look up response by ID
            aligned_responses.append(response_map.get(request.id))

    return BatchResults(aligned_responses)
