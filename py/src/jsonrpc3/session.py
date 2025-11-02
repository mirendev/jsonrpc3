"""Session management for JSON-RPC 3.0."""

import secrets
import string
import threading
from abc import ABC, abstractmethod
from dataclasses import dataclass
from datetime import datetime
from typing import Any, Dict, List, Optional


class RpcObject(ABC):
    """Abstract base class for RPC objects that can receive method calls."""

    @abstractmethod
    def call_method(self, method: str, params: Any, caller: Any) -> Any:
        """
        Call a method on this object.

        Args:
            method: Method name to call
            params: Method parameters (Params instance)
            caller: Caller for making callbacks

        Returns:
            Method result

        Raises:
            RpcError: If method not found or execution fails
        """
        pass


@dataclass
class RefInfo:
    """Metadata about a reference."""

    ref: str
    direction: str  # "local" or "remote"
    created: datetime
    type: Optional[str] = None
    last_accessed: Optional[datetime] = None
    metadata: Optional[Dict[str, Any]] = None


class Session:
    """Manages object references for a JSON-RPC connection."""

    def __init__(self, session_id: Optional[str] = None):
        """
        Initialize a new session.

        Args:
            session_id: Optional session ID (generated if not provided)
        """
        # Generate session ID (21 alphanumeric characters, similar to nanoid)
        self.id = session_id or self._generate_id()
        self._created_at = datetime.now()

        # Thread-safe storage
        self._local_refs: Dict[str, Any] = {}
        self._remote_refs: Dict[str, RefInfo] = {}
        self._ref_counter = 0
        self._lock = threading.Lock()

    def _generate_id(self) -> str:
        """Generate a random session ID."""
        alphabet = string.ascii_letters + string.digits
        return "".join(secrets.choice(alphabet) for _ in range(21))

    @property
    def created(self) -> datetime:
        """Get session creation time."""
        return self._created_at

    # Local reference methods (objects we expose to remote peer)

    def add_local_ref(self, obj_or_ref: Any, obj: Any = None) -> str:
        """
        Add a local reference.

        Args:
            obj_or_ref: Either the object to register or a reference ID string
            obj: The object if first param is a reference ID

        Returns:
            Reference ID
        """
        with self._lock:
            if isinstance(obj_or_ref, str):
                # Explicit reference ID provided
                if obj is None:
                    raise ValueError("Object is required when ref is a string")
                ref = obj_or_ref
                actual_obj = obj
            else:
                # Auto-generate reference ID
                self._ref_counter += 1
                ref = f"ref-{self.id}-{self._ref_counter}"
                actual_obj = obj_or_ref

            self._local_refs[ref] = actual_obj
            return ref

    def get_local_ref(self, ref: str) -> Optional[Any]:
        """Get a local reference by ID."""
        with self._lock:
            return self._local_refs.get(ref)

    def has_local_ref(self, ref: str) -> bool:
        """Check if local reference exists."""
        with self._lock:
            return ref in self._local_refs

    def remove_local_ref(self, ref: str) -> bool:
        """
        Remove a local reference.

        Returns:
            True if removed, False if not found
        """
        with self._lock:
            obj = self._local_refs.pop(ref, None)
            if obj is None:
                return False

            # Call dispose or close if available
            self._dispose_object(obj)
            return True

    def list_local_refs(self) -> List[str]:
        """Get list of all local reference IDs."""
        with self._lock:
            return list(self._local_refs.keys())

    def get_local_ref_info(self, ref: str) -> Optional[RefInfo]:
        """Get metadata about a local reference."""
        with self._lock:
            if ref not in self._local_refs:
                return None
            return RefInfo(
                ref=ref,
                direction="local",
                created=self._created_at,
            )

    # Remote reference methods (objects remote peer exposed to us)

    def add_remote_ref(self, ref: str, **info) -> None:
        """Add a remote reference."""
        with self._lock:
            self._remote_refs[ref] = RefInfo(
                ref=ref,
                direction="remote",
                created=datetime.now(),
                **info,
            )

    def get_remote_ref_info(self, ref: str) -> Optional[RefInfo]:
        """Get metadata about a remote reference."""
        with self._lock:
            return self._remote_refs.get(ref)

    def has_remote_ref(self, ref: str) -> bool:
        """Check if remote reference exists."""
        with self._lock:
            return ref in self._remote_refs

    def remove_remote_ref(self, ref: str) -> bool:
        """
        Remove a remote reference.

        Returns:
            True if removed, False if not found
        """
        with self._lock:
            return self._remote_refs.pop(ref, None) is not None

    def list_remote_refs(self) -> List[RefInfo]:
        """Get list of all remote reference info."""
        with self._lock:
            return list(self._remote_refs.values())

    def get_remote_refs(self) -> List[str]:
        """Get list of remote reference IDs."""
        with self._lock:
            return list(self._remote_refs.keys())

    # Cleanup methods

    def dispose_all(self) -> Dict[str, int]:
        """
        Dispose all references (both local and remote).

        Returns:
            Dictionary with counts of disposed references
        """
        with self._lock:
            local_count = len(self._local_refs)
            remote_count = len(self._remote_refs)

            # Dispose all local objects
            for obj in self._local_refs.values():
                self._dispose_object(obj)

            self._local_refs.clear()
            self._remote_refs.clear()

            return {"local_count": local_count, "remote_count": remote_count}

    def _dispose_object(self, obj: Any) -> None:
        """Call dispose or close on an object if available."""
        try:
            if hasattr(obj, "dispose") and callable(obj.dispose):
                result = obj.dispose()
                # Wait for thread if result is a thread
                if isinstance(result, threading.Thread):
                    result.join()
            elif hasattr(obj, "close") and callable(obj.close):
                result = obj.close()
                # Wait for thread if result is a thread
                if isinstance(result, threading.Thread):
                    result.join()
        except Exception:
            # Ignore disposal errors
            pass
