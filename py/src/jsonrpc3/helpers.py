"""Helper utilities for creating RPC objects."""

from typing import Any, Callable, Dict, Optional

from .errors import method_not_found_error
from .params import Params
from .session import RpcObject


class MethodMap(RpcObject):
    """
    Simple RPC object implementation with dict-based method dispatch.

    Provides an easy way to create RPC objects by registering handler functions.
    """

    def __init__(self):
        """Initialize empty method map."""
        self._methods: Dict[str, Callable] = {}
        self.type: Optional[str] = None

    def register(self, method_name: str, handler: Callable[[Params], Any]) -> None:
        """
        Register a method handler.

        Args:
            method_name: Name of the method
            handler: Callable that takes Params and returns result
        """
        self._methods[method_name] = handler

    def call_method(self, method: str, params: Params) -> Any:
        """
        Call a method on this object.

        Supports built-in introspection methods:
        - $methods: List all available methods
        - $type: Get object type

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

        # User-defined methods
        handler = self._methods.get(method)
        if handler is None:
            raise method_not_found_error(method)

        return handler(params)

    def _get_methods(self) -> list:
        """Get list of available methods."""
        methods = list(self._methods.keys())
        methods.extend(["$methods", "$type"])
        return sorted(methods)

    def _get_type(self) -> Optional[str]:
        """Get object type."""
        return self.type
