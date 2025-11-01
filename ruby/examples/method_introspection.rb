#!/usr/bin/env ruby
# frozen_string_literal: true

require_relative "../lib/jsonrpc3"

# This example demonstrates the $method introspection feature
# which allows clients to query metadata about available methods

# Create a MethodMap with various methods
map = JSONRPC3::MethodMap.new
map.type = "Calculator"

# Register a basic method without metadata
map.register("echo") do |params|
  params.decode
end

# Register a method with description
map.register("add",
             description: "Adds two numbers") do |params|
  data = params.decode
  data["a"] + data["b"]
end

# Register a method with description and named parameters
map.register("subtract",
             description: "Subtracts b from a",
             params: { "a" => "number", "b" => "number" }) do |params|
  data = params.decode
  data["a"] - data["b"]
end

# Register a method with positional parameters
map.register("multiply",
             description: "Multiplies two or more numbers",
             params: ["number", "number"]) do |params|
  data = params.decode
  data.reduce(1, :*)
end

# Register a method with complex parameter types
map.register("calculate",
             description: "Performs a calculation based on operation",
             params: {
               "operation" => "string (add|subtract|multiply|divide)",
               "values" => "array of numbers"
             }) do |params|
  data = params.decode
  operation = data["operation"]
  values = data["values"]

  case operation
  when "add"
    values.sum
  when "subtract"
    values.reduce(:-)
  when "multiply"
    values.reduce(:*)
  when "divide"
    values.reduce(:/)
  else
    raise JSONRPC3::RpcError.new("Unknown operation", code: JSONRPC3::CODE_INVALID_PARAMS)
  end
end

puts "=== Method Introspection Demo ==="
puts

# 1. List all methods
puts "1. List all methods using $methods:"
methods = map.call_method("$methods", JSONRPC3::NULL_PARAMS)
puts "   Available methods: #{methods.join(', ')}"
puts

# 2. Get object type
puts "2. Get object type using $type:"
type = map.call_method("$type", JSONRPC3::NULL_PARAMS)
puts "   Object type: #{type}"
puts

# 3. Get info for method without metadata
puts "3. Get info for 'echo' (no metadata):"
echo_info = map.call_method("$method", JSONRPC3.new_params("echo"))
puts "   #{echo_info.inspect}"
puts

# 4. Get info for method with description only
puts "4. Get info for 'add' (with description):"
add_info = map.call_method("$method", JSONRPC3.new_params("add"))
puts "   Name: #{add_info['name']}"
puts "   Description: #{add_info['description']}"
puts

# 5. Get info for method with named parameters
puts "5. Get info for 'subtract' (with named parameters):"
subtract_info = map.call_method("$method", JSONRPC3.new_params("subtract"))
puts "   Name: #{subtract_info['name']}"
puts "   Description: #{subtract_info['description']}"
puts "   Parameters: #{subtract_info['params'].inspect}"
puts

# 6. Get info for method with positional parameters
puts "6. Get info for 'multiply' (with positional parameters):"
multiply_info = map.call_method("$method", JSONRPC3.new_params("multiply"))
puts "   Name: #{multiply_info['name']}"
puts "   Description: #{multiply_info['description']}"
puts "   Parameters: #{multiply_info['params'].inspect}"
puts

# 7. Get info for method with complex parameters
puts "7. Get info for 'calculate' (with complex parameters):"
calculate_info = map.call_method("$method", JSONRPC3.new_params("calculate"))
puts "   Name: #{calculate_info['name']}"
puts "   Description: #{calculate_info['description']}"
puts "   Parameters:"
calculate_info['params'].each do |key, value|
  puts "     - #{key}: #{value}"
end
puts

# 8. Query non-existent method
puts "8. Query non-existent method 'divide':"
divide_info = map.call_method("$method", JSONRPC3.new_params("divide"))
puts "   Result: #{divide_info.inspect} (returns nil for non-existent methods)"
puts

# 9. Query introspection method itself
puts "9. Query introspection method '$methods':"
begin
  methods_info = map.call_method("$method", JSONRPC3.new_params("$methods"))
  puts "   Result: #{methods_info.inspect} (introspection methods not in metadata)"
rescue => e
  puts "   Result: #{e.message}"
end
puts

# 10. Demonstrate actual method calls work
puts "10. Test actual method calls:"
result = map.call_method("add", JSONRPC3.new_params({ "a" => 5, "b" => 3 }))
puts "   add(a=5, b=3) = #{result}"

result = map.call_method("multiply", JSONRPC3.new_params([2, 3, 4]))
puts "   multiply(2, 3, 4) = #{result}"

result = map.call_method("calculate", JSONRPC3.new_params({
  "operation" => "add",
  "values" => [1, 2, 3, 4, 5]
}))
puts "   calculate(operation='add', values=[1,2,3,4,5]) = #{result}"
puts

puts "=== Demo Complete ==="
