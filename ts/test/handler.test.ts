import { NoOpCaller } from "../src/types.ts";
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
    root.register("add", (params, caller) => {
      const nums = params.decode<number[]>();
      return nums[0]! + nums[1]!;
    });

    const handler = new Handler(session, root, new NoOpCaller());
    const req = newRequest("add", [2, 3], 1);
    const resp = await handler.handleRequest(req);

    expect(resp.error).toBeUndefined();
    expect(resp.result).toBe(5);
    expect(resp.id).toBe(1);
  });

  test("route to $rpc protocol handler", async () => {
    const session = new Session();
    const root = new MethodMap();
    const handler = new Handler(session, root, new NoOpCaller());

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

    const handler = new Handler(session, root, new NoOpCaller());
    const req = newRequest("increment", undefined, 1, ref);
    const resp = await handler.handleRequest(req);

    expect(resp.error).toBeUndefined();
    expect(resp.result).toBe(1);
  });

  test("method not found error", async () => {
    const session = new Session();
    const root = new MethodMap();
    const handler = new Handler(session, root, new NoOpCaller());

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

    const handler = new Handler(session, root, new NoOpCaller());
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
    root.register("add", (params, caller) => {
      const nums = params.decode<number[]>();
      return nums[0]! + nums[1]!;
    });

    const handler = new Handler(session, root, new NoOpCaller());
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

    const handler = new Handler(session, root, new NoOpCaller());
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
    const handler = new Handler(session, root, new NoOpCaller());

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

    const handler = new Handler(session, root, new NoOpCaller());
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

describe("MethodMap Introspection", () => {
  test("$methods returns array of method info", async () => {
    const methodMap = new MethodMap();
    methodMap.type = "TestObject";

    methodMap.register("add", (params, caller) => {
      const nums = params.decode<number[]>();
      return nums[0]! + nums[1]!;
    }, {
      description: "Adds two numbers",
      params: { a: "number", b: "number" },
      category: "math",
    });

    methodMap.register("subtract", (params, caller) => {
      const nums = params.decode<number[]>();
      return nums[0]! - nums[1]!;
    });

    const params = newParams(undefined);
    const result = await methodMap.callMethod("$methods", params);

    expect(Array.isArray(result)).toBe(true);
    const methods = result as Array<Record<string, unknown>>;

    // Should have at least 4 methods: add, subtract, $methods, $type
    expect(methods.length).toBeGreaterThanOrEqual(4);

    // Find the add method
    const addMethod = methods.find(m => m.name === "add");
    expect(addMethod).toBeDefined();
    expect(addMethod!.description).toBe("Adds two numbers");
    expect(addMethod!.category).toBe("math");
    expect(addMethod!.params).toEqual({ a: "number", b: "number" });

    // Find the subtract method (no metadata)
    const subtractMethod = methods.find(m => m.name === "subtract");
    expect(subtractMethod).toBeDefined();
    expect(subtractMethod!.name).toBe("subtract");
    expect(subtractMethod!.description).toBeUndefined();
    expect(subtractMethod!.category).toBeUndefined();
    expect(subtractMethod!.params).toBeUndefined();

    // Verify introspection methods are included
    const hasMethodsMethod = methods.some(m => m.name === "$methods");
    const hasTypeMethod = methods.some(m => m.name === "$type");
    expect(hasMethodsMethod).toBe(true);
    expect(hasTypeMethod).toBe(true);
  });

  test("$type returns custom type", async () => {
    const methodMap = new MethodMap();
    methodMap.type = "TestObject";

    const params = newParams(undefined);
    const result = await methodMap.callMethod("$type", params);

    expect(result).toBe("TestObject");
  });

  test("$type returns default when not set", async () => {
    const methodMap = new MethodMap();

    const params = newParams(undefined);
    const result = await methodMap.callMethod("$type", params);

    expect(result).toBe("MethodMap");
  });
});

describe("MethodMap with positional params", () => {
  test("$methods includes positional params info", async () => {
    const methodMap = new MethodMap();

    methodMap.register("multiply", (params, caller) => {
      const nums = params.decode<number[]>();
      return nums.reduce((a, b) => a * b, 1);
    }, {
      description: "Multiplies all provided numbers",
      params: ["number", "number", "...number"],
      category: "math",
    });

    const params = newParams(undefined);
    const result = await methodMap.callMethod("$methods", params);

    expect(Array.isArray(result)).toBe(true);
    const methods = result as Array<Record<string, unknown>>;

    // Find the multiply method
    const multiplyMethod = methods.find(m => m.name === "multiply");
    expect(multiplyMethod).toBeDefined();
    expect(multiplyMethod!.description).toBe("Multiplies all provided numbers");
    expect(multiplyMethod!.category).toBe("math");
    expect(multiplyMethod!.params).toEqual(["number", "number", "...number"]);
  });

  test("backwards compatibility - register without options", async () => {
    const methodMap = new MethodMap();

    // Old-style registration should still work
    methodMap.register("oldStyle", (params, caller) => {
      return "works";
    });

    const params = newParams(undefined);
    const result = await methodMap.callMethod("oldStyle", params);

    expect(result).toBe("works");
  });
});
