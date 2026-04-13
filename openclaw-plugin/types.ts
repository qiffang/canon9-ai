export interface PluginConfig {
  apiUrl?: string;
  apiKey?: string;
  agentName?: string;
  maxIngestBytes?: number;
}

// engram9 API request/response types

export interface RememberRequest {
  text: string;
  context?: Record<string, string>;
}

export interface RecallRequest {
  question: string;
  context?: Record<string, string>;
}

export interface APIResponse {
  result?: string;
  error?: string;
}

export interface MemoryStats {
  event_count: number;
  uncompiled_count: number;
  wiki_page_count: number;
  archived_page_count: number;
}
