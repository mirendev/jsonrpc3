"""WebSocket server for JSON-RPC 3.0."""

import asyncio
import secrets
from typing import Any, Optional, Set, Union

import websockets
from websockets.server import WebSocketServerProtocol

from .encoding import get_codec
from .errors import RpcError, CODE_TIMEOUT, CODE_CONNECTION_CLOSED, CODE_CONNECTION_ERROR
from .handler import Handler
from .session import Session
from .types import (
    MIME_TYPE_JSON,
    MessageSet,
    Reference,
    Request,
    Response,
    is_reference,
    is_request,
    is_response,
    new_request,
)


class WebSocketHandler:
    """WebSocket server handler for JSON-RPC 3.0.

    Manages WebSocket connections and routes them to connection handlers.
    """

    def __init__(self, root_object: Any):
        """
        Initialize WebSocket handler.

        Args:
            root_object: Object to handle incoming calls from clients
        """
        self.root_object = root_object
        self._mime_types = [MIME_TYPE_JSON]
        self._subprotocols = ["jsonrpc3.json"]
        self._connections: Set[WebSocketServerConn] = set()

    async def handle_connection(self, websocket: WebSocketServerProtocol) -> None:
        """
        Handle a WebSocket connection.

        Args:
            websocket: WebSocket connection
        """
        # Only JSON is supported for now
        content_type = MIME_TYPE_JSON

        # Reject non-JSON protocols
        if websocket.subprotocol and websocket.subprotocol != "jsonrpc3.json":
            await websocket.close(code=1002, reason="Only jsonrpc3.json protocol is supported")
            return

        # Create connection handler
        conn = WebSocketServerConn(
            websocket, self.root_object, content_type, self._mime_types
        )
        self._connections.add(conn)

        try:
            await conn.handle()
        finally:
            self._connections.remove(conn)
            await conn.close()

    async def close_all(self) -> None:
        """Close all active connections."""
        tasks = [conn.close() for conn in self._connections]
        await asyncio.gather(*tasks, return_exceptions=True)


class WebSocketServerConn:
    """WebSocket server connection handler.

    Manages bidirectional JSON-RPC 3.0 communication for a single client connection.
    """

    def __init__(
        self,
        websocket: WebSocketServerProtocol,
        root_object: Any,
        content_type: str,
        mime_types: list[str],
    ):
        """
        Initialize connection handler.

        Args:
            websocket: WebSocket connection
            root_object: Root object for handling requests
            content_type: Content type for encoding
            mime_types: Supported MIME types
        """
        self._ws = websocket
        self._root_object = root_object
        self._content_type = content_type
        self._codec = get_codec(content_type)

        self.session = Session()
        self._handler = Handler(self.session, root_object, self, mime_types)

        # Request tracking for server-initiated requests
        self._request_id = 0
        self._pending_requests: dict[Union[str, int], asyncio.Future] = {}
        self._request_lock = asyncio.Lock()

        # Reference ID generation
        self._ref_prefix = secrets.token_hex(4)
        self._ref_counter = 0
        self._ref_lock = asyncio.Lock()

        # Connection state
        self._closed = False
        self._close_lock = asyncio.Lock()
        self._conn_error: Optional[str] = None

        # Write queue
        self._write_queue: asyncio.Queue = asyncio.Queue()

    async def handle(self) -> None:
        """Handle the connection by processing messages."""
        # Start write loop
        write_task = asyncio.create_task(self._write_loop())

        try:
            # Read messages
            async for message in self._ws:
                if self._closed:
                    break

                try:
                    if isinstance(message, bytes):
                        await self._handle_message(message)
                    else:
                        await self._handle_message(message.encode("utf-8"))
                except Exception as e:
                    print(f"Error handling message: {e}")

        except websockets.exceptions.ConnectionClosed:
            pass
        except Exception as e:
            self._set_error(f"Connection error: {e}")
        finally:
            await self.close()
            # Cancel write task
            write_task.cancel()
            try:
                await write_task
            except asyncio.CancelledError:
                pass

    async def call(
        self,
        method: str,
        params: Any = None,
        ref: Optional[Union[str, dict, Reference]] = None,
        timeout: float = 30.0,
    ) -> Any:
        """
        Call a method on the client and wait for response.

        Args:
            method: Method name
            params: Method parameters
            ref: Target reference
            timeout: Request timeout in seconds

        Returns:
            Result value

        Raises:
            RpcError: If client returns an error
        """
        self._check_closed()

        # Generate request ID
        req_id = await self._next_id()

        # Extract ref string
        ref_string = self._extract_ref(ref)

        # Create request
        req = new_request(method, params, req_id, ref_string)

        # Create response future
        response_future: asyncio.Future = asyncio.Future()
        async with self._request_lock:
            self._pending_requests[req_id] = response_future

        try:
            # Encode and send request
            msg_set = MessageSet(messages=[req.to_dict()], is_batch=False)
            req_data = self._codec.marshal_messages(msg_set)

            # Send via write queue
            await self._write_queue.put(req_data)

            # Wait for response with timeout
            try:
                resp = await asyncio.wait_for(response_future, timeout=timeout)
            except asyncio.TimeoutError:
                raise RpcError("Request timeout", code=CODE_TIMEOUT)

            # Check for error
            if resp.error:
                raise RpcError.from_dict(resp.error)

            return resp.result

        finally:
            async with self._request_lock:
                self._pending_requests.pop(req_id, None)

    async def notify(
        self,
        method: str,
        params: Any = None,
        ref: Optional[Union[str, dict, Reference]] = None,
    ) -> None:
        """
        Send a notification to the client (no response expected).

        Args:
            method: Method name
            params: Method parameters
            ref: Target reference
        """
        self._check_closed()

        ref_string = self._extract_ref(ref)
        req = new_request(method, params, None, ref_string)

        msg_set = MessageSet(messages=[req.to_dict()], is_batch=False)
        req_data = self._codec.marshal_messages(msg_set)

        await self._write_queue.put(req_data)

    def register_object(self, obj: Any, ref: Optional[str] = None) -> Reference:
        """
        Register a local object that the client can call.

        Args:
            obj: Object to register
            ref: Reference ID (auto-generated if None)

        Returns:
            Reference object
        """
        if ref is None:
            # Generate unique ref
            self._ref_counter += 1
            ref = f"{self._ref_prefix}-{self._ref_counter}"

        self.session.add_local_ref(ref, obj)
        return Reference(ref=ref)

    async def close(self) -> None:
        """Close the connection gracefully."""
        async with self._close_lock:
            if self._closed:
                return
            self._closed = True

        # Close WebSocket
        if self._ws:
            await self._ws.close(code=1000, reason="Normal closure")

        # Dispose all refs
        self.session.dispose_all()

        # Wake up all pending requests with error
        async with self._request_lock:
            for future in self._pending_requests.values():
                if not future.done():
                    future.set_result(
                        Response(
                            jsonrpc="3.0",
                            id=None,
                            error={
                                "code": CODE_CONNECTION_CLOSED,
                                "message": "Connection closed",
                            },
                        )
                    )
            self._pending_requests.clear()

    def closed(self) -> bool:
        """Check if connection is closed."""
        return self._closed

    async def _write_loop(self) -> None:
        """Write loop - sends queued messages."""
        try:
            while not self._closed:
                try:
                    # Get next message from queue with timeout
                    msg = await asyncio.wait_for(self._write_queue.get(), timeout=0.1)

                    if self._closed:
                        break

                    # Send message
                    if isinstance(msg, str):
                        await self._ws.send(msg)
                    else:
                        await self._ws.send(msg)

                except asyncio.TimeoutError:
                    # Queue empty, continue
                    continue
                except Exception as e:
                    self._set_error(f"Write error: {e}")
                    break

        except Exception as e:
            self._set_error(f"Write loop error: {e}")

    async def _handle_message(self, data: bytes) -> None:
        """Handle incoming WebSocket message."""
        try:
            # Decode as MessageSet
            msg_set = self._codec.unmarshal_messages(data)

            # Check if all messages are requests or all are responses
            all_requests = all(is_request(msg) for msg in msg_set.messages)
            all_responses = all(is_response(msg) for msg in msg_set.messages)

            if all_requests and msg_set.is_batch:
                # Handle batch requests
                asyncio.create_task(self._handle_incoming_batch(msg_set))
            elif all_requests:
                # Handle single request
                asyncio.create_task(self._handle_incoming_request(msg_set.messages[0]))
            elif all_responses:
                # Handle responses
                for msg in msg_set.messages:
                    self._handle_incoming_response(msg)
            else:
                # Mixed batch - invalid
                print("Mixed requests and responses in batch")

        except Exception as e:
            # Silently ignore malformed messages
            print(f"Failed to decode message: {e}")

    async def _handle_incoming_request(self, msg: Union[dict, Request]) -> None:
        """Handle incoming request from client."""
        try:
            req = Request.from_dict(msg) if isinstance(msg, dict) else msg

            # Handle request via handler
            resp = self._handler.handle_request(req)

            # Send response if not a notification
            if resp:
                await self._send_response(resp)

        except Exception as e:
            print(f"Error handling request: {e}")

    async def _handle_incoming_batch(self, msg_set: MessageSet) -> None:
        """Handle incoming batch requests from client."""
        try:
            # Convert to requests
            requests = [
                Request.from_dict(msg) if isinstance(msg, dict) else msg
                for msg in msg_set.messages
            ]

            # Handle batch via handler
            responses = []
            for req in requests:
                resp = self._handler.handle_request(req)
                if resp:
                    responses.append(resp)

            # Send batch response
            if responses:
                await self._send_batch_responses(responses)

        except Exception as e:
            print(f"Error handling batch: {e}")

    def _handle_incoming_response(self, msg: Union[dict, Response]) -> None:
        """Handle incoming response to our request."""
        try:
            resp = Response.from_dict(msg) if isinstance(msg, dict) else msg

            # Find pending request
            future = self._pending_requests.get(resp.id)

            # Send response to waiting coroutine
            if future and not future.done():
                future.set_result(resp)

        except Exception as e:
            print(f"Error handling response: {e}")

    async def _send_response(self, resp: Response) -> None:
        """Send a response."""
        msg_set = MessageSet(messages=[resp.to_dict()], is_batch=False)
        resp_data = self._codec.marshal_messages(msg_set)
        await self._write_queue.put(resp_data)

    async def _send_batch_responses(self, responses: list[Response]) -> None:
        """Send batch responses."""
        msg_set = MessageSet(
            messages=[resp.to_dict() for resp in responses],
            is_batch=True,
        )
        resp_data = self._codec.marshal_messages(msg_set)
        await self._write_queue.put(resp_data)

    async def _next_id(self) -> int:
        """Generate next request ID."""
        async with self._request_lock:
            self._request_id += 1
            return self._request_id

    def _extract_ref(self, ref: Optional[Union[str, dict, Reference]]) -> Optional[str]:
        """Extract ref string from various formats."""
        if ref is None:
            return None
        elif isinstance(ref, str):
            return ref
        elif isinstance(ref, Reference):
            return ref.ref
        elif isinstance(ref, dict):
            return ref.get("$ref")
        else:
            return None

    def _set_error(self, error_msg: str) -> None:
        """Set connection error."""
        if self._conn_error is None:
            self._conn_error = error_msg
        print(f"WebSocket error: {error_msg}")

    def _get_error(self) -> Optional[str]:
        """Get connection error."""
        return self._conn_error

    def _check_closed(self) -> None:
        """Check if connection is closed and raise error if so."""
        if self._closed:
            raise RpcError("Connection closed", code=CODE_CONNECTION_CLOSED)

        if self._conn_error:
            raise RpcError(self._conn_error, code=CODE_CONNECTION_ERROR)


async def serve(
    root_object: Any,
    host: str = "localhost",
    port: int = 8765,
) -> None:
    """
    Start a WebSocket server for JSON-RPC 3.0.

    Args:
        root_object: Object to handle incoming calls
        host: Server host
        port: Server port
    """
    handler = WebSocketHandler(root_object)

    async with websockets.serve(
        handler.handle_connection,
        host,
        port,
        subprotocols=handler._subprotocols,
    ):
        await asyncio.Future()  # Run forever
