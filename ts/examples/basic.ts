/**
 * Basic JSON-RPC 3.0 example with HTTP transport
 */

import { Session, Handler, MethodMap, HttpServer, HttpClient, type LocalReference } from "../src/index.ts";

// Create a server
const serverSession = new Session();
const root = new MethodMap();

// Register some methods
root.register("add", (params) => {
  const { a, b } = params.decode<{ a: number; b: number }>();
  return a + b;
});

root.register("greet", (params) => {
  const { name } = params.decode<{ name: string }>();
  return `Hello, ${name}!`;
});

// Create an object factory
root.register("createCounter", () => {
  const counter = new MethodMap();
  counter.type = "Counter";
  let count = 0;

  counter.register("increment", () => {
    count++;
    return count;
  });

  counter.register("decrement", () => {
    count--;
    return count;
  });

  counter.register("getValue", () => {
    return count;
  });

  return counter; // Will be auto-registered and returned as { $ref: "..." }
});

const handler = new Handler(serverSession, root);
const server = new HttpServer(handler, { port: 3000 });

await server.start();
console.log(`Server running at ${server.url()}`);

// Create a client
const clientSession = new Session();
const client = new HttpClient(server.url()!, clientSession);

try {
  // Simple method call
  const sum = await client.call("add", { a: 5, b: 3 });
  console.log("5 + 3 =", sum); // 8

  // String result
  const greeting = await client.call("greet", { name: "Alice" });
  console.log(greeting); // "Hello, Alice!"

  // Create an object and get reference
  const counterRef = await client.call("createCounter") as LocalReference;
  console.log("Created counter:", counterRef); // { $ref: "ref-..." }

  // Call methods on the counter object (can pass LocalReference directly)
  const val1 = await client.call("increment", undefined, counterRef);
  console.log("After increment:", val1); // 1

  const val2 = await client.call("increment", undefined, counterRef);
  console.log("After second increment:", val2); // 2

  const val3 = await client.call("decrement", undefined, counterRef);
  console.log("After decrement:", val3); // 1

  const finalValue = await client.call("getValue", undefined, counterRef);
  console.log("Final value:", finalValue); // 1

  // Use $rpc protocol methods
  const sessionInfo = await client.call("session_id", undefined, "$rpc");
  console.log("Server session:", sessionInfo);

  const caps = await client.call("capabilities", undefined, "$rpc");
  console.log("Server capabilities:", caps);
} finally {
  await server.stop();
  console.log("Server stopped");
}
