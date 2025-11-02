/**
 * Helper utilities for creating RPC objects
 */

import type { RpcObject } from "./session.ts";
import type { Params } from "./params.ts";
import type { Caller } from "./types.ts";
import { methodNotFoundError, invalidParamsError } from "./error.ts";

/**
 * Method handler function type
 */
export type MethodHandler = (params: Params, caller: Caller) => Promise<unknown> | unknown;

/**
 * MethodInfo contains metadata about a registered method for introspection.
 */
export interface MethodInfo {
  name: string;
  description?: string;
  params?: Record<string, string> | string[];
  category?: string;
  handler: MethodHandler;
}

/**
 * RegisterOptions provides optional metadata for method registration.
 */
export interface RegisterOptions {
  description?: string;
  params?: Record<string, string> | string[];
  category?: string;
}

/**
 * MethodMap provides a simple way to create RPC objects
 * by registering method handlers as functions.
 * It automatically supports the optional $methods and $type introspection methods.
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
   * Automatically handles the $methods and $type introspection methods.
   */
  async callMethod(method: string, params: Params, caller: Caller): Promise<unknown> {
    // Handle introspection methods
    if (method === "$methods") {
      return this.getMethods();
    }
    if (method === "$type") {
      return this.getType();
    }

    // Look up user-registered method
    const info = this.methods.get(method);
    if (!info) {
      throw methodNotFoundError(method);
    }

    // Call handler
    const result = info.handler(params, caller);

    // Handle both sync and async handlers
    if (result instanceof Promise) {
      return await result;
    }
    return result;
  }

  /**
   * Get detailed information about all methods (including introspection)
   */
  private getMethods(): Array<Record<string, unknown>> {
    const methods: Array<Record<string, unknown>> = [];

    // Add user-registered methods
    for (const info of this.methods.values()) {
      const methodInfo: Record<string, unknown> = {
        name: info.name,
      };

      if (info.description) {
        methodInfo.description = info.description;
      }

      if (info.params) {
        methodInfo.params = info.params;
      }

      if (info.category) {
        methodInfo.category = info.category;
      }

      methods.push(methodInfo);
    }

    // Add introspection methods
    methods.push({ name: "$methods" });
    methods.push({ name: "$type" });

    return methods;
  }

  /**
   * Get type identifier
   */
  private getType(): string {
    return this.type ?? "MethodMap";
  }
}
