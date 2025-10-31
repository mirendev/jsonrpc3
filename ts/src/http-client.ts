/**
 * HTTP client transport using fetch
 */

import type { Session } from "./session.ts";
import type { Batch, BatchResponse, Request, Response, Reference } from "./types.ts";
import { newRequest, toRequest, isReference } from "./types.ts";
import { getCodec, MimeTypeJSON } from "./encoding.ts";
import { RpcError } from "./error.ts";

export interface HttpClientOptions {
  mimeType?: string;
}

/**
 * HTTP Client for JSON-RPC 3.0
 */
export class HttpClient {
  private codec;
  private requestId = 1;
  private sessionId?: string;

  constructor(
    private url: string,
    private session: Session,
    private options: HttpClientOptions = {},
  ) {
    const mimeType = options.mimeType ?? MimeTypeJSON;
    this.codec = getCodec(mimeType);
  }

  /**
   * Call a method and wait for response
   */
  async call(method: string, params?: unknown, ref?: string | Reference): Promise<unknown> {
    const id = this.requestId++;
    const refString = typeof ref === "string" ? ref : ref?.$ref;
    const req = newRequest(method, params, id, refString);

    const msgSet = {
      messages: [req],
      isBatch: false,
    };

    const reqBytes = this.codec.marshalMessages(msgSet);

    const headers: Record<string, string> = {
      "Content-Type": this.codec.mimeType(),
    };

    // Include session ID if we have one
    if (this.sessionId) {
      headers["RPC-Session-Id"] = this.sessionId;
    }

    const httpResp = await fetch(this.url, {
      method: "POST",
      headers,
      body: reqBytes,
    });

    if (!httpResp.ok) {
      throw new Error(`HTTP error: ${httpResp.status}`);
    }

    // Extract and store session ID from response
    const respSessionId = httpResp.headers.get("RPC-Session-Id");
    if (respSessionId) {
      this.sessionId = respSessionId;
    }

    const respBytes = new Uint8Array(await httpResp.arrayBuffer());
    const respMsgSet = this.codec.unmarshalMessages(respBytes);

    // Get the single response message
    if (respMsgSet.messages.length !== 1) {
      throw new Error("Expected single response");
    }
    const msg = respMsgSet.messages[0];
    if (!msg || !("result" in msg || "error" in msg)) {
      throw new Error("Invalid response message");
    }
    const resp = msg as Response;

    // Check for error
    if (resp.error) {
      throw RpcError.fromObject(resp.error);
    }

    // Track remote references
    this.trackRemoteReferences(resp.result);

    return resp.result;
  }

  /**
   * Send a notification (no response expected)
   */
  async notify(method: string, params?: unknown, ref?: string | Reference): Promise<void> {
    const refString = typeof ref === "string" ? ref : ref?.$ref;
    const req = newRequest(method, params, undefined, refString);

    const msgSet = {
      messages: [req],
      isBatch: false,
    };

    const reqBytes = this.codec.marshalMessages(msgSet);

    const headers: Record<string, string> = {
      "Content-Type": this.codec.mimeType(),
    };

    // Include session ID if we have one
    if (this.sessionId) {
      headers["RPC-Session-Id"] = this.sessionId;
    }

    const httpResp = await fetch(this.url, {
      method: "POST",
      headers,
      body: reqBytes,
    });

    // Extract and store session ID from response
    const respSessionId = httpResp.headers.get("RPC-Session-Id");
    if (respSessionId) {
      this.sessionId = respSessionId;
    }
  }

  /**
   * Send a batch of requests
   */
  async batch(requests: Request[]): Promise<BatchResponse> {
    const msgSet = {
      messages: requests,
      isBatch: true,
    };

    const reqBytes = this.codec.marshalMessages(msgSet);

    const headers: Record<string, string> = {
      "Content-Type": this.codec.mimeType(),
    };

    // Include session ID if we have one
    if (this.sessionId) {
      headers["RPC-Session-Id"] = this.sessionId;
    }

    const httpResp = await fetch(this.url, {
      method: "POST",
      headers,
      body: reqBytes,
    });

    if (!httpResp.ok) {
      throw new Error(`HTTP error: ${httpResp.status}`);
    }

    // Extract and store session ID from response
    const respSessionId = httpResp.headers.get("RPC-Session-Id");
    if (respSessionId) {
      this.sessionId = respSessionId;
    }

    const respBytes = new Uint8Array(await httpResp.arrayBuffer());
    const respMsgSet = this.codec.unmarshalMessages(respBytes);

    const responses: BatchResponse = [];
    for (const msg of respMsgSet.messages) {
      if ("result" in msg || "error" in msg) {
        responses.push(msg as Response);
        // Track remote references from each result
        if ("result" in msg) {
          this.trackRemoteReferences(msg.result);
        }
      }
    }

    return responses;
  }

  /**
   * Track remote references in results
   */
  private trackRemoteReferences(value: unknown): void {
    if (value === null || value === undefined) {
      return;
    }

    // If it's a Reference, track it
    if (isReference(value)) {
      this.session.addRemoteRef(value.$ref);
      return;
    }

    // Recursively check arrays
    if (Array.isArray(value)) {
      for (const item of value) {
        this.trackRemoteReferences(item);
      }
      return;
    }

    // Recursively check objects
    if (typeof value === "object") {
      for (const val of Object.values(value)) {
        this.trackRemoteReferences(val);
      }
    }
  }
}
