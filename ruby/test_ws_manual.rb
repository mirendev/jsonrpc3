#!/usr/bin/env ruby
# Manual WebSocket test

require_relative 'lib/jsonrpc3'

# Enable logging
require 'logger'
JSONRPC3.logger = Logger.new($stdout)
JSONRPC3.logger.level = Logger::DEBUG

puts "Creating client..."
client = JSONRPC3::WebSocketClient.new("ws://localhost:18084")
puts "Client created, connection established"

puts "Calling add(2, 3)..."
begin
  result = client.call("add", [2, 3])
  puts "Result: #{result}"
  puts "Success!"
rescue => e
  puts "Error: #{e.message}"
  puts e.backtrace.first(5)
ensure
  client.close
end
