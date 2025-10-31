/**
 * Cross-language integration tests
 * Tests TypeScript clients against Go servers
 */

import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { Session } from "../src/session.ts";
import { HttpClient } from "../src/http-client.ts";
import { WsClient } from "../src/ws-client.ts";
import { Peer } from "../src/peer.ts";
import { MethodMap } from "../src/helpers.ts";
import { newRequest } from "../src/types.ts";

// Start the Go test server
let goServerProcess: ReturnType<typeof Bun.spawn> | null = null;

const HTTP_PORT = 18080;
const WS_PORT = 18081;
const PEER_PORT = 18082;

// Helper to wait for a port to be ready
async function waitForPort(port: number, maxAttempts = 20, delayMs = 100): Promise<void> {
  for (let i = 0; i < maxAttempts; i++) {
    try {
      const conn = await Bun.connect({
        hostname: "localhost",
        port,
        socket: {
          data() {},
          open(socket) {
            socket.end();
          },
        },
      });
      conn.end();
      return;
    } catch (err) {
      if (i === maxAttempts - 1) {
        throw new Error(`Port ${port} not ready after ${maxAttempts} attempts`);
      }
      await new Promise((resolve) => setTimeout(resolve, delayMs));
    }
  }
}

beforeAll(async () => {
  // Start Go server
  const goServerPath = "../go/jsonrpc3/testserver";

  goServerProcess = Bun.spawn(
    [goServerPath, "-http", HTTP_PORT.toString(), "-ws", WS_PORT.toString(), "-peer", PEER_PORT.toString()],
    {
      stdout: "pipe",
      stderr: "pipe",
    }
  );

  // Wait for all server ports to be ready
  await waitForPort(HTTP_PORT);
  await waitForPort(WS_PORT);
  await waitForPort(PEER_PORT);

  console.log("Go test server started on ports HTTP:", HTTP_PORT, "WS:", WS_PORT, "Peer:", PEER_PORT);
});

afterAll(() => {
  if (goServerProcess) {
    goServerProcess.kill();
    console.log("Go test server stopped");
  }
});

describe("Cross-Language HTTP", () => {
  test("TypeScript HTTP client can call Go HTTP server - basic add", async () => {
    const session = new Session();
    const client = new HttpClient(`http://localhost:${HTTP_PORT}`, session);

    const result = await client.call("add", [2, 3]);
    expect(result).toBe(5);
  });

  test("TypeScript HTTP client can call Go HTTP server - echo", async () => {
    const session = new Session();
    const client = new HttpClient(`http://localhost:${HTTP_PORT}`, session);

    const data = { message: "hello", value: 42 };
    const result = await client.call("echo", data);
    expect(result).toEqual(data);
  });

  test("TypeScript HTTP client can use object references from Go server", async () => {
    const session = new Session();
    const client = new HttpClient(`http://localhost:${HTTP_PORT}`, session);

    // Create counter on Go server
    const counter = await client.call("createCounter");
    expect(counter).toHaveProperty("$ref");

    const ref = (counter as { $ref: string }).$ref;

    // Call increment
    const val1 = await client.call("increment", undefined, ref);
    expect(val1).toBe(1);

    const val2 = await client.call("increment", undefined, ref);
    expect(val2).toBe(2);

    // Get value
    const finalValue = await client.call("getValue", undefined, ref);
    expect(finalValue).toBe(2);
  });

  test("TypeScript HTTP client can send notifications to Go server", async () => {
    const session = new Session();
    const client = new HttpClient(`http://localhost:${HTTP_PORT}`, session);

    // Should not throw
    await client.notify("add", [1, 2]);
    expect(true).toBe(true);
  });

  test("TypeScript HTTP client can send batch requests to Go server", async () => {
    const session = new Session();
    const client = new HttpClient(`http://localhost:${HTTP_PORT}`, session);

    const requests = [
      newRequest("add", [1, 1], 1, undefined),
      newRequest("add", [2, 2], 2, undefined),
      newRequest("add", [3, 3], 3, undefined),
    ];

    const responses = await client.batch(requests);
    expect(responses).toHaveLength(3);

    const results = responses.map((r) => r.result);
    expect(results).toEqual([2, 4, 6]);
  });
});

describe("Cross-Language WebSocket", () => {
  test("TypeScript WS client can call Go WS server - basic add", async () => {
    const session = new Session();
    const client = new WsClient(`ws://localhost:${WS_PORT}`, session);
    await client.connect();

    const result = await client.call("add", [5, 7]);
    expect(result).toBe(12);

    client.close();
  });

  test("TypeScript WS client can call Go WS server - echo", async () => {
    const session = new Session();
    const client = new WsClient(`ws://localhost:${WS_PORT}`, session);
    await client.connect();

    const data = { foo: "bar", num: 123 };
    const result = await client.call("echo", data);
    expect(result).toEqual(data);

    client.close();
  });

  test("TypeScript WS client can use object references from Go server", async () => {
    const session = new Session();
    const client = new WsClient(`ws://localhost:${WS_PORT}`, session);
    await client.connect();

    // Create counter on Go server
    const counter = await client.call("createCounter");
    expect(counter).toHaveProperty("$ref");

    const ref = (counter as { $ref: string }).$ref;

    // Call increment multiple times
    const val1 = await client.call("increment", undefined, ref);
    expect(val1).toBe(1);

    const val2 = await client.call("increment", undefined, ref);
    expect(val2).toBe(2);

    const val3 = await client.call("increment", undefined, ref);
    expect(val3).toBe(3);

    client.close();
  });

  test("TypeScript WS client can send concurrent requests to Go server", async () => {
    const session = new Session();
    const client = new WsClient(`ws://localhost:${WS_PORT}`, session);
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

  test("TypeScript WS client can send batch requests to Go server", async () => {
    const session = new Session();
    const client = new WsClient(`ws://localhost:${WS_PORT}`, session);
    await client.connect();

    const requests = [
      newRequest("add", [10, 20], 1, undefined),
      newRequest("add", [30, 40], 2, undefined),
    ];

    const responses = await client.batch(requests);
    expect(responses).toHaveLength(2);

    expect(responses[0]!.result).toBe(30);
    expect(responses[1]!.result).toBe(70);

    client.close();
  });
});

describe("Cross-Language Peer", () => {
  test("TypeScript Peer can call Go Peer - basic add", async () => {
    // Spawn Go server in stdio mode
    const proc = Bun.spawn(
      ["../go/jsonrpc3/testserver", "-stdio"],
      {
        stdin: "pipe",
        stdout: "pipe",
        stderr: "pipe",
      }
    );

    // Wrap stdin in a WritableStream
    const writable = new WritableStream<Uint8Array>({
      write(chunk) {
        proc.stdin.write(chunk);
      },
      close() {
        proc.stdin.end();
      },
    });

    const session = new Session();
    const root = new MethodMap();
    const peer = new Peer(proc.stdout, writable, root, { session });

    // Give peer time to initialize
    await new Promise((resolve) => setTimeout(resolve, 100));

    const result = await peer.call("add", [10, 15]);
    expect(result).toBe(25);

    peer.close();
    proc.kill();
  });

  test("TypeScript Peer can call Go Peer - echo", async () => {
    const proc = Bun.spawn(
      ["../go/jsonrpc3/testserver", "-stdio"],
      {
        stdin: "pipe",
        stdout: "pipe",
        stderr: "pipe",
      }
    );

    const writable = new WritableStream<Uint8Array>({
      write(chunk) {
        proc.stdin.write(chunk);
      },
      close() {
        proc.stdin.end();
      },
    });

    const session = new Session();
    const root = new MethodMap();
    const peer = new Peer(proc.stdout, writable, root, { session });

    await new Promise((resolve) => setTimeout(resolve, 100));

    const data = { test: "data", number: 999 };
    const result = await peer.call("echo", data);
    expect(result).toEqual(data);

    peer.close();
    proc.kill();
  });

  test("TypeScript Peer can use object references from Go server", async () => {
    const proc = Bun.spawn(
      ["../go/jsonrpc3/testserver", "-stdio"],
      {
        stdin: "pipe",
        stdout: "pipe",
        stderr: "pipe",
      }
    );

    const writable = new WritableStream<Uint8Array>({
      write(chunk) {
        proc.stdin.write(chunk);
      },
      close() {
        proc.stdin.end();
      },
    });

    const session = new Session();
    const root = new MethodMap();
    const peer = new Peer(proc.stdout, writable, root, { session });

    await new Promise((resolve) => setTimeout(resolve, 100));

    // Create counter on Go server
    const counter = await peer.call("createCounter");
    expect(counter).toHaveProperty("$ref");

    const ref = (counter as { $ref: string }).$ref;

    // Increment
    const val1 = await peer.call("increment", undefined, ref);
    expect(val1).toBe(1);

    const val2 = await peer.call("increment", undefined, ref);
    expect(val2).toBe(2);

    peer.close();
    proc.kill();
  });
});
