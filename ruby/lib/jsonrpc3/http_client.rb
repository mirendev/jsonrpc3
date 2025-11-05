# frozen_string_literal: true

require "net/http"
require "uri"

module JSONRPC3
  # HTTP Client for JSON-RPC 3.0
  class HttpClient
    include Caller
    attr_reader :session

    def initialize(url, session = nil, options = {})
      @url = url
      @uri = URI.parse(url)
      @session = session || Session.new
      @codec = JSONRPC3.get_codec(options[:mime_type] || MIME_TYPE_JSON)
      @headers = options[:headers] || {}
      @request_id = 0
      @mutex = Mutex.new
    end

    # Call a method and wait for response
    def call(method, params = nil, ref = nil)
      id = next_id
      ref_string = case ref
                   when String then ref
                   when Hash then ref["$ref"] || ref[:$ref]
                   else ref&.ref
                   end
      req = JSONRPC3.new_request(method, params, id, ref_string)

      msg_set = MessageSet.new(messages: [req.to_h], is_batch: false)
      resp_msg_set = send_request(msg_set)

      # Get the single response message
      raise "Expected single response" if resp_msg_set.messages.size != 1

      msg = resp_msg_set.messages.first
      raise "Invalid response message" unless JSONRPC3.response?(msg)

      resp = msg.is_a?(Response) ? msg : Response.from_h(msg)

      # Check for error
      raise RpcError.from_h(resp.error) if resp.error

      # Track remote references
      track_remote_references(resp.result)

      resp.result
    end

    # Send a notification (no response expected)
    def notify(method, params = nil, ref = nil)
      ref_string = case ref
                   when String then ref
                   when Hash then ref["$ref"] || ref[:$ref]
                   else ref&.ref
                   end
      req = JSONRPC3.new_request(method, params, nil, ref_string)

      msg_set = MessageSet.new(messages: [req.to_h], is_batch: false)
      send_request(msg_set)
      nil
    end

    # Send a batch of requests
    def batch(requests)
      msg_set = MessageSet.new(
        messages: requests.map(&:to_h),
        is_batch: true
      )

      resp_msg_set = send_request(msg_set)

      responses = resp_msg_set.messages.map do |msg|
        next unless JSONRPC3.response?(msg)

        resp = msg.is_a?(Response) ? msg : Response.from_h(msg)
        track_remote_references(resp.result) if resp.result
        resp
      end.compact

      responses
    end

    private

    # Get next request ID
    def next_id
      @mutex.synchronize { @request_id += 1 }
    end

    # Send HTTP request
    def send_request(msg_set)
      req_bytes = @codec.marshal_messages(msg_set)

      http = Net::HTTP.new(@uri.host, @uri.port)
      http.use_ssl = @uri.scheme == "https"

      request = Net::HTTP::Post.new(@uri.path.empty? ? "/" : @uri.path)
      request["Content-Type"] = @codec.mime_type

      # Add custom headers
      @headers.each do |key, value|
        request[key] = value
      end

      request.body = req_bytes

      response = http.request(request)

      # 204 No Content is valid for notifications (batch of all notifications)
      if response.code.to_i == 204
        return MessageSet.new(messages: [], is_batch: msg_set.is_batch)
      end

      raise "HTTP error: #{response.code}" unless response.code.to_i == 200

      @codec.unmarshal_messages(response.body)
    end

    # Track remote references in results
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
