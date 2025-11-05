/**
 * WebSocket client transport for JSON-RPC 3.0
 * Provides bidirectional communication with pending request tracking
 */

import { nanoid } from "nanoid";
import { Session, type RpcObject } from "./session.ts";
import type { Request, Response, MessageSet, BatchResponse, ReferenceType, RequestId } from "./types.ts";
import { newRequest, isReference, isNotification, toBatch, batchResponseToMessageSet, Reference } from "./types.ts";
import { Handler } from "./handler.ts";
import { getCodec, MimeTypeJSON, type Codec } from "./encoding.ts";
import { RpcError } from "./error.ts";

export interface WsClientOptions {
  mimeType?: string;
  session?: Session;
  rootObject?: RpcObject;
  headers?: Record<string, string>; // Note: Browser WebSocket API doesn't support custom headers
}

interface PendingRequest {
  resolve: (value: unknown) => void;
  reject: (error: Error) => void;
}

/**
 * Promote ReferenceType objects to Reference class instances
 * This allows calling methods directly on returned references
 */
function promoteReferences(value: unknown): unknown {
  if (value === null || value === undefined) {
    return value;
  }

  // If it's a reference, promote to Reference class
  if (isReference(value)) {
    return new Reference(value);
  }

  // Recursively handle arrays
  if (Array.isArray(value)) {
    return value.map(item => promoteReferences(item));
  }

  // Recursively handle plain objects
  if (typeof value === "object") {
    const result: Record<string, unknown> = {};
    for (const [key, val] of Object.entries(value)) {
      result[key] = promoteReferences(val);
    }
    return result;
  }

  return value;
}

/**
 * WebSocket Client for JSON-RPC 3.0
 * Supports bidirectional communication where both client and server can initiate calls
 */
export class WsClient {
  private codec: Codec;
  private session: Session;
  private handler?: Handler;
  private socket?: WebSocket;

  // Request tracking
  private pendingRequests = new Map<RequestId, PendingRequest>();
  private nextId = 1;

  // Reference ID generation
  private refPrefix: string;
  private refCounter = 0;

  // Lifecycle
  private closed = false;
  private connError?: Error;
  private closePromise: Promise<void>;
  private closeResolve!: () => void;

  constructor(
    private url: string,
    session?: Session,
    private options: WsClientOptions = {},
  ) {
    const mimeType = options.mimeType ?? MimeTypeJSON;
    this.codec = getCodec(mimeType);
    this.session = session ?? options.session ?? new Session();
    this.refPrefix = nanoid();

    // Initialize close promise
    this.closePromise = new Promise((resolve) => {
      this.closeResolve = resolve;
    });

    // Setup handler if rootObject provided (for bidirectional support)
    if (options.rootObject) {
      this.handler = new Handler(this.session, options.rootObject, this);
    }
  }

  /**
   * Connect to the WebSocket server
   */
  async connect(): Promise<void> {
    if (this.socket) {
      throw new Error("Already connected");
    }

    return new Promise((resolve, reject) => {
      this.socket = new WebSocket(this.url);
      this.socket.binaryType = "arraybuffer";

      this.socket.onopen = () => {
        resolve();
      };

      this.socket.onerror = (event) => {
        const error = new Error("WebSocket error");
        if (!this.socket || this.socket.readyState === WebSocket.CONNECTING) {
          reject(error);
        }
      };

      this.socket.onmessage = (event) => {
        this.handleMessage(event).catch((err) => {
          console.error("Error handling message:", err);
        });
      };

      this.socket.onclose = () => {
        this.handleClose();
      };
    });
  }

  /**
   * Call a method and wait for response
   */
  async call(method: string, params?: unknown, ref?: string | Reference): Promise<unknown> {
    if (this.closed || this.connError) {
      throw this.connError ?? new Error("Connection closed");
    }
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
      throw new Error("Not connected");
    }

    const id = this.nextId++;
    const refString = typeof ref === "string" ? ref : ref?.$ref;
    const req = newRequest(method, params, id, refString);

    // Create promise for response
    const responsePromise = new Promise<unknown>((resolve, reject) => {
      this.pendingRequests.set(id, {
        resolve: (value) => {
          // Extract result from Response and promote references
          const resp = value as Response;
          resolve(promoteReferences(resp.result));
        },
        reject,
      });
    });

    // Send request
    const msgSet: MessageSet = {
      messages: [req],
      isBatch: false,
    };

    this.sendMessage(msgSet);

    return responsePromise;
  }

  /**
   * Send a notification (no response expected)
   */
  async notify(method: string, params?: unknown, ref?: string | Reference): Promise<void> {
    if (this.closed || this.connError) {
      throw this.connError ?? new Error("Connection closed");
    }
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
      throw new Error("Not connected");
    }

    const refString = typeof ref === "string" ? ref : ref?.$ref;
    const req = newRequest(method, params, undefined, refString);

    const msgSet: MessageSet = {
      messages: [req],
      isBatch: false,
    };

    this.sendMessage(msgSet);
  }

  /**
   * Send a batch of requests
   */
  async batch(requests: Request[]): Promise<BatchResponse> {
    if (this.closed || this.connError) {
      throw this.connError ?? new Error("Connection closed");
    }
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
      throw new Error("Not connected");
    }

    const msgSet: MessageSet = {
      messages: requests,
      isBatch: true,
    };

    // Create promises for all requests with IDs
    const responsePromises: Array<Promise<Response>> = [];

    for (const req of requests) {
      if (req.id !== undefined) {
        const promise = new Promise<Response>((resolve, reject) => {
          this.pendingRequests.set(req.id!, {
            resolve: (value) => resolve(value as Response),
            reject,
          });
        });
        responsePromises.push(promise);
      }
    }

    this.sendMessage(msgSet);

    const responses = await Promise.all(responsePromises);
    return responses;
  }

  /**
   * Register a local object that the remote peer can call
   */
  registerObject(obj: RpcObject, ref?: string): Reference {
    if (!ref) {
      this.refCounter++;
      ref = `${this.refPrefix}-${this.refCounter}`;
    }
    this.session.addLocalRef(ref, obj);
    return new Reference(ref);
  }

  /**
   * Unregister a local object
   */
  unregisterObject(ref: string | ReferenceType): boolean {
    const refString = typeof ref === "string" ? ref : ref.$ref;
    return this.session.removeLocalRef(refString);
  }

  /**
   * Get the session
   */
  getSession(): Session {
    return this.session;
  }

  /**
   * Close the WebSocket connection
   */
  close(): void {
    if (this.closed) {
      return;
    }

    this.closed = true;

    // Reject all pending requests
    for (const [id, pending] of this.pendingRequests.entries()) {
      pending.reject(new Error("Connection closed"));
      this.pendingRequests.delete(id);
    }

    // Dispose all references
    this.session.disposeAll();

    // Close WebSocket
    if (this.socket && this.socket.readyState === WebSocket.OPEN) {
      this.socket.close();
    }

    // Resolve wait promise
    this.closeResolve();
  }

  /**
   * Wait for the connection to close
   */
  async wait(): Promise<void> {
    return this.closePromise;
  }

  /**
   * Send a message over WebSocket
   */
  private sendMessage(msgSet: MessageSet): void {
    if (this.closed || !this.socket || this.socket.readyState !== WebSocket.OPEN) {
      throw new Error("Connection closed");
    }

    try {
      const bytes = this.codec.marshalMessages(msgSet);
      this.socket.send(bytes);
    } catch (err) {
      this.connError = err as Error;
      this.close();
      throw err;
    }
  }

  /**
   * Handle incoming WebSocket message
   */
  private async handleMessage(event: MessageEvent): Promise<void> {
    try {
      const bytes = new Uint8Array(event.data);
      const msgSet = this.codec.unmarshalMessages(bytes);

      // Determine if all messages are requests or responses
      const allRequests = msgSet.messages.every((msg) => "method" in msg);
      const allResponses = msgSet.messages.every((msg) => "result" in msg || "error" in msg);

      if (allRequests && msgSet.isBatch) {
        // Handle batch requests (if we have a handler)
        if (this.handler) {
          await this.handleIncomingBatch(msgSet);
        }
      } else if (allRequests) {
        // Handle single request (if we have a handler)
        if (this.handler) {
          await this.handleIncomingRequest(msgSet.messages[0] as Request);
        }
      } else if (allResponses) {
        // Handle responses
        for (const msg of msgSet.messages) {
          this.handleIncomingResponse(msg as Response);
        }
      }
    } catch (err) {
      console.error("Error processing WebSocket message:", err);
    }
  }

  /**
   * Handle incoming request from server
   */
  private async handleIncomingRequest(req: Request): Promise<void> {
    if (!this.handler) {
      return;
    }

    const resp = await this.handler.handleRequest(req);

    // Send response only if not a notification
    if (!isNotification(req)) {
      const msgSet: MessageSet = {
        messages: [resp],
        isBatch: false,
      };
      try {
        this.sendMessage(msgSet);
      } catch (err) {
        // Ignore write errors for responses
      }
    }
  }

  /**
   * Handle incoming batch of requests
   */
  private async handleIncomingBatch(msgSet: MessageSet): Promise<void> {
    if (!this.handler) {
      return;
    }

    const batch = toBatch(msgSet);
    const responses = await this.handler.handleBatch(batch);

    // Filter out responses to notifications
    const nonNotificationResponses = responses.filter((resp, i) => {
      const req = batch[i];
      return req && !isNotification(req);
    });

    if (nonNotificationResponses.length > 0) {
      const responseMsgSet = batchResponseToMessageSet(nonNotificationResponses);
      try {
        this.sendMessage(responseMsgSet);
      } catch (err) {
        // Ignore write errors for responses
      }
    }
  }

  /**
   * Handle incoming response to our request
   */
  private handleIncomingResponse(resp: Response): void {
    const pending = this.pendingRequests.get(resp.id);
    if (!pending) {
      return;
    }

    this.pendingRequests.delete(resp.id);

    if (resp.error) {
      pending.reject(RpcError.fromObject(resp.error));
    } else {
      // Track remote references
      this.trackRemoteReferences(resp.result);
      // Promote ReferenceType objects to Reference class instances
      resp.result = promoteReferences(resp.result);
      // Resolve with full response for batch, or just result for single calls
      pending.resolve(resp);
    }
  }

  /**
   * Handle WebSocket close
   */
  private handleClose(): void {
    if (!this.closed) {
      this.close();
    }
  }

  /**
   * Track remote references in results
   */
  private trackRemoteReferences(value: unknown): void {
    if (value === null || value === undefined) {
      return;
    }

    if (isReference(value)) {
      this.session.addRemoteRef(value.$ref);
      return;
    }

    if (Array.isArray(value)) {
      for (const item of value) {
        this.trackRemoteReferences(item);
      }
      return;
    }

    if (typeof value === "object") {
      for (const val of Object.values(value)) {
        this.trackRemoteReferences(val);
      }
    }
  }
}
