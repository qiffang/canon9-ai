import type { Engram9Backend } from "./backend.js";
import type { APIResponse, MemoryStats } from "./types.js";

export class ServerBackend implements Engram9Backend {
  private baseUrl: string;
  private apiKey: string;

  constructor(apiUrl: string, apiKey: string) {
    this.baseUrl = apiUrl.replace(/\/+$/, "");
    this.apiKey = apiKey;
  }

  async remember(
    text: string,
    context?: Record<string, string>
  ): Promise<APIResponse> {
    return this.request<APIResponse>("POST", "/remember", { text, context });
  }

  async recall(
    question: string,
    context?: Record<string, string>
  ): Promise<APIResponse> {
    return this.request<APIResponse>("POST", "/recall", { question, context });
  }

  async compile(): Promise<APIResponse> {
    return this.request<APIResponse>("POST", "/compile", {});
  }

  async status(): Promise<MemoryStats> {
    return this.request<MemoryStats>("GET", "/status");
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<T> {
    const url = this.baseUrl + path;
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    if (this.apiKey) {
      headers["Authorization"] = `Bearer ${this.apiKey}`;
    }

    const resp = await fetch(url, {
      method,
      headers,
      body: body != null ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(30_000),
    });

    if (resp.status === 204) {
      return undefined as T;
    }

    const data = await resp.json();
    if (!resp.ok) {
      throw new Error(
        (data as { error?: string }).error || `HTTP ${resp.status}`
      );
    }
    return data as T;
  }
}
