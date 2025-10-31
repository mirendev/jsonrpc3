/**
 * JSON-RPC 3.0 TypeScript implementation
 * Main exports
 */

// Types
export type {
  RequestId,
  Reference,
  Request,
  Response,
  Message,
  MessageSet,
  Batch,
  BatchResponse,
} from "./types.ts";

export {
  Version20,
  Version30,
  isReference,
  isRequest,
  isResponse,
  isNotification,
  newRequest,
  newResponse,
  newErrorResponse,
  toRequest,
  toBatch,
  batchResponseToMessageSet,
} from "./types.ts";

// Errors
export type { ErrorObject } from "./error.ts";
export {
  RpcError,
  CodeParseError,
  CodeInvalidRequest,
  CodeMethodNotFound,
  CodeInvalidParams,
  CodeInternalError,
  CodeInvalidReference,
  CodeReferenceNotFound,
  CodeReferenceTypeError,
  parseError,
  invalidRequestError,
  methodNotFoundError,
  invalidParamsError,
  internalError,
  invalidReferenceError,
  referenceNotFoundError,
  referenceTypeError,
} from "./error.ts";

// Encoding
export type { Codec } from "./encoding.ts";
export {
  MimeTypeJSON,
  MimeTypeCBOR,
  MimeTypeCBORCompact,
  JsonCodec,
  getCodec,
} from "./encoding.ts";

// Params
export type { Params } from "./params.ts";
export { ValueParams, newParams, nullParams } from "./params.ts";

// Session
export type { RpcObject, RefInfo } from "./session.ts";
export { Session } from "./session.ts";

// Handler
export { Handler } from "./handler.ts";

// Protocol
export { ProtocolHandler } from "./protocol.ts";

// Helpers
export type { MethodHandler, MethodInfo, RegisterOptions } from "./helpers.ts";
export { MethodMap } from "./helpers.ts";

// HTTP Transport
export type { HttpServerOptions } from "./http-server.ts";
export type { HttpClientOptions } from "./http-client.ts";
export { HttpServer } from "./http-server.ts";
export { HttpClient } from "./http-client.ts";

// Peer Transport
export type { PeerOptions } from "./peer.ts";
export { Peer } from "./peer.ts";

// WebSocket Transport
export type { WsServerOptions } from "./ws-server.ts";
export type { WsClientOptions } from "./ws-client.ts";
export { WsServer, WsServerClient } from "./ws-server.ts";
export { WsClient } from "./ws-client.ts";
