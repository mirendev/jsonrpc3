# frozen_string_literal: true

require "securerandom"

module JSONRPC3
  # RpcObject module - objects that can receive method calls
  module RpcObject
    # Call a method on this object
    # @param method [String] method name
    # @param params [Params] method parameters
    # @return [Object] method result
    def call_method(method, params)
      raise NotImplementedError, "#{self.class} must implement call_method"
    end
  end

  # RefInfo contains metadata about a reference
  RefInfo = Struct.new(:ref, :type, :direction, :created, :last_accessed, :metadata, keyword_init: true)

  # Session manages object references for a connection
  class Session
    attr_reader :id

    def initialize(id = nil)
      @id = id || SecureRandom.alphanumeric(21) # Similar to nanoid length
      @created_at = Time.now
      @local_refs = {}
      @remote_refs = {}
      @ref_counter = 0
      @mutex = Mutex.new
    end

    # Get session creation time
    def created
      @created_at
    end

    # Add a local reference (object we expose to remote peer)
    # @param ref_or_obj [String, RpcObject] reference ID or object
    # @param obj [RpcObject, nil] object if first param is string
    # @return [String] reference ID
    def add_local_ref(ref_or_obj, obj = nil)
      @mutex.synchronize do
        if ref_or_obj.is_a?(String)
          raise ArgumentError, "Object is required when ref is a string" unless obj

          ref = ref_or_obj
          actual_obj = obj
        else
          # Auto-generate ref
          @ref_counter += 1
          ref = "ref-#{@id}-#{@ref_counter}"
          actual_obj = ref_or_obj
        end

        @local_refs[ref] = actual_obj
        ref
      end
    end

    # Get a local reference
    def get_local_ref(ref)
      @mutex.synchronize { @local_refs[ref] }
    end

    # Check if local reference exists
    def has_local_ref?(ref)
      @mutex.synchronize { @local_refs.key?(ref) }
    end

    # Remove a local reference
    # @return [Boolean] true if removed, false if not found
    def remove_local_ref(ref)
      @mutex.synchronize do
        obj = @local_refs.delete(ref)
        return false unless obj

        dispose_object(obj)
        true
      end
    end

    # List all local reference IDs
    def list_local_refs
      @mutex.synchronize { @local_refs.keys }
    end

    # Get local reference info
    def get_local_ref_info(ref)
      @mutex.synchronize do
        return nil unless @local_refs.key?(ref)

        RefInfo.new(
          ref: ref,
          direction: "local",
          created: @created_at
        )
      end
    end

    # Add a remote reference (object remote peer exposed to us)
    def add_remote_ref(ref, info = {})
      @mutex.synchronize do
        @remote_refs[ref] = RefInfo.new(
          ref: ref,
          direction: "remote",
          created: Time.now,
          **info
        )
      end
    end

    # Get remote reference info
    def get_remote_ref_info(ref)
      @mutex.synchronize { @remote_refs[ref] }
    end

    # Check if remote reference exists
    def has_remote_ref?(ref)
      @mutex.synchronize { @remote_refs.key?(ref) }
    end

    # Remove a remote reference
    # @return [Boolean] true if removed, false if not found
    def remove_remote_ref(ref)
      @mutex.synchronize { !@remote_refs.delete(ref).nil? }
    end

    # List all remote references
    def list_remote_refs
      @mutex.synchronize { @remote_refs.values }
    end

    # Get remote reference IDs (alias for convenience)
    def get_remote_refs
      @mutex.synchronize { @remote_refs.keys }
    end

    # Dispose all references (both local and remote)
    # @return [Hash] counts of disposed references
    def dispose_all
      @mutex.synchronize do
        local_count = @local_refs.size
        remote_count = @remote_refs.size

        # Dispose all local objects
        @local_refs.each_value { |obj| dispose_object(obj) }

        @local_refs.clear
        @remote_refs.clear

        { local_count: local_count, remote_count: remote_count }
      end
    end

    private

    # Call dispose or close on an object if it has such methods
    def dispose_object(obj)
      if obj.respond_to?(:dispose)
        begin
          result = obj.dispose
          result.wait if result.is_a?(Thread)
        rescue StandardError
          # Ignore disposal errors
        end
      elsif obj.respond_to?(:close)
        begin
          result = obj.close
          result.wait if result.is_a?(Thread)
        rescue StandardError
          # Ignore disposal errors
        end
      end
    end
  end
end
