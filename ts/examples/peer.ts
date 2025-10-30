/**
 * Bidirectional Peer communication example
 * Demonstrates two peers communicating over streams
 */

import { Peer, MethodMap } from "../src/index.ts";

// Create stream pairs for bidirectional communication
// Each peer gets a readable and writable stream
const transform1 = new TransformStream<Uint8Array, Uint8Array>();
const transform2 = new TransformStream<Uint8Array, Uint8Array>();

// Peer 1 methods
const peer1Root = new MethodMap();
peer1Root.register("greet", (params) => {
  const { name } = params.decode<{ name: string }>();
  console.log(`[Peer 1] Received greet request for: ${name}`);
  return `Hello, ${name}! (from Peer 1)`;
});

peer1Root.register("createCounter", () => {
  console.log("[Peer 1] Creating counter object");
  const counter = new MethodMap();
  let count = 0;

  counter.register("increment", () => {
    count++;
    console.log(`[Peer 1] Counter incremented to: ${count}`);
    return count;
  });

  counter.register("getValue", () => {
    console.log(`[Peer 1] Getting counter value: ${count}`);
    return count;
  });

  // Will be auto-registered and returned as { $ref: "..." }
  return counter;
});

// Peer 2 methods
const peer2Root = new MethodMap();
peer2Root.register("add", (params) => {
  const { a, b } = params.decode<{ a: number; b: number }>();
  console.log(`[Peer 2] Received add request: ${a} + ${b}`);
  return a + b;
});

peer2Root.register("notify", (params) => {
  const message = params.decode<string>();
  console.log(`[Peer 2] Received notification: ${message}`);
  return null;
});

// Create peers with connected streams
const peer1 = new Peer(
  transform1.readable,
  transform2.writable,
  peer1Root
);

const peer2 = new Peer(
  transform2.readable,
  transform1.writable,
  peer2Root
);

console.log("=== JSON-RPC 3.0 Peer Example ===\n");

// Give peers time to start
await new Promise((resolve) => setTimeout(resolve, 50));

try {
  // 1. Peer 2 calls Peer 1
  console.log("1. Peer 2 calling greet on Peer 1");
  const greeting = await peer2.call("greet", { name: "Alice" });
  console.log(`   Result: ${greeting}\n`);

  // 2. Peer 1 calls Peer 2
  console.log("2. Peer 1 calling add on Peer 2");
  const sum = await peer1.call("add", { a: 5, b: 3 });
  console.log(`   Result: ${sum}\n`);

  // 3. Peer 2 sends notification to Peer 1
  console.log("3. Peer 1 sending notification to Peer 2");
  await peer1.notify("notify", "This is a one-way message");
  console.log("   Notification sent\n");

  // Wait for notification to be processed
  await new Promise((resolve) => setTimeout(resolve, 50));

  // 4. Create and use remote object
  console.log("4. Peer 2 creating counter object on Peer 1");
  const counterRef = await peer2.call("createCounter");
  console.log(`   Got reference: ${JSON.stringify(counterRef)}\n`);

  // 5. Call methods on the remote object
  console.log("5. Peer 2 calling methods on the counter object");
  const val1 = await peer2.call("increment", undefined, counterRef);
  console.log(`   After first increment: ${val1}`);

  const val2 = await peer2.call("increment", undefined, counterRef);
  console.log(`   After second increment: ${val2}`);

  const finalValue = await peer2.call("getValue", undefined, counterRef);
  console.log(`   Final value: ${finalValue}\n`);

  // 6. Concurrent calls
  console.log("6. Making concurrent calls");
  const [result1, result2, result3] = await Promise.all([
    peer2.call("greet", { name: "Bob" }),
    peer1.call("add", { a: 10, b: 20 }),
    peer2.call("greet", { name: "Charlie" }),
  ]);
  console.log(`   Concurrent results:`);
  console.log(`   - ${result1}`);
  console.log(`   - ${result2}`);
  console.log(`   - ${result3}\n`);

  console.log("=== All operations completed successfully! ===");
} catch (error) {
  console.error("Error:", error);
} finally {
  // Clean up
  peer1.close();
  peer2.close();
}
