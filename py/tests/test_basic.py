from jsonrpc3 import NoOpCaller
"""Basic tests for JSON-RPC 3.0 core functionality."""

import json
import pytest

from jsonrpc3 import (
    VERSION_30,
    Reference,
    Request,
    Response,
    MessageSet,
    RpcError,
    CODE_METHOD_NOT_FOUND,
    method_not_found_error,
    new_request,
    is_reference,
    is_request,
    is_response,
    is_notification,
    Session,
    MethodMap,
    JsonCodec,
    NULL_PARAMS,
    new_params,
    to_ref,
)


def test_version_constant():
    """Test version constant."""
    assert VERSION_30 == "3.0"


def test_reference_creation():
    """Test Reference creation and serialization."""
    ref = Reference(ref="test-ref")
    assert ref.ref == "test-ref"
    assert ref.to_dict() == {"$ref": "test-ref"}


def test_reference_detection():
    """Test reference detection."""
    ref_dict = {"$ref": "test-ref"}
    assert is_reference(ref_dict)

    not_ref = {"key": "value"}
    assert not is_reference(not_ref)


def test_new_request():
    """Test creating requests."""
    req = new_request("test_method", {"a": 1}, 123, None)
    assert req.jsonrpc == "3.0"
    assert req.method == "test_method"
    assert req.params == {"a": 1}
    assert req.id == 123
    assert req.ref is None


def test_request_detection():
    """Test request detection."""
    req = {"jsonrpc": "3.0", "method": "test", "id": 1}
    assert is_request(req)

    notification = {"jsonrpc": "3.0", "method": "test"}
    assert is_request(notification)

    response = {"jsonrpc": "3.0", "result": 42, "id": 1}
    assert not is_request(response)


def test_notification_detection():
    """Test notification detection."""
    notification = {"jsonrpc": "3.0", "method": "test"}
    assert is_notification(notification)

    request = {"jsonrpc": "3.0", "method": "test", "id": 1}
    assert not is_notification(request)


def test_response_detection():
    """Test response detection."""
    response = {"jsonrpc": "3.0", "result": 42, "id": 1}
    assert is_response(response)

    error_response = {"jsonrpc": "3.0", "error": {}, "id": 1}
    assert is_response(error_response)

    request = {"jsonrpc": "3.0", "method": "test", "id": 1}
    assert not is_response(request)


def test_rpc_error():
    """Test RPC error."""
    error = RpcError("Test error", code=-32600, data={"detail": "info"})
    assert str(error) == "Test error"
    assert error.code == -32600
    assert error.data == {"detail": "info"}

    error_dict = error.to_dict()
    assert error_dict["code"] == -32600
    assert error_dict["message"] == "Test error"
    assert error_dict["data"] == {"detail": "info"}


def test_method_not_found_error():
    """Test method not found error factory."""
    error = method_not_found_error("test")
    assert isinstance(error, RpcError)
    assert error.code == CODE_METHOD_NOT_FOUND
    assert "test" in str(error)


def test_session_creation():
    """Test session creation."""
    session = Session()
    assert session.id
    assert len(session.id) == 21  # 21 alphanumeric characters


def test_session_local_refs():
    """Test local reference management."""
    session = Session()
    obj = MethodMap()

    ref = session.add_local_ref(obj)
    assert ref.startswith("ref-")

    retrieved = session.get_local_ref(ref)
    assert retrieved is obj


def test_session_remote_refs():
    """Test remote reference management."""
    session = Session()

    session.add_remote_ref("remote-1")
    session.add_remote_ref("remote-2")

    refs = session.get_remote_refs()
    assert "remote-1" in refs
    assert "remote-2" in refs


def test_method_map():
    """Test MethodMap functionality."""
    map_obj = MethodMap()

    def add_handler(params, caller):
        data = params.decode()
        return data["a"] + data["b"]

    map_obj.register("add", add_handler)

    params = new_params({"a": 5, "b": 3})
    result = map_obj.call_method("add", params, NoOpCaller())
    assert result == 8


def test_method_map_introspection():
    """Test MethodMap introspection methods."""
    map_obj = MethodMap()
    map_obj.type = "Counter"

    def increment_handler(params, caller):
        return 1

    map_obj.register("increment", increment_handler)

    type_result = map_obj.call_method("$type", NULL_PARAMS, NoOpCaller())
    assert type_result == "Counter"

    methods = map_obj.call_method("$methods", NULL_PARAMS, NoOpCaller())
    assert "increment" in methods
    assert "$type" in methods
    assert "$methods" in methods
    assert "$method" in methods


def test_method_introspection_with_metadata():
    """Test $method introspection with full metadata."""
    map_obj = MethodMap()

    def add_handler(params, caller):
        data = params.decode()
        return data["a"] + data["b"]

    # Register with full metadata
    map_obj.register(
        "add",
        add_handler,
        description="Adds two numbers",
        params={"a": "number", "b": "number"}
    )

    # Get method info
    result = map_obj.call_method("$method", new_params("add"), NoOpCaller())
    assert result is not None
    assert result["name"] == "add"
    assert result["description"] == "Adds two numbers"
    assert result["params"] == {"a": "number", "b": "number"}


def test_method_introspection_with_positional_params():
    """Test $method introspection with positional parameters."""
    map_obj = MethodMap()

    def multiply_handler(params, caller):
        data = params.decode()
        return data[0] * data[1]

    # Register with positional params
    map_obj.register(
        "multiply",
        multiply_handler,
        description="Multiplies two numbers",
        params=["number", "number"]
    )

    # Get method info
    result = map_obj.call_method("$method", new_params("multiply"), NoOpCaller())
    assert result is not None
    assert result["name"] == "multiply"
    assert result["description"] == "Multiplies two numbers"
    assert result["params"] == ["number", "number"]


def test_method_introspection_minimal():
    """Test $method introspection with minimal metadata."""
    map_obj = MethodMap()

    def simple_handler(params, caller):
        return "ok"

    # Register without optional metadata
    map_obj.register("simple", simple_handler)

    # Get method info
    result = map_obj.call_method("$method", new_params("simple"), NoOpCaller())
    assert result is not None
    assert result["name"] == "simple"
    assert "description" not in result
    assert "params" not in result


def test_method_introspection_nonexistent():
    """Test $method introspection for non-existent method."""
    map_obj = MethodMap()

    # Query non-existent method should return None
    result = map_obj.call_method("$method", new_params("nonexistent"), NoOpCaller())
    assert result is None


def test_method_introspection_invalid_params():
    """Test $method introspection with invalid parameter type."""
    map_obj = MethodMap()

    # Pass non-string parameter should raise error
    with pytest.raises(RpcError) as exc_info:
        map_obj.call_method("$method", new_params({"invalid": "dict"}), NoOpCaller())

    assert exc_info.value.code == -32602  # CODE_INVALID_PARAMS


def test_backwards_compatibility():
    """Test that register() works without optional parameters."""
    map_obj = MethodMap()

    def old_style_handler(params, caller):
        return "works"

    # Should work without description or params arguments
    map_obj.register("old_style", old_style_handler)

    # Should be callable
    result = map_obj.call_method("old_style", NULL_PARAMS, NoOpCaller())
    assert result == "works"

    # Should appear in methods list
    methods = map_obj.call_method("$methods", NULL_PARAMS, NoOpCaller())
    assert "old_style" in methods


def test_json_codec_single_message():
    """Test JSON codec with single message."""
    codec = JsonCodec()

    msg = {"jsonrpc": "3.0", "method": "test", "id": 1}
    json_str = json.dumps(msg)

    msg_set = codec.unmarshal_messages(json_str)
    assert not msg_set.is_batch
    assert len(msg_set.messages) == 1
    assert msg_set.messages[0]["method"] == "test"


def test_json_codec_batch():
    """Test JSON codec with batch messages."""
    codec = JsonCodec()

    msgs = [
        {"jsonrpc": "3.0", "method": "test1", "id": 1},
        {"jsonrpc": "3.0", "method": "test2", "id": 2},
    ]
    json_str = json.dumps(msgs)

    msg_set = codec.unmarshal_messages(json_str)
    assert msg_set.is_batch
    assert len(msg_set.messages) == 2


def test_json_codec_marshal():
    """Test JSON codec marshaling."""
    codec = JsonCodec()

    req = new_request("test", {"a": 1}, 123, None)
    msg_set = MessageSet(messages=[req.to_dict()], is_batch=False)

    json_bytes = codec.marshal_messages(msg_set)
    assert isinstance(json_bytes, bytes)

    parsed = json.loads(json_bytes)
    assert parsed["method"] == "test"
    assert parsed["id"] == 123


def test_to_ref_from_string():
    """Test to_ref() with a string."""
    ref = to_ref("test-ref")
    assert isinstance(ref, Reference)
    assert ref.ref == "test-ref"


def test_to_ref_from_reference():
    """Test to_ref() with a Reference object."""
    original = Reference(ref="test-ref")
    ref = to_ref(original)
    assert ref is original
    assert ref.ref == "test-ref"


def test_reference_call_convenience():
    """Test Reference.call() convenience method."""
    # Create a mock caller
    class MockCaller:
        def __init__(self):
            self.last_call = None

        def call(self, method, params=None, ref=None):
            self.last_call = {"method": method, "params": params, "ref": ref}
            return {"result": "success"}

    caller = MockCaller()
    ref = Reference(ref="test-ref")

    # Call method using convenience method
    result = ref.call(caller, "testMethod", {"param": "value"})

    # Verify the call was made correctly
    assert caller.last_call is not None
    assert caller.last_call["method"] == "testMethod"
    assert caller.last_call["params"] == {"param": "value"}
    assert caller.last_call["ref"] is ref
    assert result == {"result": "success"}


def test_reference_notify_convenience():
    """Test Reference.notify() convenience method."""
    # Create a mock caller
    class MockCaller:
        def __init__(self):
            self.last_notify = None

        def notify(self, method, params=None, ref=None):
            self.last_notify = {"method": method, "params": params, "ref": ref}

    caller = MockCaller()
    ref = Reference(ref="test-ref")

    # Send notification using convenience method
    ref.notify(caller, "notifyMethod", {"param": "value"})

    # Verify the notification was sent correctly
    assert caller.last_notify is not None
    assert caller.last_notify["method"] == "notifyMethod"
    assert caller.last_notify["params"] == {"param": "value"}
    assert caller.last_notify["ref"] is ref


def test_reference_async_call_convenience():
    """Test Reference.async_call() convenience method."""
    import asyncio

    # Create a mock async caller
    class MockAsyncCaller:
        def __init__(self):
            self.last_call = None

        async def call(self, method, params=None, ref=None):
            self.last_call = {"method": method, "params": params, "ref": ref}
            return {"result": "async_success"}

    async def run_test():
        caller = MockAsyncCaller()
        ref = Reference(ref="test-ref")

        # Call method using async convenience method
        result = await ref.async_call(caller, "asyncMethod", {"param": "value"})

        # Verify the call was made correctly
        assert caller.last_call is not None
        assert caller.last_call["method"] == "asyncMethod"
        assert caller.last_call["params"] == {"param": "value"}
        assert caller.last_call["ref"] is ref
        assert result == {"result": "async_success"}

    asyncio.run(run_test())


def test_reference_async_notify_convenience():
    """Test Reference.async_notify() convenience method."""
    import asyncio

    # Create a mock async caller
    class MockAsyncCaller:
        def __init__(self):
            self.last_notify = None

        async def notify(self, method, params=None, ref=None):
            self.last_notify = {"method": method, "params": params, "ref": ref}

    async def run_test():
        caller = MockAsyncCaller()
        ref = Reference(ref="test-ref")

        # Send notification using async convenience method
        await ref.async_notify(caller, "asyncNotify", {"param": "value"})

        # Verify the notification was sent correctly
        assert caller.last_notify is not None
        assert caller.last_notify["method"] == "asyncNotify"
        assert caller.last_notify["params"] == {"param": "value"}
        assert caller.last_notify["ref"] is ref

    asyncio.run(run_test())
