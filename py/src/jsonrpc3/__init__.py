"""JSON-RPC 3.0 Python implementation."""

__version__ = "0.1.0"

# Core types
from .types import (
    VERSION_30,
    MIME_TYPE_JSON,
    MIME_TYPE_CBOR,
    Reference,
    Request,
    Response,
    MessageSet,
    NoOpCaller,
    reference,
    to_ref,
    new_request,
    new_response,
    new_error_response,
    is_reference,
    is_request,
    is_response,
    is_notification,
)

# Errors
from .errors import (
    RpcError,
    CODE_PARSE_ERROR,
    CODE_INVALID_REQUEST,
    CODE_METHOD_NOT_FOUND,
    CODE_INVALID_PARAMS,
    CODE_INTERNAL_ERROR,
    CODE_INVALID_REFERENCE,
    CODE_REFERENCE_NOT_FOUND,
    CODE_REFERENCE_TYPE_ERROR,
    CODE_TIMEOUT,
    CODE_CONNECTION_CLOSED,
    CODE_CONNECTION_ERROR,
    parse_error,
    invalid_request_error,
    method_not_found_error,
    invalid_params_error,
    internal_error,
    invalid_reference_error,
    reference_not_found_error,
    reference_type_error,
)

# Encoding
from .encoding import Codec, JsonCodec, get_codec

# Session and params
from .session import Session, RpcObject, RefInfo
from .params import Params, NULL_PARAMS, new_params

# Handler and protocol
from .handler import Handler
from .protocol import ProtocolHandler

# Helpers
from .helpers import MethodMap, MethodInfo

# Transports
from .http_client import HttpClient
from .http_server import HttpServer
from .peer import Peer
from .websocket_client import WebSocketClient
from .websocket_server import WebSocketHandler, WebSocketServerConn, serve

__all__ = [
    # Version
    "__version__",
    # Constants
    "VERSION_30",
    "MIME_TYPE_JSON",
    "MIME_TYPE_CBOR",
    # Types
    "Reference",
    "Request",
    "Response",
    "MessageSet",
    "reference",
    "to_ref",
    "new_request",
    "new_response",
    "new_error_response",
    "is_reference",
    "is_request",
    "is_response",
    "is_notification",
    # Errors
    "RpcError",
    "CODE_PARSE_ERROR",
    "CODE_INVALID_REQUEST",
    "CODE_METHOD_NOT_FOUND",
    "CODE_INVALID_PARAMS",
    "CODE_INTERNAL_ERROR",
    "CODE_INVALID_REFERENCE",
    "CODE_REFERENCE_NOT_FOUND",
    "CODE_REFERENCE_TYPE_ERROR",
    "CODE_TIMEOUT",
    "CODE_CONNECTION_CLOSED",
    "CODE_CONNECTION_ERROR",
    "parse_error",
    "invalid_request_error",
    "method_not_found_error",
    "invalid_params_error",
    "internal_error",
    "invalid_reference_error",
    "reference_not_found_error",
    "reference_type_error",
    # Encoding
    "Codec",
    "JsonCodec",
    "get_codec",
    # Session and params
    "Session",
    "RpcObject",
    "RefInfo",
    "Params",
    "NULL_PARAMS",
    "new_params",
    # Handler and protocol
    "Handler",
    "ProtocolHandler",
    # Helpers
    "MethodMap",
    "MethodInfo",
    # Transports
    "HttpClient",
    "HttpServer",
    "Peer",
    "WebSocketClient",
    "WebSocketHandler",
    "WebSocketServerConn",
    "serve",
]
