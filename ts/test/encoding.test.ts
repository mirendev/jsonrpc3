import { test, expect, describe } from "bun:test";
import { JsonCodec } from "../src/encoding.ts";
import { newRequest, newResponse, newErrorResponse } from "../src/types.ts";
import { RpcError } from "../src/error.ts";

describe("JsonCodec", () => {
  const codec = new JsonCodec();

  test("encode and decode single request", () => {
    const req = newRequest("add", [1, 2], 1);
    const msgSet = { messages: [req], isBatch: false };

    const encoded = codec.marshalMessages(msgSet);
    const decoded = codec.unmarshalMessages(encoded);

    expect(decoded.isBatch).toBe(false);
    expect(decoded.messages).toHaveLength(1);
    expect(decoded.messages[0]).toMatchObject({
      jsonrpc: "3.0",
      method: "add",
      params: [1, 2],
      id: 1,
    });
  });

  test("encode and decode single response", () => {
    const resp = newResponse(42, 1);
    const msgSet = { messages: [resp], isBatch: false };

    const encoded = codec.marshalMessages(msgSet);
    const decoded = codec.unmarshalMessages(encoded);

    expect(decoded.isBatch).toBe(false);
    expect(decoded.messages).toHaveLength(1);
    expect(decoded.messages[0]).toMatchObject({
      jsonrpc: "3.0",
      result: 42,
      id: 1,
    });
  });

  test("encode and decode batch request", () => {
    const req1 = newRequest("add", [1, 2], 1);
    const req2 = newRequest("subtract", [5, 3], 2);
    const msgSet = { messages: [req1, req2], isBatch: true };

    const encoded = codec.marshalMessages(msgSet);
    const decoded = codec.unmarshalMessages(encoded);

    expect(decoded.isBatch).toBe(true);
    expect(decoded.messages).toHaveLength(2);
    expect(decoded.messages[0]).toMatchObject({
      jsonrpc: "3.0",
      method: "add",
      params: [1, 2],
      id: 1,
    });
    expect(decoded.messages[1]).toMatchObject({
      jsonrpc: "3.0",
      method: "subtract",
      params: [5, 3],
      id: 2,
    });
  });

  test("detect batch from JSON array", () => {
    const json = '[{"jsonrpc":"3.0","method":"test","id":1}]';
    const data = new TextEncoder().encode(json);
    const decoded = codec.unmarshalMessages(data);

    expect(decoded.isBatch).toBe(true);
    expect(decoded.messages).toHaveLength(1);
  });

  test("detect single from JSON object", () => {
    const json = '{"jsonrpc":"3.0","method":"test","id":1}';
    const data = new TextEncoder().encode(json);
    const decoded = codec.unmarshalMessages(data);

    expect(decoded.isBatch).toBe(false);
    expect(decoded.messages).toHaveLength(1);
  });

  test("encode and decode error response", () => {
    const error = new RpcError(-32601, "Method not found");
    const resp = newErrorResponse(error.toObject(), 1);
    const msgSet = { messages: [resp], isBatch: false };

    const encoded = codec.marshalMessages(msgSet);
    const decoded = codec.unmarshalMessages(encoded);

    expect(decoded.isBatch).toBe(false);
    expect(decoded.messages[0]).toMatchObject({
      jsonrpc: "3.0",
      error: {
        code: -32601,
        message: "Method not found",
      },
      id: 1,
    });
  });

  test("encode notification (no id)", () => {
    const notif = newRequest("notify", { message: "hello" });
    const msgSet = { messages: [notif], isBatch: false };

    const encoded = codec.marshalMessages(msgSet);
    const decoded = codec.unmarshalMessages(encoded);

    const msg = decoded.messages[0];
    expect(msg).not.toHaveProperty("id");
    expect(msg).toMatchObject({
      jsonrpc: "3.0",
      method: "notify",
      params: { message: "hello" },
    });
  });
});
