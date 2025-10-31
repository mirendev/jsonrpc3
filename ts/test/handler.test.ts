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

describe("MethodMap Introspection", () => {
  test("$methods includes introspection methods", async () => {
    const methodMap = new MethodMap();
    methodMap.type = "TestObject";

    methodMap.register("add", (params) => {
      const nums = params.decode<number[]>();
      return nums[0]! + nums[1]!;
    });

    methodMap.register("subtract", (params) => {
      const nums = params.decode<number[]>();
      return nums[0]! - nums[1]!;
    });

    const params = newParams(undefined);
    const result = await methodMap.callMethod("$methods", params);

    expect(Array.isArray(result)).toBe(true);
    const methods = result as string[];
    expect(methods).toContain("add");
    expect(methods).toContain("subtract");
    expect(methods).toContain("$methods");
    expect(methods).toContain("$type");
    expect(methods).toContain("$method");
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

describe("MethodMap $method Introspection", () => {
  test("$method with named params", async () => {
    const methodMap = new MethodMap();

    methodMap.register("add", (params) => {
      const { a, b } = params.decode<{ a: number; b: number }>();
      return a + b;
    }, {
      description: "Adds two numbers and returns the sum",
      params: {
        a: "number",
        b: "number",
      },
    });

    const params = newParams("add");
    const result = await methodMap.callMethod("$method", params);

    expect(result).toBeDefined();
    const info = result as Record<string, unknown>;
    expect(info.name).toBe("add");
    expect(info.description).toBe("Adds two numbers and returns the sum");
    expect(info.params).toEqual({
      a: "number",
      b: "number",
    });
  });

  test("$method with positional params", async () => {
    const methodMap = new MethodMap();

    methodMap.register("multiply", (params) => {
      const nums = params.decode<number[]>();
      return nums.reduce((a, b) => a * b, 1);
    }, {
      description: "Multiplies all provided numbers",
      params: ["number", "number", "...number"],
    });

    const params = newParams("multiply");
    const result = await methodMap.callMethod("$method", params);

    expect(result).toBeDefined();
    const info = result as Record<string, unknown>;
    expect(info.name).toBe("multiply");
    expect(info.description).toBe("Multiplies all provided numbers");
    expect(info.params).toEqual(["number", "number", "...number"]);
  });

  test("$method without metadata", async () => {
    const methodMap = new MethodMap();

    methodMap.register("noMeta", () => {
      return "test";
    });

    const params = newParams("noMeta");
    const result = await methodMap.callMethod("$method", params);

    expect(result).toBeDefined();
    const info = result as Record<string, unknown>;
    expect(info.name).toBe("noMeta");
    expect(info.description).toBeUndefined();
    expect(info.params).toBeUndefined();
  });

  test("$method for non-existent method returns null", async () => {
    const methodMap = new MethodMap();

    const params = newParams("doesNotExist");
    const result = await methodMap.callMethod("$method", params);

    expect(result).toBeNull();
  });

  test("$method with invalid params throws error", async () => {
    const methodMap = new MethodMap();

    const params = newParams(123); // Not a string

    expect(methodMap.callMethod("$method", params)).rejects.toThrow();
  });

  test("backwards compatibility - register without options", async () => {
    const methodMap = new MethodMap();

    // Old-style registration should still work
    methodMap.register("oldStyle", (params) => {
      return "works";
    });

    const params = newParams(undefined);
    const result = await methodMap.callMethod("oldStyle", params);

    expect(result).toBe("works");
  });

  test("backwards compatibility - register with only description", async () => {
    const methodMap = new MethodMap();

    methodMap.register("withDesc", (params) => {
      return "works";
    }, {
      description: "A method with only description",
    });

    const params = newParams("withDesc");
    const result = await methodMap.callMethod("$method", params);

    expect(result).toBeDefined();
    const info = result as Record<string, unknown>;
    expect(info.name).toBe("withDesc");
    expect(info.description).toBe("A method with only description");
    expect(info.params).toBeUndefined();
  });

  test("backwards compatibility - register with only params", async () => {
    const methodMap = new MethodMap();

    methodMap.register("withParams", (params) => {
      return "works";
    }, {
      params: { x: "number" },
    });

    const params = newParams("withParams");
    const result = await methodMap.callMethod("$method", params);

    expect(result).toBeDefined();
    const info = result as Record<string, unknown>;
    expect(info.name).toBe("withParams");
    expect(info.description).toBeUndefined();
    expect(info.params).toEqual({ x: "number" });
  });
});
