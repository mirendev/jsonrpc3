# frozen_string_literal: true

module JSONRPC3
  # Handler routes requests to objects
  class Handler
    attr_reader :session, :root_object

    def initialize(session, root_object, caller, mime_types = [MIME_TYPE_JSON])
      @session = session
      @root_object = root_object
      @caller = caller
      @protocol = ProtocolHandler.new(session, mime_types)
    end

    # Handle a single request
    def handle_request(req)
      # Determine which object to call
      ref = req.is_a?(Request) ? req.ref : req["ref"]

      obj = if ref == "$rpc"
              @protocol
            elsif ref
              local_obj = @session.get_local_ref(ref)
              raise JSONRPC3.reference_not_found_error(ref) unless local_obj

              local_obj
            else
              @root_object
            end

      # Extract parameters
      params_value = req.is_a?(Request) ? req.params : req["params"]
      params = params_value ? JSONRPC3.new_params(params_value) : NULL_PARAMS

      # Call method
      method = req.is_a?(Request) ? req.method : req["method"]
      result = obj.call_method(method, params, @caller)

      # Process result for automatic reference registration
      processed_result = process_result(result)

      # Create response
      jsonrpc = req.is_a?(Request) ? req.jsonrpc : (req["jsonrpc"] || VERSION_30)
      req_id = req.is_a?(Request) ? req.id : req["id"]
      JSONRPC3.new_response(processed_result, req_id, jsonrpc)
    rescue RpcError => e
      # Create error response
      jsonrpc = req.is_a?(Request) ? req.jsonrpc : (req["jsonrpc"] || VERSION_30)
      req_id = req.is_a?(Request) ? req.id : req["id"]
      JSONRPC3.new_error_response(e.to_h, req_id, jsonrpc)
    rescue StandardError => e
      # Convert to internal error
      rpc_error = JSONRPC3.internal_error(e.message)
      jsonrpc = req.is_a?(Request) ? req.jsonrpc : (req["jsonrpc"] || VERSION_30)
      req_id = req.is_a?(Request) ? req.id : req["id"]
      JSONRPC3.new_error_response(rpc_error.to_h, req_id, jsonrpc)
    end

    # Handle a batch request
    def handle_batch(batch)
      responses = []
      batch_results = []

      batch.each_with_index do |req, i|
        begin
          # Resolve batch-local references
          resolved_ref = req.is_a?(Request) ? req.ref : req["ref"]
          if resolved_ref&.start_with?("\\")
            resolved_ref = resolve_batch_local_ref(resolved_ref, i, batch_results)
          end

          # Create modified request with resolved ref
          resolved_req = if req.is_a?(Request)
                           Request.new(**req.to_h.merge(ref: resolved_ref))
                         else
                           req.merge("ref" => resolved_ref)
                         end

          # Handle the request
          resp = handle_request(resolved_req)

          # Store result for batch-local reference resolution
          batch_results[i] = resp.is_a?(Response) ? resp.result : resp["result"]

          # Only add response if not a notification
          is_notif = req.is_a?(Request) ? JSONRPC3.notification?(req) : !req.key?("id")
          responses << resp unless is_notif
        rescue StandardError => e
          # Store nil for failed requests
          batch_results[i] = nil

          # Only add error response if not a notification
          is_notif = req.is_a?(Request) ? JSONRPC3.notification?(req) : !req.key?("id")
          unless is_notif
            rpc_error = e.is_a?(RpcError) ? e : JSONRPC3.internal_error(e.message)
            jsonrpc = req.is_a?(Request) ? req.jsonrpc : (req["jsonrpc"] || VERSION_30)
            req_id = req.is_a?(Request) ? req.id : req["id"]
            responses << JSONRPC3.new_error_response(rpc_error.to_h, req_id, jsonrpc)
          end
        end
      end

      responses
    end

    private

    # Resolve batch-local reference (\0, \1, etc.)
    def resolve_batch_local_ref(ref, current_index, results)
      # Parse index from \N format
      index_str = ref[1..]
      index = Integer(index_str, 10)

      # Check for forward reference
      if index >= current_index
        raise JSONRPC3.invalid_reference_error(ref, "Forward reference not allowed: #{ref} at index #{current_index}")
      end

      # Check bounds
      if index.negative? || index >= results.size
        raise JSONRPC3.invalid_reference_error(ref, "Batch-local reference out of bounds: #{ref}")
      end

      # Get the result
      result = results[index]
      raise JSONRPC3.invalid_reference_error(ref, "Referenced request failed: index #{index}") if result.nil?

      # Result must be a Reference
      unless JSONRPC3.reference?(result)
        raise JSONRPC3.reference_type_error(ref, "Result at index #{index} is not a reference")
      end

      result["$ref"]
    rescue ArgumentError
      raise JSONRPC3.invalid_reference_error(ref, "Invalid batch-local reference format: #{ref}")
    end

    # Process result to handle automatic reference registration
    def process_result(result)
      # Nil or basic types - return as-is
      return result if result.nil? || result.is_a?(String) || result.is_a?(Numeric) ||
                       result.is_a?(TrueClass) || result.is_a?(FalseClass)

      # If it's already a Reference, return as-is
      return result if JSONRPC3.reference?(result)

      # Check if result implements RpcObject (duck typing)
      if rpc_object?(result)
        # Register and return Reference
        ref = @session.add_local_ref(result)
        { "$ref" => ref }
      elsif result.is_a?(Array)
        # Process array elements
        result.map { |item| process_result(item) }
      elsif result.is_a?(Hash)
        # Process hash values
        result.transform_values { |value| process_result(value) }
      else
        # Other types return as-is
        result
      end
    end

    # Check if value implements RpcObject interface
    def rpc_object?(value)
      value.respond_to?(:call_method)
    end
  end
end
