import { app } from "../wailsjs/go/models";

export type Section = "console" | "projects" | "settings" | "diagnostics" | "about";

export type CodexState = {
  configured: boolean;
  ready: boolean;
  running: boolean;
  version: string;
  executable_path: string;
  executable_sha256: string;
  browser_mcp_ready: boolean;
  process_mcp_ready: boolean;
  node_path: string;
  browser_path: string;
};

export type MCPRequestState = {
  request_id: string;
  source_message_id: string;
  method: string;
  tool_name: string;
  execution_state: string;
  delivery_state: string;
  terminal: boolean;
  created_at: number;
  updated_at: number;
  elapsed_ms: number;
};

export type DesktopState = {
  generated_at: number;
  runtime: app.RuntimeSnapshot;
  config: app.ConfigSnapshot;
  slack: app.SlackSnapshot;
  codex: CodexState;
  mcp_requests: MCPRequestState[];
  observability: app.ObservabilitySnapshot;
};

export type DiagnosticsState = {
  generated_at: number;
  version: string;
  source_commit: string;
  architecture: string;
  platform: string;
  stage: string;
  config_path: string;
  state_path: string;
  state_schema: string;
  slack: app.SlackSnapshot;
  codex: CodexState;
  mcp_requests: MCPRequestState[];
  components: app.ComponentSnapshot[];
};

export type ProjectForm = {
  id: string | null;
  displayName: string;
  localPath: string;
  remoteURL: string;
};

export type GuiPreferences = {
  logFontSize: number;
  autoFollow: boolean;
};

export type LogEntry = {
  key: string;
  time: number;
  head: string;
  message: string;
  status?: string;
  duration?: number;
};

export function runtimeLogPresentation(level: string, message: string, fieldsJSON: string): { status: string; message: string } {
  let fields: Record<string, unknown> = {};
  try {
    const parsed = JSON.parse(fieldsJSON || "{}");
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) fields = parsed as Record<string, unknown>;
  } catch {
    // A malformed historical field must not hide the runtime log row.
  }
  const state = typeof fields.state === "string" ? fields.state : "";
  const detail = typeof fields.detail === "string" ? fields.detail : "";
  const normalizedLevel = String(level || "").toLowerCase();
  const status = state || (normalizedLevel === "error" || normalizedLevel === "fatal" ? "failed" : normalizedLevel === "warn" ? "degraded" : "");
  return { status, message: detail ? `${message} · ${detail}` : message };
}

export const SECTION_KEY = "cwapi.last-section.v1";
export const PREF_KEY = "cwapi.gui-preferences.v2";
export const NAV: Array<{ id: Section; label: string }> = [
  { id: "console", label: "控制台" },
  { id: "projects", label: "项目" },
  { id: "settings", label: "设置" },
  { id: "diagnostics", label: "诊断" },
  { id: "about", label: "关于" },
];

export const EMPTY_PROJECT: ProjectForm = {
  id: null,
  displayName: "",
  localPath: "",
  remoteURL: "",
};

const DEFAULT_PREFS: GuiPreferences = { logFontSize: 12, autoFollow: true };

export function initialSection(): Section {
  const saved = localStorage.getItem(SECTION_KEY) as Section | null;
  return NAV.some((item) => item.id === saved) ? saved! : "console";
}

export function getGuiPreferences(): GuiPreferences {
  try {
    const parsed = JSON.parse(localStorage.getItem(PREF_KEY) || "{}") as Partial<GuiPreferences>;
    return {
      logFontSize: Math.min(20, Math.max(10, Number(parsed.logFontSize ?? 12))),
      autoFollow: parsed.autoFollow !== false,
    };
  } catch {
    return DEFAULT_PREFS;
  }
}

export function formatTime(timestamp: number): string {
  if (!timestamp) return "—";
  return new Date(timestamp).toLocaleString();
}

export function durationText(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "—";
  if (value < 1000) return `${Math.round(value)} ms`;
  const seconds = value / 1000;
  if (seconds < 60) return `${seconds < 10 ? seconds.toFixed(1) : seconds.toFixed(0)} 秒`;
  return `${Math.floor(seconds / 60)} 分 ${Math.floor(seconds % 60)} 秒`;
}

export function statusTone(status: unknown): "green" | "yellow" | "red" | "gray" {
  const normalized = String(status ?? "").toLowerCase();
  if (["healthy", "ready", "completed", "delivered", "connected", "running"].some((item) => normalized.includes(item))) return "green";
  if (["connecting", "starting", "preparing", "pending", "received", "claimed", "waiting", "retry", "degraded", "attention"].some((item) => normalized.includes(item))) return "yellow";
  if (["failed", "error", "invalid", "timed_out", "blocked", "unavailable"].some((item) => normalized.includes(item))) return "red";
  return "gray";
}

export function statusText(status: unknown): string {
  const normalized = String(status ?? "").toLowerCase();
  const mapping: Record<string, string> = {
    healthy: "运行中",
    ready: "运行中",
    connected: "已连接",
    starting: "启动中",
    running: "运行中",
    received: "已接收",
    claimed: "已领取",
    pending: "等待中",
    preparing_workspace: "准备工作区",
    completed: "已完成",
    stopped: "已停止",
    timed_out: "已超时",
    blocked: "已阻止",
    unavailable: "不可用",
    failed: "失败",
    degraded: "异常",
    attention: "需处理",
    setup_required: "未配置",
  };
  return mapping[normalized] ?? String(status || "未知");
}

export function parseGitHubRepository(remote: string): string {
  const value = remote.trim().replace(/\/+$/, "").replace(/\.git$/i, "");
  const match = value.match(/github\.com(?::|\/)([^/\s:]+)\/([^/\s]+)$/i);
  if (!match) throw new Error("Git 地址必须是有效的 GitHub 仓库地址。");
  return `${match[1]}/${match[2]}`;
}
