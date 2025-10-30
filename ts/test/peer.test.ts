/**
 * Tests for Peer bidirectional communication
 */

import { describe, test, expect } from "bun:test";
import { Peer } from "../src/peer.ts";
import { MethodMap } from "../src/helpers.ts";
import type { LocalReference } from "../src/types.ts";

/**
 * Create a pair of connected streams for testing
 * Returns [stream1, stream2] where writes to stream1 are readable from stream2 and vice versa
 */
function createStreamPair(): [
  { readable: ReadableStream<Uint8Array>; writable: WritableStream<Uint8Array> },
  { readable: ReadableStream<Uint8Array>; writable: WritableStream<Uint8Array> }
] {
  const transform1 = new TransformStream<Uint8Array, Uint8Array>();
  const transform2 = new TransformStream<Uint8Array, Uint8Array>();

  return [
    {
      readable: transform1.readable,
      writable: transform2.writable,
    },
    {
      readable: transform2.readable,
      writable: transform1.writable,
    },
  ];
}

/**
 * Wait for a short time to allow async operations to complete
 */
function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

describe("Peer", () => {
  test("basic communication - echo method", async () => {
    const [stream1, stream2] = createStreamPair();

    // Create server root with echo method
    const serverRoot = new MethodMap();
    serverRoot.register("echo", (params) => {
      const msg = params.decode<string>();
      return msg;
    });

    // Create peers
    const peer1 = new Peer(stream1.readable, stream1.writable, serverRoot);
    const peer2 = new Peer(stream2.readable, stream2.writable, new MethodMap());

    // Give peers time to start
    await sleep(50);

    try {
      // Test echo call from peer2 to peer1
      const result = await peer2.call("echo", "hello");
      expect(result).toBe("hello");
    } finally {
      peer1.close();
      peer2.close();
    }
  });

  test("bidirectional calls - both peers can call each other", async () => {
    const [stream1, stream2] = createStreamPair();

    // Peer 1 methods
    const peer1Root = new MethodMap();
    peer1Root.register("greet", (params) => {
      const name = params.decode<string>();
      return `Hello, ${name}`;
    });

    // Peer 2 methods
    const peer2Root = new MethodMap();
    peer2Root.register("add", (params) => {
      const nums = params.decode<number[]>();
      return nums.reduce((sum, n) => sum + n, 0);
    });

    const peer1 = new Peer(stream1.readable, stream1.writable, peer1Root);
    const peer2 = new Peer(stream2.readable, stream2.writable, peer2Root);

    await sleep(50);

    try {
      // Peer 2 calls peer 1
      const greeting = await peer2.call("greet", "Alice");
      expect(greeting).toBe("Hello, Alice");

      // Peer 1 calls peer 2
      const sum = await peer1.call("add", [1, 2, 3, 4, 5]);
      expect(sum).toBe(15);
    } finally {
      peer1.close();
      peer2.close();
    }
  });

  test("notifications - one-way messages", async () => {
    const [stream1, stream2] = createStreamPair();

    let notifCount = 0;
    const serverRoot = new MethodMap();
    serverRoot.register("log", () => {
      notifCount++;
      return null;
    });

    const peer1 = new Peer(stream1.readable, stream1.writable, serverRoot);
    const peer2 = new Peer(stream2.readable, stream2.writable, new MethodMap());

    await sleep(50);

    try {
      // Send notifications
      await peer2.notify("log", "message 1");
      await peer2.notify("log", "message 2");
      await peer2.notify("log", "message 3");

      // Wait for notifications to be processed
      await sleep(100);

      // Verify notifications were received
      expect(notifCount).toBe(3);
    } finally {
      peer1.close();
      peer2.close();
    }
  });

  test("concurrent requests - multiple requests in flight", async () => {
    const [stream1, stream2] = createStreamPair();

    let callCount = 0;
    const serverRoot = new MethodMap();
    serverRoot.register("increment", () => {
      callCount++;
      return callCount;
    });

    const peer1 = new Peer(stream1.readable, stream1.writable, serverRoot);
    const peer2 = new Peer(stream2.readable, stream2.writable, new MethodMap());

    await sleep(50);

    try {
      // Make concurrent calls
      const numCalls = 10;
      const promises = Array.from({ length: numCalls }, () =>
        peer2.call("increment")
      );

      const results = await Promise.all(promises);

      // Verify all calls completed
      expect(results).toHaveLength(numCalls);
      expect(callCount).toBe(numCalls);
    } finally {
      peer1.close();
      peer2.close();
    }
  });

  test("object registration and remote calls", async () => {
    const [stream1, stream2] = createStreamPair();

    // Create counter object
    const counterObj = new MethodMap();
    let count = 0;

    counterObj.register("increment", () => {
      count++;
      return count;
    });

    counterObj.register("getCount", () => {
      return count;
    });

    // Server returns reference to counter
    const serverRoot = new MethodMap();
    serverRoot.register("getCounter", () => {
      return { $ref: "counter-1" };
    });

    const peer1 = new Peer(stream1.readable, stream1.writable, serverRoot);
    peer1.registerObject(counterObj, "counter-1");

    const peer2 = new Peer(stream2.readable, stream2.writable, new MethodMap());

    await sleep(50);

    try {
      // Get counter reference
      const counterRef = (await peer2.call("getCounter")) as LocalReference;
      expect(counterRef).toHaveProperty("$ref");

      // Call methods on the counter object
      const result1 = await peer2.call("increment", undefined, counterRef);
      expect(result1).toBe(1);

      const result2 = await peer2.call("increment", undefined, counterRef);
      expect(result2).toBe(2);

      const result3 = await peer2.call("getCount", undefined, counterRef);
      expect(result3).toBe(2);
    } finally {
      peer1.close();
      peer2.close();
    }
  });

  test("error handling - errors are propagated correctly", async () => {
    const [stream1, stream2] = createStreamPair();

    const serverRoot = new MethodMap();
    serverRoot.register("divide", (params) => {
      const { a, b } = params.decode<{ a: number; b: number }>();
      if (b === 0) {
        throw new Error("Division by zero");
      }
      return a / b;
    });

    const peer1 = new Peer(stream1.readable, stream1.writable, serverRoot);
    const peer2 = new Peer(stream2.readable, stream2.writable, new MethodMap());

    await sleep(50);

    try {
      // Test valid division
      const result = await peer2.call("divide", { a: 10, b: 2 });
      expect(result).toBe(5);

      // Test division by zero - should throw
      await expect(peer2.call("divide", { a: 10, b: 0 })).rejects.toThrow();

      // Test method not found - should throw
      await expect(peer2.call("nonexistent")).rejects.toThrow();
    } finally {
      peer1.close();
      peer2.close();
    }
  });

  test("auto-generated reference IDs", async () => {
    const [stream1, stream2] = createStreamPair();

    const obj1 = new MethodMap();
    obj1.register("test1", () => "result1");

    const obj2 = new MethodMap();
    obj2.register("test2", () => "result2");

    const peer1 = new Peer(stream1.readable, stream1.writable, new MethodMap());

    // Register objects without specifying ref IDs
    const ref1 = peer1.registerObject(obj1);
    const ref2 = peer1.registerObject(obj2);

    // Refs should be auto-generated and different
    expect(ref1).toBeTruthy();
    expect(ref2).toBeTruthy();
    expect(ref1).not.toBe(ref2);

    peer1.close();
  });

  test("close handling - calls after close are rejected", async () => {
    const [stream1, stream2] = createStreamPair();

    const serverRoot = new MethodMap();
    serverRoot.register("echo", (params) => {
      return params.decode<string>();
    });

    const peer1 = new Peer(stream1.readable, stream1.writable, serverRoot);
    const peer2 = new Peer(stream2.readable, stream2.writable, new MethodMap());

    await sleep(50);

    // Make a successful call
    const result = await peer2.call("echo", "test");
    expect(result).toBe("test");

    // Close both peers
    peer1.close();
    peer2.close();

    // Wait a moment for close to complete
    await sleep(50);

    // Try to call after close - should fail
    await expect(peer2.call("echo", "test")).rejects.toThrow();
  });
});
