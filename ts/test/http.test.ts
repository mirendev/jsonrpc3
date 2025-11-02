import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { Session } from "../src/session.ts";
import { Handler } from "../src/handler.ts";
import { MethodMap } from "../src/helpers.ts";
import { HttpServer } from "../src/http-server.ts";
import { HttpClient } from "../src/http-client.ts";
import { RpcError } from "../src/error.ts";

describe("HTTP Transport", () => {
  let server: HttpServer;
  let serverUrl: string;

  beforeAll(async () => {
    // Create server
    const session = new Session();
    const root = new MethodMap();

    root.register("add", (params, caller) => {
      const nums = params.decode<number[]>();
      return nums[0]! + nums[1]!;
    });

    root.register("echo", (params, caller) => {
      return params.decode();
    });

    root.register("createCounter", () => {
      const counter = new MethodMap();
      let count = 0;
      counter.register("increment", () => ++count);
      counter.register("getValue", () => count);
      return counter;
    });

    // Handler now created by server
    server = new HttpServer(root, { port: 0 }); // Random port
    await server.start();
    serverUrl = server.url()!;
  });

  afterAll(async () => {
    await server.stop();
  });

  test("basic RPC call", async () => {
    const session = new Session();
    const client = new HttpClient(serverUrl, session);

    const result = await client.call("add", [2, 3]);
    expect(result).toBe(5);
  });

  test("call with different param types", async () => {
    const session = new Session();
    const client = new HttpClient(serverUrl, session);

    const result = await client.call("echo", { message: "hello", value: 42 });
    expect(result).toEqual({ message: "hello", value: 42 });
  });

  test("method not found error", async () => {
    const session = new Session();
    const client = new HttpClient(serverUrl, session);

    try {
      await client.call("nonexistent");
      expect(true).toBe(false); // Should not reach here
    } catch (error) {
      expect(error).toBeInstanceOf(RpcError);
      expect((error as RpcError).code).toBe(-32601);
    }
  });

  test("object reference workflow", async () => {
    const session = new Session();
    const client = new HttpClient(serverUrl, session);

    // Create a counter object
    const result = await client.call("createCounter");
    expect(result).toHaveProperty("$ref");

    const ref = (result as { $ref: string }).$ref;
    expect(session.hasRemoteRef(ref)).toBe(true);

    // Call method on the counter
    const incrementResult = await client.call("increment", undefined, ref);
    expect(incrementResult).toBe(1);

    const valueResult = await client.call("getValue", undefined, ref);
    expect(valueResult).toBe(1);
  });

  test("notification (no response)", async () => {
    const session = new Session();
    const client = new HttpClient(serverUrl, session);

    // Notifications don't return anything
    await client.notify("echo", { msg: "notification" });
    // If we get here without error, it worked
    expect(true).toBe(true);
  });

  test("$rpc protocol methods", async () => {
    const session = new Session();
    const client = new HttpClient(serverUrl, session);

    const result = await client.call("session_id", undefined, "$rpc");
    expect(result).toHaveProperty("session_id");
    expect(typeof (result as { session_id: string }).session_id).toBe("string");
  });
});
