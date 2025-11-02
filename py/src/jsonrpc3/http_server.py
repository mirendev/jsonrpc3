"""HTTP server for JSON-RPC 3.0.

Note: This HTTP server uses Python's built-in http.server which is synchronous.
WebSocket support requires an async framework. For production use with WebSockets,
consider using an ASGI server like uvicorn with async handlers.

This implementation detects WebSocket upgrade attempts and returns a helpful error.
"""

import http.server
import socketserver
import threading
from typing import Optional

from .encoding import get_codec
from .errors import RpcError
from .handler import Handler
from .types import (
    MIME_TYPE_JSON,
    MessageSet,
    batch_response_to_message_set,
    is_request,
    new_error_response,
    to_batch,
    to_request,
)


class HttpServer:
    """HTTP server for JSON-RPC 3.0 using http.server."""

    def __init__(
        self,
        handler: Handler,
        port: int = 3000,
        host: str = "localhost",
        mime_type: str = MIME_TYPE_JSON,
    ):
        """
        Initialize HTTP server.

        Args:
            handler: Request handler
            port: Port to listen on (use 0 for random available port)
            host: Host to bind to
            mime_type: MIME type for encoding
        """
        self.handler = handler
        self._requested_port = port
        self._host = host
        self._codec = get_codec(mime_type)
        self._server: Optional[socketserver.TCPServer] = None
        self._server_thread: Optional[threading.Thread] = None
        self.port: Optional[int] = None

    def start(self) -> "HttpServer":
        """Start the HTTP server."""

        # Create request handler class with access to our handler
        handler_instance = self.handler
        codec_instance = self._codec

        class RequestHandler(http.server.BaseHTTPRequestHandler):
            """Custom request handler for JSON-RPC."""

            def log_message(self, format, *args):
                """Suppress default logging."""
                pass

            def do_POST(self):
                """Handle POST requests."""
                try:
                    # Read request body
                    content_length = int(self.headers.get("Content-Length", 0))
                    body_bytes = self.rfile.read(content_length)

                    # Decode message set
                    msg_set = codec_instance.unmarshal_messages(body_bytes)

                    # Handle batch or single request
                    if msg_set.is_batch:
                        batch = to_batch(msg_set)
                        batch_resp = handler_instance.handle_batch(batch)
                        response_msg_set = batch_response_to_message_set(batch_resp)
                    else:
                        request = to_request(msg_set)
                        response = handler_instance.handle_request(request)
                        response_msg_set = MessageSet(
                            messages=[response], is_batch=False
                        )

                    # Encode response
                    resp_bytes = codec_instance.marshal_messages(response_msg_set)

                    # Send response
                    self.send_response(200)
                    self.send_header("Content-Type", codec_instance.mime_type)
                    self.send_header("Content-Length", str(len(resp_bytes)))
                    self.end_headers()
                    self.wfile.write(resp_bytes)

                except RpcError as e:
                    # Return RPC error as valid response
                    error_resp = new_error_response(e.to_dict(), None)
                    resp_msg_set = MessageSet(
                        messages=[error_resp], is_batch=False
                    )
                    resp_bytes = codec_instance.marshal_messages(resp_msg_set)

                    self.send_response(200)
                    self.send_header("Content-Type", codec_instance.mime_type)
                    self.send_header("Content-Length", str(len(resp_bytes)))
                    self.end_headers()
                    self.wfile.write(resp_bytes)

                except Exception as e:
                    # Internal server error
                    error_msg = f"Internal Server Error: {str(e)}"
                    self.send_error(500, error_msg)

            def do_GET(self):
                """Handle GET requests - check for WebSocket upgrade."""
                # Check if this is a WebSocket upgrade request
                upgrade = self.headers.get("Upgrade", "").lower()
                connection = self.headers.get("Connection", "").lower()

                if upgrade == "websocket" and "upgrade" in connection:
                    # WebSocket upgrade detected - return helpful error
                    self.send_response(501)
                    self.send_header("Content-Type", "text/plain")
                    self.end_headers()
                    msg = (
                        "WebSocket upgrade detected. This HTTP server is synchronous and does not support WebSocket upgrades.\n"
                        "For WebSocket support, use the WebSocketHandler with an async server, or use an ASGI server like uvicorn."
                    )
                    self.wfile.write(msg.encode())
                else:
                    # Regular GET request - not allowed
                    self.send_error(405, "Method Not Allowed")

        # Create threaded HTTP server
        class ThreadedHTTPServer(socketserver.ThreadingMixIn, http.server.HTTPServer):
            """HTTP server with threading support."""

            daemon_threads = True

        # Start server in background thread
        def run_server():
            self._server = ThreadedHTTPServer((self._host, self._requested_port), RequestHandler)
            self.port = self._server.server_address[1]
            self._server.serve_forever()

        self._server_thread = threading.Thread(target=run_server, daemon=True)
        self._server_thread.start()

        # Wait for server to start
        import time
        while self.port is None:
            time.sleep(0.01)

        return self

    def stop(self) -> None:
        """Stop the HTTP server."""
        if self._server:
            self._server.shutdown()
            self._server.server_close()
            self._server = None

        if self._server_thread:
            self._server_thread.join(timeout=1.0)
            self._server_thread = None

        self.port = None

    @property
    def url(self) -> Optional[str]:
        """Get server URL."""
        if self._server and self.port:
            return f"http://{self._host}:{self.port}"
        return None
