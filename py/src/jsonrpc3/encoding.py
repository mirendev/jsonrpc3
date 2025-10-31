"""Message encoding and decoding."""

import json
from abc import ABC, abstractmethod
from typing import Any

from .types import MessageSet, MIME_TYPE_JSON, MIME_TYPE_CBOR


class Codec(ABC):
    """Abstract base class for message codecs."""

    @property
    @abstractmethod
    def mime_type(self) -> str:
        """Get the MIME type for this codec."""
        pass

    @abstractmethod
    def marshal_messages(self, msg_set: MessageSet) -> bytes:
        """Encode a MessageSet to bytes."""
        pass

    @abstractmethod
    def unmarshal_messages(self, data: bytes) -> MessageSet:
        """Decode bytes to a MessageSet."""
        pass


class JsonCodec(Codec):
    """JSON codec for JSON-RPC 3.0 messages."""

    @property
    def mime_type(self) -> str:
        """Get the MIME type for JSON."""
        return MIME_TYPE_JSON

    def marshal_messages(self, msg_set: MessageSet) -> bytes:
        """Encode a MessageSet to JSON bytes."""
        data = msg_set.to_dict()
        json_str = json.dumps(data, separators=(",", ":"))
        return json_str.encode("utf-8")

    def unmarshal_messages(self, data: bytes) -> MessageSet:
        """Decode JSON bytes to a MessageSet."""
        # Handle both bytes and str
        if isinstance(data, bytes):
            json_str = data.decode("utf-8")
        else:
            json_str = data

        # Detect batch by looking at first character
        trimmed = json_str.lstrip()
        is_batch = trimmed.startswith("[")

        # Parse JSON
        decoded = json.loads(json_str)

        # Create MessageSet
        if is_batch:
            return MessageSet(messages=decoded, is_batch=True)
        else:
            return MessageSet(messages=[decoded], is_batch=False)


def get_codec(mime_type: str = MIME_TYPE_JSON) -> Codec:
    """Get a codec by MIME type."""
    if mime_type == MIME_TYPE_JSON:
        return JsonCodec()
    elif mime_type == MIME_TYPE_CBOR:
        raise NotImplementedError("CBOR encoding not yet implemented")
    else:
        raise ValueError(f"Unknown MIME type: {mime_type}")
