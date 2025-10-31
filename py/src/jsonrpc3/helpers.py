"""Helper utilities for creating RPC objects."""

from dataclasses import dataclass
from typing import Any, Callable, Dict, List, Optional, Union

from .errors import invalid_params_error, method_not_found_error
from .params import Params
from .session import RpcObject


@dataclass
class MethodInfo:
    """
    Metadata about a registered method for introspection.

    Attributes:
        name: The method name
        handler: The callable that handles the method
        description: Optional human-readable description
        params: Optional parameter specification (dict for named params, list for positional)
    """
    name: str
    handler: Callable[[Params], Any]
    description: Optional[str] = None
    params: Optional[Union[Dict[str, str], List[str]]] = None


class MethodMap(RpcObject):
    """
    Simple RPC object implementation with dict-based method dispatch.

    Provides an easy way to create RPC objects by registering handler functions.
    Automatically supports $methods, $type, and $method introspection methods.
    """

    def __init__(self):
        """Initialize empty method map."""
        self._methods: Dict[str, MethodInfo] = {}
        self.type: Optional[str] = None

    def register(
        self,
        method_name: str,
        handler: Callable[[Params], Any],
        description: Optional[str] = None,
        params: Optional[Union[Dict[str, str], List[str]]] = None
    ) -> None:
        """
        Register a method handler with optional metadata for introspection.

        Args:
            method_name: Name of the method
            handler: Callable that takes Params and returns result
            description: Optional human-readable description of the method
            params: Optional parameter specification. Can be:
                - Dict[str, str] for named parameters (e.g., {"a": "number", "b": "number"})
                - List[str] for positional parameters (e.g., ["number", "number"])

        Example:
            map.register("add", add_handler,
                        description="Adds two numbers",
                        params={"a": "number", "b": "number"})
        """
        info = MethodInfo(
            name=method_name,
            handler=handler,
            description=description,
            params=params
        )
        self._methods[method_name] = info

    def call_method(self, method: str, params: Params) -> Any:
        """
        Call a method on this object.

        Supports built-in introspection methods:
        - $methods: List all available methods
        - $type: Get object type
        - $method: Get metadata about a specific method

        Args:
            method: Method name
            params: Method parameters

        Returns:
            Method result

        Raises:
            RpcError: If method not found
        """
        # Built-in introspection methods
        if method == "$methods":
            return self._get_methods()
        elif method == "$type":
            return self._get_type()
        elif method == "$method":
            return self._get_method_info(params)

        # User-defined methods
        method_info = self._methods.get(method)
        if method_info is None:
            raise method_not_found_error(method)

        return method_info.handler(params)

    def _get_methods(self) -> list:
        """Get list of available methods including introspection methods."""
        methods = list(self._methods.keys())
        methods.extend(["$methods", "$type", "$method"])
        return sorted(methods)

    def _get_type(self) -> Optional[str]:
        """Get object type."""
        return self.type

    def _get_method_info(self, params: Params) -> Optional[Dict[str, Any]]:
        """
        Get detailed information about a specific method.

        Implements the $method introspection method.

        Args:
            params: Should contain a string with the method name

        Returns:
            Dict with method metadata (name, description, params) or None if method not found

        Raises:
            RpcError: If params is not a string
        """
        method_name = params.decode()

        # Validate that params is a string
        if not isinstance(method_name, str):
            raise invalid_params_error("$method expects a string parameter")

        # Look up the method info
        method_info = self._methods.get(method_name)
        if method_info is None:
            return None  # Return null for non-existent methods

        # Build result object with only non-empty fields
        result: Dict[str, Any] = {
            "name": method_info.name
        }

        if method_info.description is not None:
            result["description"] = method_info.description

        if method_info.params is not None:
            result["params"] = method_info.params

        return result
