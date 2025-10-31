# frozen_string_literal: true

require_relative "test_helper"
require "net/http"

# Cross-language integration tests
# Tests Ruby clients against Go servers
class CrossLanguageTest < Minitest::Test
  HTTP_PORT = 18083
  GO_SERVER_PROCESS = nil

  def self.startup
    # Start Go test server
    go_server_path = File.expand_path("../../go/jsonrpc3/testserver", __dir__)

    unless File.exist?(go_server_path)
      raise "Go test server not found at #{go_server_path}. Run 'go build' in go/jsonrpc3/cmd/testserver"
    end

    @@go_server_process = spawn(
      go_server_path,
      "-http", HTTP_PORT.to_s,
      out: File::NULL,
      err: File::NULL
    )

    # Wait for server to be ready
    30.times do
      begin
        Net::HTTP.get(URI("http://localhost:#{HTTP_PORT}"))
        break
      rescue Errno::ECONNREFUSED, Errno::EADDRNOTAVAIL
        sleep 0.1
      end
    end

    puts "Go test server started on HTTP port #{HTTP_PORT}"
  end

  def self.shutdown
    if defined?(@@go_server_process) && @@go_server_process
      Process.kill("TERM", @@go_server_process)
      Process.wait(@@go_server_process)
      puts "Go test server stopped"
    end
  end

  def test_ruby_http_client_can_call_go_server_add
    client = JSONRPC3::HttpClient.new("http://localhost:#{HTTP_PORT}")

    result = client.call("add", [2, 3])
    assert_equal 5, result
  end

  def test_ruby_http_client_can_call_go_server_echo
    client = JSONRPC3::HttpClient.new("http://localhost:#{HTTP_PORT}")

    data = { "message" => "hello", "value" => 42 }
    result = client.call("echo", data)
    assert_equal data, result
  end

  def test_ruby_http_client_can_use_batch_with_references
    client = JSONRPC3::HttpClient.new("http://localhost:#{HTTP_PORT}")

    # Use batch to keep everything in same request
    results = JSONRPC3.execute_batch(client) do |b|
      counter = b.call("createCounter", nil)
      counter.call("increment", nil)
      counter.call("increment", nil)
      counter.call("getValue", nil)
    end

    assert_equal 4, results.length

    # First result should be a reference
    counter_ref = results.get_result(0)
    assert counter_ref.is_a?(Hash)
    assert counter_ref.key?("$ref")

    # Subsequent results should be increment values
    assert_equal 1, results.get_result(1)
    assert_equal 2, results.get_result(2)
    assert_equal 2, results.get_result(3)
  end

  def test_ruby_http_client_can_send_notifications
    client = JSONRPC3::HttpClient.new("http://localhost:#{HTTP_PORT}")

    # Should not raise
    client.notify("add", [1, 2])
    assert true
  end

  def test_ruby_http_client_can_send_batch_requests
    client = JSONRPC3::HttpClient.new("http://localhost:#{HTTP_PORT}")

    requests = [
      JSONRPC3::Request.new(jsonrpc: "3.0", method: "add", params: [1, 1], id: 1),
      JSONRPC3::Request.new(jsonrpc: "3.0", method: "add", params: [2, 2], id: 2),
      JSONRPC3::Request.new(jsonrpc: "3.0", method: "add", params: [3, 3], id: 3)
    ]

    responses = client.batch(requests)
    assert_equal 3, responses.length

    results = responses.map(&:result)
    assert_equal [2, 4, 6], results
  end

  def test_ruby_http_client_can_use_batch_builder
    client = JSONRPC3::HttpClient.new("http://localhost:#{HTTP_PORT}")

    results = JSONRPC3.execute_batch(client) do |b|
      b.call("add", [1, 2])
      b.call("add", [3, 4])
      b.call("add", [5, 6])
    end

    assert_equal 3, results.length
    assert_equal 3, results.get_result(0)
    assert_equal 7, results.get_result(1)
    assert_equal 11, results.get_result(2)
  end

  def test_ruby_client_can_query_go_server_methods
    client = JSONRPC3::HttpClient.new("http://localhost:#{HTTP_PORT}")

    # Query available methods
    methods = client.call("$methods")
    assert methods.is_a?(Array)
    assert_includes methods, "add"
    assert_includes methods, "echo"
    assert_includes methods, "createCounter"
    assert_includes methods, "$methods"
    assert_includes methods, "$type"
    assert_includes methods, "$method"
  end

  def test_ruby_client_can_query_go_server_method_info
    client = JSONRPC3::HttpClient.new("http://localhost:#{HTTP_PORT}")

    # Query info for 'add' method
    info = client.call("$method", "add")
    assert_equal "add", info["name"]
    assert_equal "Adds a list of numbers", info["description"]
    assert_equal ["number"], info["params"]

    # Query info for 'echo' method
    info = client.call("$method", "echo")
    assert_equal "echo", info["name"]
    assert_equal "Echoes back the input", info["description"]
    refute info.key?("params")

    # Query info for 'createCounter' method
    info = client.call("$method", "createCounter")
    assert_equal "createCounter", info["name"]
    assert_equal "Creates a new counter object", info["description"]
  end

  def test_ruby_client_can_query_go_counter_introspection
    client = JSONRPC3::HttpClient.new("http://localhost:#{HTTP_PORT}")

    # Create a counter
    counter_ref = client.call("createCounter")
    assert counter_ref.is_a?(Hash)
    assert counter_ref.key?("$ref")

    # Query counter's type
    type = client.call("$type", nil, counter_ref)
    assert_equal "Counter", type

    # Query counter's methods
    methods = client.call("$methods", nil, counter_ref)
    assert_includes methods, "increment"
    assert_includes methods, "getValue"

    # Query counter's method info
    info = client.call("$method", "increment", counter_ref)
    assert_equal "increment", info["name"]
    assert_equal "Increments the counter by 1", info["description"]

    info = client.call("$method", "getValue", counter_ref)
    assert_equal "getValue", info["name"]
    assert_equal "Gets the current counter value", info["description"]
  end

  def test_ruby_client_can_query_nonexistent_method
    client = JSONRPC3::HttpClient.new("http://localhost:#{HTTP_PORT}")

    # Query info for non-existent method
    info = client.call("$method", "nonexistent")
    assert_nil info
  end

end

# Run startup/shutdown hooks
Minitest.after_run do
  CrossLanguageTest.shutdown
end

CrossLanguageTest.startup
