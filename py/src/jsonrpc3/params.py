"""Parameter wrapper for JSON-RPC 3.0."""

from typing import Any


class Params:
    """Wrapper for method parameters."""

    def __init__(self, value: Any):
        self._value = value

    def decode(self) -> Any:
        """Get the decoded parameter value."""
        return self._value

    def __repr__(self) -> str:
        return f"Params({self._value!r})"


# Singleton for null parameters
NULL_PARAMS = Params(None)


def new_params(value: Any) -> Params:
    """Create a new Params instance."""
    if value is None:
        return NULL_PARAMS
    return Params(value)
