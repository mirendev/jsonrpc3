# frozen_string_literal: true

module JSONRPC3
  # BatchBuilder collects requests to be sent as a single batch.
  # It automatically manages batch-local references when chaining method calls.
  class BatchBuilder
    attr_reader :requests

    def initialize(id_gen)
      @requests = []
      @id_gen = id_gen
    end

    # Add a method call to the batch and return a promise that can be chained.
    # @param method [String] Method name to call
    # @param params [Object] Method parameters
    # @return [BatchBuilderPromise] Promise for chaining
    def call(method, params = nil)
      id = @id_gen.call

      request = Request.new(
        jsonrpc: "3.0",
        method: method,
        params: params,
        id: id
      )

      @requests << request

      BatchBuilderPromise.new(self, @requests.length - 1)
    end
  end

  # BatchBuilderPromise represents a pending result in a batch request.
  # It can be used to chain method calls that reference the result of a previous call
  # using batch-local references (\0, \1, \2, etc.).
  class BatchBuilderPromise
    def initialize(builder, index)
      @builder = builder
      @index = index
    end

    # Chain a method call on the result of the previous call.
    # This automatically uses a batch-local reference (e.g., \0, \1) to reference
    # the result from the promise's position in the batch.
    # @param method [String] Method name to call
    # @param params [Object] Method parameters
    # @return [BatchBuilderPromise] New promise for further chaining
    def call(method, params = nil)
      id = @builder.instance_variable_get(:@id_gen).call

      # Create batch-local reference: \0, \1, \2, etc.
      ref = "\\#{@index}"

      request = Request.new(
        jsonrpc: "3.0",
        ref: ref,
        method: method,
        params: params,
        id: id
      )

      @builder.requests << request

      BatchBuilderPromise.new(@builder, @builder.requests.length - 1)
    end
  end

  # BatchResults contains the responses from a batch request.
  class BatchResults
    def initialize(responses)
      @responses = responses
    end

    # Get the raw response at the specified index.
    # @param index [Integer] Index of the response
    # @return [Response, nil] Response or nil for notifications
    def get_response(index)
      raise "Index #{index} out of bounds (batch size: #{@responses.length})" if index < 0 || index >= @responses.length

      @responses[index]
    end

    # Get and decode the result at the specified index.
    # @param index [Integer] Index of the response
    # @return [Object] Decoded result
    # @raise [StandardError] If response is nil, has an error, or index is out of bounds
    def get_result(index)
      response = get_response(index)

      raise "No response at index #{index} (notification)" if response.nil?

      if response.error
        raise "Error at index #{index}: #{response.error["message"]} (code: #{response.error["code"]})"
      end

      response.result
    end

    # Get the number of responses in the batch.
    # @return [Integer] Number of responses
    def length
      @responses.length
    end

    alias size length

    # Check if any response in the batch contains an error.
    # @return [Boolean] True if any response has an error
    def has_error?
      @responses.any? { |resp| resp && resp.error }
    end

    # Get all responses (including nils for notifications).
    # @return [Array<Response, nil>] All responses
    def all_responses
      @responses
    end
  end

  # ExecuteBatch executes a batch of requests using the builder pattern with automatic
  # batch-local reference management. The provided block receives a BatchBuilder
  # that collects requests. When the block returns, the batch is sent via the
  # provided caller and responses are returned.
  #
  # @example
  #   results = JSONRPC3.execute_batch(client) do |b|
  #     counter = b.call("createCounter", nil)
  #     counter.call("increment", nil)  # Uses \0 reference
  #     counter.call("increment", nil)  # Uses \0 reference
  #     counter.call("getValue", nil)   # Uses \0 reference
  #   end
  #
  #   final_value = results.get_result(3)
  #
  # @param caller [Object] Caller implementation that supports #batch method
  # @yield [BatchBuilder] Builder for constructing batch requests
  # @return [BatchResults] Results of the batch execution
  def self.execute_batch(caller)
    # Create ID generator
    id_counter = 0
    id_gen = -> { id_counter += 1 }

    # Create builder
    builder = BatchBuilder.new(id_gen)

    # Execute user's batch building block
    yield builder

    # Get collected requests
    requests = builder.requests

    return BatchResults.new([]) if requests.empty?

    # Send batch via caller
    responses = caller.batch(requests)

    # Build response map by ID for alignment
    response_map = {}
    responses.each do |response|
      response_map[response.id] = response if response.id
    end

    # Align responses with requests (notifications get nil)
    aligned_responses = []
    requests.each do |request|
      if request.id.nil?
        # Notification - no response
        aligned_responses << nil
      else
        # Look up response by ID
        aligned_responses << response_map[request.id]
      end
    end

    BatchResults.new(aligned_responses)
  end
end
