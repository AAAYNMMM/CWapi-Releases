import {
  type ReactNode,
  useMemo,
  useRef,
  useState,
} from "react";
import { desktopRevealPath } from "./ipc";
import {
  DiagnosticsMaintenance,
  ProjectsManagement,
  SettingsManagement,
  useGuiPreferences,
} from "./management";
import type {
  BackendHealth,
  DesktopStatus,
  ExecutionLiveSnapshot,
  LiveEvent,
  RuntimeComponent,
  RuntimeStateSnapshot,
  TaskDetail,
  TaskSummary,
  WorkbenchSnapshot,
} from "./ipc";

export type SectionId =
  | "console"
  | "projects"
  | "settings"
  | "diagnostics"
  | "about";

const NAV: Array<{ id: SectionId; label: string }> = [
  { id: "console", label: "控制台" },
  { id: "projects", label: "项目" },
  { id: "settings", label: "设置" },
  { id: "diagnostics", label: "诊断" },
  { id: "about", label: "关于" },
];

const ANSI_RE = /\u001b\[[0-?]*[ -/]*[@-~]/g;

function cleanAnsi(value: string) {
  return value.replace(ANSI_RE, "");
}

function valueText(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "boolean") return value ? "是" : "否";
  if (typeof value === "string" || typeof value === "number") return String(value);
  return JSON.stringify(value);
}

function statusText(value: unknown): string {
  const raw = String(value ?? "").toLowerCase();
  const mapping: Record<string, string> = {
    claimed: "已领取",
    running: "执行中",
    completed: "已完成",
    failed: "失败",
    cancelled: "已取消",
    timed_out: "已超时",
    pending: "等待中",
    result_pending: "结果待发送",
  };
  return mapping[raw] ?? valueText(value);
}

function statusTone(value: unknown, enabled = true): "green" | "yellow" | "red" | "gray" {
  if (!enabled) return "gray";
  const raw = String(value ?? "").toLowerCase();
  if (["failed", "error", "invalid"].some((item) => raw.includes(item))) return "red";
  if (["warning", "retry", "degraded", "attention", "waiting"].some((item) => raw.includes(item))) return "yellow";
  if (["unavailable", "stopped", "missing", "disabled", "not_configured"].some((item) => raw.includes(item))) return "gray";
  if (["healthy", "working", "running", "ready", "available", "verified", "completed", "pass", "true"].some((item) => raw.includes(item))) return "green";
  return raw ? "green" : "gray";
}

function StatusDot({
  label,
  state,
  enabled = true,
  detail,
}: {
  label: string;
  state: unknown;
  enabled?: boolean;
  detail?: string;
}) {
  const tone = statusTone(state, enabled);
  return (
    <div className="dashboard-status-item" title={label}>
      <span className={`status-dot status-dot-${tone}`} aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
}

type StructuredLogEntry = {
  id: string;
  kind: "function" | "step";
  name: string;
  detail: string;
  status: string;
  durationMs: number | null;
  error: string;
};

function durationMsText(value: number | null): string {
  if (value === null || !Number.isFinite(value)) return "—";
  if (value < 1000) return `${Math.max(0, Math.round(value))} ms`;
  const seconds = value / 1000;
  if (seconds < 60) return `${seconds < 10 ? seconds.toFixed(1) : seconds.toFixed(0)} 秒`;
  const minutes = Math.floor(seconds / 60);
  return `${minutes} 分 ${Math.floor(seconds % 60)} 秒`;
}

function structuredLogEntries(snapshot: ExecutionLiveSnapshot | null): StructuredLogEntry[] {
  if (!snapshot) return [];
  return snapshot.events
    .filter((event) => event.type === "function" || event.type === "step")
    .map((event) => {
      const data = event.data ?? {};
      const kind: StructuredLogEntry["kind"] = event.type === "function" ? "function" : "step";
      const functionName = String(data.function ?? event.message ?? "Python 函数");
      const stepId = String(data.step_id ?? "");
      const action = String(data.action ?? "");
      const file = String(data.file ?? "");
      const line = Number(data.line ?? 0);
      const duration = Number(data.duration_ms);
      const detail = kind === "function"
        ? [file && (line ? `${file}:${line}` : file)].filter(Boolean).join(" · ")
        : [action, stepId && `步骤 ${stepId}`].filter(Boolean).join(" · ");
      return {
        id: `structured:${event.id}`,
        kind,
        name: kind === "function" ? functionName : (stepId ? `步骤 ${stepId}` : action || "任务步骤"),
        detail,
        status: String(event.status ?? ""),
        durationMs: Number.isFinite(duration) ? duration : null,
        error: String(data.error ?? ""),
      };
    })
    .slice(-500);
}

function structuredStatusText(status: string): string {
  const mapping: Record<string, string> = {
    running: "运行中",
    claimed: "已领取",
    completed: "已完成",
    failed: "失败",
    cancelled: "已取消",
    timed_out: "已超时",
  };
  return mapping[status.toLowerCase()] ?? statusText(status);
}

function StructuredExecutionLog({ snapshot }: { snapshot: ExecutionLiveSnapshot | null }) {
  const preferences = useGuiPreferences();
  const viewport = useRef<HTMLDivElement | null>(null);
  const [autoFollow, setAutoFollow] = useState(() => preferences.autoFollow);
  const entries = useMemo(() => structuredLogEntries(snapshot), [snapshot]);
  const active = snapshot?.trace_current?.active ?? null;
  const activeStartedAt = snapshot?.trace_current?.updated_at ?? null;
  const activeElapsed = activeStartedAt ? Math.max(0, Date.now() - Date.parse(activeStartedAt)) : null;

  const scrollToLatest = () => {
    const element = viewport.current;
    if (!element) return;
    element.scrollTop = element.scrollHeight;
  };

  const onScroll = () => {
    const element = viewport.current;
    if (!element) return;
    const atBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 40;
    setAutoFollow(atBottom);
  };

  if (autoFollow && viewport.current) requestAnimationFrame(scrollToLatest);

  return (
    <section className="panel structured-log-panel">
      <div className="panel-title-row">
        <div>
          <h2>结构化执行日志</h2>
          <p className="log-panel-subtitle">显示任务步骤、Python 函数状态、耗时和执行结果。</p>
        </div>
        <div className="button-row">
          {!autoFollow && (
            <button type="button" onClick={() => { setAutoFollow(true); requestAnimationFrame(scrollToLatest); }}>
              回到最新
            </button>
          )}
          {snapshot?.task_id && <code className="task-id-chip">{snapshot.task_id}</code>}
        </div>
      </div>

      {active && (
        <div className="structured-active-row">
          <span className="structured-state-dot structured-state-running" aria-hidden="true" />
          <div className="structured-main">
            <strong>{active.function || "Python 函数"}</strong>
            <span>{[active.file, active.line ? `第 ${active.line} 行` : ""].filter(Boolean).join(" · ")}</span>
          </div>
          <span className="structured-status structured-status-running">运行中</span>
          <time>{durationMsText(activeElapsed)}</time>
        </div>
      )}

      <div
        ref={viewport}
        className="structured-log-viewport"
        style={{ fontSize: preferences.logFontSize }}
        onScroll={onScroll}
      >
        {entries.map((entry) => (
          <div className={`structured-log-row structured-${entry.kind}`} key={entry.id}>
            <span className={`structured-state-dot structured-state-${entry.status || "unknown"}`} aria-hidden="true" />
            <div className="structured-main">
              <strong>{entry.name}</strong>
              {entry.detail && <span>{entry.detail}</span>}
              {entry.error && <span className="structured-error">{entry.error}</span>}
            </div>
            <span className={`structured-status structured-status-${entry.status || "unknown"}`}>
              {structuredStatusText(entry.status)}
            </span>
            <time>{durationMsText(entry.durationMs)}</time>
          </div>
        ))}
        {!active && entries.length === 0 && (
          <div className="empty-state">当前没有结构化执行日志。收到任务后，函数和步骤状态会显示在这里。</div>
        )}
      </div>
    </section>
  );
}

function RunnerRuntimeLog({ snapshot }: { snapshot: ExecutionLiveSnapshot | null }) {
  const preferences = useGuiPreferences();
  const viewport = useRef<HTMLDivElement | null>(null);
  const [autoFollow, setAutoFollow] = useState(() => preferences.autoFollow);
  const runner = snapshot?.streams.find((stream) => stream.stream === "runner") ?? null;
  const lines = useMemo(
    () => runner
      ? cleanAnsi(runner.text).split(/\r?\n/).filter((line) => line.trim().length > 0).slice(-1200)
      : [],
    [runner?.id, runner?.text],
  );

  const scrollToLatest = () => {
    const element = viewport.current;
    if (!element) return;
    element.scrollTop = element.scrollHeight;
  };

  const onScroll = () => {
    const element = viewport.current;
    if (!element) return;
    const atBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 40;
    setAutoFollow(atBottom);
  };

  if (autoFollow && viewport.current) requestAnimationFrame(scrollToLatest);

  return (
    <section className="panel runner-log-panel">
      <div className="panel-title-row">
        <div>
          <h2>CWapi Runner 运行日志</h2>
          <p className="log-panel-subtitle">只显示 Runner 自身的启动、通信、重试、警告和运行状态。</p>
        </div>
        <div className="button-row">
          {!autoFollow && (
            <button type="button" onClick={() => { setAutoFollow(true); requestAnimationFrame(scrollToLatest); }}>
              回到最新
            </button>
          )}
          {runner?.path && (
            <button className="icon-button" type="button" title="打开 Runner 日志" aria-label="打开 Runner 日志" onClick={() => void desktopRevealPath(runner.path)}>
              📂
            </button>
          )}
        </div>
      </div>
      <div
        ref={viewport}
        className="runner-log-viewport"
        style={{ fontSize: preferences.logFontSize }}
        onScroll={onScroll}
      >
        {lines.map((line, index) => (
          <div className="runner-log-row" key={`${runner?.id ?? "runner"}:${index}`}>{line}</div>
        ))}
        {lines.length === 0 && <div className="empty-state">当前没有 Runner 运行日志。</div>}
      </div>
    </section>
  );
}

export function LiveExecution({ snapshot }: { snapshot: ExecutionLiveSnapshot | null }) {
  return (
    <div className="console-log-stack">
      <StructuredExecutionLog snapshot={snapshot} />
      <RunnerRuntimeLog snapshot={snapshot} />
    </div>
  );
}

function durationText(receivedAt?: string | null, finishedAt?: string | null): string {
  if (!receivedAt) return "—";
  const start = Date.parse(receivedAt);
  const end = finishedAt ? Date.parse(finishedAt) : Date.now();
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return "—";
  const total = Math.max(0, Math.floor((end - start) / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  if (hours > 0) return `${hours} 小时 ${minutes} 分`;
  if (minutes > 0) return `${minutes} 分 ${seconds} 秒`;
  return `${seconds} 秒`;
}

function ConsolePage({
  desktop,
  runtime,
  health,
  live,
}: {
  desktop: DesktopStatus | null;
  runtime: RuntimeStateSnapshot | null;
  health: BackendHealth | null;
  live: ExecutionLiveSnapshot | null;
}) {
  const codex = runtime?.components?.codex;
  const codexRawState = String(codex?.state ?? "").toLowerCase();
  const codexProcessState = codex?.pid
    ? "running"
    : ["failed", "unhealthy", "error"].includes(codexRawState)
      ? "failed"
      : ["starting", "restarting", "waiting"].includes(codexRawState)
        ? "waiting"
        : "stopped";
  const desktopState = desktop ? "running" : "stopped";
  const backendState = desktop?.backend_running && desktop.backend_pid
    ? "running"
    : desktop?.startup_error
      ? "failed"
      : "stopped";
  const transportState = health?.transport.pid
    ? "running"
    : desktop?.backend_running
      ? "failed"
      : "stopped";

  return (
    <div className="page-stack dashboard-page">
      <section className="panel">
        <h2>组件运行状态</h2>
        <div className="dashboard-status-grid">
          <StatusDot label="CWapi 桌面程序" state={desktopState} />
          <StatusDot label="Python Runner" state={backendState} />
          <StatusDot label="Go Transport" state={transportState} />
          <StatusDot label="Codex app-server" state={codexProcessState} />
        </div>
      </section>
      <LiveExecution snapshot={live} />
    </div>
  );
}

function PathRow({ label, path }: { label: string; path: unknown }) {
  if (typeof path !== "string" || !path) return null;
  return (
    <div className="path-row">
      <span>{label}</span>
      <code>{path}</code>
      <button
        className="icon-button"
        type="button"
        title="打开所在位置"
        aria-label={`打开${label}所在位置`}
        onClick={() => void desktopRevealPath(path)}
      >
        📂
      </button>
    </div>
  );
}


function AboutPage({ desktop, workbench }: { desktop: DesktopStatus | null; workbench: WorkbenchSnapshot | null }) {
  const paths = workbench?.environment.paths ?? {};
  const build = (workbench?.other?.runtime_build ?? {}) as Record<string, unknown>;
  const copySystemInfo = async () => {
    const tools = workbench?.environment.tools ?? {};
    const text = [
      `CWapi ${String(build.cwapi_version ?? "1.5.1")}`,
      `系统：${String((tools.platform as Record<string, unknown> | undefined)?.system ?? "Windows")}`,
      `Python：${String((tools.python as Record<string, unknown> | undefined)?.version ?? "")}`,
      `Git：${String((tools.git as Record<string, unknown> | undefined)?.version ?? "")}`,
      `Node.js：${String((tools.node as Record<string, unknown> | undefined)?.version ?? "")}`,
    ].join("\n");
    await navigator.clipboard.writeText(text);
  };
  return (
    <div className="page-stack">
      <section className="panel about-card">
        <div className="brand-mark brand-mark-large">CW</div>
        <div>
          <h2>CWapi</h2>
          <p>版本 {String(build.cwapi_version ?? "1.5.1")}</p>
        </div>
      </section>
      <section className="panel">
        <h2>位置</h2>
        <PathRow label="程序目录" path={desktop?.app_root} />
        <PathRow label="数据目录" path={desktop?.data_root} />
        <PathRow label="日志目录" path={paths.logs} />
        <PathRow label="结果目录" path={paths.results} />
      </section>
      <button className="primary-button copy-system-info" type="button" onClick={() => void copySystemInfo()}>
        复制系统信息
      </button>
    </div>
  );
}

function PageHeader({ section }: { section: SectionId }) {
  const item = NAV.find((entry) => entry.id === section) ?? NAV[0];
  return (
    <header className="workspace-header simplified-header">
      <h1>{item.label}</h1>
    </header>
  );
}

export function Workbench({
  desktop,
  section,
  onSection,
  runtime,
  health,
  tasks,
  workbench,
  live,
  selectedTaskId,
}: {
  desktop: DesktopStatus | null;
  section: SectionId;
  onSection: (section: SectionId) => void;
  runtime: RuntimeStateSnapshot | null;
  health: BackendHealth | null;
  tasks: TaskSummary[];
  workbench: WorkbenchSnapshot | null;
  live: ExecutionLiveSnapshot | null;
  selectedTaskId: string | null;
  taskDetail: TaskDetail | null;
  taskLive: ExecutionLiveSnapshot | null;
  onSelectTask: (id: string) => void;
  onCancelTask: (id: string) => void;
}) {
  let page: ReactNode;
  switch (section) {
    case "projects":
      page = <ProjectsManagement projects={workbench?.projects ?? []} />;
      break;
    case "settings":
      page = <SettingsManagement />;
      break;
    case "diagnostics":
      page = <DiagnosticsMaintenance selectedTaskId={selectedTaskId} onNavigate={onSection} />;
      break;
    case "about":
      page = <AboutPage desktop={desktop} workbench={workbench} />;
      break;
    default:
      page = <ConsolePage desktop={desktop} runtime={runtime} health={health} live={live} />;
  }
  if (desktop && !desktop.backend_running && section !== "about" && section !== "console") {
    page = (
      <section className="panel">
        <h2>后台服务未启动</h2>
        <p className="error-text">{desktop.startup_error || "CWapi 后台服务当前不可用。"}</p>
        <p>请关闭当前窗口并重新启动 CWapi；如果仍然出现此提示，可在“关于”页面复制系统信息。</p>
      </section>
    );
  }
  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand-block">
          <div className="brand-mark">CW</div>
          <div><strong>CWapi</strong></div>
        </div>
        <nav aria-label="CWapi 功能区">
          {NAV.map((item) => (
            <button
              className={`nav-item ${section === item.id ? "nav-item-active" : ""}`}
              type="button"
              key={item.id}
              onClick={() => onSection(item.id)}
            >
              <span>{item.label}</span>
            </button>
          ))}
        </nav>
      </aside>
      <section className="workspace">
        <PageHeader section={section} />
        <div className="page-content">{page}</div>
      </section>
    </main>
  );
}
