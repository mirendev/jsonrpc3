/**
 * Tests for batch request builder API
 */

import { describe, it, expect, beforeEach } from "bun:test";
import {
  BatchBuilder,
  BatchBuilderPromise,
  BatchResults,
  executeBatch,
  type Caller,
} from "../src/batch.ts";
import type { Request, Response } from "../src/types.ts";

// Mock caller for testing
class MockCaller implements Caller {
  private responses: Response[] = [];

  setResponses(responses: Response[]) {
    this.responses = responses;
  }

  async batch(requests: Request[]): Promise<Response[]> {
    // Return pre-configured responses
    return this.responses;
  }
}

describe("BatchBuilder", () => {
  it("should create a simple batch with one call", () => {
    let idCounter = 0;
    const builder = new BatchBuilder(() => ++idCounter);

    builder.call("testMethod", { value: 42 });

    const requests = builder.getRequests();
    expect(requests).toHaveLength(1);
    expect(requests[0].method).toBe("testMethod");
    expect(requests[0].params).toEqual({ value: 42 });
    expect(requests[0].id).toBe(1);
  });

  it("should create a batch with multiple independent calls", () => {
    let idCounter = 0;
    const builder = new BatchBuilder(() => ++idCounter);

    builder.call("method1", { a: 1 });
    builder.call("method2", { b: 2 });
    builder.call("method3", { c: 3 });

    const requests = builder.getRequests();
    expect(requests).toHaveLength(3);
    expect(requests[0].method).toBe("method1");
    expect(requests[1].method).toBe("method2");
    expect(requests[2].method).toBe("method3");
    expect(requests[0].id).toBe(1);
    expect(requests[1].id).toBe(2);
    expect(requests[2].id).toBe(3);
  });

  it("should create chained calls with batch-local references", () => {
    let idCounter = 0;
    const builder = new BatchBuilder(() => ++idCounter);

    const promise1 = builder.call("createCounter", null);
    promise1.call("increment", null);
    promise1.call("getValue", null);

    const requests = builder.getRequests();
    expect(requests).toHaveLength(3);

    // First request has no ref
    expect(requests[0].method).toBe("createCounter");
    expect(requests[0].ref).toBeUndefined();

    // Second request references first (\0)
    expect(requests[1].method).toBe("increment");
    expect(requests[1].ref).toBe("\\0");

    // Third request also references first (\0)
    expect(requests[2].method).toBe("getValue");
    expect(requests[2].ref).toBe("\\0");
  });

  it("should create nested chained calls", () => {
    let idCounter = 0;
    const builder = new BatchBuilder(() => ++idCounter);

    const p1 = builder.call("createCounter", null);
    const p2 = p1.call("increment", null);
    p2.call("increment", null);

    const requests = builder.getRequests();
    expect(requests).toHaveLength(3);

    // First: createCounter
    expect(requests[0].method).toBe("createCounter");
    expect(requests[0].ref).toBeUndefined();

    // Second: increment on \0 (createCounter result)
    expect(requests[1].method).toBe("increment");
    expect(requests[1].ref).toBe("\\0");

    // Third: increment on \1 (first increment result)
    expect(requests[2].method).toBe("increment");
    expect(requests[2].ref).toBe("\\1");
  });
});

describe("BatchResults", () => {
  const mockResponse1: Response = {
    jsonrpc: "3.0",
    result: 42,
    id: 1,
  };

  const mockResponse2: Response = {
    jsonrpc: "3.0",
    result: "hello",
    id: 2,
  };

  const mockErrorResponse: Response = {
    jsonrpc: "3.0",
    error: {
      code: -32600,
      message: "Invalid Request",
    },
    id: 3,
  };

  it("should get response by index", () => {
    const results = new BatchResults([mockResponse1, mockResponse2]);

    const resp1 = results.getResponse(0);
    expect(resp1).toEqual(mockResponse1);

    const resp2 = results.getResponse(1);
    expect(resp2).toEqual(mockResponse2);
  });

  it("should throw error for out of bounds index", () => {
    const results = new BatchResults([mockResponse1]);

    expect(() => results.getResponse(1)).toThrow("out of bounds");
    expect(() => results.getResponse(-1)).toThrow("out of bounds");
  });

  it("should get result by index", () => {
    const results = new BatchResults([mockResponse1, mockResponse2]);

    expect(results.getResult(0)).toBe(42);
    expect(results.getResult(1)).toBe("hello");
  });

  it("should throw error when getting result with error response", () => {
    const results = new BatchResults([mockErrorResponse]);

    expect(() => results.getResult(0)).toThrow("Invalid Request");
  });

  it("should throw error when getting result from notification (null response)", () => {
    const results = new BatchResults([null, mockResponse1]);

    expect(() => results.getResult(0)).toThrow("No response at index 0");
  });

  it("should return correct length", () => {
    const results = new BatchResults([mockResponse1, mockResponse2]);
    expect(results.len()).toBe(2);

    const empty = new BatchResults([]);
    expect(empty.len()).toBe(0);
  });

  it("should detect errors in responses", () => {
    const noErrors = new BatchResults([mockResponse1, mockResponse2]);
    expect(noErrors.hasError()).toBe(false);

    const withError = new BatchResults([mockResponse1, mockErrorResponse]);
    expect(withError.hasError()).toBe(true);

    const withNull = new BatchResults([null, mockResponse1]);
    expect(withNull.hasError()).toBe(false);
  });

  it("should get all responses including nulls", () => {
    const responses = [null, mockResponse1, mockResponse2];
    const results = new BatchResults(responses);

    expect(results.getAllResponses()).toEqual(responses);
  });
});

describe("executeBatch", () => {
  let mockCaller: MockCaller;

  beforeEach(() => {
    mockCaller = new MockCaller();
  });

  it("should execute a simple batch", async () => {
    // Configure mock responses
    mockCaller.setResponses([
      { jsonrpc: "3.0", result: 10, id: 1 },
      { jsonrpc: "3.0", result: 20, id: 2 },
    ]);

    const results = await executeBatch(mockCaller, (b) => {
      b.call("add", [1, 2, 3, 4]);
      b.call("multiply", [2, 10]);
    });

    expect(results.len()).toBe(2);
    expect(results.getResult(0)).toBe(10);
    expect(results.getResult(1)).toBe(20);
  });

  it("should execute batch with chained calls", async () => {
    // Configure mock responses
    mockCaller.setResponses([
      { jsonrpc: "3.0", result: { $ref: "counter1" }, id: 1 },
      { jsonrpc: "3.0", result: 1, id: 2 },
      { jsonrpc: "3.0", result: 2, id: 3 },
      { jsonrpc: "3.0", result: 2, id: 4 },
    ]);

    const results = await executeBatch(mockCaller, (b) => {
      const counter = b.call("createCounter", null);
      counter.call("increment", null);
      counter.call("increment", null);
      counter.call("getValue", null);
    });

    expect(results.len()).toBe(4);
    expect(results.getResult(0)).toEqual({ $ref: "counter1" });
    expect(results.getResult(1)).toBe(1);
    expect(results.getResult(2)).toBe(2);
    expect(results.getResult(3)).toBe(2);
  });

  it("should handle notifications (no ID)", async () => {
    // Configure mock responses - notifications don't get responses
    mockCaller.setResponses([
      { jsonrpc: "3.0", result: "ok", id: 1 },
      // No response for notification
      { jsonrpc: "3.0", result: "ok", id: 3 },
    ]);

    // Manually create requests including a notification
    let idCounter = 0;
    const builder = new BatchBuilder(() => {
      idCounter++;
      // Return undefined for second call to make it a notification
      return idCounter === 2 ? undefined : idCounter;
    });

    builder.call("method1", null);
    builder.call("notify", null); // This will be a notification
    builder.call("method3", null);

    const requests = builder.getRequests();
    const responses = await mockCaller.batch(requests);

    // Build response map and align
    const responseMap = new Map();
    for (const response of responses) {
      if (response.id !== null && response.id !== undefined) {
        responseMap.set(response.id, response);
      }
    }

    const alignedResponses = [];
    for (const request of requests) {
      if (request.id === undefined) {
        alignedResponses.push(null);
      } else {
        alignedResponses.push(responseMap.get(request.id) || null);
      }
    }

    const results = new BatchResults(alignedResponses);

    expect(results.len()).toBe(3);
    expect(results.getResult(0)).toBe("ok");
    expect(() => results.getResult(1)).toThrow("No response at index 1");
    expect(results.getResult(2)).toBe("ok");
  });

  it("should handle empty batch", async () => {
    const results = await executeBatch(mockCaller, () => {
      // Don't add any calls
    });

    expect(results.len()).toBe(0);
  });

  it("should support async batch building function", async () => {
    mockCaller.setResponses([
      { jsonrpc: "3.0", result: 42, id: 1 },
    ]);

    const results = await executeBatch(mockCaller, async (b) => {
      // Simulate async operation
      await new Promise((resolve) => setTimeout(resolve, 1));
      b.call("test", null);
    });

    expect(results.len()).toBe(1);
    expect(results.getResult(0)).toBe(42);
  });
});
