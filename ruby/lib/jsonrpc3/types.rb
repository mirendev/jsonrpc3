# frozen_string_literal: true

module JSONRPC3
  # Version constants
  VERSION_20 = "2.0"
  VERSION_30 = "3.0"

  # Reference represents an object reference in wire format
  Reference = Struct.new(:ref, keyword_init: true) do
    def to_h
      { "$ref" => ref }
    end

    def self.from_h(hash)
      new(ref: hash["$ref"] || hash[:$ref])
    end

    # Call a method on this reference using the provided caller
    # @param caller [Object] The caller object (Peer, HttpClient, etc.) that implements call()
    # @param method [String] The method name to call
    # @param params [Object] The parameters to pass to the method
    # @return [Object] The result of the method call
    def call(caller, method, params = nil)
      caller.call(method, params, self)
    end

    # Send a notification to this reference using the provided caller
    # @param caller [Object] The caller object (Peer, HttpClient, etc.) that implements notify()
    # @param method [String] The method name to call
    # @param params [Object] The parameters to pass to the method
    # @return [nil]
    def notify(caller, method, params = nil)
      caller.notify(method, params, self)
    end
  end

  # Check if value is a Reference
  def self.reference?(value)
    value.is_a?(Hash) &&
      value.size == 1 &&
      (value.key?("$ref") || value.key?(:$ref)) &&
      value.values.first.is_a?(String)
  end

  # Request represents a JSON-RPC request
  Request = Struct.new(:jsonrpc, :method, :params, :id, :ref, keyword_init: true) do
    def to_h
      h = { "jsonrpc" => jsonrpc, "method" => method }
      h["params"] = params if params
      h["id"] = id if id
      h["ref"] = ref if ref
      h
    end

    def self.from_h(hash)
      new(
        jsonrpc: hash["jsonrpc"] || hash[:jsonrpc],
        method: hash["method"] || hash[:method],
        params: hash["params"] || hash[:params],
        id: hash["id"] || hash[:id],
        ref: hash["ref"] || hash[:ref]
      )
    end
  end

  # Response represents a JSON-RPC response
  Response = Struct.new(:jsonrpc, :result, :error, :id, keyword_init: true) do
    def to_h
      h = { "jsonrpc" => jsonrpc, "id" => id }
      h["result"] = result if result
      h["error"] = error if error
      h
    end

    def self.from_h(hash)
      new(
        jsonrpc: hash["jsonrpc"] || hash[:jsonrpc],
        result: hash["result"] || hash[:result],
        error: hash["error"] || hash[:error],
        id: hash["id"] || hash[:id]
      )
    end
  end

  # MessageSet represents one or more messages with batch information
  MessageSet = Struct.new(:messages, :is_batch, keyword_init: true)

  # Type guards

  def self.request?(msg)
    msg.is_a?(Hash) && msg.key?("method")
  end

  def self.response?(msg)
    msg.is_a?(Hash) && (msg.key?("result") || msg.key?("error"))
  end

  def self.notification?(req)
    req.is_a?(Request) ? req.id.nil? : !req.key?("id")
  end

  # Constructors

  def self.new_request(method, params = nil, id = nil, ref = nil)
    Request.new(
      jsonrpc: VERSION_30,
      method: method,
      params: params,
      id: id,
      ref: ref
    )
  end

  def self.new_response(result, id, jsonrpc = VERSION_30)
    Response.new(
      jsonrpc: jsonrpc,
      result: result,
      id: id
    )
  end

  def self.new_error_response(error, id, jsonrpc = VERSION_30)
    Response.new(
      jsonrpc: jsonrpc,
      error: error,
      id: id
    )
  end

  # Converters

  def self.to_request(msg_set)
    raise "Cannot convert batch to single request" if msg_set.is_batch
    raise "MessageSet must have exactly one message" if msg_set.messages.size != 1

    msg = msg_set.messages.first
    raise "Message is not a request" unless request?(msg)

    msg.is_a?(Request) ? msg : Request.from_h(msg)
  end

  def self.to_batch(msg_set)
    raise "MessageSet is not a batch" unless msg_set.is_batch

    msg_set.messages.map do |msg|
      raise "Batch contains non-request message" unless request?(msg)

      msg.is_a?(Request) ? msg : Request.from_h(msg)
    end
  end

  def self.batch_response_to_message_set(batch_response)
    MessageSet.new(
      messages: batch_response,
      is_batch: batch_response.size != 1
    )
  end
end
