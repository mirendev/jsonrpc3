/**
 * Session management for object references
 */

import { nanoid } from "nanoid";
import type { Params } from "./params.ts";

import type { Caller } from "./types.ts";

/**
 * Object interface - all RPC objects must implement this
 */
export interface RpcObject {
  callMethod(method: string, params: Params, caller: Caller): Promise<unknown>;
}

/**
 * Reference metadata
 */
export interface RefInfo {
  ref: string;
  type?: string;
  direction: "local" | "remote";
  created: Date;
  lastAccessed?: Date;
  metadata?: unknown;
}

/**
 * Session manages object references for a connection
 */
export class Session {
  private _id: string;
  private createdAt: Date;
  private localRefs: Map<string, RpcObject> = new Map();
  private remoteRefs: Map<string, RefInfo> = new Map();
  private refCounter = 0;

  constructor(id?: string) {
    this._id = id ?? nanoid();
    this.createdAt = new Date();
  }

  /**
   * Get session ID
   */
  id(): string {
    return this._id;
  }

  /**
   * Get session creation time
   */
  created(): Date {
    return this.createdAt;
  }

  /**
   * Add a local reference (object we expose to remote peer)
   * If ref is not provided, generates one automatically
   * Returns the ref ID
   */
  addLocalRef(refOrObj: string | RpcObject, obj?: RpcObject): string {
    let ref: string;
    let actualObj: RpcObject;

    if (typeof refOrObj === "string") {
      ref = refOrObj;
      if (!obj) {
        throw new Error("Object is required when ref is a string");
      }
      actualObj = obj;
    } else {
      // Auto-generate ref
      this.refCounter++;
      ref = `ref-${this._id}-${this.refCounter}`;
      actualObj = refOrObj;
    }

    this.localRefs.set(ref, actualObj);
    return ref;
  }

  /**
   * Get a local reference
   */
  getLocalRef(ref: string): RpcObject | undefined {
    return this.localRefs.get(ref);
  }

  /**
   * Check if local reference exists
   */
  hasLocalRef(ref: string): boolean {
    return this.localRefs.has(ref);
  }

  /**
   * Remove a local reference
   * Returns true if removed, false if not found
   */
  removeLocalRef(ref: string): boolean {
    const obj = this.localRefs.get(ref);
    if (!obj) {
      return false;
    }

    // Call dispose/close if available
    this.disposeObject(obj);

    this.localRefs.delete(ref);
    return true;
  }

  /**
   * List all local reference IDs
   */
  listLocalRefs(): string[] {
    return Array.from(this.localRefs.keys());
  }

  /**
   * Get local reference info
   */
  getLocalRefInfo(ref: string): RefInfo | undefined {
    if (!this.localRefs.has(ref)) {
      return undefined;
    }

    return {
      ref,
      direction: "local",
      created: this.createdAt,
    };
  }

  /**
   * Add a remote reference (object the remote peer exposed to us)
   */
  addRemoteRef(ref: string, info?: Partial<Omit<RefInfo, "ref" | "direction" | "created">>): void {
    const refInfo: RefInfo = {
      ref,
      direction: "remote",
      created: new Date(),
      ...info,
    };

    this.remoteRefs.set(ref, refInfo);
  }

  /**
   * Get remote reference info
   */
  getRemoteRefInfo(ref: string): RefInfo | undefined {
    return this.remoteRefs.get(ref);
  }

  /**
   * Check if remote reference exists
   */
  hasRemoteRef(ref: string): boolean {
    return this.remoteRefs.has(ref);
  }

  /**
   * Remove a remote reference
   * Returns true if removed, false if not found
   */
  removeRemoteRef(ref: string): boolean {
    return this.remoteRefs.delete(ref);
  }

  /**
   * List all remote references
   */
  listRemoteRefs(): RefInfo[] {
    return Array.from(this.remoteRefs.values());
  }

  /**
   * Dispose all references (both local and remote)
   * Returns counts of disposed references
   */
  disposeAll(): { localCount: number; remoteCount: number } {
    const localCount = this.localRefs.size;
    const remoteCount = this.remoteRefs.size;

    // Dispose all local objects
    for (const obj of this.localRefs.values()) {
      this.disposeObject(obj);
    }

    this.localRefs.clear();
    this.remoteRefs.clear();

    return { localCount, remoteCount };
  }

  /**
   * Call dispose/close on an object if it has such methods
   */
  private disposeObject(obj: RpcObject): void {
    // Check if object has dispose or close methods
    const disposable = obj as { dispose?: () => void | Promise<void> };
    const closeable = obj as { close?: () => void | Promise<void> };

    if (typeof disposable.dispose === "function") {
      try {
        const result = disposable.dispose();
        if (result instanceof Promise) {
          result.catch(() => {}); // Ignore errors, fire-and-forget
        }
      } catch {
        // Ignore disposal errors
      }
    } else if (typeof closeable.close === "function") {
      try {
        const result = closeable.close();
        if (result instanceof Promise) {
          result.catch(() => {}); // Ignore errors, fire-and-forget
        }
      } catch {
        // Ignore disposal errors
      }
    }
  }
}
