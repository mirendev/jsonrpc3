/**
 * Core JSON-RPC 3.0 types
 */

import type { ErrorObject } from "./error.ts";

// Version constants
export const Version20 = "2.0";
export const Version30 = "3.0";

/**
 * Request ID can be string, number, or null
 */
export type RequestId = string | number | null;

/**
 * LocalReference represents an object reference in wire format
 */
export interface LocalReference {
  $ref: string;
}

/**
 * Check if a value is a LocalReference
 */
export function isLocalReference(value: unknown): value is LocalReference {
  return (
    typeof value === "object" &&
    value !== null &&
    "$ref" in value &&
    typeof value.$ref === "string" &&
    Object.keys(value).length === 1
  );
}

/**
 * JSON-RPC Request
 */
export interface Request {
  jsonrpc: string;
  ref?: string;
  method: string;
  params?: unknown;
  id?: RequestId;
}

/**
 * JSON-RPC Response
 */
export interface Response {
  jsonrpc: string;
  result?: unknown;
  error?: ErrorObject;
  id: RequestId;
}

/**
 * Message is a discriminated union of Request or Response
 */
export type Message = Request | Response;

/**
 * Type guards for Message discrimination
 */
export function isRequest(msg: Message): msg is Request {
  return "method" in msg;
}

export function isResponse(msg: Message): msg is Response {
  return "result" in msg || "error" in msg;
}

/**
 * Check if request is a notification (no id)
 */
export function isNotification(req: Request): boolean {
  return req.id === undefined;
}

/**
 * MessageSet represents one or more messages with batch information
 */
export interface MessageSet {
  messages: Message[];
  isBatch: boolean;
}

/**
 * Batch is an array of requests
 */
export type Batch = Request[];

/**
 * BatchResponse is an array of responses
 */
export type BatchResponse = Response[];

/**
 * Convert MessageSet to a single Request (error if batch or response)
 */
export function toRequest(msgSet: MessageSet): Request {
  if (msgSet.isBatch) {
    throw new Error("Cannot convert batch to single request");
  }
  if (msgSet.messages.length !== 1) {
    throw new Error("MessageSet must have exactly one message");
  }
  const msg = msgSet.messages[0];
  if (!msg) {
    throw new Error("MessageSet has no messages");
  }
  if (!isRequest(msg)) {
    throw new Error("Message is not a request");
  }
  return msg;
}

/**
 * Convert MessageSet to a Batch (error if not batch or contains responses)
 */
export function toBatch(msgSet: MessageSet): Batch {
  if (!msgSet.isBatch) {
    throw new Error("MessageSet is not a batch");
  }
  const batch: Batch = [];
  for (const msg of msgSet.messages) {
    if (!isRequest(msg)) {
      throw new Error("Batch contains non-request message");
    }
    batch.push(msg);
  }
  return batch;
}

/**
 * Convert BatchResponse to MessageSet
 */
export function batchResponseToMessageSet(batch: BatchResponse): MessageSet {
  return {
    messages: batch,
    isBatch: batch.length !== 1,
  };
}

/**
 * Create a new Request
 */
export function newRequest(
  method: string,
  params?: unknown,
  id?: RequestId,
  ref?: string,
): Request {
  const req: Request = {
    jsonrpc: Version30,
    method,
  };
  if (ref !== undefined) {
    req.ref = ref;
  }
  if (params !== undefined) {
    req.params = params;
  }
  if (id !== undefined) {
    req.id = id;
  }
  return req;
}

/**
 * Create a new Response
 */
export function newResponse(
  result: unknown,
  id: RequestId,
  jsonrpc: string = Version30,
): Response {
  return {
    jsonrpc,
    result,
    id,
  };
}

/**
 * Create a new Error Response
 */
export function newErrorResponse(
  error: ErrorObject,
  id: RequestId,
  jsonrpc: string = Version30,
): Response {
  return {
    jsonrpc,
    error,
    id,
  };
}
