#!/usr/bin/env ruby
# frozen_string_literal: true

require_relative "../lib/jsonrpc3"

# Create a server
server_session = JSONRPC3::Session.new
root = JSONRPC3::MethodMap.new

# Register some methods
root.register("add") do |params|
  data = params.decode
  a = data["a"]
  b = data["b"]
  a + b
end

root.register("greet") do |params|
  data = params.decode
  name = data["name"]
  "Hello, #{name}!"
end

# Create an object factory
root.register("createCounter") do |_params|
  counter = JSONRPC3::MethodMap.new
  counter.type = "Counter"
  count = 0

  counter.register("increment") do |_params|
    count += 1
  end

  counter.register("decrement") do |_params|
    count -= 1
  end

  counter.register("getValue") do |_params|
    count
  end

  counter # Will be auto-registered and returned as { "$ref" => "..." }
end

handler = JSONRPC3::Handler.new(server_session, root)
server = JSONRPC3::HttpServer.new(handler, port: 3000)

server.start
puts "Server running at #{server.url}"

# Create a client
client_session = JSONRPC3::Session.new
client = JSONRPC3::HttpClient.new(server.url, client_session)

begin
  # Simple method call
  sum = client.call("add", { "a" => 5, "b" => 3 })
  puts "5 + 3 = #{sum}" # 8

  # String result
  greeting = client.call("greet", { "name" => "Alice" })
  puts greeting # "Hello, Alice!"

  # Create an object and get reference
  counter_ref = client.call("createCounter")
  puts "Created counter: #{counter_ref.inspect}"

  # Call methods on the counter object
  val1 = client.call("increment", nil, counter_ref)
  puts "After increment: #{val1}" # 1

  val2 = client.call("increment", nil, counter_ref)
  puts "After second increment: #{val2}" # 2

  val3 = client.call("decrement", nil, counter_ref)
  puts "After decrement: #{val3}" # 1

  final_value = client.call("getValue", nil, counter_ref)
  puts "Final value: #{final_value}" # 1

  # Use $rpc protocol methods
  session_info = client.call("session_id", nil, "$rpc")
  puts "Server session: #{session_info.inspect}"

  caps = client.call("capabilities", nil, "$rpc")
  puts "Server capabilities: #{caps.inspect}"
ensure
  server.stop
  puts "Server stopped"
end
