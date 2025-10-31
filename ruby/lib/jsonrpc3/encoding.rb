# frozen_string_literal: true

require "json"

module JSONRPC3
  # MIME type constants
  MIME_TYPE_JSON = "application/json"
  MIME_TYPE_CBOR = "application/cbor"
  MIME_TYPE_CBOR_COMPACT = "application/cbor-compact"

  # Codec interface for encoding/decoding messages
  module Codec
    # Marshal a MessageSet to bytes
    def marshal_messages(msg_set)
      raise NotImplementedError
    end

    # Unmarshal bytes to a MessageSet
    def unmarshal_messages(data)
      raise NotImplementedError
    end

    # Marshal a value to bytes
    def marshal(value)
      raise NotImplementedError
    end

    # Unmarshal bytes to a value
    def unmarshal(data)
      raise NotImplementedError
    end

    # Get the MIME type for this codec
    def mime_type
      raise NotImplementedError
    end
  end

  # JSON codec implementation
  class JsonCodec
    include Codec

    def marshal_messages(msg_set)
      if msg_set.is_batch
        # Batch: array of messages
        json = msg_set.messages.map { |msg| msg.is_a?(Hash) ? msg : msg.to_h }.to_json
      else
        # Single message: unwrap from array
        msg = msg_set.messages.first
        json = (msg.is_a?(Hash) ? msg : msg.to_h).to_json
      end
      json.encode("UTF-8")
    end

    def unmarshal_messages(data)
      json_str = data.is_a?(String) ? data : data.force_encoding("UTF-8")

      # Detect batch by looking at first non-whitespace character
      first_char = json_str.lstrip[0]
      is_batch = first_char == "["

      decoded = JSON.parse(json_str)

      if is_batch
        messages = decoded.is_a?(Array) ? decoded : [decoded]
        MessageSet.new(messages: messages, is_batch: true)
      else
        MessageSet.new(messages: [decoded], is_batch: false)
      end
    rescue JSON::ParserError => e
      raise parse_error("JSON parse error: #{e.message}")
    end

    def marshal(value)
      value.to_json.encode("UTF-8")
    end

    def unmarshal(data)
      json_str = data.is_a?(String) ? data : data.force_encoding("UTF-8")
      JSON.parse(json_str)
    rescue JSON::ParserError => e
      raise parse_error("JSON parse error: #{e.message}")
    end

    def mime_type
      MIME_TYPE_JSON
    end
  end

  # Get codec by MIME type
  def self.get_codec(mime_type)
    case mime_type
    when MIME_TYPE_JSON
      JsonCodec.new
    when MIME_TYPE_CBOR, MIME_TYPE_CBOR_COMPACT
      raise NotImplementedError, "CBOR encoding not yet implemented"
    else
      raise ArgumentError, "Unknown MIME type: #{mime_type}"
    end
  end
end
