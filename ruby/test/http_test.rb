# frozen_string_literal: true

require_relative "test_helper"

class HttpTest < Minitest::Test
  def setup
    @server_session = JSONRPC3::Session.new
    @root = JSONRPC3::MethodMap.new

    @root.register("add") do |params|
      data = params.decode
      data["a"] + data["b"]
    end

    @root.register("echo") do |params|
      params.decode
    end

    @handler = JSONRPC3::Handler.new(@server_session, @root)
    @server = JSONRPC3::HttpServer.new(@handler, port: 0) # Use random available port
    @server.start

    # Wait for server to be ready
    sleep 0.5

    @client_session = JSONRPC3::Session.new
    @client = JSONRPC3::HttpClient.new(@server.url, @client_session)
  end

  def teardown
    @server.stop
  end

  def test_simple_call
    result = @client.call("add", { "a" => 10, "b" => 20 })
    assert_equal 30, result
  end

  def test_echo
    data = { "foo" => "bar", "num" => 42 }
    result = @client.call("echo", data)
    assert_equal data, result
  end

  def test_object_reference
    @root.register("createCounter") do |_params|
      counter = JSONRPC3::MethodMap.new
      count = 0

      counter.register("increment") { count += 1 }
      counter.register("getValue") { count }

      counter
    end

    # Create counter
    counter_ref = @client.call("createCounter")
    assert counter_ref.is_a?(Hash)
    assert counter_ref["$ref"]

    # Increment
    val1 = @client.call("increment", nil, counter_ref)
    assert_equal 1, val1

    val2 = @client.call("increment", nil, counter_ref)
    assert_equal 2, val2

    # Get value
    final = @client.call("getValue", nil, counter_ref)
    assert_equal 2, final
  end

  def test_protocol_methods
    # Session ID
    session_info = @client.call("session_id", nil, "$rpc")
    assert_equal @server_session.id, session_info["session_id"]

    # Capabilities
    caps = @client.call("capabilities", nil, "$rpc")
    assert_includes caps, "references"
    assert_includes caps, "batch-local-references"
    assert_includes caps, "bidirectional-calls"
    assert_includes caps, "introspection"
  end

  def test_method_not_found
    error = assert_raises JSONRPC3::RpcError do
      @client.call("nonexistent")
    end
    assert_equal JSONRPC3::CODE_METHOD_NOT_FOUND, error.code
  end

  def test_notification
    # Should not raise error and return nil
    result = @client.notify("add", { "a" => 1, "b" => 2 })
    assert_nil result
  end

  def test_batch_requests
    req1 = JSONRPC3.new_request("add", { "a" => 1, "b" => 2 }, 1, nil)
    req2 = JSONRPC3.new_request("add", { "a" => 10, "b" => 20 }, 2, nil)

    responses = @client.batch([req1, req2])
    assert_equal 2, responses.size

    # Find responses by ID
    resp1 = responses.find { |r| r.id == 1 }
    resp2 = responses.find { |r| r.id == 2 }

    assert_equal 3, resp1.result
    assert_equal 30, resp2.result
  end

  def test_reference_convenience_call_method
    @root.register("createCounter") do |_params|
      counter = JSONRPC3::MethodMap.new
      count = 0

      counter.register("increment") { count += 1 }
      counter.register("getValue") { count }

      counter
    end

    # Create counter and get reference
    counter_ref_hash = @client.call("createCounter")

    # Convert hash to Reference object
    counter_ref = JSONRPC3::Reference.from_h(counter_ref_hash)

    # Use convenience call method
    val1 = counter_ref.call(@client, "increment")
    assert_equal 1, val1

    val2 = counter_ref.call(@client, "increment")
    assert_equal 2, val2

    # Get value
    final = counter_ref.call(@client, "getValue")
    assert_equal 2, final
  end

  def test_reference_convenience_notify_method
    @root.register("createCounter") do |_params|
      counter = JSONRPC3::MethodMap.new
      count = 0

      counter.register("increment") { count += 1 }
      counter.register("getValue") { count }

      counter
    end

    # Create counter and get reference
    counter_ref_hash = @client.call("createCounter")

    # Convert hash to Reference object
    counter_ref = JSONRPC3::Reference.from_h(counter_ref_hash)

    # Use convenience notify method (should return nil)
    result = counter_ref.notify(@client, "increment")
    assert_nil result
  end
end
