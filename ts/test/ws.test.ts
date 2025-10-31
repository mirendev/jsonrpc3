import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { Session } from "../src/session.ts";
import { Handler } from "../src/handler.ts";
import { MethodMap } from "../src/helpers.ts";
import { WsServer, WsServerClient } from "../src/ws-server.ts";
import { WsClient } from "../src/ws-client.ts";
import { RpcError } from "../src/error.ts";
import { newRequest } from "../src/types.ts";

describe("WebSocket Transport", () => {
  let server: WsServer;
  let serverUrl: string;

  beforeAll(async () => {
    // Create server
    const session = new Session();
    const root = new MethodMap();

    root.register("add", (params) => {
      const nums = params.decode<number[]>();
      return nums[0]! + nums[1]!;
    });

    root.register("echo", (params) => {
      return params.decode();
    });

    root.register("createCounter", () => {
      const counter = new MethodMap();
      let count = 0;
      counter.register("increment", () => ++count);
      counter.register("getValue", () => count);
      return counter;
    });

    const handler = new Handler(session, root);
    server = new WsServer(handler, { port: 0 }); // Random port
    await server.start();
    serverUrl = server.url()!;

    // Wait a bit for server to be ready
    await new Promise((resolve) => setTimeout(resolve, 100));
  });

  afterAll(async () => {
    await server.stop();
  });

  test("connect and disconnect", async () => {
    const session = new Session();
    const client = new WsClient(serverUrl, session);

    await client.connect();
    expect(client.getSession()).toBe(session);

    client.close();
    await client.wait();
  });

  test("basic RPC call", async () => {
    const session = new Session();
    const client = new WsClient(serverUrl, session);
    await client.connect();

    const result = await client.call("add", [2, 3]);
    expect(result).toBe(5);

    client.close();
  });

  test("call with different param types", async () => {
    const session = new Session();
    const client = new WsClient(serverUrl, session);
    await client.connect();

    const result = await client.call("echo", { message: "hello", value: 42 });
    expect(result).toEqual({ message: "hello", value: 42 });

    client.close();
  });

  test("method not found error", async () => {
    const session = new Session();
    const client = new WsClient(serverUrl, session);
    await client.connect();

    try {
      await client.call("nonexistent");
      expect(true).toBe(false); // Should not reach here
    } catch (error) {
      expect(error).toBeInstanceOf(RpcError);
      expect((error as RpcError).code).toBe(-32601);
    }

    client.close();
  });

  test("object reference workflow", async () => {
    const session = new Session();
    const client = new WsClient(serverUrl, session);
    await client.connect();

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

    client.close();
  });

  test("notification (no response)", async () => {
    const session = new Session();
    const client = new WsClient(serverUrl, session);
    await client.connect();

    // Notifications don't return anything
    await client.notify("echo", { msg: "notification" });
    // If we get here without error, it worked
    expect(true).toBe(true);

    client.close();
  });

  test("batch requests", async () => {
    const session = new Session();
    const client = new WsClient(serverUrl, session);
    await client.connect();

    const requests = [
      newRequest("add", [1, 2], 1, undefined),
      newRequest("add", [3, 4], 2, undefined),
      newRequest("add", [5, 6], 3, undefined),
    ];

    const responses = await client.batch(requests);
    expect(responses).toHaveLength(3);

    const results = responses.map((r) => r.result);
    expect(results).toEqual([3, 7, 11]);

    client.close();
  });

  test("concurrent requests", async () => {
    const session = new Session();
    const client = new WsClient(serverUrl, session);
    await client.connect();

    const promises = [
      client.call("add", [1, 1]),
      client.call("add", [2, 2]),
      client.call("add", [3, 3]),
      client.call("add", [4, 4]),
      client.call("add", [5, 5]),
    ];

    const results = await Promise.all(promises);
    expect(results).toEqual([2, 4, 6, 8, 10]);

    client.close();
  });

  test("close handling - calls after close rejected", async () => {
    const session = new Session();
    const client = new WsClient(serverUrl, session);
    await client.connect();

    client.close();
    await client.wait();

    try {
      await client.call("add", [1, 1]);
      expect(true).toBe(false); // Should not reach here
    } catch (error) {
      expect(error).toBeInstanceOf(Error);
      expect((error as Error).message).toContain("Connection closed");
    }
  });

  test("$rpc protocol methods", async () => {
    const session = new Session();
    const client = new WsClient(serverUrl, session);
    await client.connect();

    const result = await client.call("session_id", undefined, "$rpc");
    expect(result).toHaveProperty("session_id");
    expect(typeof (result as { session_id: string }).session_id).toBe("string");

    client.close();
  });

  test("multiple concurrent clients", async () => {
    const session1 = new Session();
    const client1 = new WsClient(serverUrl, session1);
    await client1.connect();

    const session2 = new Session();
    const client2 = new WsClient(serverUrl, session2);
    await client2.connect();

    const session3 = new Session();
    const client3 = new WsClient(serverUrl, session3);
    await client3.connect();

    const result1 = await client1.call("add", [1, 1]);
    const result2 = await client2.call("add", [2, 2]);
    const result3 = await client3.call("add", [3, 3]);

    expect(result1).toBe(2);
    expect(result2).toBe(4);
    expect(result3).toBe(6);

    client1.close();
    client2.close();
    client3.close();
  });
});

describe("WebSocket Bidirectional", () => {
  test("server calls client method", async () => {
    // Create server
    const serverSession = new Session();
    const serverRoot = new MethodMap();
    serverRoot.register("serverEcho", (params) => {
      return { server: params.decode() };
    });
    const handler = new Handler(serverSession, serverRoot);
    const wsServer = new WsServer(handler, { port: 0 });
    await wsServer.start();

    await new Promise((resolve) => setTimeout(resolve, 100));

    // Create client with its own methods
    const clientSession = new Session();
    const clientRoot = new MethodMap();
    clientRoot.register("clientEcho", (params) => {
      return { client: params.decode() };
    });

    const client = new WsClient(wsServer.url()!, clientSession, {
      rootObject: clientRoot,
    });
    await client.connect();

    // Wait for server to register the client
    await new Promise((resolve) => setTimeout(resolve, 100));

    // Client calls server
    const result1 = await client.call("serverEcho", "from-client");
    expect(result1).toEqual({ server: "from-client" });

    // Server calls client
    const clients = wsServer.getClients();
    expect(clients.length).toBe(1);

    const serverClient = clients[0]!;
    const result2 = await serverClient.call("clientEcho", "from-server");
    expect(result2).toEqual({ client: "from-server" });

    client.close();
    await wsServer.stop();
  });

  test("client and server exchange notifications", async () => {
    // Create server
    const serverSession = new Session();
    const serverRoot = new MethodMap();
    const serverMessages: string[] = [];
    serverRoot.register("logMessage", (params) => {
      serverMessages.push(params.decode<string>());
    });
    const handler = new Handler(serverSession, serverRoot);
    const wsServer = new WsServer(handler, { port: 0 });
    await wsServer.start();

    await new Promise((resolve) => setTimeout(resolve, 100));

    // Create client with its own methods
    const clientSession = new Session();
    const clientRoot = new MethodMap();
    const clientMessages: string[] = [];
    clientRoot.register("logMessage", (params) => {
      clientMessages.push(params.decode<string>());
    });

    const client = new WsClient(wsServer.url()!, clientSession, {
      rootObject: clientRoot,
    });
    await client.connect();

    // Wait for server to register the client
    await new Promise((resolve) => setTimeout(resolve, 100));

    // Client sends notification to server
    await client.notify("logMessage", "client-message-1");
    await client.notify("logMessage", "client-message-2");

    // Server sends notification to client
    const clients = wsServer.getClients();
    const serverClient = clients[0]!;
    await serverClient.notify("logMessage", "server-message-1");
    await serverClient.notify("logMessage", "server-message-2");

    // Wait for messages to be processed
    await new Promise((resolve) => setTimeout(resolve, 100));

    expect(serverMessages).toEqual(["client-message-1", "client-message-2"]);
    expect(clientMessages).toEqual(["server-message-1", "server-message-2"]);

    client.close();
    await wsServer.stop();
  });

  test("object references across bidirectional connection", async () => {
    // Create server with counter factory
    const serverSession = new Session();
    const serverRoot = new MethodMap();
    serverRoot.register("createCounter", () => {
      const counter = new MethodMap();
      let count = 0;
      counter.register("increment", () => ++count);
      counter.register("getValue", () => count);
      return counter;
    });
    const handler = new Handler(serverSession, serverRoot);
    const wsServer = new WsServer(handler, { port: 0 });
    await wsServer.start();

    await new Promise((resolve) => setTimeout(resolve, 100));

    // Create client
    const clientSession = new Session();
    const client = new WsClient(wsServer.url()!, clientSession);
    await client.connect();

    // Client creates counter on server
    const result = await client.call("createCounter");
    expect(result).toHaveProperty("$ref");
    const ref = (result as { $ref: string }).$ref;

    // Client calls methods on the counter
    const val1 = await client.call("increment", undefined, ref);
    expect(val1).toBe(1);

    const val2 = await client.call("increment", undefined, ref);
    expect(val2).toBe(2);

    const val3 = await client.call("getValue", undefined, ref);
    expect(val3).toBe(2);

    client.close();
    await wsServer.stop();
  });
});
