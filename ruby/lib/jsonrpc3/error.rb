# frozen_string_literal: true

module JSONRPC3
  # JSON-RPC error codes
  CODE_PARSE_ERROR = -32_700
  CODE_INVALID_REQUEST = -32_600
  CODE_METHOD_NOT_FOUND = -32_601
  CODE_INVALID_PARAMS = -32_602
  CODE_INTERNAL_ERROR = -32_603
  CODE_INVALID_REFERENCE = -32_001
  CODE_REFERENCE_NOT_FOUND = -32_002
  CODE_REFERENCE_TYPE_ERROR = -32_003

  # RpcError represents a JSON-RPC error
  class RpcError < StandardError
    attr_reader :code, :data

    def initialize(message, code:, data: nil)
      super(message)
      @code = code
      @data = data
    end

    # Convert error to hash for wire format
    def to_h
      h = { code: @code, message: message }
      h[:data] = @data if @data
      h
    end

    # Create RpcError from hash
    def self.from_h(hash)
      new(
        hash[:message] || hash["message"],
        code: hash[:code] || hash["code"],
        data: hash[:data] || hash["data"]
      )
    end
  end

  # Factory methods for common errors

  def self.parse_error(data = nil)
    RpcError.new("Parse error", code: CODE_PARSE_ERROR, data: data)
  end

  def self.invalid_request_error(data = nil)
    RpcError.new("Invalid request", code: CODE_INVALID_REQUEST, data: data)
  end

  def self.method_not_found_error(method)
    RpcError.new("Method not found: #{method}", code: CODE_METHOD_NOT_FOUND, data: { method: method })
  end

  def self.invalid_params_error(data = nil)
    RpcError.new("Invalid params", code: CODE_INVALID_PARAMS, data: data)
  end

  def self.internal_error(data = nil)
    RpcError.new("Internal error", code: CODE_INTERNAL_ERROR, data: data)
  end

  def self.invalid_reference_error(ref, message = nil)
    msg = message ? "Invalid reference #{ref}: #{message}" : "Invalid reference: #{ref}"
    RpcError.new(msg, code: CODE_INVALID_REFERENCE, data: { ref: ref })
  end

  def self.reference_not_found_error(ref)
    RpcError.new("Reference not found: #{ref}", code: CODE_REFERENCE_NOT_FOUND, data: { ref: ref })
  end

  def self.reference_type_error(ref, message = nil)
    msg = message ? "Reference type error #{ref}: #{message}" : "Reference type error: #{ref}"
    RpcError.new(msg, code: CODE_REFERENCE_TYPE_ERROR, data: { ref: ref })
  end
end
