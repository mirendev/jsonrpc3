# frozen_string_literal: true

require "faye/websocket"
require "rack"
require "securerandom"
require "thread"
require "timeout"

module JSONRPC3
  # WebSocket Handler for JSON-RPC 3.0
  # Rack-compatible middleware that upgrades HTTP connections to WebSocket
  # for bidirectional JSON-RPC 3.0 communication.
  class WebSocketHandler
    attr_reader :root_object

    # Create a new WebSocket handler
    # @param root_object [RpcObject] Object to handle incoming calls from clients
    # @param options [Hash] Configuration options
    def initialize(root_object, options = {})
      @root_object = root_object
      @mime_types = [MIME_TYPE_JSON]
      @subprotocols = ["jsonrpc3.json"]
    end

    # Rack interface - handle incoming HTTP requests
    # @param env [Hash] Rack environment
    # @return [Array] Rack response tuple
    def call(env)
      # Check if this is a WebSocket upgrade request
      if Faye::WebSocket.websocket?(env)
        # Upgrade to WebSocket
        ws = Faye::WebSocket.new(env, @subprotocols)

        # Only JSON is supported for now
        content_type = MIME_TYPE_JSON

        # Reject non-JSON protocols
        if ws.protocol && ws.protocol != "jsonrpc3.json"
          ws.close(1002, "Only jsonrpc3.json protocol is supported")
          return ws.rack_response
        end

        # Create connection handler
        conn = WebSocketServerConn.new(ws, @root_object, content_type, @mime_types)

        # Set up event handlers
        ws.on :open do |event|
          conn.on_open
        end

        ws.on :message do |event|
          conn.on_message(event.data)
        end

        ws.on :close do |event|
          conn.on_close(event.code, event.reason)
        end

        ws.on :error do |event|
          conn.on_error(event.message)
        end

        # Return async Rack response
        ws.rack_response
      else
        # Not a WebSocket request
        [400, { "Content-Type" => "text/plain" }, ["WebSocket connection required"]]
      end
    end
  end

  # WebSocketServerConn represents a single WebSocket client connection
  # Manages bidirectional JSON-RPC 3.0 communication for one client
  class WebSocketServerConn
    include Caller
    attr_reader :session

    # Create a new WebSocket connection handler
    # @param ws [Faye::WebSocket] WebSocket connection
    # @param root_object [RpcObject] Root object for handling requests
    # @param content_type [String] Content type for encoding
    # @param mime_types [Array<String>] Supported mime types
    def initialize(ws, root_object, content_type, mime_types)
      @ws = ws
      @root_object = root_object
      @content_type = content_type
      @codec = JSONRPC3.get_codec(content_type)

      @session = Session.new
      # Pass this connection as caller for bidirectional communication
      @handler = Handler.new(@session, @root_object, self, mime_types)

      # Request tracking for server-initiated requests
      @request_id = 0
      @pending_requests = {}
      @request_mutex = Mutex.new

      # Reference ID generation
      @ref_prefix = SecureRandom.alphanumeric(8)
      @ref_counter = 0
      @ref_mutex = Mutex.new

      # Threading
      @write_queue = Queue.new
      @closed = false
      @close_mutex = Mutex.new
      @conn_error = nil
      @error_mutex = Mutex.new

      # Start write loop in separate thread
      @write_thread = Thread.new { write_loop }
    end

    # Called when connection is opened
    def on_open
      # Connection is ready
      JSONRPC3.logger&.debug("WebSocket connection opened")
    end

    # Called when message is received
    # @param data [String] Message data
    def on_message(data)
      handle_message(data)
    end

    # Called when connection is closed
    # @param code [Integer] Close code
    # @param reason [String] Close reason
    def on_close(code, reason)
      JSONRPC3.logger&.debug("WebSocket connection closed: #{code} #{reason}")
      close
    end

    # Called when error occurs
    # @param message [String] Error message
    def on_error(message)
      set_error("WebSocket error: #{message}")
    end

    # Call a method on the client and wait for response
    # @param method [String] Method name
    # @param params [Object, nil] Method parameters
    # @param ref [String, Reference, nil] Target reference
    # @return [Object] Result value
    # @raise [RpcError] if client returns an error
    def call(method, params = nil, ref = nil)
      check_closed!

      # Generate request ID
      id = next_id

      # Extract ref string
      ref_string = extract_ref_string(ref)

      # Create request
      req = JSONRPC3.new_request(method, params, id, ref_string)

      # Create response channel
      response_queue = Queue.new
      @request_mutex.synchronize do
        @pending_requests[id] = response_queue
      end

      begin
        # Encode and send request
        msg_set = MessageSet.new(messages: [req.to_h], is_batch: false)
        req_data = @codec.marshal_messages(msg_set)

        # Send via write queue
        @write_queue << req_data

        # Wait for response with timeout (30 seconds)
        # Ruby 3.2+ supports Queue#pop with timeout keyword parameter
        # Ruby 3.0 requires Timeout.timeout
        resp = if RUBY_VERSION >= "3.2"
                 response_queue.pop(timeout: 30)
               else
                 Timeout.timeout(30) { response_queue.pop }
               end

        raise RpcError.new("Request timeout", code: CODE_TIMEOUT) if resp.nil?

        # Check for error
        raise RpcError.from_h(resp.error) if resp.error

        resp.result
      rescue Timeout::Error
        raise RpcError.new("Request timeout", code: CODE_TIMEOUT)
      ensure
        @request_mutex.synchronize do
          @pending_requests.delete(id)
        end
      end
    end

    # Send a notification to the client (no response expected)
    # @param method [String] Method name
    # @param params [Object, nil] Method parameters
    # @param ref [String, Reference, nil] Target reference
    # @return [nil]
    def notify(method, params = nil, ref = nil)
      check_closed!

      ref_string = extract_ref_string(ref)
      req = JSONRPC3.new_request(method, params, nil, ref_string)

      msg_set = MessageSet.new(messages: [req.to_h], is_batch: false)
      req_data = @codec.marshal_messages(msg_set)

      @write_queue << req_data
      nil
    end

    # Register a local object that the client can call
    # @param obj [RpcObject] Object to register
    # @param ref [String, nil] Reference ID (auto-generated if nil)
    # @return [Reference] Reference object
    def register_object(obj, ref = nil)
      if ref.nil?
        @ref_mutex.synchronize do
          @ref_counter += 1
          ref = "#{@ref_prefix}-#{@ref_counter}"
        end
      end

      @session.add_local_ref(ref, obj)
      Reference.new(ref: ref)
    end

    # Close the WebSocket connection gracefully
    # @return [void]
    def close
      @close_mutex.synchronize do
        return if @closed
        @closed = true
      end

      # Send close message
      @ws&.close(1000, "Normal closure") if @ws

      # Dispose all refs
      @session.dispose_all

      # Wake up all pending requests with error
      @request_mutex.synchronize do
        @pending_requests.each do |_id, queue|
          queue << Response.new(
            jsonrpc: VERSION_30,
            id: nil,
            error: { "code" => CODE_CONNECTION_CLOSED, "message" => "Connection closed" }
          )
        end
        @pending_requests.clear
      end

      # Wait for write thread to finish
      @write_thread&.join(1)
    end

    # Check if connection is closed
    # @return [Boolean]
    def closed?
      @close_mutex.synchronize { @closed }
    end

    private

    # Write loop - sends queued messages
    def write_loop
      loop do
        begin
          # Get next message from queue (with timeout to check closed)
          msg = nil
          begin
            msg = @write_queue.pop(true) # non-blocking
          rescue ThreadError
            # Queue is empty
            sleep 0.01
            next if !closed?
            break
          end

          break if closed? && msg.nil?
          next if msg.nil?

          # Send message
          @ws.send(msg)
        rescue => e
          set_error("Write error: #{e.message}")
          break
        end
      end
    rescue => e
      set_error("Write loop error: #{e.message}")
    end

    # Handle incoming WebSocket message
    def handle_message(data)
      # Decode as MessageSet
      msg_set = @codec.unmarshal_messages(data)

      # Check if all messages are requests or all are responses
      all_requests = msg_set.messages.all? { |msg| JSONRPC3.request?(msg) }
      all_responses = msg_set.messages.all? { |msg| JSONRPC3.response?(msg) }

      if all_requests && msg_set.is_batch
        # Handle batch requests
        Thread.new { handle_incoming_batch(msg_set) }
      elsif all_requests
        # Handle single request
        Thread.new { handle_incoming_request(msg_set.messages.first) }
      elsif all_responses
        # Handle responses
        msg_set.messages.each { |msg| handle_incoming_response(msg) }
      else
        # Mixed batch - invalid
        error_resp = JSONRPC3.new_error_response(
          RpcError.new("Mixed requests and responses in batch", code: CODE_INVALID_REQUEST).to_h,
          nil,
          VERSION_30
        )
        send_response(error_resp)
      end
    rescue => e
      # Silently ignore malformed messages
      JSONRPC3.logger&.warn("Failed to decode message: #{e.message}")
    end

    # Handle incoming request from client
    def handle_incoming_request(msg)
      req = Request.from_h(msg)

      # Handle request via handler
      resp = @handler.handle_request(req)

      # Send response if not a notification
      send_response(resp) if resp
    rescue => e
      JSONRPC3.logger&.error("Error handling request: #{e.message}")
    end

    # Handle incoming batch requests from client
    def handle_incoming_batch(msg_set)
      # Convert to batch
      requests = msg_set.messages.map { |msg| Request.from_h(msg) }
      batch = Batch.new(requests: requests)

      # Handle batch via handler (preserves batch-local references)
      responses = @handler.handle_batch(batch)

      # Send batch response
      send_batch_responses(responses) if responses && !responses.empty?
    rescue => e
      JSONRPC3.logger&.error("Error handling batch: #{e.message}")
    end

    # Handle incoming response to our request
    def handle_incoming_response(msg)
      resp = Response.from_h(msg)

      # Find pending request
      queue = @request_mutex.synchronize do
        @pending_requests[resp.id]
      end

      # Send response to waiting thread
      queue << resp if queue
    rescue => e
      JSONRPC3.logger&.error("Error handling response: #{e.message}")
    end

    # Send a response
    def send_response(resp)
      msg_set = MessageSet.new(messages: [resp.to_h], is_batch: false)
      resp_data = @codec.marshal_messages(msg_set)
      @write_queue << resp_data
    end

    # Send batch responses
    def send_batch_responses(responses)
      msg_set = MessageSet.new(
        messages: responses.map(&:to_h),
        is_batch: true
      )
      resp_data = @codec.marshal_messages(msg_set)
      @write_queue << resp_data
    end

    # Generate next request ID
    def next_id
      @request_mutex.synchronize do
        @request_id += 1
        @request_id.to_f # Use float for JSON compatibility
      end
    end

    # Extract ref string from various formats
    def extract_ref_string(ref)
      case ref
      when String
        ref
      when Reference
        ref.ref
      when Hash
        ref["$ref"] || ref[:$ref]
      else
        nil
      end
    end

    # Set connection error
    def set_error(error_msg)
      @error_mutex.synchronize do
        @conn_error ||= error_msg
      end
      JSONRPC3.logger&.error(error_msg)
    end

    # Get connection error
    def get_error
      @error_mutex.synchronize { @conn_error }
    end

    # Check if connection is closed and raise error if so
    def check_closed!
      if closed?
        raise RpcError.new("Connection closed", code: CODE_CONNECTION_CLOSED)
      end

      if (err = get_error)
        raise RpcError.new(err, code: CODE_CONNECTION_ERROR)
      end
    end
  end
end
