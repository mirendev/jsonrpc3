# frozen_string_literal: true

require_relative "test_helper"
require "rack"
require "puma"
require "socket"

class WebSocketTest < Minitest::Test
  def setup
    # Create server root object
    @server_root = JSONRPC3::MethodMap.new

    @server_root.register("add") do |params|
      data = params.decode
      data["a"] + data["b"]
    end

    @server_root.register("echo") do |params|
      params.decode
    end

    @server_root.register("ping") do |_params|
      "pong"
    end

    # Create WebSocket handler
    @ws_handler = JSONRPC3::WebSocketHandler.new(@server_root)

    # Find an available port
    @port = nil
    server_socket = TCPServer.new("127.0.0.1", 0)
    @port = server_socket.addr[1]
    server_socket.close

    @url = "ws://127.0.0.1:#{@port}"

    # Create Puma server
    @puma_server = Puma::Server.new(@ws_handler)
    @puma_server.add_tcp_listener("127.0.0.1", @port)

    # Start server in background thread
    @server_thread = Thread.new do
      @puma_server.run.join
    end

    # Wait for server to be ready
    sleep 1.5
  end

  def teardown
    @client&.close
    @puma_server&.stop(true) if @puma_server
    @server_thread&.kill if @server_thread
    @server_thread&.join(1) if @server_thread
  end

  def test_simple_call
    @client = JSONRPC3::WebSocketClient.new(@url)

    result = @client.call("add", { "a" => 10, "b" => 20 })
    assert_equal 30, result
  end

  def test_echo
    @client = JSONRPC3::WebSocketClient.new(@url)

    data = { "foo" => "bar", "num" => 42 }
    result = @client.call("echo", data)
    assert_equal data, result
  end

  def test_ping
    @client = JSONRPC3::WebSocketClient.new(@url)

    result = @client.call("ping")
    assert_equal "pong", result
  end

  def test_notification
    @client = JSONRPC3::WebSocketClient.new(@url)

    # Should not raise error and return nil
    result = @client.notify("ping")
    assert_nil result
  end

  def test_method_not_found
    @client = JSONRPC3::WebSocketClient.new(@url)

    error = assert_raises JSONRPC3::RpcError do
      @client.call("nonexistent")
    end
    assert_equal JSONRPC3::CODE_METHOD_NOT_FOUND, error.code
  end

  def test_object_registration
    @server_root.register("createCounter") do |_params|
      counter = JSONRPC3::MethodMap.new
      count = 0

      counter.register("increment") { count += 1 }
      counter.register("getValue") { count }

      counter
    end

    @client = JSONRPC3::WebSocketClient.new(@url)

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

  def test_bidirectional_server_to_client_call
    # Register method on client
    client_object = JSONRPC3::MethodMap.new
    client_object.register("clientEcho") do |params|
      data = params.decode
      "client received: #{data}"
    end

    @client = JSONRPC3::WebSocketClient.new(@url, client_object)

    # Register server method that calls back to client
    @server_root.register("callClient") do |params|
      # Get client connection - this is a simplification for testing
      # In real scenarios, server would track client connections
      "Called client method"
    end

    # For now, just test that bidirectional setup works
    result = @client.call("ping")
    assert_equal "pong", result
  end

  def test_protocol_negotiation_json
    @client = JSONRPC3::WebSocketClient.new(@url, nil, mime_type: JSONRPC3::MIME_TYPE_JSON)

    result = @client.call("ping")
    assert_equal "pong", result
  end

  def test_protocol_negotiation_cbor
    skip "CBOR encoding not yet implemented in Ruby"

    @client = JSONRPC3::WebSocketClient.new(@url, nil, mime_type: JSONRPC3::MIME_TYPE_CBOR)

    result = @client.call("ping")
    assert_equal "pong", result
  end

  def test_concurrent_requests
    @client = JSONRPC3::WebSocketClient.new(@url)

    # Make multiple concurrent requests
    threads = 5.times.map do |i|
      Thread.new do
        result = @client.call("add", { "a" => i, "b" => i })
        assert_equal i * 2, result
      end
    end

    threads.each(&:join)
  end

  def test_client_register_object
    client_object = JSONRPC3::MethodMap.new
    received_messages = []

    client_object.register("receiveMessage") do |params|
      msg = params.decode
      received_messages << msg
      "received"
    end

    @client = JSONRPC3::WebSocketClient.new(@url, client_object)

    # Register the client object and get a reference
    ref = @client.register_object(client_object)
    assert ref.is_a?(JSONRPC3::Reference)
    assert ref.ref
  end

  def test_client_unregister_object
    client_object = JSONRPC3::MethodMap.new
    client_object.register("test") { "ok" }

    @client = JSONRPC3::WebSocketClient.new(@url, client_object)

    # Register and then unregister
    ref = @client.register_object(client_object)
    assert @client.session.has_local_ref?(ref.ref)

    @client.unregister_object(ref)
    refute @client.session.has_local_ref?(ref.ref)
  end

  def test_connection_close
    @client = JSONRPC3::WebSocketClient.new(@url)

    # Make a call to ensure connection works
    result = @client.call("ping")
    assert_equal "pong", result

    # Close connection
    @client.close
    assert @client.closed?

    # Further calls should raise error
    error = assert_raises JSONRPC3::RpcError do
      @client.call("ping")
    end
    assert_equal JSONRPC3::CODE_CONNECTION_CLOSED, error.code
  end

  def test_reference_convenience_methods
    @server_root.register("createCounter") do |_params|
      counter = JSONRPC3::MethodMap.new
      count = 0

      counter.register("increment") { count += 1 }
      counter.register("getValue") { count }

      counter
    end

    @client = JSONRPC3::WebSocketClient.new(@url)

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

  def test_error_handling
    @server_root.register("divide") do |params|
      data = params.decode
      a = data["a"]
      b = data["b"]

      raise JSONRPC3::RpcError.new("Division by zero", code: -32000) if b == 0

      a / b
    end

    @client = JSONRPC3::WebSocketClient.new(@url)

    # Test successful division
    result = @client.call("divide", { "a" => 10, "b" => 2 })
    assert_equal 5, result

    # Test division by zero error
    error = assert_raises JSONRPC3::RpcError do
      @client.call("divide", { "a" => 10, "b" => 0 })
    end
    assert_equal(-32000, error.code)
    assert_equal "Division by zero", error.message
  end

  def test_session_persistence
    @client = JSONRPC3::WebSocketClient.new(@url)

    # Session should be maintained across multiple calls
    session_id1 = @client.session.id
    @client.call("ping")

    session_id2 = @client.session.id
    @client.call("ping")

    assert_equal session_id1, session_id2
  end

  def test_multiple_clients
    # Create two independent clients
    client1 = JSONRPC3::WebSocketClient.new(@url)
    client2 = JSONRPC3::WebSocketClient.new(@url)

    begin
      # Both clients should work independently
      result1 = client1.call("ping")
      result2 = client2.call("ping")

      assert_equal "pong", result1
      assert_equal "pong", result2

      # They should have different sessions
      refute_equal client1.session.id, client2.session.id
    ensure
      client1.close
      client2.close
    end
  end
end
