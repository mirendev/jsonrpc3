"""Request handler for JSON-RPC 3.0."""

from typing import Any, List, Union

from .errors import (
    RpcError,
    internal_error,
    invalid_reference_error,
    reference_not_found_error,
    reference_type_error,
)
from .params import NULL_PARAMS, new_params
from .protocol import ProtocolHandler
from .session import RpcObject, Session
from .types import (
    Request,
    Response,
    VERSION_30,
    is_notification,
    is_reference,
    new_error_response,
    new_response,
)


class Handler:
    """Handles JSON-RPC 3.0 requests and routes to appropriate objects."""

    def __init__(
        self,
        session: Session,
        root_object: RpcObject,
        caller: Any,
        mime_types: List[str] = None,
    ):
        """
        Initialize handler.

        Args:
            session: Session for reference management
            root_object: Root RPC object for method dispatch
            caller: Caller for making callbacks
            mime_types: Supported MIME types
        """
        self.session = session
        self.root_object = root_object
        self.caller = caller
        self.protocol = ProtocolHandler(session, mime_types)

    def handle_request(self, req: Union[dict, Request]) -> Response:
        """
        Handle a single request.

        Args:
            req: Request message (dict or Request object)

        Returns:
            Response message
        """
        try:
            # Determine which object to call
            ref = req.ref if isinstance(req, Request) else req.get("ref")

            if ref == "$rpc":
                obj = self.protocol
            elif ref:
                local_obj = self.session.get_local_ref(ref)
                if not local_obj:
                    raise reference_not_found_error(ref)
                obj = local_obj
            else:
                obj = self.root_object

            # Extract parameters
            params_value = req.params if isinstance(req, Request) else req.get("params")
            params = new_params(params_value) if params_value is not None else NULL_PARAMS

            # Call method
            method = req.method if isinstance(req, Request) else req["method"]
            result = obj.call_method(method, params, self.caller)

            # Process result for automatic reference registration
            processed_result = self._process_result(result)

            # Create response
            jsonrpc = req.jsonrpc if isinstance(req, Request) else req.get("jsonrpc", VERSION_30)
            req_id = req.id if isinstance(req, Request) else req.get("id")
            return new_response(processed_result, req_id, jsonrpc)

        except RpcError as e:
            # Create error response
            jsonrpc = req.jsonrpc if isinstance(req, Request) else req.get("jsonrpc", VERSION_30)
            req_id = req.id if isinstance(req, Request) else req.get("id")
            return new_error_response(e.to_dict(), req_id, jsonrpc)

        except Exception as e:
            # Convert to internal error
            rpc_error = internal_error(str(e))
            jsonrpc = req.jsonrpc if isinstance(req, Request) else req.get("jsonrpc", VERSION_30)
            req_id = req.id if isinstance(req, Request) else req.get("id")
            return new_error_response(rpc_error.to_dict(), req_id, jsonrpc)

    def handle_batch(self, batch: List[Union[dict, Request]]) -> List[Response]:
        """
        Handle a batch request.

        Args:
            batch: List of requests

        Returns:
            List of responses (excluding notifications)
        """
        responses = []
        batch_results = []

        for i, req in enumerate(batch):
            try:
                # Resolve batch-local references
                resolved_ref = req.ref if isinstance(req, Request) else req.get("ref")
                if resolved_ref and resolved_ref.startswith("\\"):
                    resolved_ref = self._resolve_batch_local_ref(
                        resolved_ref, i, batch_results
                    )

                # Create modified request with resolved ref
                if isinstance(req, Request):
                    resolved_req = Request(
                        jsonrpc=req.jsonrpc,
                        method=req.method,
                        params=req.params,
                        id=req.id,
                        ref=resolved_ref,
                    )
                else:
                    resolved_req = {**req, "ref": resolved_ref}

                # Handle the request
                resp = self.handle_request(resolved_req)

                # Store result for batch-local reference resolution
                batch_results.append(resp.result if isinstance(resp, Response) else resp.get("result"))

                # Only add response if not a notification
                is_notif = is_notification(req)
                if not is_notif:
                    responses.append(resp)

            except Exception as e:
                # Store None for failed requests
                batch_results.append(None)

                # Only add error response if not a notification
                is_notif = is_notification(req)
                if not is_notif:
                    rpc_error = e if isinstance(e, RpcError) else internal_error(str(e))
                    jsonrpc = req.jsonrpc if isinstance(req, Request) else req.get("jsonrpc", VERSION_30)
                    req_id = req.id if isinstance(req, Request) else req.get("id")
                    responses.append(new_error_response(rpc_error.to_dict(), req_id, jsonrpc))

        return responses

    def _resolve_batch_local_ref(
        self, ref: str, current_index: int, results: List[Any]
    ) -> str:
        """Resolve batch-local reference (\\0, \\1, etc.)."""
        try:
            # Parse index from \\N format
            index = int(ref[1:])

            # Check for forward reference
            if index >= current_index:
                raise invalid_reference_error(
                    ref, f"Forward reference not allowed: {ref} at index {current_index}"
                )

            # Check bounds
            if index < 0 or index >= len(results):
                raise invalid_reference_error(ref, f"Batch-local reference out of bounds: {ref}")

            # Get the result
            result = results[index]
            if result is None:
                raise invalid_reference_error(ref, f"Referenced request failed: index {index}")

            # Result must be a Reference
            if not is_reference(result):
                raise reference_type_error(
                    ref, f"Result at index {index} is not a reference"
                )

            return result["$ref"]

        except ValueError:
            raise invalid_reference_error(ref, f"Invalid batch-local reference format: {ref}")

    def _process_result(self, result: Any) -> Any:
        """Process result to handle automatic reference registration."""
        # Nil or basic types - return as-is
        if result is None or isinstance(result, (str, int, float, bool)):
            return result

        # If it's already a Reference, return as-is
        if is_reference(result):
            return result

        # Check if result implements RpcObject
        if self._is_rpc_object(result):
            # Register and return Reference
            ref = self.session.add_local_ref(result)
            return {"$ref": ref}
        elif isinstance(result, list):
            # Process array elements
            return [self._process_result(item) for item in result]
        elif isinstance(result, dict):
            # Process dict values
            return {k: self._process_result(v) for k, v in result.items()}
        else:
            # Other types return as-is
            return result

    def _is_rpc_object(self, value: Any) -> bool:
        """Check if value implements RpcObject interface."""
        return isinstance(value, RpcObject) or (
            hasattr(value, "call_method") and callable(value.call_method)
        )
