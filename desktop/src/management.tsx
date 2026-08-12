import { useEffect, useMemo, useState } from "react";
import {
  backendAuthorizeGmail,
  backendDoctor,
  backendGmailStatus,
  backendMaintenance,
  backendManagement,
  backendSaveSettings,
  backendValidateSettings,
  desktopPickDirectory,
  desktopReplaceGmailCredentials,
  desktopRemoveGmailAuthorization,
  desktopRestartBackend,
  desktopRevealPath,
  type DoctorSnapshot,
  type ManagementSnapshot,
  type ProjectSnapshot,
} from "./ipc";

export type GuiPreferences = {
  logFontSize: number;
  autoFollow: boolean;
};

const PREF_KEY = "cwapi.gui-preferences.v2";
const OLD_PREF_KEY = "cwapi.gui-preferences.v1";
const DEFAULT_PREFS: GuiPreferences = {
  logFontSize: 12,
  autoFollow: true,
};

export function getGuiPreferences(): GuiPreferences {
  try {
    const raw = localStorage.getItem(PREF_KEY) ?? localStorage.getItem(OLD_PREF_KEY);
    if (!raw) return DEFAULT_PREFS;
    const parsed = JSON.parse(raw) as Partial<GuiPreferences>;
    return {
      logFontSize: Math.min(20, Math.max(10, Number(parsed.logFontSize ?? 12))),
      autoFollow: parsed.autoFollow !== false,
    };
  } catch {
    return DEFAULT_PREFS;
  }
}

export function useGuiPreferences() {
  const [prefs, setPrefs] = useState(getGuiPreferences);
  useEffect(() => {
    const update = () => setPrefs(getGuiPreferences());
    window.addEventListener("cwapi-preferences-changed", update);
    return () => window.removeEventListener("cwapi-preferences-changed", update);
  }, []);
  return prefs;
}

function saveGuiPreferences(value: GuiPreferences) {
  localStorage.setItem(PREF_KEY, JSON.stringify(value));
  localStorage.removeItem(OLD_PREF_KEY);
  window.dispatchEvent(new Event("cwapi-preferences-changed"));
}

const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value));

function TextField({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  return (
    <label className="management-field">
      <span>{label}</span>
      <input value={value} placeholder={placeholder} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}


function SettingsSwitch({
  label,
  description,
  checked,
  onChange,
  disabled = false,
}: {
  label: string;
  description?: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <label className={`settings-switch-row ${disabled ? "settings-switch-disabled" : ""}`}>
      <span className="settings-switch-copy">
        <strong>{label}</strong>
        {description && <small>{description}</small>}
      </span>
      <span className="ios-switch">
        <input
          type="checkbox"
          checked={checked}
          disabled={disabled}
          onChange={(event) => onChange(event.target.checked)}
        />
        <span className="ios-switch-track" aria-hidden="true">
          <span className="ios-switch-thumb" />
        </span>
      </span>
    </label>
  );
}

function statusDot(ok: boolean | null) {
  const tone = ok === null ? "gray" : ok ? "green" : "yellow";
  return <span className={`status-dot status-dot-${tone}`} aria-hidden="true" />;
}

function parseGitHubRepository(remote: string): string {
  const value = remote.trim().replace(/\/+$/, "").replace(/\.git$/i, "");
  const match = value.match(/github\.com(?::|\/)([^/\s:]+)\/([^/\s]+)$/i);
  if (!match) throw new Error("Git 地址必须是有效的 GitHub 仓库地址。");
  return `${match[1]}/${match[2]}`;
}

async function saveConfigSnapshot(snapshot: ManagementSnapshot, editable: Record<string, any>) {
  await backendValidateSettings("config", editable);
  const result = await backendSaveSettings("config", snapshot.config.revision, editable);
  if (!result.deferred && result.restart_required) await desktopRestartBackend();
  return result;
}

type ProjectForm = {
  originalRepository: string | null;
  name: string;
  path: string;
  remoteUrl: string;
};

const EMPTY_PROJECT: ProjectForm = {
  originalRepository: null,
  name: "",
  path: "",
  remoteUrl: "",
};

export function ProjectsManagement({ projects }: { projects: ProjectSnapshot[] }) {
  const [snapshot, setSnapshot] = useState<ManagementSnapshot | null>(null);
  const [form, setForm] = useState<ProjectForm | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");

  const refresh = async () => {
    setSnapshot(await backendManagement());
  };

  useEffect(() => {
    void refresh();
  }, []);

  const pickPath = async () => {
    const result = await desktopPickDirectory();
    if (!result.cancelled && result.path) {
      setForm((current) => current ? { ...current, path: result.path ?? current.path } : current);
    }
  };

  const startEdit = (project: ProjectSnapshot) => {
    setMessage("");
    setForm({
      originalRepository: project.repository,
      name: project.name || project.repository,
      path: project.path,
      remoteUrl: project.remote_url,
    });
  };

  const saveProject = async () => {
    if (!snapshot || !form) return;
    setBusy(true);
    setMessage("");
    try {
      const name = form.name.trim();
      const path = form.path.trim();
      const remoteUrl = form.remoteUrl.trim();
      if (!name) throw new Error("请输入项目名称。");
      if (!path) throw new Error("请选择或输入本地路径。");
      const repository = parseGitHubRepository(remoteUrl);
      const editable = clone(snapshot.config.editable);
      editable.projects ??= {};
      editable.security ??= {};
      const oldRepository = form.originalRepository;
      const oldProject = oldRepository ? editable.projects[oldRepository] : undefined;
      if (oldRepository && oldRepository !== repository) delete editable.projects[oldRepository];
      editable.projects[repository] = {
        ...(oldProject ?? {}),
        name,
        path,
        remote_url: remoteUrl,
      };
      const allowed = new Set<string>((editable.security.allowed_repositories ?? []).map(String));
      if (oldRepository) allowed.delete(oldRepository);
      allowed.add(repository);
      editable.security.allowed_repositories = Array.from(allowed).sort();
      const result = await saveConfigSnapshot(snapshot, editable);
      setMessage(result.deferred ? "项目修改已保存，将在当前任务结束后应用。" : "项目已保存。");
      setForm(null);
      await refresh();
    } catch (cause) {
      setMessage(String(cause));
    } finally {
      setBusy(false);
    }
  };

  const removeProject = async (project: ProjectSnapshot) => {
    if (!snapshot) return;
    if (!window.confirm(`从 CWapi 移除项目“${project.name || project.repository}”？\n\n只会删除 CWapi 中的项目配置，不会删除本地文件，也不会删除 GitHub 仓库。`)) return;
    setBusy(true);
    setMessage("");
    try {
      const editable = clone(snapshot.config.editable);
      editable.projects ??= {};
      editable.security ??= {};
      delete editable.projects[project.repository];
      editable.security.allowed_repositories = (editable.security.allowed_repositories ?? [])
        .map(String)
        .filter((value: string) => value !== project.repository);
      const result = await saveConfigSnapshot(snapshot, editable);
      setMessage(result.deferred ? "项目移除请求已保存，将在当前任务结束后应用。" : "项目已从 CWapi 移除。");
      await refresh();
    } catch (cause) {
      setMessage(String(cause));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="page-stack">
      <div className="page-action-row">
        <button className="primary-button" type="button" disabled={busy} onClick={() => { setMessage(""); setForm({ ...EMPTY_PROJECT }); }}>
          ＋ 添加项目
        </button>
      </div>

      {form && (
        <section className="panel project-editor">
          <h2>{form.originalRepository ? "编辑项目" : "添加项目"}</h2>
          <div className="project-user-grid">
            <TextField label="项目名称" value={form.name} onChange={(value) => setForm({ ...form, name: value })} placeholder="例如：CWapi" />
            <label className="management-field">
              <span>本地路径</span>
              <div className="field-with-icon">
                <input value={form.path} onChange={(event) => setForm({ ...form, path: event.target.value })} placeholder="E:\\Projects\\CWapi" />
                <button className="icon-button" type="button" title="选择文件夹" aria-label="选择项目文件夹" onClick={() => void pickPath()}>📂</button>
              </div>
            </label>
            <TextField label="Git 地址" value={form.remoteUrl} onChange={(value) => setForm({ ...form, remoteUrl: value })} placeholder="https://github.com/owner/repository.git" />
          </div>
          <div className="button-row project-editor-actions">
            <button type="button" disabled={busy} onClick={() => setForm(null)}>取消</button>
            <button className="primary-button" type="button" disabled={busy} onClick={() => void saveProject()}>{busy ? "正在保存…" : "保存"}</button>
          </div>
        </section>
      )}

      <div className="project-card-grid">
        {projects.map((project) => (
          <article className="panel project-user-card" key={project.repository}>
            <div className="panel-title-row">
              <div className="project-title-with-status">
                {statusDot(project.git?.available === true ? true : false)}
                <h2>{project.name || project.repository}</h2>
              </div>
              <div className="button-row">
                <button className="icon-button" type="button" title="打开项目文件夹" aria-label="打开项目文件夹" onClick={() => void desktopRevealPath(project.path)}>📂</button>
                <button type="button" disabled={busy} onClick={() => startEdit(project)}>编辑</button>
                <button className="danger-button" type="button" disabled={busy} onClick={() => void removeProject(project)}>删除</button>
              </div>
            </div>
            <div className="project-user-values">
              <div><span>本地路径</span><code>{project.path}</code></div>
              <div><span>Git 地址</span><code>{project.remote_url}</code></div>
            </div>
          </article>
        ))}
      </div>

      {projects.length === 0 && !form && (
        <section className="panel empty-state-action">
          <h2>还没有添加项目</h2>
          <p>添加项目后，CWapi 才能处理该项目中的任务。</p>
          <button className="primary-button" type="button" onClick={() => setForm({ ...EMPTY_PROJECT })}>添加项目</button>
        </section>
      )}
      {message && <div className="panel setup-message">{message}</div>}
    </div>
  );
}

export function SettingsManagement() {
  const [snapshot, setSnapshot] = useState<ManagementSnapshot | null>(null);
  const [gmail, setGmail] = useState<Record<string, any> | null>(null);
  const [prefs, setPrefs] = useState(getGuiPreferences);
  const [drivePath, setDrivePath] = useState("");
  const [driveEnabled, setDriveEnabled] = useState(false);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");
  const [loadError, setLoadError] = useState("");

  const refresh = async () => {
    setLoadError("");
    try {
      const [next, auth] = await Promise.all([backendManagement(), backendGmailStatus()]);
      setSnapshot(next);
      setGmail(auth);
      const path = String(next.config.editable.storage?.drive_sync_path ?? "");
      setDrivePath(path);
      setDriveEnabled(Boolean(path));
    } catch (cause) {
      setLoadError(String(cause));
    }
  };

  useEffect(() => {
    void refresh();
  }, []);

  const pickDrive = async () => {
    const result = await desktopPickDirectory();
    if (!result.cancelled && result.path) {
      setDrivePath(result.path);
      setDriveEnabled(true);
    }
  };

  const saveDrive = async () => {
    if (!snapshot) return;
    setBusy("drive");
    setMessage("");
    try {
      if (driveEnabled && !drivePath.trim()) throw new Error("启用 Google Drive 同步前，请先选择本地同步目录。");
      const editable = clone(snapshot.config.editable);
      editable.storage ??= {};
      editable.storage.drive_sync_path = driveEnabled ? drivePath.trim() : null;
      const result = await saveConfigSnapshot(snapshot, editable);
      setMessage(result.deferred ? "Google Drive 设置已保存，将在当前任务结束后应用。" : "Google Drive 设置已保存。");
      await refresh();
    } catch (cause) {
      setMessage(String(cause));
    } finally {
      setBusy("");
    }
  };

  const savePreferences = () => {
    saveGuiPreferences(prefs);
    setMessage("界面设置已保存。");
  };

  const replaceCredentials = async () => {
    setBusy("credentials");
    setMessage("");
    try {
      const result = await desktopReplaceGmailCredentials();
      if (result.cancelled) { setMessage("未选择新的 OAuth 配置文件。"); return; }
      setMessage("OAuth 配置文件已更新，Gmail 授权已完成。");
      await refresh();
    } catch (cause) {
      setMessage(String(cause));
    } finally {
      setBusy("");
    }
  };

  const reauthorize = async () => {
    setBusy("gmail");
    setMessage("");
    try {
      await backendAuthorizeGmail();
      setMessage("Gmail 授权已更新。");
      await refresh();
    } catch (cause) {
      setMessage(String(cause));
    } finally {
      setBusy("");
    }
  };

  const removeAuthorization = async () => {
    if (!window.confirm("确认移除本机 Gmail 授权？之后需要在默认浏览器中由你本人重新授权。")) return;
    setBusy("gmail");
    setMessage("");
    try {
      await desktopRemoveGmailAuthorization();
      setMessage("Gmail 授权已移除。");
      await refresh();
    } catch (cause) {
      setMessage(String(cause));
    } finally {
      setBusy("");
    }
  };

  if (!snapshot) {
    if (loadError) {
      return (
        <section className="panel">
          <h2>设置加载失败</h2>
          <p className="error-text">{loadError}</p>
          <button type="button" onClick={() => void refresh()}>重新加载</button>
        </section>
      );
    }
    return <section className="panel">正在加载设置…</section>;
  }
  const readOnly = snapshot.config.read_only as Record<string, unknown>;
  const credentialsPath = String(readOnly.credentials_path ?? "");

  return (
    <div className="page-stack settings-simple">
      <section className="panel settings-section">
        <div className="settings-section-heading">
          <div>
            <h2>界面</h2>
            <p>控制日志显示方式和界面行为。</p>
          </div>
        </div>
        <div className="settings-list">
          <div className="settings-value-row">
            <span className="settings-switch-copy">
              <strong>日志字号</strong>
              <small>同时应用于结构化执行日志和 Runner 运行日志。</small>
            </span>
            <input
              className="settings-number-input"
              type="number"
              min={10}
              max={20}
              value={prefs.logFontSize}
              onChange={(event) => setPrefs({ ...prefs, logFontSize: Math.min(20, Math.max(10, Number(event.target.value))) })}
            />
          </div>
          <SettingsSwitch
            label="自动滚动到最新日志"
            description="日志有新内容时自动保持在最底部；长日志始终自动换行。"
            checked={prefs.autoFollow}
            onChange={(checked) => setPrefs({ ...prefs, autoFollow: checked })}
          />
        </div>
        <div className="settings-section-actions">
          <button className="primary-button" type="button" onClick={savePreferences}>保存界面设置</button>
        </div>
      </section>

      <section className="panel settings-section">
        <div className="settings-section-heading">
          <div>
            <h2>Gmail</h2>
            <p>管理任务邮箱和 Google OAuth 授权。</p>
          </div>
          <div className="settings-account-row">
            {statusDot(gmail?.authorization_required ? false : true)}
            <strong>{String(gmail?.account ?? "未配置账号")}</strong>
          </div>
        </div>
        {credentialsPath && (
          <div className="path-row">
            <span>OAuth 配置文件</span>
            <code>{credentialsPath}</code>
            <button className="icon-button" type="button" title="打开所在位置" aria-label="打开 OAuth 配置文件所在位置" onClick={() => void desktopRevealPath(credentialsPath)}>📂</button>
          </div>
        )}
        <div className="button-row settings-actions">
          <button type="button" disabled={Boolean(busy)} onClick={() => void replaceCredentials()}>{busy === "credentials" ? "等待浏览器授权…" : "选择 OAuth 配置文件"}</button>
          <button type="button" disabled={Boolean(busy)} onClick={() => void reauthorize()}>{busy === "gmail" ? "处理中…" : "重新授权"}</button>
          <button className="danger-button" type="button" disabled={Boolean(busy)} onClick={() => void removeAuthorization()}>移除授权</button>
        </div>
      </section>

      <section className="panel settings-section">
        <div className="settings-section-heading">
          <div>
            <h2>Google Drive</h2>
            <p>把大文件写入 Google Drive 桌面版的本地同步目录。</p>
          </div>
        </div>
        <div className="settings-list">
          <SettingsSwitch
            label="启用 Google Drive 同步"
            description="关闭后 CWapi 不会把结果复制到同步目录。"
            checked={driveEnabled}
            onChange={setDriveEnabled}
          />
          <label className="management-field settings-path-field">
            <span>本地同步目录</span>
            <div className="field-with-icon">
              <input disabled={!driveEnabled} value={drivePath} onChange={(event) => setDrivePath(event.target.value)} placeholder="选择 Google Drive 官方软件的本地同步文件夹" />
              <button className="icon-button" type="button" disabled={!driveEnabled} title="选择文件夹" aria-label="选择 Google Drive 本地同步目录" onClick={() => void pickDrive()}>📂</button>
            </div>
          </label>
        </div>
        <p className="drive-note"><strong>CWapi 不负责向 Google Drive 上传文件。</strong> CWapi 只把需要同步的文件写入本地同步目录，上传和下载仍由 Google Drive 官方桌面软件完成。</p>
        <div className="settings-section-actions">
          <button className="primary-button" type="button" disabled={Boolean(busy)} onClick={() => void saveDrive()}>{busy === "drive" ? "正在保存…" : "保存 Google Drive 设置"}</button>
        </div>
      </section>

      {message && <div className="panel setup-message">{message}</div>}
    </div>
  );
}

const CHECK_NAMES: Record<string, string> = {
  "cwapi.yaml": "CWapi 配置",
  "Codex capability policy": "Codex 配置",
  "Go Transport protocol": "Gmail 通信服务",
  "Runner heartbeat": "任务执行服务",
  "Gmail authorization": "Gmail 授权",
  "Runner lock": "任务执行锁",
  "SQLite quick_check": "本地数据库",
  "Runtime logs": "日志目录",
  "Results": "结果目录",
  "Google Drive sync": "Google Drive 同步目录",
  "Codex runtime": "Codex 运行时",
  "Portable release": "便携版完整性",
  "Pending settings": "待应用设置",
};

function checkName(name: string) {
  if (name.startsWith("Project ")) return `项目 ${name.slice(8)}`;
  return CHECK_NAMES[name] ?? name;
}

export function DiagnosticsMaintenance({ selectedTaskId: _selectedTaskId, onNavigate }: { selectedTaskId: string | null; onNavigate?: (section: "projects" | "settings") => void }) {
  const [doctor, setDoctor] = useState<DoctorSnapshot | null>(null);
  const [management, setManagement] = useState<ManagementSnapshot | null>(null);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");
  const [loadError, setLoadError] = useState("");

  const refresh = async () => {
    setLoadError("");
    try {
      const [nextDoctor, nextManagement] = await Promise.all([backendDoctor(), backendManagement()]);
      setDoctor(nextDoctor);
      setManagement(nextManagement);
    } catch (cause) {
      setLoadError(String(cause));
    }
  };

  useEffect(() => {
    void refresh();
  }, []);

  const reauthorizeFromDiagnostics = async () => {
    setBusy("gmail");
    setMessage("");
    try {
      await backendAuthorizeGmail();
      setMessage("Gmail 授权已更新。");
      await refresh();
    } catch (cause) {
      setMessage(String(cause));
    } finally {
      setBusy("");
    }
  };

  const executeCleanup = async () => {
    setBusy("cleanup");
    setMessage("");
    try {
      await backendMaintenance("cleanup");
      setMessage("临时文件清理完成。");
      await refresh();
    } catch (cause) {
      setMessage(String(cause));
    } finally {
      setBusy("");
    }
  };

  const restart = async () => {
    setBusy("restart");
    setMessage("");
    try {
      await desktopRestartBackend();
      setMessage("后台服务已重新启动。");
      await refresh();
    } catch (cause) {
      setMessage(String(cause));
    } finally {
      setBusy("");
    }
  };

  const issues = useMemo(() => (doctor?.checks ?? []).filter((item) => !item.ok), [doctor]);
  if (!doctor || !management) {
    if (loadError) {
      return (
        <section className="panel">
          <h2>诊断加载失败</h2>
          <p className="error-text">{loadError}</p>
          <button type="button" onClick={() => void refresh()}>重新检查</button>
        </section>
      );
    }
    return <section className="panel">正在检查系统…</section>;
  }
  const readOnly = management.config.read_only as Record<string, unknown>;

  return (
    <div className="page-stack diagnostics-simple">
      <section className="panel">
        <div className="panel-title-row">
          <h2>需要处理</h2>
          <button type="button" disabled={Boolean(busy)} onClick={() => void refresh()}>重新检查</button>
        </div>
        {issues.map((item) => (
          <article className="diagnostic-issue" key={item.name}>
            <span className="status-dot status-dot-red" aria-hidden="true" />
            <div>
              <strong>{checkName(item.name)}</strong>
              {(item.name.startsWith("Project ") || item.name === "Google Drive sync") && <p>{item.detail}</p>}
            </div>
            {item.name === "Gmail authorization" && <button type="button" disabled={Boolean(busy)} onClick={() => void reauthorizeFromDiagnostics()}>重新授权</button>}
            {item.name === "Google Drive sync" && <button type="button" onClick={() => onNavigate?.("settings")}>前往设置</button>}
            {item.name.startsWith("Project ") && <button type="button" onClick={() => onNavigate?.("projects")}>前往项目</button>}
          </article>
        ))}
        {issues.length === 0 && <div className="empty-state">未发现需要修复的问题。</div>}
      </section>

      <section className="panel">
        <h2>维护</h2>
        <div className="maintenance-grid simple-maintenance-grid">
          <button type="button" disabled={Boolean(busy)} onClick={() => void executeCleanup()}>{busy === "cleanup" ? "正在清理…" : "清理临时文件"}</button>
          <button type="button" disabled={Boolean(busy) || Boolean(management.active_task_id)} onClick={() => void restart()}>{busy === "restart" ? "正在重启…" : "重启后台服务"}</button>
        </div>
        <div className="diagnostic-paths">
          <div className="path-row">
            <span>日志目录</span>
            <code>{String(readOnly.logs_path ?? "")}</code>
            <button className="icon-button" type="button" title="打开日志目录" aria-label="打开日志目录" onClick={() => void desktopRevealPath(String(readOnly.logs_path ?? ""))}>📂</button>
          </div>
          <div className="path-row">
            <span>数据目录</span>
            <code>{String(readOnly.data_root ?? "")}</code>
            <button className="icon-button" type="button" title="打开数据目录" aria-label="打开数据目录" onClick={() => void desktopRevealPath(String(readOnly.data_root ?? ""))}>📂</button>
          </div>
        </div>
      </section>
      {message && <div className="panel setup-message">{message}</div>}
    </div>
  );
}
