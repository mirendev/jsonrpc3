/**
 * HTTP server transport using Bun.serve
 * Automatically upgrades to WebSocket when upgrade headers are detected
 */

import type { Server, ServerWebSocket } from "bun";
import { Session, type RpcObject } from "./session.ts";
import { Handler } from "./handler.ts";
import { NoOpCaller } from "./types.ts";
import { getCodec, MimeTypeJSON } from "./encoding.ts";
import { toRequest, toBatch, batchResponseToMessageSet } from "./types.ts";
import { parseError, invalidRequestError } from "./error.ts";

export interface HttpServerOptions {
  port?: number;
  hostname?: string;
  mimeType?: string;
}

/**
 * HTTP Server for JSON-RPC 3.0
 */
export class HttpServer {
  private server?: Server;
  private codec;
  private handler: Handler;

  constructor(
    rootObject: RpcObject,
    private options: HttpServerOptions = {},
  ) {
    const mimeType = options.mimeType ?? MimeTypeJSON;
    this.codec = getCodec(mimeType);
    // HTTP requests don't support callbacks, so use NoOpCaller
    const session = new Session();
    this.handler = new Handler(session, rootObject, new NoOpCaller(), [mimeType]);
  }

  /**
   * Start the server
   */
  async start(): Promise<void> {
    const port = this.options.port ?? 3000;
    const hostname = this.options.hostname ?? "localhost";

    this.server = Bun.serve({
      port,
      hostname,
      fetch: async (req, server) => this.handleHttp(req, server),
      websocket: {
        open: (ws) => this.handleWebSocketOpen(ws),
        message: (ws, message) => this.handleWebSocketMessage(ws, message),
        close: (ws) => this.handleWebSocketClose(ws),
      },
    });

    console.log(`JSON-RPC server listening on http://${hostname}:${port}`);
  }

  /**
   * Stop the server
   */
  async stop(): Promise<void> {
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
    return `http://${this.server.hostname}:${this.server.port}`;
  }

  /**
   * Handle HTTP request
   */
  private async handleHttp(req: Request, server: Server): Promise<Response> {
    // Check for WebSocket upgrade
    if (this.isWebSocketUpgrade(req)) {
      // Upgrade to WebSocket
      if (server.upgrade(req)) {
        return undefined!; // Connection was upgraded
      }
      return new Response("WebSocket upgrade failed", { status: 500 });
    }

    // Only accept POST for JSON-RPC
    if (req.method !== "POST") {
      return new Response("Method not allowed", { status: 405 });
    }

    try {
      // Read body
      const bodyBytes = new Uint8Array(await req.arrayBuffer());

      // Decode messages
      let msgSet;
      try {
        msgSet = this.codec.unmarshalMessages(bodyBytes);
      } catch (error) {
        // Parse error
        const err = parseError(error instanceof Error ? error.message : String(error));
        const errorResp = {
          jsonrpc: "3.0",
          error: err.toObject(),
          id: null,
        };
        const respBytes = this.codec.marshal(errorResp);
        return new Response(respBytes, {
          status: 200,
          headers: { "Content-Type": this.codec.mimeType() },
        });
      }

      // Handle based on batch or single
      let responseMsgSet;
      if (msgSet.isBatch) {
        // Batch request
        const batch = toBatch(msgSet);
        const batchResp = await this.handler.handleBatch(batch);
        responseMsgSet = batchResponseToMessageSet(batchResp);
      } else {
        // Single request
        const request = toRequest(msgSet);
        const response = await this.handler.handleRequest(request);
        responseMsgSet = {
          messages: [response],
          isBatch: false,
        };
      }

      // Encode response
      const respBytes = this.codec.marshalMessages(responseMsgSet);

      return new Response(respBytes, {
        status: 200,
        headers: { "Content-Type": this.codec.mimeType() },
      });
    } catch (error) {
      // Internal server error
      return new Response("Internal server error", { status: 500 });
    }
  }

  /**
   * Check if request is a WebSocket upgrade
   */
  private isWebSocketUpgrade(req: Request): boolean {
    const upgrade = req.headers.get("upgrade");
    const connection = req.headers.get("connection");
    return (
      req.method === "GET" &&
      upgrade?.toLowerCase() === "websocket" &&
      connection?.toLowerCase().includes("upgrade") === true
    );
  }

  /**
   * Handle WebSocket connection open
   */
  private handleWebSocketOpen(ws: ServerWebSocket<unknown>): void {
    // Create a new session and handler for this connection
    const session = new Session();
    const handler = new Handler(session, this.handler.rootObject, new NoOpCaller(), [
      this.codec.mimeType(),
    ]);

    // Store session and handler in WebSocket data
    ws.data = { session, handler, codec: this.codec };
  }

  /**
   * Handle WebSocket message
   */
  private async handleWebSocketMessage(
    ws: ServerWebSocket<{ session: Session; handler: Handler; codec: any }>,
    message: string | Buffer,
  ): Promise<void> {
    try {
      const { handler, codec } = ws.data;

      // Convert message to Uint8Array
      const msgBytes =
        typeof message === "string"
          ? new TextEncoder().encode(message)
          : new Uint8Array(message);

      // Decode messages
      let msgSet;
      try {
        msgSet = codec.unmarshalMessages(msgBytes);
      } catch (error) {
        // Parse error - send error response
        const err = parseError(error instanceof Error ? error.message : String(error));
        const errorResp = {
          jsonrpc: "3.0",
          error: err.toObject(),
          id: null,
        };
        const respBytes = codec.marshal(errorResp);
        ws.send(respBytes);
        return;
      }

      // Handle based on batch or single
      let responseMsgSet;
      if (msgSet.isBatch) {
        // Batch request
        const batch = toBatch(msgSet);
        const batchResp = await handler.handleBatch(batch);
        responseMsgSet = batchResponseToMessageSet(batchResp);
      } else {
        // Single request
        const request = toRequest(msgSet);
        const response = await handler.handleRequest(request);
        if (response) {
          responseMsgSet = {
            messages: [response],
            isBatch: false,
          };
        } else {
          // Notification - no response
          return;
        }
      }

      // Encode and send response
      const respBytes = codec.marshalMessages(responseMsgSet);
      ws.send(respBytes);
    } catch (error) {
      console.error("WebSocket message handling error:", error);
    }
  }

  /**
   * Handle WebSocket connection close
   */
  private handleWebSocketClose(
    ws: ServerWebSocket<{ session: Session; handler: Handler; codec: any }>,
  ): void {
    // Dispose all references in the session
    if (ws.data?.session) {
      ws.data.session.disposeAll();
    }
  }
}
