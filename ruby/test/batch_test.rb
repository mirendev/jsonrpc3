# frozen_string_literal: true

require_relative "test_helper"
require "jsonrpc3/batch"

class BatchTest < Minitest::Test
  # Mock caller for testing
  class MockCaller
    attr_accessor :responses

    def initialize
      @responses = []
    end

    def batch(requests)
      @responses
    end
  end

  def test_batch_builder_simple_call
    id_counter = 0
    id_gen = -> { id_counter += 1 }
    builder = JSONRPC3::BatchBuilder.new(id_gen)

    builder.call("testMethod", { value: 42 })

    requests = builder.requests
    assert_equal 1, requests.length
    assert_equal "testMethod", requests[0].method
    assert_equal({ value: 42 }, requests[0].params)
    assert_equal 1, requests[0].id
  end

  def test_batch_builder_multiple_calls
    id_counter = 0
    id_gen = -> { id_counter += 1 }
    builder = JSONRPC3::BatchBuilder.new(id_gen)

    builder.call("method1", { a: 1 })
    builder.call("method2", { b: 2 })
    builder.call("method3", { c: 3 })

    requests = builder.requests
    assert_equal 3, requests.length
    assert_equal "method1", requests[0].method
    assert_equal "method2", requests[1].method
    assert_equal "method3", requests[2].method
    assert_equal 1, requests[0].id
    assert_equal 2, requests[1].id
    assert_equal 3, requests[2].id
  end

  def test_batch_builder_chained_calls
    id_counter = 0
    id_gen = -> { id_counter += 1 }
    builder = JSONRPC3::BatchBuilder.new(id_gen)

    promise1 = builder.call("createCounter", nil)
    promise1.call("increment", nil)
    promise1.call("getValue", nil)

    requests = builder.requests
    assert_equal 3, requests.length

    # First request has no ref
    assert_equal "createCounter", requests[0].method
    assert_nil requests[0].ref

    # Second request references first (\0)
    assert_equal "increment", requests[1].method
    assert_equal "\\0", requests[1].ref

    # Third request also references first (\0)
    assert_equal "getValue", requests[2].method
    assert_equal "\\0", requests[2].ref
  end

  def test_batch_builder_nested_chained_calls
    id_counter = 0
    id_gen = -> { id_counter += 1 }
    builder = JSONRPC3::BatchBuilder.new(id_gen)

    p1 = builder.call("createCounter", nil)
    p2 = p1.call("increment", nil)
    p2.call("increment", nil)

    requests = builder.requests
    assert_equal 3, requests.length

    # First: createCounter
    assert_equal "createCounter", requests[0].method
    assert_nil requests[0].ref

    # Second: increment on \0 (createCounter result)
    assert_equal "increment", requests[1].method
    assert_equal "\\0", requests[1].ref

    # Third: increment on \1 (first increment result)
    assert_equal "increment", requests[2].method
    assert_equal "\\1", requests[2].ref
  end

  def test_batch_results_get_response
    mock_response1 = JSONRPC3::Response.new(
      jsonrpc: "3.0",
      result: 42,
      id: 1
    )
    mock_response2 = JSONRPC3::Response.new(
      jsonrpc: "3.0",
      result: "hello",
      id: 2
    )

    results = JSONRPC3::BatchResults.new([mock_response1, mock_response2])

    assert_equal mock_response1, results.get_response(0)
    assert_equal mock_response2, results.get_response(1)
  end

  def test_batch_results_get_response_out_of_bounds
    mock_response = JSONRPC3::Response.new(
      jsonrpc: "3.0",
      result: 42,
      id: 1
    )

    results = JSONRPC3::BatchResults.new([mock_response])

    error = assert_raises(RuntimeError) { results.get_response(1) }
    assert_match(/out of bounds/, error.message)

    error = assert_raises(RuntimeError) { results.get_response(-1) }
    assert_match(/out of bounds/, error.message)
  end

  def test_batch_results_get_result
    mock_response1 = JSONRPC3::Response.new(
      jsonrpc: "3.0",
      result: 42,
      id: 1
    )
    mock_response2 = JSONRPC3::Response.new(
      jsonrpc: "3.0",
      result: "hello",
      id: 2
    )

    results = JSONRPC3::BatchResults.new([mock_response1, mock_response2])

    assert_equal 42, results.get_result(0)
    assert_equal "hello", results.get_result(1)
  end

  def test_batch_results_get_result_with_error
    mock_error_response = JSONRPC3::Response.new(
      jsonrpc: "3.0",
      error: { "code" => -32600, "message" => "Invalid Request" },
      id: 1
    )

    results = JSONRPC3::BatchResults.new([mock_error_response])

    error = assert_raises(RuntimeError) { results.get_result(0) }
    assert_match(/Invalid Request/, error.message)
  end

  def test_batch_results_get_result_from_notification
    results = JSONRPC3::BatchResults.new([nil])

    error = assert_raises(RuntimeError) { results.get_result(0) }
    assert_match(/No response at index 0/, error.message)
  end

  def test_batch_results_length
    mock_response1 = JSONRPC3::Response.new(jsonrpc: "3.0", result: 42, id: 1)
    mock_response2 = JSONRPC3::Response.new(jsonrpc: "3.0", result: "hello", id: 2)

    results = JSONRPC3::BatchResults.new([mock_response1, mock_response2])
    assert_equal 2, results.length

    empty = JSONRPC3::BatchResults.new([])
    assert_equal 0, empty.length
  end

  def test_batch_results_has_error
    mock_response1 = JSONRPC3::Response.new(jsonrpc: "3.0", result: 42, id: 1)
    mock_response2 = JSONRPC3::Response.new(jsonrpc: "3.0", result: "hello", id: 2)
    mock_error = JSONRPC3::Response.new(
      jsonrpc: "3.0",
      error: { "code" => -32600, "message" => "Invalid Request" },
      id: 3
    )

    no_errors = JSONRPC3::BatchResults.new([mock_response1, mock_response2])
    assert_equal false, no_errors.has_error?

    with_error = JSONRPC3::BatchResults.new([mock_response1, mock_error])
    assert_equal true, with_error.has_error?

    with_nil = JSONRPC3::BatchResults.new([nil, mock_response1])
    assert_equal false, with_nil.has_error?
  end

  def test_execute_batch_simple
    mock_caller = MockCaller.new
    mock_caller.responses = [
      JSONRPC3::Response.new(jsonrpc: "3.0", result: 10, id: 1),
      JSONRPC3::Response.new(jsonrpc: "3.0", result: 20, id: 2)
    ]

    results = JSONRPC3.execute_batch(mock_caller) do |b|
      b.call("add", [1, 2, 3, 4])
      b.call("multiply", [2, 10])
    end

    assert_equal 2, results.length
    assert_equal 10, results.get_result(0)
    assert_equal 20, results.get_result(1)
  end

  def test_execute_batch_with_chained_calls
    mock_caller = MockCaller.new
    mock_caller.responses = [
      JSONRPC3::Response.new(jsonrpc: "3.0", result: { "$ref" => "counter1" }, id: 1),
      JSONRPC3::Response.new(jsonrpc: "3.0", result: 1, id: 2),
      JSONRPC3::Response.new(jsonrpc: "3.0", result: 2, id: 3),
      JSONRPC3::Response.new(jsonrpc: "3.0", result: 2, id: 4)
    ]

    results = JSONRPC3.execute_batch(mock_caller) do |b|
      counter = b.call("createCounter", nil)
      counter.call("increment", nil)
      counter.call("increment", nil)
      counter.call("getValue", nil)
    end

    assert_equal 4, results.length
    assert_equal({ "$ref" => "counter1" }, results.get_result(0))
    assert_equal 1, results.get_result(1)
    assert_equal 2, results.get_result(2)
    assert_equal 2, results.get_result(3)
  end

  def test_execute_batch_empty
    mock_caller = MockCaller.new

    results = JSONRPC3.execute_batch(mock_caller) do |_b|
      # Don't add any calls
    end

    assert_equal 0, results.length
  end
end
