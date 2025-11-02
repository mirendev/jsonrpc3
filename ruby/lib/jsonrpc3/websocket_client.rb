# frozen_string_literal: true

require "websocket"
require "socket"
require "uri"
require "securerandom"
require "thread"
require "timeout"

module JSONRPC3
  # WebSocket Client for JSON-RPC 3.0
  # Supports full bidirectional communication where both client and server
  # can initiate method calls at any time.
  # Uses pure Ruby websocket gem (no EventMachine dependency)
  class WebSocketClient
    include Caller
    attr_reader :session, :url

    # Create a new WebSocket client and connect to the server.
    # @param url [String] WebSocket URL (ws:// or wss://)
    # @param root_object [RpcObject, nil] Object to handle incoming calls from server
    # @param options [Hash] Configuration options
    # @option options [String] :mime_type Content type (default: MIME_TYPE_JSON)
    def initialize(url, root_object = nil, options = {})
      @url = url
      @root_object = root_object
      @mime_type = options[:mime_type] || MIME_TYPE_JSON
      @codec = JSONRPC3.get_codec(@mime_type)

      @session = Session.new
      # Pass this client as caller for bidirectional communication
      @handler = Handler.new(@session, @root_object, self, [@mime_type])

      # Request tracking
      @request_id = 0
      @pending_requests = {}
      @request_mutex = Mutex.new

      # Reference ID generation
      @ref_prefix = SecureRandom.alphanumeric(8)
      @ref_counter = 0
      @ref_mutex = Mutex.new

      # Threading
      @write_queue = Queue.new
      @close_requested = false
      @closed = false
      @close_mutex = Mutex.new
      @conn_error = nil
      @error_mutex = Mutex.new

      connect!
    end

    # Call a method on the server and wait for response
    # @param method [String] Method name
    # @param params [Object, nil] Method parameters
    # @param ref [String, Reference, nil] Target reference (for calling methods on remote objects)
    # @return [Object] Result value
    # @raise [RpcError] if server returns an error
    def call(method, params = nil, ref = nil)
      check_closed!

      # Generate request ID
      id = next_id

      # Extract ref string
      ref_string = extract_ref_string(ref)

      JSONRPC3.logger&.debug("call(#{method}) id=#{id} ref=#{ref_string}")

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

        JSONRPC3.logger&.debug("Sending request: #{req_data.bytesize} bytes")

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

        # Track remote references
        track_remote_references(resp.result)

        resp.result
      rescue Timeout::Error
        raise RpcError.new("Request timeout", code: CODE_TIMEOUT)
      ensure
        @request_mutex.synchronize do
          @pending_requests.delete(id)
        end
      end
    end

    # Send a notification (no response expected)
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

    # Register a local object that the server can call
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

    # Unregister a local object
    # @param ref [Reference, String] Reference to unregister
    # @return [Boolean] true if removed, false if not found
    def unregister_object(ref)
      ref_string = ref.is_a?(Reference) ? ref.ref : ref.to_s
      @session.remove_local_ref(ref_string)
    end

    # Close the WebSocket connection gracefully
    # @return [void]
    def close
      @close_mutex.synchronize do
        return if @closed
        @closed = true
        @close_requested = true
      end

      # Send close frame
      if @socket && !@socket.closed?
        begin
          frame = WebSocket::Frame::Outgoing::Client.new(version: @handshake.version, data: "", type: :close, code: 1000)
          @socket.write(frame.to_s)
        rescue
          # Ignore errors during close
        end
        @socket.close rescue nil
      end

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

      # Wait for threads to finish (but not if we're in one of them)
      @read_thread&.join(1) unless Thread.current == @read_thread
      @write_thread&.join(1) unless Thread.current == @write_thread
    end

    # Check if connection is closed
    # @return [Boolean]
    def closed?
      @close_mutex.synchronize { @closed }
    end

    private

    # Connect to the WebSocket server
    def connect!
      uri = URI.parse(@url)

      # Connect TCP socket
      @socket = TCPSocket.new(uri.host, uri.port || 80)

      # Perform WebSocket handshake
      @handshake = WebSocket::Handshake::Client.new(url: @url, headers: { "Sec-WebSocket-Protocol" => "jsonrpc3.json" })
      @socket.write(@handshake.to_s)

      # Read handshake response
      loop do
        line = @socket.gets
        @handshake << line
        break if @handshake.finished?
      end

      unless @handshake.valid?
        raise "WebSocket handshake failed: #{@handshake.error}"
      end

      # Start read and write loops
      @read_thread = Thread.new { read_loop }
      @write_thread = Thread.new { write_loop }
    rescue => e
      @socket&.close
      raise "Failed to connect to #{@url}: #{e.message}"
    end

    # Read loop - reads WebSocket frames from socket
    def read_loop
      frame_parser = WebSocket::Frame::Incoming::Client.new(version: @handshake.version)

      JSONRPC3.logger&.debug("Read loop started")

      loop do
        break if @close_requested

        begin
          data = @socket.readpartial(4096)
          JSONRPC3.logger&.debug("Received #{data.bytesize} bytes")
          frame_parser << data

          while (frame = frame_parser.next)
            JSONRPC3.logger&.debug("Frame received: type=#{frame.type}")
            case frame.type
            when :text, :binary
              handle_message(frame.data)
            when :close
              JSONRPC3.logger&.debug("Close frame received")
              set_error("Connection closed by server")
              @close_mutex.synchronize { @closed = true }
              return
            when :ping
              JSONRPC3.logger&.debug("Ping frame received, sending pong")
              # Respond to ping with pong
              pong = WebSocket::Frame::Outgoing::Client.new(version: @handshake.version, data: frame.data, type: :pong)
              @socket.write(pong.to_s)
            end
          end
        rescue EOFError, IOError => e
          JSONRPC3.logger&.debug("EOF/IOError in read loop: #{e.class} #{e.message}")
          set_error("Connection closed")
          break
        rescue => e
          JSONRPC3.logger&.debug("Error in read loop: #{e.class} #{e.message}\n#{e.backtrace.first(3).join("\n")}")
          set_error("Read error: #{e.message}")
          break
        end
      end
    rescue => e
      JSONRPC3.logger&.debug("Outer read loop error: #{e.class} #{e.message}")
      set_error("Read loop error: #{e.message}")
    ensure
      JSONRPC3.logger&.debug("Read loop exiting")
      close
    end

    # Write loop - sends queued messages
    def write_loop
      loop do
        begin
          # Get next message from queue (with timeout to check close_requested)
          msg = nil
          begin
            msg = @write_queue.pop(true) # non-blocking
          rescue ThreadError
            # Queue is empty
            sleep 0.01
            next if !@close_requested
            break
          end

          break if @close_requested && msg.nil?
          next if msg.nil?

          # Send as WebSocket frame
          frame = WebSocket::Frame::Outgoing::Client.new(version: @handshake.version, data: msg, type: :text)
          @socket.write(frame.to_s)
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
      JSONRPC3.logger&.debug("handle_message: #{data.bytesize} bytes, first 50: #{data[0...50].inspect}")

      # Decode as MessageSet
      msg_set = @codec.unmarshal_messages(data)
      JSONRPC3.logger&.debug("Decoded #{msg_set.messages.length} messages")

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

    # Handle incoming request from server
    def handle_incoming_request(msg)
      JSONRPC3.logger&.debug("Handling incoming request id=#{msg['id'].inspect}")
      req = Request.from_h(msg)

      # Handle request via handler
      resp = @handler.handle_request(req)

      # Send response if not a notification
      send_response(resp) if resp
    rescue => e
      JSONRPC3.logger&.error("Error handling request: #{e.message}")
    end

    # Handle incoming batch requests from server
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
      JSONRPC3.logger&.debug("Handling incoming response id=#{msg['id'].inspect}")
      resp = Response.from_h(msg)

      # Find pending request
      queue = @request_mutex.synchronize do
        @pending_requests[resp.id.to_i]
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

    # Track remote references in result
    def track_remote_references(value)
      case value
      when Hash
        if JSONRPC3.reference?(value)
          ref = value["$ref"] || value[:$ref]
          @session.add_remote_ref(ref)
        else
          value.each_value { |v| track_remote_references(v) }
        end
      when Array
        value.each { |v| track_remote_references(v) }
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
