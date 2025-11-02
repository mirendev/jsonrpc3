/**
 * HTTP server transport using Bun.serve
 */

import type { Server } from "bun";
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
      fetch: async (req) => this.handleHttp(req),
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
  private async handleHttp(req: Request): Promise<Response> {
    // Only accept POST
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
}
