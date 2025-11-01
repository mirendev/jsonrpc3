# frozen_string_literal: true

require_relative "../lib/jsonrpc3"

# Example demonstrating the Reference convenience methods

puts "Reference Convenience Methods Example"
puts "=" * 50

# Create a reference
ref = JSONRPC3::Reference.new(ref: "counter-123")
puts "\nCreated reference: #{ref.to_h.inspect}"

# Demonstrate the API pattern
# In a real scenario, you would have a client (HttpClient or Peer)
# Here we show the method signatures:

puts "\nUsage Pattern:"
puts "  ref.call(client, 'methodName', params)    # Makes a call"
puts "  ref.notify(client, 'methodName', params)  # Sends a notification"

puts "\nComparison with Go:"
puts "  Go:   ref.Call(caller, 'method', params, &result)"
puts "  Ruby: result = ref.call(caller, 'method', params)"

puts "\nExample code:"
puts <<~CODE
  # Instead of:
  client.call('increment', nil, ref)

  # You can now use:
  ref.call(client, 'increment')

  # And for notifications:
  ref.notify(client, 'update', { value: 42 })
CODE

puts "\nBenefits:"
puts "  • More object-oriented API"
puts "  • Consistent with Go implementation"
puts "  • Clearer intent when working with references"
