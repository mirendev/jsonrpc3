"""Core JSON-RPC 3.0 types and data structures."""

from dataclasses import dataclass, asdict
from typing import Any, Optional, Union, List

# Constants
VERSION_30 = "3.0"
MIME_TYPE_JSON = "application/json"
MIME_TYPE_CBOR = "application/cbor"


@dataclass(frozen=True)
class Reference:
    """Reference to a remote object."""

    ref: str

    def to_dict(self) -> dict:
        """Convert to dictionary representation."""
        return {"$ref": self.ref}

    @classmethod
    def from_dict(cls, data: dict) -> "Reference":
        """Create from dictionary representation."""
        return cls(ref=data["$ref"])

    def call(self, caller: Any, method: str, params: Any = None) -> Any:
        """Call a method on this reference using the provided caller.

        This is a convenience wrapper that calls caller.call(method, params, ref=self).
        For synchronous callers (like HttpClient), this blocks until the response is received.
        For async callers (like Peer), use async_call() instead.

        Args:
            caller: A caller object with a call() method (e.g., HttpClient)
            method: The method name to call
            params: Optional parameters for the method

        Returns:
            The result of the method call

        Raises:
            RpcError: If the remote returns an error
        """
        return caller.call(method, params, ref=self)

    async def async_call(self, caller: Any, method: str, params: Any = None) -> Any:
        """Call a method on this reference using an async caller.

        This is a convenience wrapper for async callers like Peer.
        It calls await caller.call(method, params, ref=self).

        Args:
            caller: An async caller object with a call() method (e.g., Peer)
            method: The method name to call
            params: Optional parameters for the method

        Returns:
            The result of the method call

        Raises:
            RpcError: If the remote returns an error
        """
        return await caller.call(method, params, ref=self)

    def notify(self, caller: Any, method: str, params: Any = None) -> None:
        """Send a notification to this reference using the provided caller.

        This is a convenience wrapper that calls caller.notify(method, params, ref=self).
        Notifications do not expect a response.
        For synchronous callers (like HttpClient), this returns immediately.
        For async callers (like Peer), use async_notify() instead.

        Args:
            caller: A caller object with a notify() method (e.g., HttpClient)
            method: The method name to notify
            params: Optional parameters for the notification
        """
        caller.notify(method, params, ref=self)

    async def async_notify(self, caller: Any, method: str, params: Any = None) -> None:
        """Send a notification to this reference using an async caller.

        This is a convenience wrapper for async callers like Peer.
        It calls await caller.notify(method, params, ref=self).

        Args:
            caller: An async caller object with a notify() method (e.g., Peer)
            method: The method name to notify
            params: Optional parameters for the notification
        """
        await caller.notify(method, params, ref=self)


@dataclass
class Request:
    """JSON-RPC 3.0 request message."""

    jsonrpc: str
    method: str
    params: Any = None
    id: Optional[Union[str, int]] = None
    ref: Optional[str] = None

    def to_dict(self) -> dict:
        """Convert to dictionary representation."""
        result = {
            "jsonrpc": self.jsonrpc,
            "method": self.method,
        }
        if self.params is not None:
            result["params"] = self.params
        if self.id is not None:
            result["id"] = self.id
        if self.ref is not None:
            result["ref"] = self.ref
        return result

    @classmethod
    def from_dict(cls, data: dict) -> "Request":
        """Create from dictionary representation."""
        return cls(
            jsonrpc=data.get("jsonrpc", VERSION_30),
            method=data["method"],
            params=data.get("params"),
            id=data.get("id"),
            ref=data.get("ref"),
        )


@dataclass
class Response:
    """JSON-RPC 3.0 response message."""

    jsonrpc: str
    id: Optional[Union[str, int]]
    result: Any = None
    error: Optional[dict] = None

    def to_dict(self) -> dict:
        """Convert to dictionary representation."""
        result = {
            "jsonrpc": self.jsonrpc,
            "id": self.id,
        }
        if self.error is not None:
            result["error"] = self.error
        else:
            result["result"] = self.result
        return result

    @classmethod
    def from_dict(cls, data: dict) -> "Response":
        """Create from dictionary representation."""
        return cls(
            jsonrpc=data.get("jsonrpc", VERSION_30),
            id=data.get("id"),
            result=data.get("result"),
            error=data.get("error"),
        )


@dataclass
class MessageSet:
    """Container for one or more messages with batch indicator."""

    messages: List[Union[dict, Request, Response]]
    is_batch: bool

    def to_dict(self) -> Union[dict, List[dict]]:
        """Convert to dictionary representation."""
        msg_dicts = []
        for msg in self.messages:
            if isinstance(msg, (Request, Response)):
                msg_dicts.append(msg.to_dict())
            else:
                msg_dicts.append(msg)

        return msg_dicts if self.is_batch else msg_dicts[0]


# Helper functions for creating instances

def reference(ref: str) -> dict:
    """Create a reference dictionary."""
    return {"$ref": ref}


def to_ref(ref: Union[str, Reference]) -> Reference:
    """Convert a string or Reference to a Reference object.

    Args:
        ref: Either a reference string or a Reference object

    Returns:
        A Reference object
    """
    if isinstance(ref, Reference):
        return ref
    return Reference(ref=ref)


def new_request(
    method: str,
    params: Any = None,
    id: Optional[Union[str, int]] = None,
    ref: Optional[str] = None,
) -> Request:
    """Create a new request."""
    return Request(
        jsonrpc=VERSION_30,
        method=method,
        params=params,
        id=id,
        ref=ref,
    )


def new_response(
    result: Any,
    id: Optional[Union[str, int]],
    jsonrpc: str = VERSION_30,
) -> Response:
    """Create a new response."""
    return Response(
        jsonrpc=jsonrpc,
        id=id,
        result=result,
    )


def new_error_response(
    error: dict,
    id: Optional[Union[str, int]],
    jsonrpc: str = VERSION_30,
) -> Response:
    """Create a new error response."""
    return Response(
        jsonrpc=jsonrpc,
        id=id,
        error=error,
    )


# Type checking functions

def is_reference(value: Any) -> bool:
    """Check if value is a reference."""
    if not isinstance(value, dict):
        return False
    return len(value) == 1 and "$ref" in value


def is_request(msg: Any) -> bool:
    """Check if message is a request."""
    if not isinstance(msg, dict):
        return isinstance(msg, Request)
    return "method" in msg


def is_response(msg: Any) -> bool:
    """Check if message is a response."""
    if not isinstance(msg, dict):
        return isinstance(msg, Response)
    return "result" in msg or "error" in msg


def is_notification(msg: Union[dict, Request]) -> bool:
    """Check if message is a notification (request without id)."""
    if isinstance(msg, Request):
        return msg.id is None
    return "id" not in msg and "method" in msg


# Helper functions for batch handling

def to_batch(msg_set: MessageSet) -> List[Union[dict, Request]]:
    """Convert MessageSet to list of requests."""
    return msg_set.messages


def to_request(msg_set: MessageSet) -> Union[dict, Request]:
    """Extract single request from MessageSet."""
    if len(msg_set.messages) != 1:
        raise ValueError("MessageSet must contain exactly one message")
    return msg_set.messages[0]


def batch_response_to_message_set(responses: List[Response]) -> MessageSet:
    """Convert list of responses to MessageSet."""
    return MessageSet(messages=responses, is_batch=True)
