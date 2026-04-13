import type { Engram9Backend } from "./backend.js";
import { ServerBackend } from "./server-backend.js";
import { registerHooks } from "./hooks.js";
import type { PluginConfig, APIResponse, MemoryStats } from "./types.js";

const DEFAULT_API_URL = "http://localhost:9090";

function jsonResult(data: unknown) {
  if (typeof data === "string") return data;
  try {
    return JSON.stringify(data, null, 2);
  } catch {
    return String(data);
  }
}

interface OpenClawPluginApi {
  pluginConfig?: unknown;
  logger: {
    info: (...args: unknown[]) => void;
    error: (...args: unknown[]) => void;
  };
  registerTool: (
    factory: ToolFactory | (() => AnyAgentTool[]),
    opts: { names: string[] }
  ) => void;
  on: (
    hookName: string,
    handler: (...args: unknown[]) => unknown,
    opts?: { priority?: number }
  ) => void;
}

interface ToolContext {
  workspaceDir?: string;
  agentId?: string;
  sessionKey?: string;
  messageChannel?: string;
}

type ToolFactory = (
  ctx: ToolContext
) => AnyAgentTool | AnyAgentTool[] | null | undefined;

interface AnyAgentTool {
  name: string;
  label: string;
  description: string;
  parameters: {
    type: "object";
    properties: Record<string, unknown>;
    required: string[];
  };
  execute: (_id: string, params: unknown) => Promise<unknown>;
}

function buildTools(backend: Engram9Backend): AnyAgentTool[] {
  return [
    {
      name: "memory_remember",
      label: "Remember",
      description:
        "Store information into long-term memory. The server's ingest agent " +
        "will encode it into the wiki knowledge base (semantic, episodic, " +
        "procedural, or prospective memory). Returns confirmation of what " +
        "was stored and which wiki pages were updated.",
      parameters: {
        type: "object",
        properties: {
          text: {
            type: "string",
            description: "The information to remember (max 50000 chars)",
          },
          context: {
            type: "object",
            description:
              "Optional context: {project, task, session_id} to improve encoding quality",
          },
        },
        required: ["text"],
      },
      async execute(_id: string, params: unknown) {
        try {
          const { text, context } = params as {
            text: string;
            context?: Record<string, string>;
          };
          const result = await backend.remember(text, context);
          return jsonResult({ ok: true, data: result });
        } catch (err) {
          return jsonResult({
            ok: false,
            error: err instanceof Error ? err.message : String(err),
          });
        }
      },
    },

    {
      name: "memory_recall",
      label: "Recall",
      description:
        "Recall information from memory by asking a question. The server's " +
        "query agent reconstructs an answer from the wiki knowledge base, " +
        "cross-referencing multiple pages. It may also fix errors and add " +
        "new insights back to the wiki (retrieval-time consolidation).",
      parameters: {
        type: "object",
        properties: {
          question: {
            type: "string",
            description: "The question to answer from memory",
          },
          context: {
            type: "object",
            description:
              "Optional context: {project, task, session_id} to improve retrieval quality",
          },
        },
        required: ["question"],
      },
      async execute(_id: string, params: unknown) {
        try {
          const { question, context } = params as {
            question: string;
            context?: Record<string, string>;
          };
          const result = await backend.recall(question, context);
          return jsonResult({ ok: true, data: result });
        } catch (err) {
          return jsonResult({
            ok: false,
            error: err instanceof Error ? err.message : String(err),
          });
        }
      },
    },

    {
      name: "memory_compile",
      label: "Compile Memory",
      description:
        "Trigger a compile cycle (sleep-time consolidation). The server's " +
        "compile agent distills episodic events into semantic knowledge, " +
        "detects contradictions, rebuilds the wiki index, and archives " +
        "stale pages. Normally runs on a timer, but can be triggered manually.",
      parameters: {
        type: "object",
        properties: {},
        required: [],
      },
      async execute(_id: string, _params: unknown) {
        try {
          const result = await backend.compile();
          return jsonResult({ ok: true, data: result });
        } catch (err) {
          return jsonResult({
            ok: false,
            error: err instanceof Error ? err.message : String(err),
          });
        }
      },
    },

    {
      name: "memory_status",
      label: "Memory Status",
      description:
        "Get memory system statistics: event count, uncompiled events, " +
        "wiki page count, and archived page count.",
      parameters: {
        type: "object",
        properties: {},
        required: [],
      },
      async execute(_id: string, _params: unknown) {
        try {
          const result = await backend.status();
          return jsonResult({ ok: true, data: result });
        } catch (err) {
          return jsonResult({
            ok: false,
            error: err instanceof Error ? err.message : String(err),
          });
        }
      },
    },
  ];
}

const toolNames = [
  "memory_remember",
  "memory_recall",
  "memory_compile",
  "memory_status",
];

const engram9Plugin = {
  id: "engram9",
  name: "Engram9 Memory",
  description:
    "Brain-inspired agent memory — three-timing consolidation model " +
    "(encoding, retrieval, sleep) with semantic/episodic/procedural/prospective memory types.",

  register(api: OpenClawPluginApi) {
    const cfg = (api.pluginConfig ?? {}) as PluginConfig;
    const effectiveApiUrl = cfg.apiUrl ?? DEFAULT_API_URL;
    if (!cfg.apiUrl) {
      api.logger.info(
        `[engram9] apiUrl not configured, using default ${DEFAULT_API_URL}`
      );
    }

    const apiKey = cfg.apiKey ?? "";

    api.logger.info(`[engram9] Connecting to ${effectiveApiUrl}`);

    const backend = new ServerBackend(effectiveApiUrl, apiKey);

    const factory: ToolFactory = () => {
      return buildTools(backend);
    };

    api.registerTool(factory, { names: toolNames });

    const hookAgentId = cfg.agentName ?? "agent";

    registerHooks(api, backend, api.logger, {
      maxIngestBytes: cfg.maxIngestBytes,
      fallbackAgentId: hookAgentId,
    });
  },
};

export default engram9Plugin;
