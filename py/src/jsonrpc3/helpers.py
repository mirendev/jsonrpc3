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
        category: Optional category for the method
    """
    name: str
    handler: Callable[[Params, Any], Any]
    description: Optional[str] = None
    params: Optional[Union[Dict[str, str], List[str]]] = None
    category: Optional[str] = None


class MethodMap(RpcObject):
    """
    Simple RPC object implementation with dict-based method dispatch.

    Provides an easy way to create RPC objects by registering handler functions.
    Automatically supports $methods and $type introspection methods.
    """

    def __init__(self):
        """Initialize empty method map."""
        self._methods: Dict[str, MethodInfo] = {}
        self.type: Optional[str] = None

    def register(
        self,
        method_name: str,
        handler: Callable[[Params, Any], Any],
        description: Optional[str] = None,
        params: Optional[Union[Dict[str, str], List[str]]] = None,
        category: Optional[str] = None
    ) -> None:
        """
        Register a method handler with optional metadata for introspection.

        Args:
            method_name: Name of the method
            handler: Callable that takes Params, Caller and returns result
            description: Optional human-readable description of the method
            params: Optional parameter specification. Can be:
                - Dict[str, str] for named parameters (e.g., {"a": "number", "b": "number"})
                - List[str] for positional parameters (e.g., ["number", "number"])
            category: Optional category for the method

        Example:
            map.register("add", add_handler,
                        description="Adds two numbers",
                        params={"a": "number", "b": "number"},
                        category="math")
        """
        info = MethodInfo(
            name=method_name,
            handler=handler,
            description=description,
            params=params,
            category=category
        )
        self._methods[method_name] = info

    def call_method(self, method: str, params: Params, caller: Any) -> Any:
        """
        Call a method on this object.

        Supports built-in introspection methods:
        - $methods: List all available methods with metadata
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
        method_info = self._methods.get(method)
        if method_info is None:
            raise method_not_found_error(method)

        return method_info.handler(params, caller)

    def _get_methods(self) -> List[Dict[str, Any]]:
        """Get detailed information about all available methods including introspection methods."""
        methods = []

        # Add user-registered methods with their metadata
        for method_info in self._methods.values():
            method_dict: Dict[str, Any] = {
                "name": method_info.name
            }

            if method_info.description is not None:
                method_dict["description"] = method_info.description

            if method_info.params is not None:
                method_dict["params"] = method_info.params

            if method_info.category is not None:
                method_dict["category"] = method_info.category

            methods.append(method_dict)

        # Add introspection methods
        methods.append({"name": "$methods"})
        methods.append({"name": "$type"})

        return methods

    def _get_type(self) -> Optional[str]:
        """Get object type."""
        return self.type
