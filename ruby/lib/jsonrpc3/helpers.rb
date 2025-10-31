# frozen_string_literal: true

module JSONRPC3
  # MethodMap provides a simple way to create RPC objects
  class MethodMap
    include RpcObject

    attr_accessor :type

    def initialize
      @methods = {}
      @type = nil
    end

    # Register a method handler
    # @param method_name [String, Symbol] method name
    # @param handler [Proc, nil] optional handler block
    # @yield [params] block to handle the method call
    def register(method_name, handler = nil, &block)
      handler ||= block
      raise ArgumentError, "Handler required" unless handler

      @methods[method_name.to_s] = handler
    end

    # Call a method on this object
    def call_method(method, params)
      method_str = method.to_s

      # Built-in introspection methods
      case method_str
      when "$methods"
        get_methods
      when "$type"
        get_type
      else
        handler = @methods[method_str]
        raise JSONRPC3.method_not_found_error(method) unless handler

        handler.call(params)
      end
    end

    private

    # Get list of available methods
    def get_methods
      methods = @methods.keys
      methods << "$methods" << "$type"
      methods.sort
    end

    # Get object type
    def get_type
      @type
    end
  end
end
