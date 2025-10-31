# frozen_string_literal: true

module JSONRPC3
  # MethodInfo contains metadata about a registered method for introspection
  class MethodInfo
    attr_accessor :name, :description, :params, :handler

    def initialize(name:, handler:, description: nil, params: nil)
      @name = name
      @handler = handler
      @description = description
      @params = params
    end

    # Convert to hash for introspection (excluding handler)
    def to_h
      result = { "name" => @name }
      result["description"] = @description if @description
      result["params"] = @params if @params
      result
    end
  end

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
    # @param description [String, nil] optional description for introspection
    # @param params [Hash, Array, nil] optional parameter metadata (Hash for named params, Array for positional)
    # @yield [params] block to handle the method call
    def register(method_name, handler = nil, description: nil, params: nil, &block)
      handler ||= block
      raise ArgumentError, "Handler required" unless handler

      method_str = method_name.to_s
      @methods[method_str] = MethodInfo.new(
        name: method_str,
        handler: handler,
        description: description,
        params: params
      )
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
      when "$method"
        get_method_info(params)
      else
        method_info = @methods[method_str]
        raise JSONRPC3.method_not_found_error(method) unless method_info

        method_info.handler.call(params)
      end
    end

    private

    # Get list of available methods
    def get_methods
      methods = @methods.keys
      methods << "$methods" << "$type" << "$method"
      methods.sort
    end

    # Get object type
    def get_type
      @type
    end

    # Get detailed information about a specific method
    # Implements the $method introspection method
    def get_method_info(params)
      method_name = params.decode

      # Validate that we received a string parameter
      unless method_name.is_a?(String)
        raise JSONRPC3::RpcError.new(
          "$method expects a string parameter",
          code: JSONRPC3::CODE_INVALID_PARAMS
        )
      end

      method_info = @methods[method_name]

      # Return nil for non-existent methods
      return nil unless method_info

      # Return metadata hash (without handler)
      method_info.to_h
    end
  end
end
