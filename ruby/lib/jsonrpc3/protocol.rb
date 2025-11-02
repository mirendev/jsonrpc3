# frozen_string_literal: true

module JSONRPC3
  # ProtocolHandler implements built-in $rpc methods
  class ProtocolHandler
    include RpcObject

    def initialize(session, mime_types = [MIME_TYPE_JSON])
      @session = session
      @mime_types = mime_types
    end

    # Call a protocol method
    def call_method(method, params, _caller)
      case method
      when "dispose"
        handle_dispose(params)
      when "session_id"
        handle_session_id(params)
      when "list_refs"
        handle_list_refs(params)
      when "ref_info"
        handle_ref_info(params)
      when "dispose_all"
        handle_dispose_all(params)
      when "mimetypes"
        handle_mimetypes(params)
      when "capabilities"
        handle_capabilities(params)
      else
        raise method_not_found_error(method)
      end
    end

    private

    # Dispose a reference
    def handle_dispose(params)
      data = params.decode
      ref = data.is_a?(Hash) ? (data["ref"] || data[:ref]) : data
      raise invalid_params_error("ref parameter required") unless ref

      success = @session.remove_local_ref(ref)
      { success: success }
    end

    # Get session ID
    def handle_session_id(_params)
      { session_id: @session.id }
    end

    # List all references
    def handle_list_refs(_params)
      {
        local: @session.list_local_refs,
        remote: @session.list_remote_refs.map(&:ref)
      }
    end

    # Get reference info
    def handle_ref_info(params)
      data = params.decode
      ref = data.is_a?(Hash) ? (data["ref"] || data[:ref]) : data
      raise invalid_params_error("ref parameter required") unless ref

      # Check local refs first
      info = @session.get_local_ref_info(ref)
      info ||= @session.get_remote_ref_info(ref)

      raise reference_not_found_error(ref) unless info

      info.to_h.transform_keys(&:to_s)
    end

    # Dispose all references
    def handle_dispose_all(_params)
      @session.dispose_all
    end

    # Get supported MIME types
    def handle_mimetypes(_params)
      @mime_types
    end

    # Get capabilities
    def handle_capabilities(_params)
      caps = [
        "references",
        "batch-local-references",
        "bidirectional-calls",
        "introspection"
      ]

      # Add encoding capabilities
      caps << "cbor-encoding" if @mime_types.include?(MIME_TYPE_CBOR)
      caps << "cbor-compact-encoding" if @mime_types.include?(MIME_TYPE_CBOR_COMPACT)

      caps
    end
  end
end
