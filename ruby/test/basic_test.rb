# frozen_string_literal: true

require_relative "test_helper"

class BasicTest < Minitest::Test
  def test_version_constant
    assert_equal "3.0", JSONRPC3::VERSION_30
  end

  def test_reference_creation
    ref = JSONRPC3::Reference.new(ref: "test-ref")
    assert_equal "test-ref", ref.ref
    assert_equal({ "$ref" => "test-ref" }, ref.to_h)
  end

  def test_reference_detection
    ref_hash = { "$ref" => "test-ref" }
    assert JSONRPC3.reference?(ref_hash)

    ref_symbol = { :"$ref" => "test-ref" }
    assert JSONRPC3.reference?(ref_symbol)

    not_ref = { "key" => "value" }
    refute JSONRPC3.reference?(not_ref)
  end

  def test_new_request
    req = JSONRPC3.new_request("test_method", { "a" => 1 }, 123, nil)
    assert_equal "3.0", req.jsonrpc
    assert_equal "test_method", req.method
    assert_equal({ "a" => 1 }, req.params)
    assert_equal 123, req.id
    assert_nil req.ref
  end

  def test_request_detection
    req = { "jsonrpc" => "3.0", "method" => "test", "id" => 1 }
    assert JSONRPC3.request?(req)

    notification = { "jsonrpc" => "3.0", "method" => "test" }
    assert JSONRPC3.request?(notification)

    response = { "jsonrpc" => "3.0", "result" => 42, "id" => 1 }
    refute JSONRPC3.request?(response)
  end

  def test_notification_detection
    notification = { "jsonrpc" => "3.0", "method" => "test" }
    assert JSONRPC3.notification?(notification)

    request = { "jsonrpc" => "3.0", "method" => "test", "id" => 1 }
    refute JSONRPC3.notification?(request)
  end

  def test_response_detection
    response = { "jsonrpc" => "3.0", "result" => 42, "id" => 1 }
    assert JSONRPC3.response?(response)

    error_response = { "jsonrpc" => "3.0", "error" => {}, "id" => 1 }
    assert JSONRPC3.response?(error_response)

    request = { "jsonrpc" => "3.0", "method" => "test", "id" => 1 }
    refute JSONRPC3.response?(request)
  end

  def test_rpc_error
    error = JSONRPC3::RpcError.new("Test error", code: -32600, data: { detail: "info" })
    assert_equal "Test error", error.message
    assert_equal(-32600, error.code)
    assert_equal({ detail: "info" }, error.data)

    hash = error.to_h
    assert_equal(-32600, hash[:code])
    assert_equal "Test error", hash[:message]
    assert_equal({ detail: "info" }, hash[:data])
  end

  def test_method_not_found_error
    error = JSONRPC3.method_not_found_error("test")
    assert_instance_of JSONRPC3::RpcError, error
    assert_equal JSONRPC3::CODE_METHOD_NOT_FOUND, error.code
    assert_match(/test/, error.message)
  end

  def test_session_creation
    session = JSONRPC3::Session.new
    assert session.id
    assert_match(/^[a-zA-Z0-9]{21}$/, session.id)
  end

  def test_session_local_refs
    session = JSONRPC3::Session.new
    obj = JSONRPC3::MethodMap.new

    ref = session.add_local_ref(obj)
    assert_match(/^ref-/, ref)

    retrieved = session.get_local_ref(ref)
    assert_equal obj, retrieved
  end

  def test_session_remote_refs
    session = JSONRPC3::Session.new

    session.add_remote_ref("remote-1")
    session.add_remote_ref("remote-2")

    refs = session.get_remote_refs
    assert_includes refs, "remote-1"
    assert_includes refs, "remote-2"
  end

  def test_method_map
    map = JSONRPC3::MethodMap.new

    map.register("add") do |params|
      data = params.decode
      data["a"] + data["b"]
    end

    params = JSONRPC3.new_params({ "a" => 5, "b" => 3 })
    result = map.call_method("add", params)
    assert_equal 8, result
  end

  def test_method_map_introspection
    map = JSONRPC3::MethodMap.new
    map.type = "Counter"

    map.register("increment") { 1 }

    type = map.call_method("$type", JSONRPC3::NULL_PARAMS)
    assert_equal "Counter", type

    methods = map.call_method("$methods", JSONRPC3::NULL_PARAMS)
    assert_includes methods, "increment"
    assert_includes methods, "$type"
    assert_includes methods, "$methods"
    assert_includes methods, "$method"
  end

  def test_method_map_method_info_basic
    map = JSONRPC3::MethodMap.new

    map.register("add") do |params|
      data = params.decode
      data["a"] + data["b"]
    end

    params = JSONRPC3.new_params("add")
    info = map.call_method("$method", params)

    assert_equal "add", info["name"]
    refute info.key?("description")
    refute info.key?("params")
  end

  def test_method_map_method_info_with_description
    map = JSONRPC3::MethodMap.new

    map.register("add", description: "Adds two numbers") do |params|
      data = params.decode
      data["a"] + data["b"]
    end

    params = JSONRPC3.new_params("add")
    info = map.call_method("$method", params)

    assert_equal "add", info["name"]
    assert_equal "Adds two numbers", info["description"]
  end

  def test_method_map_method_info_with_named_params
    map = JSONRPC3::MethodMap.new

    map.register("add",
                 description: "Adds two numbers",
                 params: { "a" => "number", "b" => "number" }) do |params|
      data = params.decode
      data["a"] + data["b"]
    end

    params = JSONRPC3.new_params("add")
    info = map.call_method("$method", params)

    assert_equal "add", info["name"]
    assert_equal "Adds two numbers", info["description"]
    assert_equal({ "a" => "number", "b" => "number" }, info["params"])
  end

  def test_method_map_method_info_with_positional_params
    map = JSONRPC3::MethodMap.new

    map.register("add",
                 description: "Adds two numbers",
                 params: ["number", "number"]) do |params|
      data = params.decode
      data[0] + data[1]
    end

    params = JSONRPC3.new_params("add")
    info = map.call_method("$method", params)

    assert_equal "add", info["name"]
    assert_equal "Adds two numbers", info["description"]
    assert_equal ["number", "number"], info["params"]
  end

  def test_method_map_method_info_nonexistent
    map = JSONRPC3::MethodMap.new

    map.register("add") do |params|
      data = params.decode
      data["a"] + data["b"]
    end

    params = JSONRPC3.new_params("subtract")
    info = map.call_method("$method", params)

    assert_nil info
  end

  def test_method_map_method_info_invalid_params
    map = JSONRPC3::MethodMap.new

    map.register("add") do |params|
      data = params.decode
      data["a"] + data["b"]
    end

    # Pass a number instead of a string
    params = JSONRPC3.new_params(123)

    error = assert_raises(JSONRPC3::RpcError) do
      map.call_method("$method", params)
    end

    assert_equal JSONRPC3::CODE_INVALID_PARAMS, error.code
    assert_match(/string parameter/, error.message)
  end

  def test_method_map_backwards_compatibility
    # Test that register still works without optional parameters
    map = JSONRPC3::MethodMap.new

    map.register("add") do |params|
      data = params.decode
      data["a"] + data["b"]
    end

    # Should work normally
    params = JSONRPC3.new_params({ "a" => 5, "b" => 3 })
    result = map.call_method("add", params)
    assert_equal 8, result

    # Should have basic info
    info_params = JSONRPC3.new_params("add")
    info = map.call_method("$method", info_params)
    assert_equal "add", info["name"]
  end

  def test_json_codec_single_message
    codec = JSONRPC3::JsonCodec.new

    msg = { "jsonrpc" => "3.0", "method" => "test", "id" => 1 }
    json = JSON.generate(msg)

    msg_set = codec.unmarshal_messages(json)
    refute msg_set.is_batch
    assert_equal 1, msg_set.messages.size
    assert_equal "test", msg_set.messages.first["method"]
  end

  def test_json_codec_batch
    codec = JSONRPC3::JsonCodec.new

    msgs = [
      { "jsonrpc" => "3.0", "method" => "test1", "id" => 1 },
      { "jsonrpc" => "3.0", "method" => "test2", "id" => 2 }
    ]
    json = JSON.generate(msgs)

    msg_set = codec.unmarshal_messages(json)
    assert msg_set.is_batch
    assert_equal 2, msg_set.messages.size
  end

  def test_json_codec_marshal
    codec = JSONRPC3::JsonCodec.new

    req = JSONRPC3.new_request("test", { "a" => 1 }, 123, nil)
    msg_set = JSONRPC3::MessageSet.new(messages: [req.to_h], is_batch: false)

    json = codec.marshal_messages(msg_set)
    assert json.is_a?(String)

    parsed = JSON.parse(json)
    assert_equal "test", parsed["method"]
    assert_equal 123, parsed["id"]
  end
end
