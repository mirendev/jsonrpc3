/**
 * Protocol handler for $rpc methods
 */

import type { RpcObject } from "./session.ts";
import type { Session, RefInfo } from "./session.ts";
import type { Params } from "./params.ts";
import type { Caller } from "./types.ts";
import { methodNotFoundError, invalidParamsError, referenceNotFoundError } from "./error.ts";

/**
 * Parameters for dispose method
 */
interface DisposeParams {
  ref: string;
}

/**
 * Parameters for ref_info method
 */
interface RefInfoParams {
  ref: string;
}

/**
 * Result for session_id method
 */
interface SessionIDResult {
  session_id: string;
}

/**
 * Result for ref_info and list_refs methods
 */
interface RefInfoResult {
  ref: string;
  type?: string;
  direction: "local" | "remote";
  created: string;
  last_accessed?: string;
  metadata?: unknown;
}

/**
 * Result for dispose_all method
 */
interface DisposeAllResult {
  local_count: number;
  remote_count: number;
}

/**
 * Result for mimetypes method
 */
interface MimeTypesResult {
  mimetypes: string[];
}

/**
 * ProtocolHandler implements built-in $rpc methods
 */
export class ProtocolHandler implements RpcObject {
  constructor(
    private session: Session,
    private mimeTypes: string[],
  ) {}

  async callMethod(method: string, params: Params, _caller: Caller): Promise<unknown> {
    switch (method) {
      case "dispose":
        return this.handleDispose(params);
      case "session_id":
        return this.handleSessionID(params);
      case "list_refs":
        return this.handleListRefs(params);
      case "ref_info":
        return this.handleRefInfo(params);
      case "dispose_all":
        return this.handleDisposeAll(params);
      case "mimetypes":
        return this.handleMimeTypes(params);
      case "capabilities":
        return this.handleCapabilities(params);
      default:
        throw methodNotFoundError(method);
    }
  }

  /**
   * Handle dispose(ref) - remove a reference
   */
  private handleDispose(params: Params): boolean {
    const p = params.decode<DisposeParams>();
    if (!p.ref) {
      throw invalidParamsError("ref parameter is required");
    }

    // Try local first, then remote
    let removed = this.session.removeLocalRef(p.ref);
    if (!removed) {
      removed = this.session.removeRemoteRef(p.ref);
    }

    if (!removed) {
      throw referenceNotFoundError(p.ref);
    }

    return true;
  }

  /**
   * Handle session_id() - get session ID
   */
  private handleSessionID(_params: Params): SessionIDResult {
    return {
      session_id: this.session.id(),
    };
  }

  /**
   * Handle list_refs() - list all references
   */
  private handleListRefs(_params: Params): RefInfoResult[] {
    const results: RefInfoResult[] = [];

    // Add local refs
    for (const ref of this.session.listLocalRefs()) {
      const info = this.session.getLocalRefInfo(ref);
      if (info) {
        results.push(this.convertRefInfo(info));
      }
    }

    // Add remote refs
    for (const info of this.session.listRemoteRefs()) {
      results.push(this.convertRefInfo(info));
    }

    return results;
  }

  /**
   * Handle ref_info(ref) - get reference metadata
   */
  private handleRefInfo(params: Params): RefInfoResult {
    const p = params.decode<RefInfoParams>();
    if (!p.ref) {
      throw invalidParamsError("ref parameter is required");
    }

    // Try local first, then remote
    let info = this.session.getLocalRefInfo(p.ref);
    if (!info) {
      info = this.session.getRemoteRefInfo(p.ref);
    }

    if (!info) {
      throw referenceNotFoundError(p.ref);
    }

    return this.convertRefInfo(info);
  }

  /**
   * Handle dispose_all() - clear all references
   */
  private handleDisposeAll(_params: Params): DisposeAllResult {
    const counts = this.session.disposeAll();
    return {
      local_count: counts.localCount,
      remote_count: counts.remoteCount,
    };
  }

  /**
   * Handle mimetypes() - get supported encodings
   */
  private handleMimeTypes(_params: Params): MimeTypesResult {
    return {
      mimetypes: this.mimeTypes,
    };
  }

  /**
   * Handle capabilities() - get feature list
   */
  private handleCapabilities(_params: Params): string[] {
    const capabilities = [
      "references",
      "batch-local-references",
      "bidirectional-calls",
      "introspection",
    ];

    // Add encoding capabilities based on supported MIME types
    for (const mimeType of this.mimeTypes) {
      if (mimeType.includes("cbor")) {
        if (mimeType.includes("compact")) {
          capabilities.push("cbor-compact-encoding");
        } else {
          capabilities.push("cbor-encoding");
        }
      }
    }

    return capabilities;
  }

  /**
   * Convert RefInfo to wire format
   */
  private convertRefInfo(info: RefInfo): RefInfoResult {
    const result: RefInfoResult = {
      ref: info.ref,
      direction: info.direction,
      created: info.created.toISOString(),
    };

    if (info.type) {
      result.type = info.type;
    }

    if (info.lastAccessed) {
      result.last_accessed = info.lastAccessed.toISOString();
    }

    if (info.metadata !== undefined) {
      result.metadata = info.metadata;
    }

    return result;
  }
}
