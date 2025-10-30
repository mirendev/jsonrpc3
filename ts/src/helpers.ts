/**
 * Helper utilities for creating RPC objects
 */

import type { RpcObject } from "./session.ts";
import type { Params } from "./params.ts";
import { methodNotFoundError } from "./error.ts";

/**
 * Method handler function type
 */
export type MethodHandler = (params: Params) => Promise<unknown> | unknown;

/**
 * MethodMap provides a simple way to create RPC objects
 * by registering method handlers as functions
 */
export class MethodMap implements RpcObject {
  private methods = new Map<string, MethodHandler>();
  public type?: string;

  /**
   * Register a method handler
   */
  register(method: string, handler: MethodHandler): void {
    this.methods.set(method, handler);
  }

  /**
   * Call a method (implements RpcObject interface)
   */
  async callMethod(method: string, params: Params): Promise<unknown> {
    // Handle introspection methods
    if (method === "$methods") {
      return this.getMethods();
    }
    if (method === "$type") {
      return this.getType();
    }

    // Look up user-registered method
    const handler = this.methods.get(method);
    if (!handler) {
      throw methodNotFoundError(method);
    }

    // Call handler
    const result = handler(params);

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
    methods.push("$methods", "$type");
    return methods;
  }

  /**
   * Get type identifier
   */
  private getType(): string {
    return this.type ?? "MethodMap";
  }
}
