# frozen_string_literal: true

module JSONRPC3
  # Peer for bidirectional JSON-RPC 3.0 communication over streams
  class Peer
    attr_reader :session

    def initialize(input_io, output_io, root_object, options = {})
      @input_io = input_io
      @output_io = output_io
      @session = options[:session] || Session.new
      @handler = Handler.new(@session, root_object, [options[:mime_type] || MIME_TYPE_JSON])
      @codec = JSONRPC3.get_codec(options[:mime_type] || MIME_TYPE_JSON)

      @next_id = 0
      @id_mutex = Mutex.new
      @pending_requests = {}
      @pending_mutex = Mutex.new
      @write_mutex = Mutex.new

      @closed = false
      @close_mutex = Mutex.new

      # Start read loop in background
      @read_thread = Thread.new { read_loop }
    end

    # Call a method on the remote peer
    def call(method, params = nil, ref = nil)
      raise "Connection closed" if @closed

      id = next_id
      ref_string = case ref
                   when String then ref
                   when Hash then ref["$ref"] || ref[:$ref]
                   else ref&.ref
                   end
      req = JSONRPC3.new_request(method, params, id, ref_string)

      # Create promise for response
      queue = Queue.new
      @pending_mutex.synchronize { @pending_requests[id] = queue }

      # Send request
      msg_set = MessageSet.new(messages: [req.to_h], is_batch: false)
      write_message(msg_set)

      # Wait for response
      resp = queue.pop
      @pending_mutex.synchronize { @pending_requests.delete(id) }

      raise RpcError.from_h(resp["error"]) if resp["error"]

      track_remote_references(resp["result"])
      resp["result"]
    end

    # Send a notification
    def notify(method, params = nil, ref = nil)
      raise "Connection closed" if @closed

      ref_string = case ref
                   when String then ref
                   when Hash then ref["$ref"] || ref[:$ref]
                   else ref&.ref
                   end
      req = JSONRPC3.new_request(method, params, nil, ref_string)

      msg_set = MessageSet.new(messages: [req.to_h], is_batch: false)
      write_message(msg_set)
      nil
    end

    # Register a local object
    def register_object(obj, ref = nil)
      @session.add_local_ref(ref || obj, ref ? obj : nil)
    end

    # Unregister a local object
    def unregister_object(ref)
      @session.remove_local_ref(ref)
    end

    # Close the peer
    def close
      @close_mutex.synchronize do
        return if @closed

        @closed = true

        # Reject all pending requests
        @pending_mutex.synchronize do
          @pending_requests.each_value { |queue| queue.push({ "error" => { "message" => "Connection closed" } }) }
          @pending_requests.clear
        end

        # Dispose all references
        @session.dispose_all

        # Close streams
        @input_io.close rescue nil
        @output_io.close rescue nil
      end
    end

    # Wait for peer to close
    def wait
      @read_thread.join
    end

    private

    # Get next request ID
    def next_id
      @id_mutex.synchronize { @next_id += 1 }
    end

    # Write a message to the stream
    def write_message(msg_set)
      @write_mutex.synchronize do
        raise "Connection closed" if @closed

        bytes = @codec.marshal_messages(msg_set)
        @output_io.write(bytes)
        @output_io.flush
      end
    end

    # Read loop - continuously reads messages
    def read_loop
      loop do
        # Read message (simplified - assumes complete messages)
        # In production, would need proper framing
        line = @input_io.gets
        break if line.nil?

        msg_set = @codec.unmarshal_messages(line)

        # Determine if requests or responses
        all_requests = msg_set.messages.all? { |msg| JSONRPC3.request?(msg) }
        all_responses = msg_set.messages.all? { |msg| JSONRPC3.response?(msg) }

        if all_requests && msg_set.is_batch
          handle_incoming_batch(msg_set)
        elsif all_requests
          handle_incoming_request(msg_set.messages.first)
        elsif all_responses
          msg_set.messages.each { |msg| handle_incoming_response(msg) }
        end
      end
    rescue StandardError => e
      # Connection error
      JSONRPC3.logger&.error("Peer read error: #{e.message}")
    ensure
      close
    end

    # Handle incoming request
    def handle_incoming_request(msg)
      Thread.new do
        resp = @handler.handle_request(msg)

        # Send response if not a notification
        unless msg["id"].nil?
          msg_set = MessageSet.new(messages: [resp.to_h], is_batch: false)
          write_message(msg_set) rescue nil
        end
      end
    end

    # Handle incoming batch
    def handle_incoming_batch(msg_set)
      Thread.new do
        batch = JSONRPC3.to_batch(msg_set)
        responses = @handler.handle_batch(batch)

        if responses.any?
          resp_msg_set = JSONRPC3.batch_response_to_message_set(responses)
          write_message(resp_msg_set) rescue nil
        end
      end
    end

    # Handle incoming response
    def handle_incoming_response(msg)
      id = msg["id"]
      queue = @pending_mutex.synchronize { @pending_requests[id] }
      queue&.push(msg)
    end

    # Track remote references
    def track_remote_references(value)
      return if value.nil?

      if JSONRPC3.reference?(value)
        @session.add_remote_ref(value["$ref"] || value[:$ref])
        return
      end

      if value.is_a?(Array)
        value.each { |item| track_remote_references(item) }
        return
      end

      if value.is_a?(Hash)
        value.each_value { |val| track_remote_references(val) }
      end
    end
  end
end
