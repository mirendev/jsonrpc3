/**
 * Request handler and routing
 */

import type { RpcObject, Session } from "./session.ts";
import type { Request, Response, Batch, BatchResponse, LocalReference } from "./types.ts";
import { Version30, newResponse, newErrorResponse, isLocalReference, isNotification } from "./types.ts";
import { ProtocolHandler } from "./protocol.ts";
import { newParams, nullParams } from "./params.ts";
import {
  RpcError,
  internalError,
  invalidReferenceError,
  referenceNotFoundError,
  referenceTypeError,
  methodNotFoundError,
} from "./error.ts";

/**
 * Handler routes requests to objects
 */
export class Handler {
  private protocol: ProtocolHandler;

  constructor(
    private session: Session,
    private rootObject: RpcObject,
    private mimeTypes: string[] = ["application/json"],
  ) {
    this.protocol = new ProtocolHandler(session, mimeTypes);
  }

  /**
   * Handle a single request
   */
  async handleRequest(req: Request): Promise<Response> {
    try {
      // Determine which object to call
      let obj: RpcObject;
      let ref = req.ref;

      if (ref === "$rpc") {
        // Protocol methods
        obj = this.protocol;
      } else if (ref) {
        // Reference to a local object
        const localObj = this.session.getLocalRef(ref);
        if (!localObj) {
          throw referenceNotFoundError(ref);
        }
        obj = localObj;
      } else {
        // Root object
        obj = this.rootObject;
      }

      // Create params
      const params = req.params !== undefined ? newParams(req.params) : nullParams;

      // Call method
      const result = await obj.callMethod(req.method, params);

      // Process result for automatic reference registration
      const processedResult = this.processResult(result);

      // Create response
      const jsonrpc = req.jsonrpc || Version30;
      return newResponse(processedResult, req.id ?? null, jsonrpc);
    } catch (error) {
      // Convert error to RPC error
      let rpcError: RpcError;
      if (error instanceof RpcError) {
        rpcError = error;
      } else if (error instanceof Error) {
        rpcError = internalError(error.message);
      } else {
        rpcError = internalError(String(error));
      }

      // Create error response
      const jsonrpc = req.jsonrpc || Version30;
      return newErrorResponse(rpcError.toObject(), req.id ?? null, jsonrpc);
    }
  }

  /**
   * Handle a batch request
   */
  async handleBatch(batch: Batch): Promise<BatchResponse> {
    const responses: BatchResponse = [];
    const batchResults: (unknown | undefined)[] = [];

    for (let i = 0; i < batch.length; i++) {
      const req = batch[i];
      if (!req) continue;

      try {
        // Resolve batch-local references
        let resolvedRef = req.ref;
        if (resolvedRef && resolvedRef.startsWith("\\")) {
          resolvedRef = this.resolveBatchLocalRef(resolvedRef, i, batchResults);
        }

        // Create modified request with resolved ref
        const resolvedReq: Request = { ...req, ref: resolvedRef };

        // Handle the request
        const resp = await this.handleRequest(resolvedReq);

        // Store result for batch-local reference resolution
        batchResults[i] = resp.result;

        // Only add response if not a notification
        if (!isNotification(req)) {
          responses.push(resp);
        }
      } catch (error) {
        // Store undefined for failed requests
        batchResults[i] = undefined;

        // Only add error response if not a notification
        if (!isNotification(req)) {
          let rpcError: RpcError;
          if (error instanceof RpcError) {
            rpcError = error;
          } else if (error instanceof Error) {
            rpcError = internalError(error.message);
          } else {
            rpcError = internalError(String(error));
          }

          const jsonrpc = req.jsonrpc || Version30;
          responses.push(newErrorResponse(rpcError.toObject(), req.id ?? null, jsonrpc));
        }
      }
    }

    return responses;
  }

  /**
   * Resolve batch-local reference (\0, \1, etc.)
   */
  private resolveBatchLocalRef(ref: string, currentIndex: number, results: (unknown | undefined)[]): string {
    // Parse index from \N format
    const indexStr = ref.slice(1);
    const index = parseInt(indexStr, 10);

    if (isNaN(index)) {
      throw invalidReferenceError(ref, `Invalid batch-local reference format: ${ref}`);
    }

    // Check for forward reference
    if (index >= currentIndex) {
      throw invalidReferenceError(ref, `Forward reference not allowed: ${ref} at index ${currentIndex}`);
    }

    // Check bounds
    if (index < 0 || index >= results.length) {
      throw invalidReferenceError(ref, `Batch-local reference out of bounds: ${ref}`);
    }

    // Get the result
    const result = results[index];
    if (result === undefined) {
      throw invalidReferenceError(ref, `Referenced request failed: index ${index}`);
    }

    // Result must be a LocalReference
    if (!isLocalReference(result)) {
      throw referenceTypeError(ref, `Result at index ${index} is not a reference`);
    }

    return result.$ref;
  }

  /**
   * Process result to handle automatic reference registration
   * For now, this is a simple implementation that checks if the result
   * is an RpcObject and registers it
   */
  private processResult(result: unknown): unknown {
    // If result is null or undefined, return as-is
    if (result === null || result === undefined) {
      return result;
    }

    // If it's already a LocalReference, return as-is
    if (isLocalReference(result)) {
      return result;
    }

    // Check if result implements RpcObject
    if (this.isRpcObject(result)) {
      // Register and return LocalReference
      const ref = this.session.addLocalRef(result);
      return { $ref: ref };
    }

    // For arrays, process each element
    if (Array.isArray(result)) {
      return result.map((item) => this.processResult(item));
    }

    // For plain objects, process each property
    if (typeof result === "object") {
      const processed: Record<string, unknown> = {};
      for (const [key, value] of Object.entries(result)) {
        processed[key] = this.processResult(value);
      }
      return processed;
    }

    // Primitive types return as-is
    return result;
  }

  /**
   * Check if value implements RpcObject interface
   */
  private isRpcObject(value: unknown): value is RpcObject {
    return (
      typeof value === "object" &&
      value !== null &&
      "callMethod" in value &&
      typeof (value as { callMethod: unknown }).callMethod === "function"
    );
  }
}
