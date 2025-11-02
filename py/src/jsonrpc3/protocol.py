"""Protocol handler for built-in $rpc methods."""

from typing import Any, List

from .errors import invalid_params_error
from .params import Params
from .session import RpcObject, Session
from .types import MIME_TYPE_JSON, MIME_TYPE_CBOR


class ProtocolHandler(RpcObject):
    """Handler for built-in $rpc protocol methods."""

    def __init__(self, session: Session, mime_types: List[str] = None):
        """
        Initialize protocol handler.

        Args:
            session: Session to manage
            mime_types: List of supported MIME types
        """
        self.session = session
        self.mime_types = mime_types or [MIME_TYPE_JSON]

    def call_method(self, method: str, params: Params, caller: Any) -> Any:
        """Call a protocol method."""
        if method == "dispose":
            return self.handle_dispose(params)
        elif method == "session_id":
            return self.handle_session_id(params)
        elif method == "list_refs":
            return self.handle_list_refs(params)
        elif method == "ref_info":
            return self.handle_ref_info(params)
        elif method == "mimetypes":
            return self.handle_mimetypes(params)
        elif method == "capabilities":
            return self.handle_capabilities(params)
        else:
            from .errors import method_not_found_error

            raise method_not_found_error(method)

    def handle_dispose(self, params: Params) -> Any:
        """Handle dispose method - dispose one or more references."""
        data = params.decode()
        if data is None:
            raise invalid_params_error("dispose requires parameters")

        refs = data.get("refs", [])
        if not isinstance(refs, list):
            refs = [refs]

        results = []
        for ref in refs:
            if self.session.has_local_ref(ref):
                self.session.remove_local_ref(ref)
                results.append({"ref": ref, "disposed": True})
            else:
                results.append({"ref": ref, "disposed": False, "reason": "not found"})

        return results if len(results) > 1 else results[0] if results else None

    def handle_session_id(self, params: Params) -> dict:
        """Handle session_id method - return session information."""
        return {
            "session_id": self.session.id,
            "created": self.session.created.isoformat(),
        }

    def handle_list_refs(self, params: Params) -> dict:
        """Handle list_refs method - list all references."""
        data = params.decode() or {}
        direction = data.get("direction", "local")

        if direction == "local":
            return {"refs": self.session.list_local_refs()}
        elif direction == "remote":
            return {"refs": self.session.get_remote_refs()}
        elif direction == "all":
            return {
                "local": self.session.list_local_refs(),
                "remote": self.session.get_remote_refs(),
            }
        else:
            raise invalid_params_error(f"Invalid direction: {direction}")

    def handle_ref_info(self, params: Params) -> Any:
        """Handle ref_info method - get information about a reference."""
        data = params.decode()
        if data is None or "ref" not in data:
            raise invalid_params_error("ref_info requires 'ref' parameter")

        ref = data["ref"]

        # Try local first
        info = self.session.get_local_ref_info(ref)
        if info:
            return {
                "ref": info.ref,
                "direction": info.direction,
                "created": info.created.isoformat(),
            }

        # Try remote
        info = self.session.get_remote_ref_info(ref)
        if info:
            return {
                "ref": info.ref,
                "direction": info.direction,
                "created": info.created.isoformat(),
            }

        from .errors import reference_not_found_error

        raise reference_not_found_error(ref)

    def handle_mimetypes(self, params: Params) -> List[str]:
        """Handle mimetypes method - return supported MIME types."""
        return self.mime_types

    def handle_capabilities(self, params: Params) -> List[str]:
        """Handle capabilities method - return server capabilities."""
        caps = [
            "references",
            "batch-local-references",
            "bidirectional-calls",
            "introspection",
        ]

        # Add CBOR if supported
        if MIME_TYPE_CBOR in self.mime_types:
            caps.append("cbor-encoding")

        return caps
