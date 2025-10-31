/**
 * Batch request builder with fluent API and automatic batch-local reference management
 */

import type { Request, Response, RequestId } from "./types.ts";

/**
 * Caller interface that supports batch operations
 */
export interface Caller {
  batch(requests: Request[]): Promise<Response[]>;
}

/**
 * BatchBuilder collects requests to be sent as a single batch.
 * It automatically manages batch-local references when chaining method calls.
 */
export class BatchBuilder {
  private requests: Request[] = [];
  private idGen: () => RequestId;

  constructor(idGen: () => RequestId) {
    this.idGen = idGen;
  }

  /**
   * Add a method call to the batch and return a promise that can be chained.
   * @param method - Method name to call
   * @param params - Method parameters
   * @returns BatchBuilderPromise for chaining
   */
  call(method: string, params?: unknown): BatchBuilderPromise {
    const id = this.idGen();
    const request: Request = {
      jsonrpc: "3.0",
      method,
      params,
      id,
    };

    this.requests.push(request);

    return new BatchBuilderPromise(this, this.requests.length - 1);
  }

  /**
   * Get all collected requests
   */
  getRequests(): Request[] {
    return this.requests;
  }
}

/**
 * BatchBuilderPromise represents a pending result in a batch request.
 * It can be used to chain method calls that reference the result of a previous call
 * using batch-local references (\0, \1, \2, etc.).
 */
export class BatchBuilderPromise {
  private builder: BatchBuilder;
  private index: number;

  constructor(builder: BatchBuilder, index: number) {
    this.builder = builder;
    this.index = index;
  }

  /**
   * Chain a method call on the result of the previous call.
   * This automatically uses a batch-local reference (e.g., \0, \1) to reference
   * the result from the promise's position in the batch.
   * @param method - Method name to call
   * @param params - Method parameters
   * @returns New BatchBuilderPromise for further chaining
   */
  call(method: string, params?: unknown): BatchBuilderPromise {
    const id = this.builder["idGen"]();

    // Create batch-local reference: \0, \1, \2, etc.
    const ref = `\\${this.index}`;

    const request: Request = {
      jsonrpc: "3.0",
      ref,
      method,
      params,
      id,
    };

    const requests = this.builder.getRequests();
    requests.push(request);

    return new BatchBuilderPromise(this.builder, requests.length - 1);
  }
}

/**
 * BatchResults contains the responses from a batch request.
 */
export class BatchResults {
  private responses: (Response | null)[];

  constructor(responses: (Response | null)[]) {
    this.responses = responses;
  }

  /**
   * Get the raw response at the specified index.
   * @param index - Index of the response
   * @returns Response or null for notifications
   */
  getResponse(index: number): Response | null {
    if (index < 0 || index >= this.responses.length) {
      throw new Error(
        `Index ${index} out of bounds (batch size: ${this.responses.length})`
      );
    }
    return this.responses[index];
  }

  /**
   * Get and decode the result at the specified index.
   * @param index - Index of the response
   * @returns Decoded result
   * @throws Error if response is null, has an error, or index is out of bounds
   */
  getResult(index: number): unknown {
    const response = this.getResponse(index);

    if (response === null) {
      throw new Error(`No response at index ${index} (notification)`);
    }

    if (response.error) {
      throw new Error(
        `Error at index ${index}: ${response.error.message} (code: ${response.error.code})`
      );
    }

    return response.result;
  }

  /**
   * Get the number of responses in the batch.
   */
  len(): number {
    return this.responses.length;
  }

  /**
   * Check if any response in the batch contains an error.
   */
  hasError(): boolean {
    return this.responses.some((resp) => resp !== null && resp.error !== undefined);
  }

  /**
   * Get all responses (including nulls for notifications).
   */
  getAllResponses(): (Response | null)[] {
    return this.responses;
  }
}

/**
 * ExecuteBatch executes a batch of requests using the builder pattern with automatic
 * batch-local reference management. The provided function receives a BatchBuilder
 * that collects requests. When the function returns, the batch is sent via the
 * provided Caller and responses are returned.
 *
 * @example
 * ```typescript
 * const results = await executeBatch(client, (b) => {
 *   const counter = b.call("createCounter", null);
 *   counter.call("increment", null);  // Uses \0 reference
 *   counter.call("increment", null);  // Uses \0 reference
 *   counter.call("getValue", null);   // Uses \0 reference
 * });
 *
 * const finalValue = results.getResult(3);
 * ```
 *
 * @param caller - Caller implementation that supports batch operations
 * @param fn - Function that builds the batch using BatchBuilder
 * @returns Promise resolving to BatchResults
 */
export async function executeBatch(
  caller: Caller,
  fn: (builder: BatchBuilder) => void | Promise<void>
): Promise<BatchResults> {
  // Create ID generator
  let idCounter = 0;
  const idGen = () => ++idCounter;

  // Create builder
  const builder = new BatchBuilder(idGen);

  // Execute user's batch building function
  await fn(builder);

  // Get collected requests
  const requests = builder.getRequests();

  if (requests.length === 0) {
    return new BatchResults([]);
  }

  // Send batch via caller
  const responses = await caller.batch(requests);

  // Build response map by ID for alignment
  const responseMap = new Map<RequestId, Response>();
  for (const response of responses) {
    if (response.id !== null && response.id !== undefined) {
      responseMap.set(response.id, response);
    }
  }

  // Align responses with requests (notifications get null)
  const alignedResponses: (Response | null)[] = [];
  for (const request of requests) {
    if (request.id === undefined) {
      // Notification - no response
      alignedResponses.push(null);
    } else {
      // Look up response by ID
      const response = responseMap.get(request.id) || null;
      alignedResponses.push(response);
    }
  }

  return new BatchResults(alignedResponses);
}
