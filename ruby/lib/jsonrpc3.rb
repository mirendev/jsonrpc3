# frozen_string_literal: true

require_relative "jsonrpc3/error"
require_relative "jsonrpc3/types"
require_relative "jsonrpc3/params"
require_relative "jsonrpc3/session"
require_relative "jsonrpc3/encoding"
require_relative "jsonrpc3/helpers"
require_relative "jsonrpc3/protocol"
require_relative "jsonrpc3/handler"
require_relative "jsonrpc3/http_client"
require_relative "jsonrpc3/http_server"
require_relative "jsonrpc3/peer"
require_relative "jsonrpc3/batch"

# JSON-RPC 3.0 Ruby implementation
module JSONRPC3
  class << self
    attr_accessor :logger
  end
end
