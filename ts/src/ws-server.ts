/**
 * WebSocket server transport using Bun's native WebSocket support
 */

import type { Server, ServerWebSocket } from "bun";
import { nanoid } from "nanoid";
import type { Handler } from "./handler.ts";
import type { Request, Response, MessageSet, Reference, RequestId } from "./types.ts";
import { newRequest, isReference, isNotification, toBatch, batchResponseToMessageSet } from "./types.ts";
import { getCodec, MimeTypeJSON, type Codec } from "./encoding.ts";
import { RpcError } from "./error.ts";

export interface WsServerOptions {
  port?: number;
  hostname?: string;
  mimeType?: string;
}

interface PendingRequest {
  resolve: (value: unknown) => void;
  reject: (error: Error) => void;
}

interface WsData {
  clientId: string;
}

/**
 * WebSocket Server for JSON-RPC 3.0
 * Supports bidirectional communication where both server and clients can initiate calls
 */
export class WsServer {
  private server?: Server;
  private codec: Codec;
  private clients = new Map<string, WsServerClient>();

  constructor(
    private handler: Handler,
    private options: WsServerOptions = {},
  ) {
    const mimeType = options.mimeType ?? MimeTypeJSON;
    this.codec = getCodec(mimeType);
  }

  /**
   * Start the WebSocket server
   */
  async start(): Promise<void> {
    const port = this.options.port ?? 3000;
    const hostname = this.options.hostname ?? "localhost";

    this.server = Bun.serve<WsData>({
      port,
      hostname,
      fetch: (req, server) => {
        const upgraded = server.upgrade(req, {
          data: {
            clientId: nanoid(),
          },
        });
        if (upgraded) {
          return undefined; // Successfully upgraded
        }
        return new Response("WebSocket upgrade failed", { status: 400 });
      },
      websocket: {
        open: (ws) => {
          const client = new WsServerClient(ws, this.handler, this.codec);
          this.clients.set(ws.data.clientId, client);
        },
        message: (ws, message) => {
          const client = this.clients.get(ws.data.clientId);
          if (client) {
            client.handleMessage(message).catch((err) => {
              console.error("Error handling message:", err);
            });
          }
        },
        close: (ws) => {
          const client = this.clients.get(ws.data.clientId);
          if (client) {
            client.handleClose();
            this.clients.delete(ws.data.clientId);
          }
        },
        error: (ws, error) => {
          console.error("WebSocket error:", error);
        },
      },
    });

    console.log(`JSON-RPC WebSocket server listening on ws://${hostname}:${port}`);
  }

  /**
   * Stop the WebSocket server
   */
  async stop(): Promise<void> {
    // Close all client connections
    for (const client of this.clients.values()) {
      client.close();
    }
    this.clients.clear();

    if (this.server) {
      this.server.stop();
      this.server = undefined;
    }
  }

  /**
   * Get server URL
   */
  url(): string | undefined {
    if (!this.server) return undefined;
    return `ws://${this.server.hostname}:${this.server.port}`;
  }

  /**
   * Get connected clients
   */
  getClients(): WsServerClient[] {
    return Array.from(this.clients.values());
  }

  /**
   * Get a specific client by ID
   */
  getClient(clientId: string): WsServerClient | undefined {
    return this.clients.get(clientId);
  }
}

/**
 * Represents a connected WebSocket client
 * Supports bidirectional communication - server can initiate calls to this client
 */
export class WsServerClient {
  private pendingRequests = new Map<RequestId, PendingRequest>();
  private nextId = 1;
  private refPrefix: string;
  private refCounter = 0;
  private closed = false;

  constructor(
    private socket: ServerWebSocket<WsData>,
    private handler: Handler,
    private codec: Codec,
  ) {
    this.refPrefix = nanoid();
  }

  /**
   * Get the client ID
   */
  get id(): string {
    return this.socket.data.clientId;
  }

  /**
   * Call a method on the client and wait for response
   * This allows the server to initiate calls to the client
   */
  async call(method: string, params?: unknown, ref?: string | Reference): Promise<unknown> {
    if (this.closed || this.socket.readyState !== WebSocket.OPEN) {
      throw new Error("Connection closed");
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

    this.sendMessage(msgSet);

    return responsePromise;
  }

  /**
   * Send a notification to the client (no response expected)
   */
  async notify(method: string, params?: unknown, ref?: string | Reference): Promise<void> {
    if (this.closed || this.socket.readyState !== WebSocket.OPEN) {
      throw new Error("Connection closed");
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
   * Check if the client is connected
   */
  isConnected(): boolean {
    return !this.closed && this.socket.readyState === WebSocket.OPEN;
  }

  /**
   * Close the client connection
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

    // Close WebSocket
    try {
      this.socket.close();
    } catch (err) {
      // Ignore close errors
    }
  }

  /**
   * Send a message to the client
   */
  private sendMessage(msgSet: MessageSet): void {
    if (this.closed || this.socket.readyState !== WebSocket.OPEN) {
      throw new Error("Connection closed");
    }

    try {
      const bytes = this.codec.marshalMessages(msgSet);
      this.socket.send(bytes);
    } catch (err) {
      this.close();
      throw err;
    }
  }

  /**
   * Handle incoming message from client
   */
  async handleMessage(message: string | ArrayBuffer): Promise<void> {
    try {
      const bytes = message instanceof ArrayBuffer
        ? new Uint8Array(message)
        : new Uint8Array(Buffer.from(message));

      const msgSet = this.codec.unmarshalMessages(bytes);

      // Determine if all messages are requests or responses
      const allRequests = msgSet.messages.every((msg) => "method" in msg);
      const allResponses = msgSet.messages.every((msg) => "result" in msg || "error" in msg);

      if (allRequests && msgSet.isBatch) {
        // Handle batch requests from client
        await this.handleIncomingBatch(msgSet);
      } else if (allRequests) {
        // Handle single request from client
        await this.handleIncomingRequest(msgSet.messages[0] as Request);
      } else if (allResponses) {
        // Handle responses to our requests
        for (const msg of msgSet.messages) {
          this.handleIncomingResponse(msg as Response);
        }
      }
    } catch (err) {
      console.error("Error processing message from client:", err);
    }
  }

  /**
   * Handle incoming request from client
   */
  private async handleIncomingRequest(req: Request): Promise<void> {
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
   * Handle incoming batch of requests from client
   */
  private async handleIncomingBatch(msgSet: MessageSet): Promise<void> {
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
      pending.resolve(resp.result);
    }
  }

  /**
   * Handle client close
   */
  handleClose(): void {
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
      this.handler.session.addRemoteRef(value.$ref);
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
