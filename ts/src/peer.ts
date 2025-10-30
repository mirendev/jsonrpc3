/**
 * Peer represents a bidirectional JSON-RPC 3.0 peer over a stream-based transport.
 * Both peers can initiate method calls at any time.
 */

import { nanoid } from "nanoid";
import { Session, type RpcObject } from "./session.ts";
import type { Request, Response, Message, MessageSet, Batch, BatchResponse, Reference, RequestId } from "./types.ts";
import { newRequest, isReference, toBatch, batchResponseToMessageSet } from "./types.ts";
import { Handler } from "./handler.ts";
import { getCodec, MimeTypeJSON, type Codec } from "./encoding.ts";
import { RpcError } from "./error.ts";

export interface PeerOptions {
  mimeType?: string;
  session?: Session;
}

interface PendingRequest {
  resolve: (value: unknown) => void;
  reject: (error: Error) => void;
}

/**
 * Peer for bidirectional JSON-RPC 3.0 communication over streams
 */
export class Peer {
  private codec: Codec;
  private session: Session;
  private handler: Handler;
  private reader: ReadableStreamDefaultReader<Uint8Array>;
  private writer: WritableStreamDefaultWriter<Uint8Array>;

  // Request tracking
  private pendingRequests = new Map<RequestId, PendingRequest>();
  private nextId = 1;

  // Reference ID generation
  private refPrefix: string;
  private refCounter = 0;

  // Lifecycle management
  private abortController: AbortController;
  private readLoopPromise: Promise<void>;
  private closed = false;
  private connError?: Error;

  // Write mutex to ensure serialized writes
  private writeMutex = Promise.resolve();

  constructor(
    readable: ReadableStream<Uint8Array>,
    writable: WritableStream<Uint8Array>,
    rootObject: RpcObject,
    options: PeerOptions = {},
  ) {
    const mimeType = options.mimeType ?? MimeTypeJSON;
    this.codec = getCodec(mimeType);

    this.session = options.session ?? new Session();
    this.handler = new Handler(this.session, rootObject);

    this.reader = readable.getReader();
    this.writer = writable.getWriter();

    this.refPrefix = nanoid();
    this.abortController = new AbortController();

    // Start read loop
    this.readLoopPromise = this.readLoop();
  }

  /**
   * Call a method on the remote peer and wait for response
   */
  async call(method: string, params?: unknown, ref?: string | Reference): Promise<unknown> {
    if (this.closed || this.connError) {
      throw this.connError ?? new Error("Connection closed");
    }

    const id = this.nextId++;
    const refString = typeof ref === "string" ? ref : ref?.$ref;
    const req = newRequest(method, params, id, refString);

    // Create promise for response
    const responsePromise = new Promise<unknown>((resolve, reject) => {
      this.pendingRequests.set(id, { resolve, reject });
    });

    // Send request
    const msgSet: MessageSet = {
      messages: [req],
      isBatch: false,
    };

    await this.writeMessage(msgSet);

    return responsePromise;
  }

  /**
   * Send a notification (no response expected)
   */
  async notify(method: string, params?: unknown, ref?: string | Reference): Promise<void> {
    if (this.closed || this.connError) {
      throw this.connError ?? new Error("Connection closed");
    }

    const refString = typeof ref === "string" ? ref : ref?.$ref;
    const req = newRequest(method, params, undefined, refString);

    const msgSet: MessageSet = {
      messages: [req],
      isBatch: false,
    };

    await this.writeMessage(msgSet);
  }

  /**
   * Send a batch of requests
   */
  async batch(requests: Request[]): Promise<BatchResponse> {
    if (this.closed || this.connError) {
      throw this.connError ?? new Error("Connection closed");
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

    await this.writeMessage(msgSet);

    const responses = await Promise.all(responsePromises);
    return responses;
  }

  /**
   * Register a local object that the remote peer can call
   */
  registerObject(obj: RpcObject, ref?: string): string {
    if (!ref) {
      this.refCounter++;
      ref = `${this.refPrefix}-${this.refCounter}`;
    }
    this.session.addLocalRef(ref, obj);
    return ref;
  }

  /**
   * Unregister a local object
   */
  unregisterObject(ref: string): boolean {
    return this.session.removeLocalRef(ref);
  }

  /**
   * Get the session
   */
  getSession(): Session {
    return this.session;
  }

  /**
   * Close the peer connection
   */
  close(): void {
    if (this.closed) {
      return;
    }

    this.closed = true;
    this.abortController.abort();

    // Reject all pending requests
    for (const [id, pending] of this.pendingRequests.entries()) {
      pending.reject(new Error("Connection closed"));
      this.pendingRequests.delete(id);
    }

    // Dispose all references
    this.session.disposeAll();

    // Close streams
    this.reader.cancel().catch(() => {});
    this.writer.close().catch(() => {});
  }

  /**
   * Wait for the peer to close
   */
  async wait(): Promise<void> {
    return this.readLoopPromise;
  }

  /**
   * Write a message to the stream (with mutex for serialization)
   */
  private async writeMessage(msgSet: MessageSet): Promise<void> {
    // Serialize writes using mutex pattern
    this.writeMutex = this.writeMutex.then(async () => {
      if (this.closed) {
        throw new Error("Connection closed");
      }

      try {
        const bytes = this.codec.marshalMessages(msgSet);
        await this.writer.write(bytes);
      } catch (err) {
        this.connError = err as Error;
        this.close();
        throw err;
      }
    });

    return this.writeMutex;
  }

  /**
   * Read loop - continuously reads messages from the stream
   */
  private async readLoop(): Promise<void> {
    try {
      while (!this.closed) {
        const { value, done } = await this.reader.read();

        if (done) {
          break;
        }

        // Decode message set
        const msgSet = this.codec.unmarshalMessages(value);

        // Determine if all messages are requests or responses
        const allRequests = msgSet.messages.every((msg) => "method" in msg);
        const allResponses = msgSet.messages.every((msg) => "result" in msg || "error" in msg);

        if (allRequests && msgSet.isBatch) {
          // Handle batch requests
          this.handleIncomingBatch(msgSet).catch((err) => {
            console.error("Error handling batch:", err);
          });
        } else if (allRequests) {
          // Handle single request
          this.handleIncomingRequest(msgSet.messages[0] as Request).catch((err) => {
            console.error("Error handling request:", err);
          });
        } else if (allResponses) {
          // Handle responses
          for (const msg of msgSet.messages) {
            this.handleIncomingResponse(msg as Response);
          }
        }
      }
    } catch (err) {
      if (!this.closed) {
        this.connError = err as Error;
      }
    } finally {
      this.close();
    }
  }

  /**
   * Handle an incoming request from the remote peer
   */
  private async handleIncomingRequest(req: Request): Promise<void> {
    const resp = await this.handler.handleRequest(req);

    // Send response if not a notification
    if (resp) {
      const msgSet: MessageSet = {
        messages: [resp],
        isBatch: false,
      };
      await this.writeMessage(msgSet).catch(() => {
        // Ignore write errors for responses
      });
    }
  }

  /**
   * Handle an incoming batch of requests
   */
  private async handleIncomingBatch(msgSet: MessageSet): Promise<void> {
    const batch = toBatch(msgSet);
    const responses = await this.handler.handleBatch(batch);

    if (responses.length > 0) {
      const responseMsgSet = batchResponseToMessageSet(responses);
      await this.writeMessage(responseMsgSet).catch(() => {
        // Ignore write errors for responses
      });
    }
  }

  /**
   * Handle an incoming response to our request
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
      pending.resolve(resp.result);
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
