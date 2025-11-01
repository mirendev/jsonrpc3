/**
 * Tests for Reference convenience methods
 */

import { describe, test, expect } from "bun:test";
import { Peer } from "../src/peer.ts";
import { MethodMap } from "../src/helpers.ts";
import { Reference, toRef, Protocol } from "../src/types.ts";
import type { ReferenceType } from "../src/types.ts";

/**
 * Create a pair of connected streams for testing
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

describe("Reference", () => {
  test("toRef converts string to Reference", () => {
    const ref = toRef("test-ref");
    expect(ref).toEqual({ $ref: "test-ref" });
  });

  test("toRef returns Reference object as-is", () => {
    const ref: ReferenceType = { $ref: "test-ref" };
    const result = toRef(ref);
    expect(result).toBe(ref);
  });

  test("Reference can be created from string", () => {
    const wrapper = new Reference("test-ref");
    expect(wrapper.$ref).toBe("test-ref");
  });

  test("Reference can be created from Reference", () => {
    const ref: ReferenceType = { $ref: "test-ref" };
    const wrapper = new Reference(ref);
    expect(wrapper.$ref).toBe("test-ref");
  });

  test("Reference.toJSON returns plain Reference", () => {
    const wrapper = new Reference("test-ref");
    const json = wrapper.toJSON();
    expect(json).toEqual({ $ref: "test-ref" });
    expect(json).not.toBeInstanceOf(Reference);
  });

  test("Protocol constant is defined", () => {
    expect(Protocol).toBeInstanceOf(Reference);
    expect(Protocol.$ref).toBe("$rpc");
  });

  test("call() method invokes caller.call with reference", async () => {
    const [stream1, stream2] = createStreamPair();

    // Create a counter object
    const counter = new MethodMap();
    let count = 0;
    counter.register("increment", () => {
      count++;
      return count;
    });
    counter.register("getValue", () => count);

    // Create server with root
    const serverRoot = new MethodMap();
    const peer1 = new Peer(stream1.readable, stream1.writable, serverRoot);

    // Register counter object (returns a Reference instance)
    const counterRef = peer1.registerObject(counter);

    // Create client peer
    const clientRoot = new MethodMap();
    const peer2 = new Peer(stream2.readable, stream2.writable, clientRoot);

    await sleep(50);

    try {
      // Use the call() convenience method directly on the returned Reference
      const result1 = await counterRef.call(peer2, "increment");
      expect(result1).toBe(1);

      const result2 = await counterRef.call(peer2, "increment");
      expect(result2).toBe(2);

      const value = await counterRef.call(peer2, "getValue");
      expect(value).toBe(2);
    } finally {
      peer1.close();
      peer2.close();
    }
  });

  test("notify() method invokes caller.notify with reference", async () => {
    const [stream1, stream2] = createStreamPair();

    // Create an object that tracks notifications
    const tracker = new MethodMap();
    const notifications: string[] = [];
    tracker.register("log", (params) => {
      const msg = params.decode<string>();
      notifications.push(msg);
      return null; // Explicitly return null for notifications
    });

    // Create server with root
    const serverRoot = new MethodMap();
    const peer1 = new Peer(stream1.readable, stream1.writable, serverRoot);

    // Register tracker object (returns a Reference instance)
    const trackerRef = peer1.registerObject(tracker);

    // Create client peer
    const clientRoot = new MethodMap();
    const peer2 = new Peer(stream2.readable, stream2.writable, clientRoot);

    await sleep(50);

    try {
      // Use the notify() convenience method directly on the returned Reference
      await trackerRef.notify(peer2, "log", "first");
      await sleep(20);
      await trackerRef.notify(peer2, "log", "second");
      await sleep(20);
      await trackerRef.notify(peer2, "log", "third");

      // Wait for notifications to be processed
      await sleep(150);

      expect(notifications).toEqual(["first", "second", "third"]);
    } finally {
      peer1.close();
      peer2.close();
    }
  });

  test("Protocol.call() can be used for introspection", async () => {
    const [stream1, stream2] = createStreamPair();

    // Create peers
    const serverRoot = new MethodMap();
    const peer1 = new Peer(stream1.readable, stream1.writable, serverRoot);

    const clientRoot = new MethodMap();
    const peer2 = new Peer(stream2.readable, stream2.writable, clientRoot);

    await sleep(50);

    try {
      // Use Protocol constant to call session_id method
      const result = await Protocol.call(peer2, "session_id") as { session_id: string };
      expect(typeof result).toBe("object");
      expect(typeof result.session_id).toBe("string");
      expect(result.session_id).toBeTruthy();
    } finally {
      peer1.close();
      peer2.close();
    }
  });

  test("call() works with params", async () => {
    const [stream1, stream2] = createStreamPair();

    // Create a calculator object
    const calculator = new MethodMap();
    calculator.register("add", (params) => {
      const [a, b] = params.decode<[number, number]>();
      return a + b;
    });
    calculator.register("multiply", (params) => {
      const [a, b] = params.decode<[number, number]>();
      return a * b;
    });

    // Create server with root
    const serverRoot = new MethodMap();
    const peer1 = new Peer(stream1.readable, stream1.writable, serverRoot);

    // Register calculator object (returns a Reference instance)
    const calcRef = peer1.registerObject(calculator);

    // Create client peer
    const clientRoot = new MethodMap();
    const peer2 = new Peer(stream2.readable, stream2.writable, clientRoot);

    await sleep(50);

    try {
      // Call with parameters directly on the returned Reference
      const sum = await calcRef.call(peer2, "add", [5, 3]);
      expect(sum).toBe(8);

      const product = await calcRef.call(peer2, "multiply", [4, 7]);
      expect(product).toBe(28);
    } finally {
      peer1.close();
      peer2.close();
    }
  });

  test("returned references are automatically promoted to Reference instances", async () => {
    const [stream1, stream2] = createStreamPair();

    // Create a counter object
    const counter = new MethodMap();
    let count = 0;
    counter.register("increment", () => {
      count++;
      return count;
    });
    counter.register("getValue", () => count);

    // Create server with root that can create counters
    const serverRoot = new MethodMap();
    const peer1 = new Peer(stream1.readable, stream1.writable, serverRoot);

    serverRoot.register("createCounter", () => {
      // registerObject now returns a Reference instance
      // We can return it directly or use toJSON() for a plain object
      return peer1.registerObject(counter);
    });

    // Create client peer
    const clientRoot = new MethodMap();
    const peer2 = new Peer(stream2.readable, stream2.writable, clientRoot);

    await sleep(50);

    try {
      // Call createCounter - result should be automatically promoted to Reference instance
      const counterRef = await peer2.call("createCounter") as Reference;

      // Verify it's a Reference instance
      expect(counterRef).toBeInstanceOf(Reference);
      expect(counterRef.$ref).toBeTruthy();

      // Can call methods directly on it without wrapping
      const result1 = await counterRef.call(peer2, "increment");
      expect(result1).toBe(1);

      const result2 = await counterRef.call(peer2, "increment");
      expect(result2).toBe(2);

      const value = await counterRef.call(peer2, "getValue");
      expect(value).toBe(2);
    } finally {
      peer1.close();
      peer2.close();
    }
  });
});
