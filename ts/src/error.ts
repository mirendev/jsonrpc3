/**
 * JSON-RPC error codes and error class
 */

// JSON-RPC 2.0 standard error codes
export const CodeParseError = -32700;
export const CodeInvalidRequest = -32600;
export const CodeMethodNotFound = -32601;
export const CodeInvalidParams = -32602;
export const CodeInternalError = -32603;

// JSON-RPC 3.0 reference error codes
export const CodeInvalidReference = -32001;
export const CodeReferenceNotFound = -32002;
export const CodeReferenceTypeError = -32003;

/**
 * JSON-RPC Error structure
 */
export interface ErrorObject {
  code: number;
  message: string;
  data?: unknown;
}

/**
 * RPC Error class that can be thrown or returned
 */
export class RpcError extends Error {
  public readonly code: number;
  public readonly data?: unknown;

  constructor(code: number, message: string, data?: unknown) {
    super(message);
    this.name = "RpcError";
    this.code = code;
    this.data = data;
  }

  /**
   * Convert to wire format
   */
  toObject(): ErrorObject {
    const obj: ErrorObject = {
      code: this.code,
      message: this.message,
    };
    if (this.data !== undefined) {
      obj.data = this.data;
    }
    return obj;
  }

  /**
   * Create from wire format
   */
  static fromObject(obj: ErrorObject): RpcError {
    return new RpcError(obj.code, obj.message, obj.data);
  }
}

/**
 * Error constructor functions matching Go implementation
 */

export function parseError(message?: string): RpcError {
  return new RpcError(
    CodeParseError,
    message ?? "Parse error",
  );
}

export function invalidRequestError(message?: string): RpcError {
  return new RpcError(
    CodeInvalidRequest,
    message ?? "Invalid Request",
  );
}

export function methodNotFoundError(method: string): RpcError {
  return new RpcError(
    CodeMethodNotFound,
    `Method not found: ${method}`,
  );
}

export function invalidParamsError(message: string): RpcError {
  return new RpcError(
    CodeInvalidParams,
    `Invalid params: ${message}`,
  );
}

export function internalError(message: string): RpcError {
  return new RpcError(
    CodeInternalError,
    `Internal error: ${message}`,
  );
}

export function invalidReferenceError(ref: string, message: string): RpcError {
  return new RpcError(
    CodeInvalidReference,
    `Invalid reference: ${message}`,
    ref,
  );
}

export function referenceNotFoundError(ref: string): RpcError {
  return new RpcError(
    CodeReferenceNotFound,
    `Reference not found: ${ref}`,
    ref,
  );
}

export function referenceTypeError(ref: string, message: string): RpcError {
  return new RpcError(
    CodeReferenceTypeError,
    `Reference type error: ${message}`,
    ref,
  );
}
