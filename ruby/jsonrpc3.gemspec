# frozen_string_literal: true

Gem::Specification.new do |spec|
  spec.name = "jsonrpc3"
  spec.version = "0.1.0"
  spec.authors = ["JSON-RPC 3.0 Team"]
  spec.email = ["dev@jsonrpc.org"]

  spec.summary = "JSON-RPC 3.0 Ruby implementation"
  spec.description = "Complete JSON-RPC 3.0 implementation with support for object references, batch requests, and bidirectional communication"
  spec.homepage = "https://github.com/evanphx/jsonrpc3"
  spec.license = "MIT"
  spec.required_ruby_version = ">= 3.0.0"

  spec.files = Dir["lib/**/*.rb", "README.md", "LICENSE"]
  spec.require_paths = ["lib"]

  # Runtime dependencies
  spec.add_dependency "rack", "~> 3.0"
  spec.add_dependency "puma", "~> 6.0"
  spec.add_dependency "faye-websocket", "~> 0.11"
  spec.add_dependency "websocket", "~> 1.2"

  # Development dependencies
  spec.add_development_dependency "minitest", "~> 5.0"
  spec.add_development_dependency "rake", "~> 13.0"
end
