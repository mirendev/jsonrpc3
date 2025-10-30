/**
 * Parameters handling for JSON-RPC requests
 */

/**
 * Params interface for decoding request parameters
 */
export interface Params {
  /**
   * Decode parameters to a typed value
   */
  decode<T = unknown>(): T;

  /**
   * Get raw parameter value
   */
  raw(): unknown;
}

/**
 * Params implementation that wraps a decoded value
 */
export class ValueParams implements Params {
  constructor(private value: unknown) {}

  decode<T = unknown>(): T {
    return this.value as T;
  }

  raw(): unknown {
    return this.value;
  }
}

/**
 * Create Params from a value
 */
export function newParams(value: unknown): Params {
  return new ValueParams(value);
}

/**
 * Null params for methods with no parameters
 */
export const nullParams: Params = new ValueParams(null);
