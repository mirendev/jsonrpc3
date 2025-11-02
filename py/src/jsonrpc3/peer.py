"""Async peer for bidirectional JSON-RPC 3.0 communication over streams."""

import asyncio
from typing import Any, Dict, Optional, Union

from .encoding import get_codec
from .errors import RpcError
from .handler import Handler
from .session import Session
from .types import (
    MIME_TYPE_JSON,
    MessageSet,
    Reference,
    batch_response_to_message_set,
    is_reference,
    is_request,
    is_response,
    new_request,
    to_batch,
)


class Peer:
    """Async peer for bidirectional JSON-RPC 3.0 communication."""

    def __init__(
        self,
        reader: asyncio.StreamReader,
        writer: asyncio.StreamWriter,
        root_object: Any,
        session: Optional[Session] = None,
        mime_type: str = MIME_TYPE_JSON,
    ):
        """
        Initialize peer.

        Args:
            reader: Input stream
            writer: Output stream
            root_object: Root RPC object
            session: Optional session (created if not provided)
            mime_type: MIME type for encoding
        """
        self.reader = reader
        self.writer = writer
        self.session = session or Session()
        self._codec = get_codec(mime_type)
        # Pass this peer as caller for bidirectional communication
        self._handler = Handler(self.session, root_object, self, [mime_type])

        self._next_id = 0
        self._id_lock = asyncio.Lock()
        self._pending_requests: Dict[int, asyncio.Queue] = {}
        self._pending_lock = asyncio.Lock()
        self._write_lock = asyncio.Lock()

        self._closed = False
        self._close_lock = asyncio.Lock()

        # Start read loop in background
        self._read_task: Optional[asyncio.Task] = None

    async def start(self) -> "Peer":
        """Start the peer read loop."""
        self._read_task = asyncio.create_task(self._read_loop())
        return self

    async def call(
        self,
        method: str,
        params: Any = None,
        ref: Optional[Union[str, dict]] = None,
    ) -> Any:
        """
        Call a method on the remote peer.

        Args:
            method: Method name
            params: Method parameters
            ref: Optional reference

        Returns:
            Method result

        Raises:
            RuntimeError: If connection is closed
            RpcError: If remote returns an error
        """
        if self._closed:
            raise RuntimeError("Connection closed")

        req_id = await self._get_next_id()
        ref_string = self._extract_ref(ref)
        req = new_request(method, params, req_id, ref_string)

        # Create queue for response
        queue: asyncio.Queue = asyncio.Queue()
        async with self._pending_lock:
            self._pending_requests[req_id] = queue

        # Send request
        msg_set = MessageSet(messages=[req.to_dict()], is_batch=False)
        await self._write_message(msg_set)

        # Wait for response
        resp = await queue.get()
        async with self._pending_lock:
            del self._pending_requests[req_id]

        if resp.get("error"):
            raise RpcError.from_dict(resp["error"])

        result = resp.get("result")
        self._track_remote_references(result)
        return result

    async def notify(
        self,
        method: str,
        params: Any = None,
        ref: Optional[Union[str, dict]] = None,
    ) -> None:
        """
        Send a notification.

        Args:
            method: Method name
            params: Method parameters
            ref: Optional reference
        """
        if self._closed:
            raise RuntimeError("Connection closed")

        ref_string = self._extract_ref(ref)
        req = new_request(method, params, None, ref_string)

        msg_set = MessageSet(messages=[req.to_dict()], is_batch=False)
        await self._write_message(msg_set)

    def register_object(self, obj: Any, ref: Optional[str] = None) -> Reference:
        """Register a local object."""
        if ref:
            ref_string = self.session.add_local_ref(ref, obj)
        else:
            ref_string = self.session.add_local_ref(obj)
        return Reference(ref=ref_string)

    def unregister_object(self, ref: Union[str, Reference]) -> bool:
        """Unregister a local object."""
        ref_string = ref.ref if isinstance(ref, Reference) else ref
        return self.session.remove_local_ref(ref_string)

    async def close(self) -> None:
        """Close the peer."""
        async with self._close_lock:
            if self._closed:
                return

            self._closed = True

            # Reject all pending requests
            async with self._pending_lock:
                for queue in self._pending_requests.values():
                    await queue.put({"error": {"message": "Connection closed"}})
                self._pending_requests.clear()

            # Dispose all references
            self.session.dispose_all()

            # Close streams
            if self.writer:
                self.writer.close()
                await self.writer.wait_closed()

            # Cancel read task
            if self._read_task:
                self._read_task.cancel()
                try:
                    await self._read_task
                except asyncio.CancelledError:
                    pass

    async def wait(self) -> None:
        """Wait for peer to close."""
        if self._read_task:
            try:
                await self._read_task
            except asyncio.CancelledError:
                pass

    async def _get_next_id(self) -> int:
        """Get next request ID."""
        async with self._id_lock:
            self._next_id += 1
            return self._next_id

    def _extract_ref(self, ref: Optional[Union[str, dict]]) -> Optional[str]:
        """Extract reference string from various formats."""
        if ref is None:
            return None
        elif isinstance(ref, str):
            return ref
        elif isinstance(ref, dict):
            return ref.get("$ref")
        else:
            return getattr(ref, "ref", None)

    async def _write_message(self, msg_set: MessageSet) -> None:
        """Write a message to the stream."""
        async with self._write_lock:
            if self._closed:
                raise RuntimeError("Connection closed")

            bytes_data = self._codec.marshal_messages(msg_set)
            # Add newline for message framing
            self.writer.write(bytes_data + b"\n")
            await self.writer.drain()

    async def _read_loop(self) -> None:
        """Continuously read messages from the stream."""
        try:
            while not self._closed:
                # Read until newline
                line = await self.reader.readline()
                if not line:
                    break

                msg_set = self._codec.unmarshal_messages(line)

                # Determine if requests or responses
                all_requests = all(is_request(msg) for msg in msg_set.messages)
                all_responses = all(is_response(msg) for msg in msg_set.messages)

                if all_requests and msg_set.is_batch:
                    asyncio.create_task(self._handle_incoming_batch(msg_set))
                elif all_requests:
                    asyncio.create_task(
                        self._handle_incoming_request(msg_set.messages[0])
                    )
                elif all_responses:
                    for msg in msg_set.messages:
                        await self._handle_incoming_response(msg)

        except asyncio.CancelledError:
            pass
        except Exception as e:
            # Connection error
            pass
        finally:
            await self.close()

    async def _handle_incoming_request(self, msg: Union[dict, Any]) -> None:
        """Handle incoming request."""
        resp = self._handler.handle_request(msg)

        # Send response if not a notification
        if msg.get("id") is not None:
            msg_set = MessageSet(messages=[resp], is_batch=False)
            try:
                await self._write_message(msg_set)
            except Exception:
                pass

    async def _handle_incoming_batch(self, msg_set: MessageSet) -> None:
        """Handle incoming batch."""
        batch = to_batch(msg_set)
        responses = self._handler.handle_batch(batch)

        if responses:
            resp_msg_set = batch_response_to_message_set(responses)
            try:
                await self._write_message(resp_msg_set)
            except Exception:
                pass

    async def _handle_incoming_response(self, msg: Union[dict, Any]) -> None:
        """Handle incoming response."""
        msg_id = msg.get("id")
        async with self._pending_lock:
            queue = self._pending_requests.get(msg_id)

        if queue:
            await queue.put(msg)

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
