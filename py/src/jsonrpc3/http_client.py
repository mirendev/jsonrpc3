"""HTTP client for JSON-RPC 3.0."""

import http.client
import threading
from typing import Any, List, Optional, Union
from urllib.parse import urlparse

from .encoding import get_codec
from .errors import RpcError
from .session import Session
from .types import (
    MIME_TYPE_JSON,
    MessageSet,
    Request,
    Response,
    is_reference,
    is_response,
    new_request,
)


class HttpClient:
    """HTTP client for JSON-RPC 3.0."""

    def __init__(
        self,
        url: str,
        session: Optional[Session] = None,
        mime_type: str = MIME_TYPE_JSON,
    ):
        """
        Initialize HTTP client.

        Args:
            url: Server URL
            session: Optional session (created if not provided)
            mime_type: MIME type for encoding
        """
        self.url = url
        self.session = session or Session()
        self._codec = get_codec(mime_type)
        self._request_id = 0
        self._mutex = threading.Lock()

        # Parse URL
        parsed = urlparse(url)
        self._scheme = parsed.scheme
        self._host = parsed.hostname
        self._port = parsed.port or (443 if parsed.scheme == "https" else 80)
        self._path = parsed.path or "/"

    def call(
        self,
        method: str,
        params: Any = None,
        ref: Optional[Union[str, dict]] = None,
    ) -> Any:
        """
        Call a method and wait for response.

        Args:
            method: Method name
            params: Method parameters
            ref: Optional reference (string, dict, or Reference)

        Returns:
            Method result

        Raises:
            RpcError: If server returns an error
        """
        req_id = self._next_id()
        ref_string = self._extract_ref(ref)
        req = new_request(method, params, req_id, ref_string)

        msg_set = MessageSet(messages=[req.to_dict()], is_batch=False)
        resp_msg_set = self._send_request(msg_set)

        # Get the single response message
        if len(resp_msg_set.messages) != 1:
            raise ValueError("Expected single response")

        msg = resp_msg_set.messages[0]
        if not is_response(msg):
            raise ValueError("Invalid response message")

        resp = msg if isinstance(msg, Response) else Response.from_dict(msg)

        # Check for error
        if resp.error:
            raise RpcError.from_dict(resp.error)

        # Track remote references
        self._track_remote_references(resp.result)

        return resp.result

    def notify(
        self,
        method: str,
        params: Any = None,
        ref: Optional[Union[str, dict]] = None,
    ) -> None:
        """
        Send a notification (no response expected).

        Args:
            method: Method name
            params: Method parameters
            ref: Optional reference
        """
        ref_string = self._extract_ref(ref)
        req = new_request(method, params, None, ref_string)

        msg_set = MessageSet(messages=[req.to_dict()], is_batch=False)
        self._send_request(msg_set)

    def batch(self, requests: List[Request]) -> List[Response]:
        """
        Send a batch of requests.

        Args:
            requests: List of Request objects

        Returns:
            List of Response objects
        """
        msg_set = MessageSet(
            messages=[req.to_dict() for req in requests],
            is_batch=True,
        )

        resp_msg_set = self._send_request(msg_set)

        responses = []
        for msg in resp_msg_set.messages:
            if not is_response(msg):
                continue

            resp = msg if isinstance(msg, Response) else Response.from_dict(msg)
            if resp.result is not None:
                self._track_remote_references(resp.result)
            responses.append(resp)

        return responses

    def _next_id(self) -> int:
        """Get next request ID."""
        with self._mutex:
            self._request_id += 1
            return self._request_id

    def _extract_ref(self, ref: Optional[Union[str, dict]]) -> Optional[str]:
        """Extract reference string from various formats."""
        if ref is None:
            return None
        elif isinstance(ref, str):
            return ref
        elif isinstance(ref, dict):
            return ref.get("$ref")
        else:
            # Assume it has a 'ref' attribute (Reference object)
            return getattr(ref, "ref", None)

    def _send_request(self, msg_set: MessageSet) -> MessageSet:
        """Send HTTP request and return response."""
        req_bytes = self._codec.marshal_messages(msg_set)

        # Create connection
        if self._scheme == "https":
            conn = http.client.HTTPSConnection(self._host, self._port)
        else:
            conn = http.client.HTTPConnection(self._host, self._port)

        try:
            # Send request
            headers = {"Content-Type": self._codec.mime_type}
            conn.request("POST", self._path, req_bytes, headers)

            # Get response
            response = conn.getresponse()

            # 204 No Content is valid for notifications (batch of all notifications)
            if response.status == 204:
                response.read()  # Consume response body
                return MessageSet(messages=[], is_batch=msg_set.is_batch)

            if response.status != 200:
                raise RuntimeError(f"HTTP error: {response.status}")

            resp_bytes = response.read()
            return self._codec.unmarshal_messages(resp_bytes)

        finally:
            conn.close()

    def _track_remote_references(self, value: Any) -> None:
        """Track remote references in results."""
        if value is None:
            return

        if is_reference(value):
            self.session.add_remote_ref(value["$ref"])
            return

        if isinstance(value, list):
            for item in value:
                self._track_remote_references(item)
        elif isinstance(value, dict):
            for val in value.values():
                self._track_remote_references(val)
