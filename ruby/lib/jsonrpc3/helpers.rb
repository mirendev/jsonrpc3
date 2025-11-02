# frozen_string_literal: true

module JSONRPC3
  # MethodInfo contains metadata about a registered method for introspection
  class MethodInfo
    attr_accessor :name, :description, :params, :category, :handler

    def initialize(name:, handler:, description: nil, params: nil, category: nil)
      @name = name
      @handler = handler
      @description = description
      @params = params
      @category = category
    end

    # Convert to hash for introspection (excluding handler)
    def to_h
      result = { "name" => @name }
      result["description"] = @description if @description
      result["params"] = @params if @params
      result["category"] = @category if @category
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
    # @param category [String, nil] optional category for introspection
    # @yield [params] block to handle the method call
    def register(method_name, handler = nil, description: nil, params: nil, category: nil, &block)
      handler ||= block
      raise ArgumentError, "Handler required" unless handler

      method_str = method_name.to_s
      @methods[method_str] = MethodInfo.new(
        name: method_str,
        handler: handler,
        description: description,
        params: params,
        category: category
      )
    end

    # Call a method on this object
    def call_method(method, params, caller)
      method_str = method.to_s

      # Built-in introspection methods
      case method_str
      when "$methods"
        get_methods
      when "$type"
        get_type
      else
        method_info = @methods[method_str]
        raise JSONRPC3.method_not_found_error(method) unless method_info

        method_info.handler.call(params, caller)
      end
    end

    private

    # Get detailed information about all available methods
    def get_methods
      methods = []

      # Add user-registered methods with their metadata
      @methods.each_value do |info|
        methods << info.to_h
      end

      # Add introspection methods
      methods << { "name" => "$methods" }
      methods << { "name" => "$type" }

      methods
    end

    # Get object type
    def get_type
      @type
    end
  end
end
