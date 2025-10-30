import { test, expect, describe } from "bun:test";
import { Session } from "../src/session.ts";
import { Handler } from "../src/handler.ts";
import { MethodMap } from "../src/helpers.ts";
import { newRequest } from "../src/types.ts";
import { newParams } from "../src/params.ts";

describe("Handler", () => {
  test("route to root object", async () => {
    const session = new Session();
    const root = new MethodMap();
    root.register("add", (params) => {
      const nums = params.decode<number[]>();
      return nums[0]! + nums[1]!;
    });

    const handler = new Handler(session, root);
    const req = newRequest("add", [2, 3], 1);
    const resp = await handler.handleRequest(req);

    expect(resp.error).toBeUndefined();
    expect(resp.result).toBe(5);
    expect(resp.id).toBe(1);
  });

  test("route to $rpc protocol handler", async () => {
    const session = new Session();
    const root = new MethodMap();
    const handler = new Handler(session, root);

    const req = newRequest("session_id", undefined, 1, "$rpc");
    const resp = await handler.handleRequest(req);

    expect(resp.error).toBeUndefined();
    expect(resp.result).toHaveProperty("session_id");
    expect((resp.result as { session_id: string }).session_id).toBe(session.id());
  });

  test("route to local reference", async () => {
    const session = new Session();
    const root = new MethodMap();

    // Create an object and register it
    const counter = new MethodMap();
    let count = 0;
    counter.register("increment", () => ++count);
    const ref = session.addLocalRef(counter);

    const handler = new Handler(session, root);
    const req = newRequest("increment", undefined, 1, ref);
    const resp = await handler.handleRequest(req);

    expect(resp.error).toBeUndefined();
    expect(resp.result).toBe(1);
  });

  test("method not found error", async () => {
    const session = new Session();
    const root = new MethodMap();
    const handler = new Handler(session, root);

    const req = newRequest("nonexistent", undefined, 1);
    const resp = await handler.handleRequest(req);

    expect(resp.result).toBeUndefined();
    expect(resp.error).toBeDefined();
    expect(resp.error?.code).toBe(-32601);
    expect(resp.error?.message).toContain("Method not found");
  });

  test("auto-register returned objects", async () => {
    const session = new Session();
    const root = new MethodMap();

    root.register("createCounter", () => {
      const counter = new MethodMap();
      let count = 0;
      counter.register("increment", () => ++count);
      return counter;
    });

    const handler = new Handler(session, root);
    const req = newRequest("createCounter", undefined, 1);
    const resp = await handler.handleRequest(req);

    expect(resp.error).toBeUndefined();
    expect(resp.result).toHaveProperty("$ref");

    const ref = (resp.result as { $ref: string }).$ref;
    expect(session.hasLocalRef(ref)).toBe(true);
  });
});

describe("Handler - Batch", () => {
  test("handle batch requests", async () => {
    const session = new Session();
    const root = new MethodMap();
    root.register("add", (params) => {
      const nums = params.decode<number[]>();
      return nums[0]! + nums[1]!;
    });

    const handler = new Handler(session, root);
    const batch = [
      newRequest("add", [1, 2], 1),
      newRequest("add", [3, 4], 2),
    ];

    const responses = await handler.handleBatch(batch);

    expect(responses).toHaveLength(2);
    expect(responses[0]?.result).toBe(3);
    expect(responses[1]?.result).toBe(7);
  });

  test("batch-local references", async () => {
    const session = new Session();
    const root = new MethodMap();

    root.register("createCounter", () => {
      const counter = new MethodMap();
      let count = 0;
      counter.register("increment", () => ++count);
      return counter;
    });

    const handler = new Handler(session, root);
    const batch = [
      newRequest("createCounter", undefined, 0),
      newRequest("increment", undefined, 1, "\\0"), // Ref to first result
    ];

    const responses = await handler.handleBatch(batch);

    expect(responses).toHaveLength(2);
    expect(responses[0]?.result).toHaveProperty("$ref");
    expect(responses[1]?.result).toBe(1);
  });

  test("forward reference error", async () => {
    const session = new Session();
    const root = new MethodMap();
    const handler = new Handler(session, root);

    const batch = [
      newRequest("someMethod", undefined, 0, "\\1"), // Forward ref!
      newRequest("anotherMethod", undefined, 1),
    ];

    const responses = await handler.handleBatch(batch);

    expect(responses).toHaveLength(2);
    expect(responses[0]?.error).toBeDefined();
    expect(responses[0]?.error?.code).toBe(-32001); // Invalid reference
  });

  test("skip notifications in batch response", async () => {
    const session = new Session();
    const root = new MethodMap();
    root.register("log", () => "logged");

    const handler = new Handler(session, root);
    const batch = [
      newRequest("log", { msg: "test" }, 1),
      newRequest("log", { msg: "test" }), // Notification (no id)
    ];

    const responses = await handler.handleBatch(batch);

    // Only one response (notification is skipped)
    expect(responses).toHaveLength(1);
    expect(responses[0]?.id).toBe(1);
  });
});
