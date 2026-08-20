import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  AddProject,
  ConfigureSlack,
  DesktopSnapshot,
  DiagnosticsSnapshot,
  RemoveProject,
  ResolveDesktopError,
  UpdatePermissionMode,
  UpdateProject,
} from "../wailsjs/go/main/App";
import { app } from "../wailsjs/go/models";
import {
  DesktopState,
  DiagnosticsState,
  EMPTY_PROJECT,
  getGuiPreferences,
  initialSection,
  NAV,
  parseGitHubRepository,
  runtimeLogPresentation,
  PREF_KEY,
  ProjectForm,
  SECTION_KEY,
} from "./app_model";
import { MCPRequestsPanel } from "./components/MCPRequestsPanel";
import {
  AboutPage,
  ConsolePage,
  DiagnosticsPage,
  ProjectsPage,
  SettingsPage,
} from "./pages/AppPages";

export default function App() {
  const [section, setSection] = useState(initialSection);
  const [snapshot, setSnapshot] = useState<DesktopState | null>(null);
  const [diagnostics, setDiagnostics] = useState<DiagnosticsState | null>(null);
  const [projectForm, setProjectForm] = useState<ProjectForm | null>(null);
  const [prefs, setPrefs] = useState(getGuiPreferences);
  const [slackEditing, setSlackEditing] = useState(false);
  const [slackAppToken, setSlackAppToken] = useState("");
  const [slackBotToken, setSlackBotToken] = useState("");
  const [slackChannelID, setSlackChannelID] = useState("");
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  async function refreshDesktop(silent = false) {
    try {
      const value = await DesktopSnapshot(150) as DesktopState;
      setSnapshot(value);
      setSlackChannelID((current) => current || value.slack.channel_id || value.config.slack.channel_id);
      if (!silent) setError("");
    } catch (cause) {
      if (!silent) setError(`desktop.snapshot: ${String(cause)}`);
    }
  }

  useEffect(() => {
    localStorage.setItem(SECTION_KEY, section);
  }, [section]);

  useEffect(() => {
    let active = true;
    void refreshDesktop();
    const timer = window.setInterval(() => { if (active) void refreshDesktop(true); }, 3000);
    return () => { active = false; window.clearInterval(timer); };
  }, []);

  useEffect(() => {
    if (section !== "diagnostics") return;
    let active = true;
    const refresh = async () => {
      try {
        const value = await DiagnosticsSnapshot() as DiagnosticsState;
        if (active) setDiagnostics(value);
      } catch (cause) {
        if (active) setError(`diagnostics.snapshot: ${String(cause)}`);
      }
    };
    void refresh();
    const timer = window.setInterval(() => void refresh(), 5000);
    return () => { active = false; window.clearInterval(timer); };
  }, [section]);

  async function configureSlack(event: FormEvent) {
    event.preventDefault();
    setBusy("slack.configure");
    setError("");
    setMessage("");
    try {
      const next = await ConfigureSlack(new app.ConfigureSlackCommand({
        app_token: slackAppToken.trim(),
        bot_token: slackBotToken.trim(),
        channel_id: slackChannelID.trim(),
      }));
      setSnapshot((current) => current ? { ...current, slack: next } : current);
      setSlackAppToken("");
      setSlackBotToken("");
      setSlackEditing(false);
      setMessage("Slack 配置已验证并保存。");
      await refreshDesktop(true);
    } catch (cause) {
      setError(`slack.configure: ${String(cause)}`);
    } finally {
      setBusy("");
    }
  }

  function startAddProject() {
    setMessage("");
    setProjectForm({ ...EMPTY_PROJECT });
  }

  function startEditProject(project: app.ProjectSnapshot) {
    setMessage("");
    setProjectForm({
      id: project.id,
      displayName: project.display_name,
      localPath: project.local_path,
      remoteURL: project.remote_url,
    });
  }

  async function saveProject(event: FormEvent) {
    event.preventDefault();
    if (!projectForm) return;
    const operation = projectForm.id ? "projects.update" : "projects.add";
    setBusy(operation);
    setError("");
    setMessage("");
    try {
      if (!projectForm.displayName.trim()) throw new Error("请输入项目名称。");
      if (!projectForm.localPath.trim()) throw new Error("请输入本地路径。");
      const repository = parseGitHubRepository(projectForm.remoteURL);
      const command = new app.ProjectCommand({
        display_name: projectForm.displayName.trim(),
        repository,
        local_path: projectForm.localPath.trim(),
        remote_url: projectForm.remoteURL.trim(),
      });
      const next = projectForm.id ? await UpdateProject(projectForm.id, command) : await AddProject(command);
      setSnapshot((current) => current ? { ...current, config: next } : current);
      setProjectForm(null);
      setMessage("项目已保存。");
    } catch (cause) {
      setError(`${operation}: ${String(cause)}`);
    } finally {
      setBusy("");
    }
  }

  async function removeProject(project: app.ProjectSnapshot) {
    setBusy(`projects.remove:${project.id}`);
    setError("");
    setMessage("");
    try {
      const next = await RemoveProject(project.id);
      setSnapshot((current) => current ? { ...current, config: next } : current);
      if (projectForm?.id === project.id) setProjectForm(null);
      setMessage("项目已从 CWapi 移除。本地文件和 GitHub 仓库不会被删除。");
    } catch (cause) {
      setError(`projects.remove: ${String(cause)}`);
    } finally {
      setBusy("");
    }
  }

  async function resolveError(fingerprint: string) {
    setBusy(`errors.resolve:${fingerprint}`);
    try {
      const observability = await ResolveDesktopError(fingerprint);
      setSnapshot((current) => current ? { ...current, observability } : current);
    } catch (cause) {
      setError(`errors.resolve: ${String(cause)}`);
    } finally {
      setBusy("");
    }
  }

  function savePreferences() {
    localStorage.setItem(PREF_KEY, JSON.stringify(prefs));
    setMessage("界面设置已保存。");
  }

  async function updatePermissionMode(mode: "safe" | "full_access") {
    setBusy(`permissions.update:${mode}`);
    setError("");
    setMessage("");
    try {
      const next = await UpdatePermissionMode(mode);
      setSnapshot((current) => current ? { ...current, config: next } : current);
      setMessage(mode === "full_access" ? "已切换为完全访问权限。" : "已切换为安全权限。");
    } catch (cause) {
      setError(`permissions.update: ${String(cause)}`);
    } finally {
      setBusy("");
    }
  }

  async function copySystemInfo() {
    if (!snapshot) return;
    const text = [
      `CWapi ${snapshot.runtime.version}`,
      `系统：${snapshot.runtime.platform}`,
      `Go Core：${snapshot.runtime.core}`,
      `Slack：${snapshot.slack.state}`,
      `Codex：${snapshot.codex.running ? "running" : snapshot.codex.ready ? "ready" : "unavailable"}`,
      `Source commit：${snapshot.runtime.source_commit}`,
    ].join("\n");
    try {
      await navigator.clipboard.writeText(text);
      setMessage("系统信息已复制。");
    } catch (cause) {
      setError(`clipboard: ${String(cause)}`);
    }
  }

  const activeErrors = snapshot?.observability.errors.filter((entry) => entry.active) ?? [];
  const relayComponent = snapshot?.observability.components.find((entry) => entry.name === "mcp-relay" || entry.name === "core");
  const codexState = snapshot?.codex.running ? "running" : snapshot?.codex.ready ? "ready" : "degraded";
  const structuredEntries = useMemo(() => snapshot?.observability.structured_execution.map((entry) => ({
    key: `e-${entry.id}`,
    time: entry.timestamp,
    head: entry.step_id ? `步骤 ${entry.step_id}` : entry.kind,
    message: entry.message,
    status: entry.status,
    duration: entry.duration_ms,
  })) ?? [], [snapshot?.observability.structured_execution]);
  const runtimeEntries = useMemo(() => snapshot?.observability.runtime_logs.map((entry) => {
    const presentation = runtimeLogPresentation(entry.level, entry.message, entry.fields_json);
    return {
      key: `r-${entry.id}`,
      time: entry.timestamp,
      head: `${entry.component} · ${entry.level}`,
      message: presentation.message,
      status: presentation.status,
    };
  }) ?? [], [snapshot?.observability.runtime_logs]);

  if (!snapshot) {
    return <main className="loading-shell"><div className="brand-mark brand-mark-large">CW</div><p>正在启动 CWapi…</p>{error && <p className="error-text" role="alert">{error}</p>}</main>;
  }

  if (!snapshot.slack.configured) {
    return (
      <main className="setup-shell">
        <form className="setup-card" onSubmit={configureSlack}>
          <div className="brand-mark brand-mark-large">CW</div>
          <p className="setup-version">CWapi v1.6.0</p>
          <h1>连接 Slack</h1>
          <p>填写 Slack App Token、Bot Token 和控制频道 ID。凭据只保存在 Windows Credential Manager 中。</p>
          <label className="management-field">App Token<input aria-label="App Token" type="password" autoComplete="off" placeholder="xapp-…" value={slackAppToken} onChange={(event) => setSlackAppToken(event.target.value)} /></label>
          <label className="management-field">Bot Token<input aria-label="Bot Token" type="password" autoComplete="off" placeholder="xoxb-…" value={slackBotToken} onChange={(event) => setSlackBotToken(event.target.value)} /></label>
          <label className="management-field">Channel ID<input aria-label="Channel ID" placeholder="C0123456789" value={slackChannelID} onChange={(event) => setSlackChannelID(event.target.value)} /></label>
          <button className="primary-button" type="submit" disabled={Boolean(busy) || !slackAppToken || !slackBotToken || !slackChannelID}>测试连接并保存</button>
          {error && <p className="error-text" role="alert">{error}</p>}
        </form>
      </main>
    );
  }

  const pageTitle = NAV.find((item) => item.id === section)?.label ?? "控制台";

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand-block"><div className="brand-mark">CW</div><div><strong>CWapi</strong></div></div>
        <nav aria-label="CWapi 功能区">
          {NAV.map((item) => (
            <button key={item.id} type="button" className={`nav-item ${section === item.id ? "nav-item-active" : ""}`} onClick={() => { setSection(item.id); setMessage(""); }}>
              <span>{item.label}</span>
            </button>
          ))}
        </nav>
      </aside>

      <section className="workspace">
        <header className="workspace-header simplified-header"><h1>{pageTitle}</h1></header>
        <div className="page-content">
          {error && <div className="global-error" role="alert">{error}</div>}
          {message && <div className="panel setup-message">{message}</div>}
          {section === "console" && <div className="console-page-shell">
            <ConsolePage snapshot={snapshot} relayState={relayComponent?.state ?? "running"} codexState={codexState} structuredEntries={structuredEntries} runtimeEntries={runtimeEntries} prefs={prefs} />
            <MCPRequestsPanel requests={snapshot.mcp_requests} />
          </div>}
          {section === "projects" && <ProjectsPage snapshot={snapshot} projectForm={projectForm} setProjectForm={setProjectForm} busy={busy} onAdd={startAddProject} onEdit={startEditProject} onSave={saveProject} onRemove={(project) => void removeProject(project)} />}
          {section === "settings" && <SettingsPage snapshot={snapshot} prefs={prefs} setPrefs={setPrefs} onSavePreferences={savePreferences} onUpdatePermissionMode={(mode) => void updatePermissionMode(mode)} slackEditing={slackEditing} setSlackEditing={setSlackEditing} appToken={slackAppToken} setAppToken={setSlackAppToken} botToken={slackBotToken} setBotToken={setSlackBotToken} channelID={slackChannelID} setChannelID={setSlackChannelID} busy={busy} onConfigureSlack={configureSlack} />}
          {section === "diagnostics" && <DiagnosticsPage diagnostics={diagnostics} snapshot={snapshot} activeErrors={activeErrors} busy={busy} onResolveError={(fingerprint) => void resolveError(fingerprint)} />}
          {section === "about" && <AboutPage snapshot={snapshot} onCopySystemInfo={() => void copySystemInfo()} />}
        </div>
      </section>
    </main>
  );
}
