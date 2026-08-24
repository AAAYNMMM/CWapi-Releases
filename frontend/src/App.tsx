import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  ArrowSync16Regular,
  ChevronRight16Regular,
  Dismiss16Regular,
  PlugConnected16Regular,
  RecordStop16Filled,
  Subtract16Regular,
} from "@fluentui/react-icons";
import { WindowHide, WindowMinimise } from "../wailsjs/runtime/runtime";
import { ConfigureSlack, DesktopSnapshot, StopProcess, UpdatePermissionMode } from "../wailsjs/go/main/App";
import { app } from "../wailsjs/go/models";
import appIcon from "../../build/appicon.png";
import {
  DesktopState,
  LatestRecord,
  activeProcess,
  buildLatestRecord,
  elapsedText,
  isActiveProcess,
  shortCommit,
  shortProcessID,
  statusTone,
} from "./app_model";
import { callWhenCoreReady } from "./core_startup";

type BusyAction = "" | "permission" | "slack" | "stop";

function TitleBar({ version }: { version?: string }) {
  return (
    <header className="titlebar" data-testid="titlebar">
      <div className="title-identity">
        <img src={appIcon} alt="" draggable={false} />
        <strong>CWapi</strong>
        <span>{version ? `v${version}` : ""}</span>
      </div>
      <div className="window-actions">
        <button type="button" aria-label="最小化" title="最小化" onClick={WindowMinimise}><Subtract16Regular /></button>
        <button type="button" aria-label="隐藏到托盘" title="隐藏到托盘" onClick={WindowHide}><Dismiss16Regular /></button>
      </div>
    </header>
  );
}

function StatusItem({ label, state }: { label: string; state: unknown }) {
  return (
    <div className="status-item" title={`${label}: ${String(state || "unknown")}`}>
      <span className={`status-light tone-${statusTone(state)}`} />
      <span>{label}</span>
    </div>
  );
}

function StatusStrip({ snapshot }: { snapshot: DesktopState | null }) {
  const core = snapshot?.components.find((item) => item.name === "mcp-relay" || item.name === "core")?.state ?? (snapshot ? "healthy" : "starting");
  const slack = snapshot?.slack.socket_ready ? "connected" : snapshot?.slack.state ?? "starting";
  const codex = snapshot?.codex.running ? "running" : snapshot?.codex.ready ? "ready" : snapshot ? "degraded" : "starting";
  return <div className="status-strip"><StatusItem label="CORE" state={core} /><StatusItem label="SLACK" state={slack} /><StatusItem label="CODEX" state={codex} /></div>;
}

function LatestViewport({ record }: { record: LatestRecord }) {
  return (
    <section className="latest-section" aria-labelledby="latest-title">
      <div className="section-heading"><h2 id="latest-title">最新记录</h2><span>latest only</span></div>
      <div className={`latest-viewport tone-text-${record.tone}`} data-testid="latest-record" aria-live="polite">
        <div className="latest-meta"><time>{record.clock}</time><span>{record.source}</span><code>{record.identity}</code></div>
        <code className="latest-data" title={record.data}>{record.data}</code>
      </div>
    </section>
  );
}

function ExecutionMonitor({ snapshot, busy, onStop }: {
  snapshot: DesktopState;
  busy: BusyAction;
  onStop: (process: app.ProcessSnapshot) => void;
}) {
  const process = activeProcess(snapshot.processes);
  const activeCount = snapshot.processes.filter(isActiveProcess).length;
  const elapsed = process ? elapsedText(process, snapshot.generated_at) : "—";
  return (
    <section className="monitor-section" data-testid="process-monitor">
      <div className="section-heading"><h2>执行状态</h2><span>ACTIVE {String(activeCount).padStart(2, "0")}</span></div>
      {process ? (
        <div className="process-record" data-testid="process-record">
          <div className="process-primary">
            <span className={`status-light tone-${statusTone(process.state)}`} />
            <code title={process.process_id}>{shortProcessID(process.process_id)}</code>
            <strong>{process.state.toUpperCase()}</strong>
            <time>{elapsed}</time>
          </div>
          <dl className="process-fields">
            <div><dt>backend</dt><dd>{process.backend || "—"}</dd></div>
            <div><dt>repository</dt><dd title={process.repository}>{process.repository || "—"}</dd></div>
            <div><dt>cwd</dt><dd title={process.working_directory}>{process.working_directory || "."}</dd></div>
            <div><dt>commit</dt><dd title={process.expected_commit}>{shortCommit(process.expected_commit)}</dd></div>
            {process.exit_code !== undefined && <div><dt>exit_code</dt><dd>{process.exit_code}</dd></div>}
          </dl>
          {isActiveProcess(process) && (
            <button className="stop-button" type="button" disabled={busy === "stop"} onClick={() => onStop(process)}>
              <RecordStop16Filled />{busy === "stop" ? "[STOPPING]" : "[STOP]"}
            </button>
          )}
        </div>
      ) : (
        <div className="process-empty"><code>state=idle active=0</code><span>等待新的执行记录</span></div>
      )}
    </section>
  );
}

function PermissionControl({ mode, busy, onChange }: {
  mode: string;
  busy: BusyAction;
  onChange: (mode: "safe" | "full_access") => void;
}) {
  const full = mode === "full_access";
  return (
    <section className="compact-row">
      <div><span>权限模式</span><strong>{full ? "FULL" : "SAFE"}</strong></div>
      <button className={`mode-switch ${full ? "mode-switch-full" : ""}`} type="button" role="switch" aria-label="权限模式" aria-checked={full} disabled={busy === "permission"} onClick={() => onChange(full ? "safe" : "full_access")}>
        <span />
      </button>
    </section>
  );
}

function SlackControl({ snapshot, onOpen }: { snapshot: DesktopState; onOpen: () => void }) {
  const connected = snapshot.slack.socket_ready;
  return (
    <button className="slack-row" type="button" data-testid="slack-control" onClick={onOpen}>
      <PlugConnected16Regular />
      <div><span>Slack</span><strong>{connected ? "CONNECTED" : snapshot.slack.state.toUpperCase()}</strong></div>
      <small>{snapshot.slack.channel_name ? `#${snapshot.slack.channel_name}` : snapshot.slack.channel_id}</small>
      <ChevronRight16Regular />
    </button>
  );
}

function SlackForm({ snapshot, appToken, botToken, channelID, busy, dismissible, onAppToken, onBotToken, onChannelID, onSubmit, onClose }: {
  snapshot: DesktopState;
  appToken: string;
  botToken: string;
  channelID: string;
  busy: BusyAction;
  dismissible: boolean;
  onAppToken: (value: string) => void;
  onBotToken: (value: string) => void;
  onChannelID: (value: string) => void;
  onSubmit: (event: FormEvent) => void;
  onClose: () => void;
}) {
  return (
    <div className={dismissible ? "sheet-backdrop" : "setup-area"}>
      <form className="slack-sheet" onSubmit={onSubmit}>
        <div className="sheet-heading"><div><span>SLACK TRANSPORT</span><h1>连接 Slack</h1></div>{dismissible && <button type="button" aria-label="关闭 Slack 设置" onClick={onClose}><Dismiss16Regular /></button>}</div>
        <p>Token 仅保存到 Windows Credential Manager。</p>
        <label>App Token<input aria-label="App Token" type="password" autoComplete="off" placeholder="xapp-…" value={appToken} onChange={(event) => onAppToken(event.target.value)} /></label>
        <label>Bot Token<input aria-label="Bot Token" type="password" autoComplete="off" placeholder="xoxb-…" value={botToken} onChange={(event) => onBotToken(event.target.value)} /></label>
        <label>Channel ID<input aria-label="Channel ID" placeholder="C0123456789" value={channelID} onChange={(event) => onChannelID(event.target.value)} /></label>
        <button className="connect-button" type="submit" disabled={busy === "slack" || !appToken || !botToken || !channelID}>{busy === "slack" ? "CONNECTING" : snapshot.slack.configured ? "更新连接" : "测试连接并保存"}</button>
      </form>
    </div>
  );
}

export default function App() {
  const [snapshot, setSnapshot] = useState<DesktopState | null>(null);
  const [appToken, setAppToken] = useState("");
  const [botToken, setBotToken] = useState("");
  const [channelID, setChannelID] = useState("");
  const [slackOpen, setSlackOpen] = useState(false);
  const [busy, setBusy] = useState<BusyAction>("");
  const [localRecord, setLocalRecord] = useState<LatestRecord | null>(null);

  async function refreshDesktop(silent = false) {
    try {
      const value = await callWhenCoreReady(() => DesktopSnapshot(12));
      setSnapshot(value);
      setChannelID((current) => current || value.slack.channel_id || value.config.slack.channel_id);
      if (!silent) setLocalRecord(null);
    } catch (cause) {
      if (!silent) setLocalRecord({ timestamp: Date.now(), clock: new Date().toLocaleTimeString(), source: "stderr", identity: "desktop", data: `error=${JSON.stringify(String(cause))}`, tone: "red" });
    }
  }

  useEffect(() => {
    let active = true;
    void refreshDesktop();
    const timer = window.setInterval(() => { if (active) void refreshDesktop(true); }, 1000);
    return () => { active = false; window.clearInterval(timer); };
  }, []);

  async function updatePermission(mode: "safe" | "full_access") {
    setBusy("permission");
    try {
      const config = await UpdatePermissionMode(mode);
      setSnapshot((current) => current ? Object.assign(Object.create(Object.getPrototypeOf(current)), current, { config }) : current);
      setLocalRecord({ timestamp: Date.now(), clock: new Date().toLocaleTimeString(), source: "state", identity: "permission", data: `state=completed mode=${mode}`, tone: "green" });
    } catch (cause) {
      setLocalRecord({ timestamp: Date.now(), clock: new Date().toLocaleTimeString(), source: "stderr", identity: "permission", data: `state=failed error=${JSON.stringify(String(cause))}`, tone: "red" });
    } finally { setBusy(""); }
  }

  async function stop(process: app.ProcessSnapshot) {
    setBusy("stop");
    try {
      const record = await StopProcess(process.process_id);
      setLocalRecord({ timestamp: Date.now(), clock: new Date().toLocaleTimeString(), source: "state", identity: shortProcessID(record.process_id), data: `state=${record.state}${record.exit_code === undefined ? "" : ` exit_code=${record.exit_code}`}`, tone: statusTone(record.state) });
      await refreshDesktop(true);
    } catch (cause) {
      setLocalRecord({ timestamp: Date.now(), clock: new Date().toLocaleTimeString(), source: "stderr", identity: shortProcessID(process.process_id), data: `state=failed error=${JSON.stringify(String(cause))}`, tone: "red" });
    } finally { setBusy(""); }
  }

  async function configureSlack(event: FormEvent) {
    event.preventDefault();
    setBusy("slack");
    try {
      const slack = await ConfigureSlack(new app.ConfigureSlackCommand({ app_token: appToken.trim(), bot_token: botToken.trim(), channel_id: channelID.trim() }));
      setSnapshot((current) => current ? Object.assign(Object.create(Object.getPrototypeOf(current)), current, { slack }) : current);
      setAppToken(""); setBotToken(""); setSlackOpen(false);
      setLocalRecord({ timestamp: Date.now(), clock: new Date().toLocaleTimeString(), source: "state", identity: "slack", data: `state=${slack.state} channel_id=${slack.channel_id}`, tone: statusTone(slack.state) });
      await refreshDesktop(true);
    } catch (cause) {
      setLocalRecord({ timestamp: Date.now(), clock: new Date().toLocaleTimeString(), source: "stderr", identity: "slack", data: `state=failed error=${JSON.stringify(String(cause))}`, tone: "red" });
    } finally { setBusy(""); }
  }

  const latest = useMemo(() => buildLatestRecord(snapshot, localRecord), [snapshot, localRecord]);
  return (
    <main className="window-shell" data-testid="single-page">
      <TitleBar version={snapshot?.runtime.version} />
      <StatusStrip snapshot={snapshot} />
      {!snapshot ? (
        <div className="loading-area"><ArrowSync16Regular /><code>state=starting</code></div>
      ) : !snapshot.slack.configured ? (
        <SlackForm snapshot={snapshot} appToken={appToken} botToken={botToken} channelID={channelID} busy={busy} dismissible={false} onAppToken={setAppToken} onBotToken={setBotToken} onChannelID={setChannelID} onSubmit={(event) => void configureSlack(event)} onClose={() => {}} />
      ) : (
        <div className="dashboard-content">
          <ExecutionMonitor snapshot={snapshot} busy={busy} onStop={(process) => void stop(process)} />
          <LatestViewport record={latest} />
          <div className="lower-controls">
            <PermissionControl mode={snapshot.config.permission_mode} busy={busy} onChange={(mode) => void updatePermission(mode)} />
            <SlackControl snapshot={snapshot} onOpen={() => setSlackOpen(true)} />
          </div>
          {slackOpen && <SlackForm snapshot={snapshot} appToken={appToken} botToken={botToken} channelID={channelID} busy={busy} dismissible onAppToken={setAppToken} onBotToken={setBotToken} onChannelID={setChannelID} onSubmit={(event) => void configureSlack(event)} onClose={() => setSlackOpen(false)} />}
        </div>
      )}
    </main>
  );
}
