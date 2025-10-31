#!/usr/bin/env python3
"""Basic JSON-RPC 3.0 example using HTTP client and server."""

import time
from jsonrpc3 import (
    Session,
    MethodMap,
    Handler,
    HttpServer,
    HttpClient,
)


def main():
    # Create server
    server_session = Session()
    root = MethodMap()

    # Register some methods
    def add_handler(params):
        data = params.decode()
        a = data["a"]
        b = data["b"]
        return a + b

    root.register("add", add_handler)

    def greet_handler(params):
        data = params.decode()
        name = data["name"]
        return f"Hello, {name}!"

    root.register("greet", greet_handler)

    # Create an object factory
    def create_counter_handler(params):
        counter = MethodMap()
        counter.type = "Counter"
        count = [0]  # Use list to allow mutation in closures

        def increment_handler(params):
            count[0] += 1
            return count[0]

        def decrement_handler(params):
            count[0] -= 1
            return count[0]

        def get_value_handler(params):
            return count[0]

        counter.register("increment", increment_handler)
        counter.register("decrement", decrement_handler)
        counter.register("getValue", get_value_handler)

        return counter  # Will be auto-registered and returned as {"$ref": "..."}

    root.register("createCounter", create_counter_handler)

    handler = Handler(server_session, root)
    server = HttpServer(handler, port=0)  # Use port 0 for random available port
    server.start()

    # Wait for server to be ready
    time.sleep(0.5)

    print(f"Server running at {server.url}")

    # Create client
    client_session = Session()
    client = HttpClient(server.url, client_session)

    try:
        # Simple method call
        sum_result = client.call("add", {"a": 5, "b": 3})
        print(f"5 + 3 = {sum_result}")  # 8

        # String result
        greeting = client.call("greet", {"name": "Alice"})
        print(greeting)  # "Hello, Alice!"

        # Create an object and get reference
        counter_ref = client.call("createCounter")
        print(f"Created counter: {counter_ref}")

        # Call methods on the counter object
        val1 = client.call("increment", None, counter_ref)
        print(f"After increment: {val1}")  # 1

        val2 = client.call("increment", None, counter_ref)
        print(f"After second increment: {val2}")  # 2

        val3 = client.call("decrement", None, counter_ref)
        print(f"After decrement: {val3}")  # 1

        final_value = client.call("getValue", None, counter_ref)
        print(f"Final value: {final_value}")  # 1

        # Use $rpc protocol methods
        session_info = client.call("session_id", None, "$rpc")
        print(f"Server session: {session_info}")

        caps = client.call("capabilities", None, "$rpc")
        print(f"Server capabilities: {caps}")

    finally:
        server.stop()
        print("Server stopped")


if __name__ == "__main__":
    main()
