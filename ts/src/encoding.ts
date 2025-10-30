/**
 * Encoding/decoding for JSON-RPC messages
 */

import type { Message, MessageSet } from "./types.ts";
import { isRequest, isResponse } from "./types.ts";

// MIME type constants
export const MimeTypeJSON = "application/json";
export const MimeTypeCBOR = "application/cbor";
export const MimeTypeCBORCompact = "application/cbor; format=compact";

/**
 * Codec interface for encoding/decoding messages
 */
export interface Codec {
  /**
   * Marshal messages to bytes
   */
  marshalMessages(msgSet: MessageSet): Uint8Array;

  /**
   * Unmarshal bytes to messages
   */
  unmarshalMessages(data: Uint8Array): MessageSet;

  /**
   * Marshal a single value to bytes
   */
  marshal(value: unknown): Uint8Array;

  /**
   * Unmarshal bytes to a value
   */
  unmarshal(data: Uint8Array): unknown;

  /**
   * Get the MIME type for this codec
   */
  mimeType(): string;
}

/**
 * JSON Codec implementation
 */
export class JsonCodec implements Codec {
  private encoder = new TextEncoder();
  private decoder = new TextDecoder();

  mimeType(): string {
    return MimeTypeJSON;
  }

  marshal(value: unknown): Uint8Array {
    const json = JSON.stringify(value);
    return this.encoder.encode(json);
  }

  unmarshal(data: Uint8Array): unknown {
    const json = this.decoder.decode(data);
    return JSON.parse(json);
  }

  marshalMessages(msgSet: MessageSet): Uint8Array {
    // If it was originally a batch, encode as array
    // If it was a single message, encode as object
    if (msgSet.isBatch) {
      return this.marshal(msgSet.messages);
    } else {
      if (msgSet.messages.length !== 1) {
        throw new Error("Non-batch MessageSet must have exactly one message");
      }
      return this.marshal(msgSet.messages[0]);
    }
  }

  unmarshalMessages(data: Uint8Array): MessageSet {
    // Detect if this is a batch by checking if first non-whitespace char is '['
    const json = this.decoder.decode(data);
    const firstChar = json.trim()[0];
    const isBatch = firstChar === "[";

    const decoded = JSON.parse(json);

    if (isBatch) {
      if (!Array.isArray(decoded)) {
        throw new Error("Expected array for batch");
      }
      const messages: Message[] = [];
      for (const item of decoded) {
        messages.push(this.validateMessage(item));
      }
      return {
        messages,
        isBatch: true,
      };
    } else {
      const msg = this.validateMessage(decoded);
      return {
        messages: [msg],
        isBatch: false,
      };
    }
  }

  private validateMessage(value: unknown): Message {
    if (typeof value !== "object" || value === null) {
      throw new Error("Message must be an object");
    }

    const obj = value as Record<string, unknown>;

    // Check if it has required fields
    if (!("jsonrpc" in obj)) {
      throw new Error("Message missing jsonrpc field");
    }

    // Determine if request or response
    if ("method" in obj) {
      // It's a request
      if (typeof obj.method !== "string") {
        throw new Error("Request method must be a string");
      }
      return obj as Message;
    } else if ("result" in obj || "error" in obj) {
      // It's a response
      if (!("id" in obj)) {
        throw new Error("Response missing id field");
      }
      return obj as Message;
    } else {
      throw new Error("Message must be either a request or response");
    }
  }
}

/**
 * Get codec for MIME type
 */
export function getCodec(mimetype: string): Codec {
  // Normalize mimetype (remove parameters for comparison)
  const baseType = mimetype.split(";")[0]?.trim();

  switch (baseType) {
    case "application/json":
    case "json":
      return new JsonCodec();
    case "application/cbor":
    case "cbor":
      throw new Error("CBOR codec not implemented yet");
    default:
      // Default to JSON
      return new JsonCodec();
  }
}
