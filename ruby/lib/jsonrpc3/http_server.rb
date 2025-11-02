# frozen_string_literal: true

require "rack"
require "puma"

module JSONRPC3
  # HTTP Server for JSON-RPC 3.0 using Rack
  class HttpServer
    attr_reader :handler, :port

    def initialize(root_object, options = {})
      mime_type = options[:mime_type] || MIME_TYPE_JSON
      @codec = JSONRPC3.get_codec(mime_type)
      # HTTP requests don't support callbacks, so use NoOpCaller
      session = Session.new
      @handler = Handler.new(session, root_object, NoOpCaller.new, [mime_type])
      @requested_port = options[:port] || 3000
      @host = options[:host] || "localhost"
      @server = nil
      @server_thread = nil
      @port = nil
    end

    # Rack call interface
    def call(env)
      # Only accept POST requests
      unless env["REQUEST_METHOD"] == "POST"
        return [405, { "Content-Type" => "text/plain" }, ["Method Not Allowed"]]
      end

      # Read request body
      body_bytes = env["rack.input"].read

      begin
        # Decode message set
        msg_set = @codec.unmarshal_messages(body_bytes)

        # Handle batch or single request
        response_msg_set = if msg_set.is_batch
                             batch = JSONRPC3.to_batch(msg_set)
                             batch_resp = @handler.handle_batch(batch)
                             JSONRPC3.batch_response_to_message_set(batch_resp)
                           else
                             request = JSONRPC3.to_request(msg_set)
                             response = @handler.handle_request(request)
                             MessageSet.new(messages: [response], is_batch: false)
                           end

        # Encode response
        resp_bytes = @codec.marshal_messages(response_msg_set)

        [200, { "Content-Type" => @codec.mime_type }, [resp_bytes]]
      rescue RpcError => e
        # Return RPC error as valid response
        error_resp = JSONRPC3.new_error_response(e.to_h, nil)
        resp_msg_set = MessageSet.new(messages: [error_resp], is_batch: false)
        resp_bytes = @codec.marshal_messages(resp_msg_set)
        [200, { "Content-Type" => @codec.mime_type }, [resp_bytes]]
      rescue StandardError => e
        # Internal server error
        [500, { "Content-Type" => "text/plain" }, ["Internal Server Error: #{e.message}"]]
      end
    end

    # Start the server
    def start
      @server_thread = Thread.new do
        @server = Puma::Server.new(self)
        @server.add_tcp_listener(@host, @requested_port)
        @server.run
      end

      # Wait for server to start and get actual port
      sleep 0.1 while @server.nil?

      # Get the actual bound port
      if @server
        @port = @server.connected_ports.first || @requested_port
      end

      self
    end

    # Stop the server
    def stop
      @server&.stop(true)
      @server_thread&.join
      @server = nil
      @server_thread = nil
    end

    # Get server URL
    def url
      @server && @port ? "http://#{@host}:#{@port}" : nil
    end
  end
end
