import { Dispatch, FormEvent, SetStateAction } from "react";
import { app } from "../../wailsjs/go/models";
import {
  DiagnosticsState,
  DesktopState,
  formatTime,
  GuiPreferences,
  LogEntry,
  ProjectForm,
  statusText,
  statusTone,
} from "../app_model";
import { Detail, LogSurface, StatusDot } from "../components/Common";

export function ConsolePage({
  snapshot,
  relayState,
  codexState,
  structuredEntries,
  runtimeEntries,
  prefs,
}: {
  snapshot: DesktopState;
  relayState: unknown;
  codexState: unknown;
  structuredEntries: LogEntry[];
  runtimeEntries: LogEntry[];
  prefs: GuiPreferences;
}) {
  return (
    <div className="page-stack dashboard-page">
      <section className="panel console-status-panel">
        <h2>组件运行状态</h2>
        <div className="dashboard-status-grid">
          <StatusDot label="CWapi 桌面程序" state="running" />
          <StatusDot label="Go Core / MCP Relay" state={relayState} />
          <StatusDot label="Slack Transport" state={snapshot.slack.socket_ready ? "connected" : snapshot.slack.state} />
          <StatusDot label="Codex app-server / MCP Relay" state={codexState} />
          <StatusDot label="安全项目进程 MCP" state={snapshot.codex.process_mcp_ready ? "ready" : "unavailable"} />
        </div>
      </section>
      <div className="console-log-stack" data-testid="console-log-grid">
        <LogSurface title="结构化执行日志" subtitle="显示 MCP 工具、运行状态、耗时和执行结果。" testId="structured-log-surface" entries={structuredEntries} fontSize={prefs.logFontSize} autoFollow={prefs.autoFollow} />
        <LogSurface title="CWapi 运行日志" subtitle="显示 Slack、MCP Relay、Codex 与运行时自身的状态和错误。" testId="runtime-log-surface" entries={runtimeEntries} fontSize={prefs.logFontSize} autoFollow={prefs.autoFollow} />
      </div>
    </div>
  );
}

export function ProjectsPage({
  snapshot,
  projectForm,
  setProjectForm,
  busy,
  onAdd,
  onEdit,
  onSave,
  onRemove,
}: {
  snapshot: DesktopState;
  projectForm: ProjectForm | null;
  setProjectForm: Dispatch<SetStateAction<ProjectForm | null>>;
  busy: string;
  onAdd: () => void;
  onEdit: (project: app.ProjectSnapshot) => void;
  onSave: (event: FormEvent) => void;
  onRemove: (project: app.ProjectSnapshot) => void;
}) {
  return (
    <div className="page-stack">
      <div className="page-action-row">
        <button className="primary-button" type="button" disabled={Boolean(busy)} onClick={onAdd}>＋ 添加项目</button>
      </div>

      {projectForm && (
        <form className="panel project-editor" onSubmit={onSave}>
          <h2>{projectForm.id ? "编辑项目" : "添加项目"}</h2>
          <div className="project-user-grid">
            <label className="management-field">项目名称<input aria-label="Display name" value={projectForm.displayName} onChange={(event) => setProjectForm({ ...projectForm, displayName: event.target.value })} placeholder="例如：CWapi" /></label>
            <label className="management-field">本地路径<input aria-label="Local path" value={projectForm.localPath} onChange={(event) => setProjectForm({ ...projectForm, localPath: event.target.value })} placeholder="E:\\Projects\\CWapi" /></label>
            <label className="management-field">Git 地址<input aria-label="Remote URL" value={projectForm.remoteURL} onChange={(event) => setProjectForm({ ...projectForm, remoteURL: event.target.value })} placeholder="https://github.com/owner/repository.git" /></label>
          </div>
          <div className="button-row project-editor-actions">
            <button type="button" disabled={Boolean(busy)} onClick={() => setProjectForm(null)}>取消</button>
            <button className="primary-button" type="submit" disabled={Boolean(busy)}>{busy ? "正在保存…" : "保存"}</button>
          </div>
        </form>
      )}

      <div className="project-card-grid">
        {snapshot.config.projects.map((project) => (
          <article className="panel project-user-card project-row" key={project.id} data-testid={`project-${project.id}`}>
            <div className="panel-title-row">
              <div className="project-title-with-status"><span className="status-dot status-dot-green" /><h2>{project.display_name}</h2></div>
              <div className="button-row">
                <button type="button" disabled={Boolean(busy)} onClick={() => onEdit(project)}>编辑</button>
                <button className="danger-button" type="button" disabled={Boolean(busy)} onClick={() => onRemove(project)}>删除</button>
              </div>
            </div>
            <div className="project-user-values">
              <div><span>项目 ID</span><code>{project.id}</code></div>
              <div><span>本地路径</span><code>{project.local_path}</code></div>
              <div><span>Git 地址</span><code>{project.remote_url}</code></div>
            </div>
          </article>
        ))}
      </div>

      {snapshot.config.projects.length === 0 && !projectForm && (
        <section className="panel empty-state-action">
          <h2>还没有添加项目</h2>
          <p>添加项目后，CWapi 才能处理该项目中的 MCP 工具请求。</p>
          <button className="primary-button" type="button" onClick={onAdd}>添加项目</button>
        </section>
      )}
    </div>
  );
}

export function SettingsPage({
  snapshot,
  prefs,
  setPrefs,
  onSavePreferences,
  onUpdatePermissionMode,
  slackEditing,
  setSlackEditing,
  appToken,
  setAppToken,
  botToken,
  setBotToken,
  channelID,
  setChannelID,
  busy,
  onConfigureSlack,
}: {
  snapshot: DesktopState;
  prefs: GuiPreferences;
  setPrefs: Dispatch<SetStateAction<GuiPreferences>>;
  onSavePreferences: () => void;
  onUpdatePermissionMode: (mode: "safe" | "full_access") => void;
  slackEditing: boolean;
  setSlackEditing: Dispatch<SetStateAction<boolean>>;
  appToken: string;
  setAppToken: Dispatch<SetStateAction<string>>;
  botToken: string;
  setBotToken: Dispatch<SetStateAction<string>>;
  channelID: string;
  setChannelID: Dispatch<SetStateAction<string>>;
  busy: string;
  onConfigureSlack: (event: FormEvent) => void;
}) {
  const permissionMode = (snapshot.config as app.ConfigSnapshot & { permission_mode?: string }).permission_mode === "full_access" ? "full_access" : "safe";
  return (
    <div className="page-stack settings-simple">
      <section className="panel settings-section">
        <div className="settings-section-heading"><div><h2>界面</h2><p>控制日志显示方式和界面行为。</p></div></div>
        <div className="settings-list">
          <div className="settings-value-row">
            <span className="settings-switch-copy"><strong>日志字号</strong><small>同时应用于结构化执行日志和 CWapi 运行日志。</small></span>
            <input className="settings-number-input" type="number" min={10} max={20} value={prefs.logFontSize} onChange={(event) => setPrefs({ ...prefs, logFontSize: Math.min(20, Math.max(10, Number(event.target.value))) })} />
          </div>
          <label className="settings-switch-row">
            <span className="settings-switch-copy"><strong>自动滚动到最新日志</strong><small>日志有新内容时自动保持在最底部。</small></span>
            <span className="ios-switch"><input type="checkbox" checked={prefs.autoFollow} onChange={(event) => setPrefs({ ...prefs, autoFollow: event.target.checked })} /><span className="ios-switch-track"><span className="ios-switch-thumb" /></span></span>
          </label>
        </div>
        <div className="settings-section-actions"><button className="primary-button" type="button" onClick={onSavePreferences}>保存界面设置</button></div>
      </section>

      <section className="panel settings-section" data-testid="permission-settings-surface">
        <div className="settings-section-heading"><div><h2>权限</h2><p>基础层永久限制始终生效，只切换 Codex 默认执行权限。</p></div></div>
        <div className="permission-mode-grid" role="radiogroup" aria-label="默认执行权限">
          <button className={`permission-mode-button ${permissionMode === "safe" ? "permission-mode-button-active" : ""}`} type="button" role="radio" aria-checked={permissionMode === "safe"} disabled={Boolean(busy)} onClick={() => onUpdatePermissionMode("safe")}>
            <strong>安全权限</strong><span>仅项目与 CWapi 管理目录</span>
          </button>
          <button className={`permission-mode-button ${permissionMode === "full_access" ? "permission-mode-button-active" : ""}`} type="button" role="radio" aria-checked={permissionMode === "full_access"} disabled={Boolean(busy)} onClick={() => onUpdatePermissionMode("full_access")}>
            <strong>完全访问权限</strong><span>允许全盘访问，基础层永久保护仍生效</span>
          </button>
        </div>
      </section>

      <section className="panel settings-section" data-testid="slack-settings-surface">
        <div className="settings-section-heading">
          <div><h2>Slack</h2><p>管理 CWapi 的 MCP 通信频道和本机凭据。</p></div>
          <div className="settings-account-row"><span className={`status-dot status-dot-${statusTone(snapshot.slack.state)}`} /><strong>{snapshot.slack.team || "未连接工作区"}</strong></div>
        </div>
        <div className="settings-list">
          <div className="settings-value-row"><span className="settings-switch-copy"><strong>控制频道</strong><small>{snapshot.slack.channel_name || "未识别频道名称"}</small></span><code>{snapshot.slack.channel_id || "—"}</code></div>
          <div className="settings-value-row"><span className="settings-switch-copy"><strong>凭据存储</strong><small>Token 不写入配置文件。</small></span><code>Windows Credential Manager</code></div>
        </div>
        {!slackEditing ? (
          <div className="settings-section-actions"><button type="button" onClick={() => setSlackEditing(true)}>更换 Slack 配置</button></div>
        ) : (
          <form className="settings-inline-form" onSubmit={onConfigureSlack}>
            <label className="management-field">App Token<input aria-label="Settings App Token" type="password" autoComplete="off" value={appToken} onChange={(event) => setAppToken(event.target.value)} placeholder="xapp-…" /></label>
            <label className="management-field">Bot Token<input aria-label="Settings Bot Token" type="password" autoComplete="off" value={botToken} onChange={(event) => setBotToken(event.target.value)} placeholder="xoxb-…" /></label>
            <label className="management-field">Channel ID<input aria-label="Settings Channel ID" value={channelID} onChange={(event) => setChannelID(event.target.value)} placeholder="C0123456789" /></label>
            <div className="button-row settings-actions"><button type="button" onClick={() => { setSlackEditing(false); setAppToken(""); setBotToken(""); }}>取消</button><button className="primary-button" type="submit" disabled={Boolean(busy) || !appToken || !botToken || !channelID}>验证并保存</button></div>
          </form>
        )}
      </section>
    </div>
  );
}

export function DiagnosticsPage({
  diagnostics,
  snapshot,
  activeErrors,
  busy,
  onResolveError,
}: {
  diagnostics: DiagnosticsState | null;
  snapshot: DesktopState;
  activeErrors: app.ErrorSnapshot[];
  busy: string;
  onResolveError: (fingerprint: string) => void;
}) {
  const codex = diagnostics?.codex ?? snapshot.codex;
  return (
    <div className="page-stack diagnostics-simple">
      <section className="panel">
        <h2>组件状态</h2>
        <div className="diagnostic-component-grid" data-testid="diagnostic-component-grid">
          {(diagnostics?.components ?? snapshot.observability.components).map((component) => (
            <div className="diagnostic-component-card" key={component.name} title={component.name}>
              <span className={`status-dot status-dot-${statusTone(component.state)}`} />
              <strong>{component.name}</strong>
              <span>{statusText(component.state)}</span>
            </div>
          ))}
        </div>
      </section>
      <section className="panel" data-testid="error-aggregate-surface">
        <h2>活动错误</h2>
        {activeErrors.length ? activeErrors.map((entry) => (
          <div className="diagnostic-issue" key={entry.fingerprint}><span className="status-dot status-dot-red" /><div><strong>{entry.component} · {entry.operation}</strong><p>{entry.message}</p><small>出现 {entry.count} 次 · {formatTime(entry.last_seen)}</small></div><button type="button" disabled={Boolean(busy)} onClick={() => onResolveError(entry.fingerprint)}>标记已解决</button></div>
        )) : <div className="empty-state">当前没有活动错误。</div>}
      </section>
      <section className="panel diagnostic-paths">
        <h2>运行环境</h2>
        <Detail label="配置文件" value={diagnostics?.config_path ?? snapshot.config.config_path} mono />
        <Detail label="本地数据库" value={diagnostics?.state_path ?? snapshot.observability.state_path} mono />
        <Detail label="Source commit" value={diagnostics?.source_commit ?? snapshot.runtime.source_commit} mono />
        <Detail label="架构" value={diagnostics?.architecture ?? snapshot.runtime.architecture} />
        <Detail label="Stock Codex" value={`${codex.version || "—"} · ${codex.running ? "running" : codex.ready ? "ready" : "unavailable"}`} />
        <Detail label="Codex executable" value={codex.executable_path || "—"} mono />
        <Detail label="Codex SHA-256" value={codex.executable_sha256 || "—"} mono />
      </section>
    </div>
  );
}

export function AboutPage({ snapshot, onCopySystemInfo }: { snapshot: DesktopState; onCopySystemInfo: () => void }) {
  return (
    <div className="page-stack">
      <section className="panel about-card"><div className="brand-mark brand-mark-large">CW</div><div><h2>CWapi</h2><p>版本 {snapshot.runtime.version}</p></div></section>
      <section className="panel"><h2>位置与版本</h2><Detail label="配置文件" value={snapshot.config.config_path} mono /><Detail label="本地数据库" value={snapshot.observability.state_path} mono /><Detail label="Source commit" value={snapshot.runtime.source_commit} mono /><Detail label="平台" value={snapshot.runtime.platform} /><Detail label="Stock Codex" value={snapshot.codex.version || "—"} /></section>
      <button className="primary-button copy-system-info" type="button" onClick={onCopySystemInfo}>复制系统信息</button>
    </div>
  );
}
