# frozen_string_literal: true

module JSONRPC3
  # Params provides access to method parameters
  class Params
    attr_reader :raw

    def initialize(value)
      @raw = value
    end

    # Decode parameters to specified type
    # For Ruby, this is mainly for consistency with other bindings
    # The value is already decoded from JSON
    def decode
      @raw
    end
  end

  # Factory method
  def self.new_params(value)
    Params.new(value)
  end

  # Constant for null parameters
  NULL_PARAMS = Params.new(nil).freeze
end
