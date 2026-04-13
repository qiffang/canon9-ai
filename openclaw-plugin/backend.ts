import type { APIResponse, MemoryStats } from "./types.js";

/**
 * Engram9Backend — the abstraction that tools and hooks call through.
 * Maps to engram9's high-level API: remember/recall/compile/status.
 */
export interface Engram9Backend {
  remember(text: string, context?: Record<string, string>): Promise<APIResponse>;
  recall(question: string, context?: Record<string, string>): Promise<APIResponse>;
  compile(): Promise<APIResponse>;
  status(): Promise<MemoryStats>;
}
