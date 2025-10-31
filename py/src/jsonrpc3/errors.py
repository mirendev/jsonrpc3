"""JSON-RPC 3.0 error codes and error handling."""

from typing import Any, Optional

# Standard JSON-RPC error codes
CODE_PARSE_ERROR = -32700
CODE_INVALID_REQUEST = -32600
CODE_METHOD_NOT_FOUND = -32601
CODE_INVALID_PARAMS = -32602
CODE_INTERNAL_ERROR = -32603

# JSON-RPC 3.0 specific error codes
CODE_INVALID_REFERENCE = -32000
CODE_REFERENCE_NOT_FOUND = -32001
CODE_REFERENCE_TYPE_ERROR = -32002


class RpcError(Exception):
    """JSON-RPC error with code and optional data."""

    def __init__(self, message: str, code: int = CODE_INTERNAL_ERROR, data: Any = None):
        super().__init__(message)
        self.code = code
        self.data = data

    def to_dict(self) -> dict:
        """Convert to dictionary representation."""
        result = {
            "code": self.code,
            "message": str(self),
        }
        if self.data is not None:
            result["data"] = self.data
        return result

    @classmethod
    def from_dict(cls, error_dict: dict) -> "RpcError":
        """Create from dictionary representation."""
        return cls(
            message=error_dict.get("message", "Unknown error"),
            code=error_dict.get("code", CODE_INTERNAL_ERROR),
            data=error_dict.get("data"),
        )


# Error factory functions

def parse_error(message: str = "Parse error", data: Any = None) -> RpcError:
    """Create a parse error."""
    return RpcError(message, code=CODE_PARSE_ERROR, data=data)


def invalid_request_error(message: str = "Invalid request", data: Any = None) -> RpcError:
    """Create an invalid request error."""
    return RpcError(message, code=CODE_INVALID_REQUEST, data=data)


def method_not_found_error(method: str, data: Any = None) -> RpcError:
    """Create a method not found error."""
    return RpcError(
        f"Method not found: {method}",
        code=CODE_METHOD_NOT_FOUND,
        data={"method": method, **(data or {})},
    )


def invalid_params_error(message: str = "Invalid params", data: Any = None) -> RpcError:
    """Create an invalid params error."""
    return RpcError(message, code=CODE_INVALID_PARAMS, data=data)


def internal_error(message: str = "Internal error", data: Any = None) -> RpcError:
    """Create an internal error."""
    return RpcError(message, code=CODE_INTERNAL_ERROR, data=data)


def invalid_reference_error(ref: str, message: Optional[str] = None, data: Any = None) -> RpcError:
    """Create an invalid reference error."""
    msg = message or f"Invalid reference: {ref}"
    return RpcError(
        msg,
        code=CODE_INVALID_REFERENCE,
        data={"ref": ref, **(data or {})},
    )


def reference_not_found_error(ref: str, message: Optional[str] = None, data: Any = None) -> RpcError:
    """Create a reference not found error."""
    msg = message or f"Reference not found: {ref}"
    return RpcError(
        msg,
        code=CODE_REFERENCE_NOT_FOUND,
        data={"ref": ref, **(data or {})},
    )


def reference_type_error(ref: str, message: Optional[str] = None, data: Any = None) -> RpcError:
    """Create a reference type error."""
    msg = message or f"Reference type error: {ref}"
    return RpcError(
        msg,
        code=CODE_REFERENCE_TYPE_ERROR,
        data={"ref": ref, **(data or {})},
    )
