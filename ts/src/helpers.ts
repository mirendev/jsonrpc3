/**
 * Helper utilities for creating RPC objects
 */

import type { RpcObject } from "./session.ts";
import type { Params } from "./params.ts";
import { methodNotFoundError, invalidParamsError } from "./error.ts";

/**
 * Method handler function type
 */
export type MethodHandler = (params: Params) => Promise<unknown> | unknown;

/**
 * MethodInfo contains metadata about a registered method for introspection.
 */
export interface MethodInfo {
  name: string;
  description?: string;
  params?: Record<string, string> | string[];
  handler: MethodHandler;
}

/**
 * RegisterOptions provides optional metadata for method registration.
 */
export interface RegisterOptions {
  description?: string;
  params?: Record<string, string> | string[];
}

/**
 * MethodMap provides a simple way to create RPC objects
 * by registering method handlers as functions.
 * It automatically supports the optional $methods, $type, and $method introspection methods.
 */
export class MethodMap implements RpcObject {
  private methods = new Map<string, MethodInfo>();
  public type?: string;

  /**
   * Register a method handler with optional metadata.
   *
   * @param method - The method name
   * @param handler - The method handler function
   * @param options - Optional metadata for introspection (description and params)
   *
   * @example
   * ```typescript
   * methodMap.register("add", addHandler, {
   *   description: "Adds two numbers",
   *   params: { a: "number", b: "number" }
   * });
   *
   * methodMap.register("concat", concatHandler, {
   *   description: "Concatenates strings",
   *   params: ["string", "...string"]
   * });
   * ```
   */
  register(method: string, handler: MethodHandler, options?: RegisterOptions): void {
    const info: MethodInfo = {
      name: method,
      handler,
      ...options,
    };
    this.methods.set(method, info);
  }

  /**
   * Call a method (implements RpcObject interface).
   * Automatically handles the $methods, $type, and $method introspection methods.
   */
  async callMethod(method: string, params: Params): Promise<unknown> {
    // Handle introspection methods
    if (method === "$methods") {
      return this.getMethods();
    }
    if (method === "$type") {
      return this.getType();
    }
    if (method === "$method") {
      return this.getMethodInfo(params);
    }

    // Look up user-registered method
    const info = this.methods.get(method);
    if (!info) {
      throw methodNotFoundError(method);
    }

    // Call handler
    const result = info.handler(params);

    // Handle both sync and async handlers
    if (result instanceof Promise) {
      return await result;
    }
    return result;
  }

  /**
   * Get list of all methods (including introspection)
   */
  private getMethods(): string[] {
    const methods = Array.from(this.methods.keys());
    methods.push("$methods", "$type", "$method");
    return methods;
  }

  /**
   * Get type identifier
   */
  private getType(): string {
    return this.type ?? "MethodMap";
  }

  /**
   * Get detailed information about a specific method.
   * Implements the $method introspection method.
   */
  private getMethodInfo(params: Params): Record<string, unknown> | null {
    const methodName = params.decode<string>();

    // Validate that the parameter is a string
    if (typeof methodName !== "string") {
      throw invalidParamsError("$method expects a string parameter");
    }

    const info = this.methods.get(methodName);
    if (!info) {
      return null; // Return null for non-existent methods
    }

    // Build result object with only non-empty fields
    const result: Record<string, unknown> = {
      name: info.name,
    };

    if (info.description) {
      result.description = info.description;
    }

    if (info.params) {
      result.params = info.params;
    }

    return result;
  }
}
