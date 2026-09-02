import { useCallback, useEffect, useRef, useState } from "react";
import {
  AgentCredential,
  ClearAgentOpenAITunnel,
  ClearOpenAITunnel,
  ConfigureAgentOpenAITunnel,
  ConfigureOpenAITunnel,
  ConnectionInfo,
  DeleteWorkspace,
  RegenerateAgentAPIKey,
  RuntimeSnapshot,
  SetAgentEnabled,
  UpdateCodexAccessProfile,
  UpdateCodexNetworkAccess,
  UpdateCodexRemoteGitRewrite,
} from "../wailsjs/go/main/App";
import { WindowHide } from "../wailsjs/runtime/runtime";

type Page = "coding" | "agent";
type Snapshot = {
  state: string;
  mcp?: { state?: string; address?: string; error?: string };
  codex_access_profile?: string;
  codex_network_access?: boolean;
  codex_remote_git_rewrite?: boolean;
  codex?: { state?: string; executable?: string; last_error?: string };
  coding?: { state?: string; active?: number; repositories?: string[] };
  workspaces?: { repository_count?: number; repositories?: string[]; invalid_entries?: number };
  agent_enabled?: boolean;
  agent_provider?: { state?: string; address?: string; last_error?: string };
  agent?: { bridge_state?: string; pending?: number; claimed?: number; active?: number; completed?: number; revision?: number; idle_count?: number; last_state?: string; last_error?: string };
  openai_tunnel?: TunnelSnapshot;
  agent_openai_tunnel?: TunnelSnapshot;
  last_error?: string;
};
type Connections = { coding_mcp?: string; agent_mcp?: string; agent_provider?: string };
type TunnelInfo = { enabled?: boolean; tunnel_id?: string; api_key_present?: boolean; state?: string; last_error?: string };
type TunnelSnapshot = Omit<TunnelInfo, "enabled"> & { configured?: boolean };
type ConfirmState = {
  title: string;
  description: string;
  confirmLabel: string;
  onConfirm: () => Promise<void>;
};

const emptySnapshot: Snapshot = { state: "starting" };

function normalizeTunnel(value?: TunnelSnapshot): TunnelInfo {
  return {
    enabled: Boolean(value?.configured),
    tunnel_id: value?.tunnel_id,
    api_key_present: value?.api_key_present,
    state: value?.state,
    last_error: value?.last_error,
  };
}

function errorCode(value: unknown) {
  const text = value instanceof Error ? value.message : String(value || "");
  return text.match(/[A-Z][A-Z0-9_]{2,}/)?.[0] || (text.trim() ? "OPERATION_FAILED" : "");
}

function snapshotIssue(page: Page, snapshot: Snapshot, tunnel: TunnelInfo) {
  const candidates: unknown[] = [tunnel.last_error];
  if (snapshot.mcp?.state === "failed") candidates.push(snapshot.mcp.error);
  if (snapshot.state === "failed") candidates.push(snapshot.last_error);
  if (page === "coding" && ["failed", "unavailable"].includes(snapshot.codex?.state || "")) candidates.push(snapshot.codex?.last_error);
  if (page === "agent") {
    if (snapshot.agent_provider?.state === "failed") candidates.push(snapshot.agent_provider.last_error);
    if (snapshot.agent_enabled && snapshot.agent?.bridge_state === "OFFLINE") candidates.push(snapshot.agent.last_error);
  }
  return candidates.map(errorCode).find(Boolean) || "";
}

function statusClass(value?: string) {
  const state = (value || "unknown").toLowerCase();
  if (["running", "ready", "active", "completed"].includes(state)) return "ok";
  if (["busy", "claimed", "queued", "starting", "restarting"].includes(state)) return "busy";
  if (["failed", "unavailable", "offline", "blocked", "missing_credentials"].includes(state)) return "bad";
  return "muted";
}

function stateLabel(value?: string) {
  switch ((value || "unknown").toLowerCase()) {
    case "running": return "运行中";
    case "ready": return "就绪";
    case "active": return "活动";
    case "completed": return "已完成";
    case "busy": return "忙碌";
    case "claimed": return "已领取";
    case "queued": return "排队中";
    case "starting": return "启动中";
    case "restarting": return "正在重连";
    case "stopped": return "已停止";
    case "stopping": return "停止中";
    case "failed": return "失败";
    case "unavailable": return "不可用";
    case "offline": return "离线";
    case "blocked": return "已阻止";
    case "missing_credentials": return "缺少凭据";
    case "disabled": return "未启用";
    case "unknown": return "未知";
    default: return value || "未知";
  }
}

export default function App() {
  const [page, setPage] = useState<Page>("coding");
  const [snapshot, setSnapshot] = useState<Snapshot>(emptySnapshot);
  const [connections, setConnections] = useState<Connections>({});
  const [codingTunnel, setCodingTunnel] = useState<TunnelInfo>({});
  const [agentTunnel, setAgentTunnel] = useState<TunnelInfo>({});
  const [codingTunnelID, setCodingTunnelID] = useState("");
  const [codingTunnelKey, setCodingTunnelKey] = useState("");
  const [agentTunnelID, setAgentTunnelID] = useState("");
  const [agentTunnelKey, setAgentTunnelKey] = useState("");
  const [working, setWorking] = useState(false);
  const [workspaceManagerOpen, setWorkspaceManagerOpen] = useState(false);
  const [confirmState, setConfirmState] = useState<ConfirmState | null>(null);
  const [confirmWorking, setConfirmWorking] = useState(false);
  const [actionError, setActionError] = useState("");
  const [refreshError, setRefreshError] = useState("");
  const refreshRun = useRef<{ token: symbol; promise: Promise<void> } | null>(null);
  const refreshPending = useRef(false);

  const refresh = useCallback(() => {
    const active = refreshRun.current;
    if (active) {
      refreshPending.current = true;
      return active.promise;
    }
    const token = Symbol("refresh");
    const promise = (async () => {
      try {
        do {
          refreshPending.current = false;
          const [nextValue, endpoints] = await Promise.all([
            RuntimeSnapshot(),
            ConnectionInfo(),
          ]);
          const next = nextValue as Snapshot;
          setSnapshot(next);
          setConnections(endpoints as Connections);
          const nextCoding = normalizeTunnel(next.openai_tunnel);
          const nextAgent = normalizeTunnel(next.agent_openai_tunnel);
          setCodingTunnel(nextCoding);
          setAgentTunnel(nextAgent);
          setCodingTunnelID((current) => current || nextCoding.tunnel_id || "");
          setAgentTunnelID((current) => current || nextAgent.tunnel_id || "");
          setRefreshError("");
        } while (refreshPending.current);
      } finally {
        if (refreshRun.current?.token === token) refreshRun.current = null;
      }
    })();
    refreshRun.current = { token, promise };
    return promise;
  }, []);

  useEffect(() => {
    const update = () => void refresh().catch((error) => setRefreshError(errorCode(error)));
    update();
    const timer = window.setInterval(update, 1500);
    return () => window.clearInterval(timer);
  }, [refresh]);

  async function copy(value?: string, label = "已复制") {
    if (!value) return;
    await navigator.clipboard.writeText(value);
  }

  async function action(run: () => Promise<unknown>, success: string) {
    if (working) return false;
    setWorking(true);
    setActionError("");
    try {
      await run();
      await refresh();
      return true;
    } catch (error) {
      setActionError(errorCode(error));
      console.error(success, error);
      return false;
    } finally {
      setWorking(false);
    }
  }

  function askConfirmation(request: ConfirmState) {
    if (working || confirmWorking) return;
    setConfirmState(request);
  }

  async function acceptConfirmation() {
    if (!confirmState || confirmWorking) return;
    const request = confirmState;
    setConfirmWorking(true);
    setConfirmState(null);
    try {
      await request.onConfirm();
    } finally {
      setConfirmWorking(false);
    }
  }

  async function copyAgentKey() {
    try {
      const credential = await AgentCredential() as { api_key?: string };
      await copy(credential.api_key, "Agent API 密钥已复制");
    } catch (error) {
      setActionError(errorCode(error));
      console.error("Agent API 密钥复制失败", error);
    }
  }

  async function configureCodingTunnel() {
    if (await action(() => ConfigureOpenAITunnel(codingTunnelID, codingTunnelKey), "Coding 隧道已连接")) {
      setCodingTunnelKey("");
    }
  }

  function clearCodingTunnel() {
    askConfirmation({
      title: "断开 Coding 隧道？",
      description: "将断开 Coding 的 ChatGPT MCP 隧道，并从 Windows 凭据管理器删除对应 Runtime API key。",
      confirmLabel: "断开隧道",
      onConfirm: async () => {
        if (await action(() => ClearOpenAITunnel(), "Coding 隧道已断开")) {
          setCodingTunnelID("");
          setCodingTunnelKey("");
        }
      },
    });
  }

  async function configureAgentTunnel() {
    if (await action(() => ConfigureAgentOpenAITunnel(agentTunnelID, agentTunnelKey), "Agent 隧道已连接")) {
      setAgentTunnelKey("");
    }
  }

  function clearAgentTunnel() {
    askConfirmation({
      title: "断开 Agent 隧道？",
      description: "将断开 Agent 的 ChatGPT MCP 隧道，并从 Windows 凭据管理器删除对应 Runtime API key。",
      confirmLabel: "断开隧道",
      onConfirm: async () => {
        if (await action(() => ClearAgentOpenAITunnel(), "Agent 隧道已断开")) {
          setAgentTunnelID("");
          setAgentTunnelKey("");
        }
      },
    });
  }

  function deleteWorkspace(repository: string) {
    askConfirmation({
      title: "删除持久化工作区？",
      description: `将删除 ${repository} 的本地工作区。tracked、untracked 和 ignored 文件都会被移除；下一次 coding_open 会重新克隆。`,
      confirmLabel: "删除并重建",
      onConfirm: async () => { await action(() => DeleteWorkspace(repository), `工作区已移除：${repository}`); },
    });
  }

  function changeRemoteGitRewrite(allowed: boolean) {
    if (!allowed) {
      void action(() => UpdateCodexRemoteGitRewrite(false), "远程 Git 重写已禁用");
      return;
    }
    askConfirmation({
      title: "启用远程 Git 重写？",
      description: "将允许 force push 与远程 branch/tag 删除。危险 transport、receive-pack 注入和 CWapi safety refs 仍会被拒绝。",
      confirmLabel: "启用高级能力",
      onConfirm: async () => { await action(() => UpdateCodexRemoteGitRewrite(true), "远程 Git 重写已启用"); },
    });
  }

  const access = snapshot.codex_access_profile || "safe";
  const remoteGitRewrite = Boolean(snapshot.codex_remote_git_rewrite);
  const bridge = snapshot.agent?.bridge_state || "offline";
  const workspaces = snapshot.workspaces?.repositories || [];
  const issue = actionError || refreshError || snapshotIssue(page, snapshot, page === "coding" ? codingTunnel : agentTunnel);
  return (
    <main className="shell">
      <header className="titlebar">
        <div className="brand-row">
          <div className="logo">CW</div>
          <div><strong>CWapi</strong><span>2.0.4</span></div>
        </div>
        <button className="window-button" onClick={WindowHide} aria-label="缩小到托盘" title="缩小到托盘">×</button>
      </header>

      <nav className="page-tabs" aria-label="功能页面" role="tablist">
        <PageTab page="coding" activePage={page} label="Coding" detail="代码与工作区" onSelect={setPage} status={snapshot.coding?.state} />
        <PageTab page="agent" activePage={page} label="Agent" detail="Web GPT 服务" onSelect={setPage} status={snapshot.agent?.bridge_state} />
      </nav>

      <div className="page-content" role="tabpanel" aria-label={`${page} 页面`}>
        {page === "coding" ? (
          <CodingPage
            snapshot={snapshot}
            tunnel={codingTunnel}
            tunnelID={codingTunnelID}
            tunnelKey={codingTunnelKey}
            working={working}
            workspaces={workspaces}
            access={access}
            networkAccess={Boolean(snapshot.codex_network_access)}
            remoteGitRewrite={remoteGitRewrite}
            onTunnelIDChange={setCodingTunnelID}
            onTunnelKeyChange={setCodingTunnelKey}
            onConfigureTunnel={configureCodingTunnel}
            onClearTunnel={clearCodingTunnel}
            onAccessChange={(next) => void action(() => UpdateCodexAccessProfile(next), next === "safe" ? "Codex 已切换为安全模式" : "Codex 已切换为完整模式")}
            onNetworkAccessChange={(allowed) => void action(() => UpdateCodexNetworkAccess(allowed), allowed ? "Coding 网络访问已启用" : "Coding 网络访问已禁用")}
            onRemoteGitRewriteChange={changeRemoteGitRewrite}
            onOpenWorkspaceManager={() => setWorkspaceManagerOpen(true)}
          />
        ) : (
          <AgentPage
            snapshot={snapshot}
            connections={connections}
            tunnel={agentTunnel}
            tunnelID={agentTunnelID}
            tunnelKey={agentTunnelKey}
            working={working}
            bridge={bridge}
            onCopy={copy}
            onTunnelIDChange={setAgentTunnelID}
            onTunnelKeyChange={setAgentTunnelKey}
            onConfigureTunnel={configureAgentTunnel}
            onClearTunnel={clearAgentTunnel}
            onEnabledChange={(enabled) => void action(() => SetAgentEnabled(enabled), enabled ? "Agent 已启用" : "Agent 已停用")}
            onCopyAgentKey={() => void copyAgentKey()}
            onRegenerateAgentKey={() => void action(() => RegenerateAgentAPIKey(), "Agent API 密钥已重新生成")}
          />
        )}
      </div>

      {issue && <div className="current-error" role="alert"><span>当前错误</span><code>{issue}</code></div>}
      <footer>Coding 与 Agent 相互独立，分别在对应页面配置和管理。</footer>

      {workspaceManagerOpen && (
        <WorkspaceManager
          snapshot={snapshot}
          workspaces={workspaces}
          working={working}
          onClose={() => setWorkspaceManagerOpen(false)}
          onDelete={deleteWorkspace}
        />
      )}

      {confirmState && <ConfirmDialog request={confirmState} working={confirmWorking} onCancel={() => setConfirmState(null)} onConfirm={() => void acceptConfirmation()} />}
    </main>
  );
}

function PageTab({ page, activePage, label, detail, status, onSelect }: { page: Page; activePage: Page; label: string; detail: string; status?: string; onSelect: (page: Page) => void }) {
  const active = page === activePage;
  return (
    <button className={`page-tab ${active ? "active" : ""}`} role="tab" aria-selected={active} onClick={() => onSelect(page)}>
      <span className={`service-mark ${page}`}>{page === "coding" ? "C" : "A"}</span>
      <span className="page-tab-copy"><strong>{label}</strong><small>{detail}</small></span>
      <span className={`tab-dot ${statusClass(status)}`} role="img" aria-label={`${label} 状态：${stateLabel(status)}`} title={`${label} 状态：${stateLabel(status)}`} />
    </button>
  );
}

function CodingPage({ snapshot, tunnel, tunnelID, tunnelKey, working, workspaces, access, networkAccess, remoteGitRewrite, onTunnelIDChange, onTunnelKeyChange, onConfigureTunnel, onClearTunnel, onAccessChange, onNetworkAccessChange, onRemoteGitRewriteChange, onOpenWorkspaceManager }: {
  snapshot: Snapshot;
  tunnel: TunnelInfo;
  tunnelID: string;
  tunnelKey: string;
  working: boolean;
  workspaces: string[];
  access: string;
  networkAccess: boolean;
  remoteGitRewrite: boolean;
  onTunnelIDChange: (value: string) => void;
  onTunnelKeyChange: (value: string) => void;
  onConfigureTunnel: () => Promise<void>;
  onClearTunnel: () => void;
  onAccessChange: (value: string) => void;
  onNetworkAccessChange: (allowed: boolean) => void;
  onRemoteGitRewriteChange: (allowed: boolean) => void;
  onOpenWorkspaceManager: () => void;
}) {
  return (
    <>
      <TunnelCard page="coding" tunnel={tunnel} tunnelID={tunnelID} tunnelKey={tunnelKey} working={working} onTunnelIDChange={onTunnelIDChange} onTunnelKeyChange={onTunnelKeyChange} onConfigure={onConfigureTunnel} onClear={onClearTunnel} />
      <section className="card">
        <div className="card-title"><span>CODEX</span><span className={`pill small ${statusClass(snapshot.codex?.state)}`}>{stateLabel(snapshot.codex?.state)}</span></div>
        <div className="segmented">
          <button disabled={working} className={access === "safe" ? "selected" : ""} onClick={() => onAccessChange("safe")}>安全 SAFE</button>
          <button disabled={working} className={access === "full" ? "selected danger" : ""} onClick={() => onAccessChange("full")}>完整 FULL</button>
        </div>
        <div className="toggle-row">
          <div className="toggle-item"><span>网络访问</span><label className="switch"><input type="checkbox" aria-label="允许 Coding 命令访问网络" checked={networkAccess} disabled={working} onChange={(event) => onNetworkAccessChange(event.target.checked)} /><span /></label></div>
          <div className="toggle-item"><span>远程Git重写</span><label className="switch"><input type="checkbox" aria-label="允许远程 Git 重写" checked={remoteGitRewrite} disabled={working} onChange={(event) => onRemoteGitRewriteChange(event.target.checked)} /><span /></label></div>
        </div>
        <div className="stats-row">
          <StatRow label="活动会话" value={snapshot.coding?.active ?? 0} />
          <StatRow label="工作区仓库" value={snapshot.workspaces?.repository_count ?? 0} />
        </div>
        {(snapshot.coding?.repositories || []).slice(0, 2).map((repo) => <div className="repo" key={repo}>{repo}</div>)}
        <button className="text-button" disabled={working} onClick={onOpenWorkspaceManager}>管理工作区</button>
      </section>
    </>
  );
}

function AgentPage({ snapshot, connections, tunnel, tunnelID, tunnelKey, working, bridge, onCopy, onTunnelIDChange, onTunnelKeyChange, onConfigureTunnel, onClearTunnel, onEnabledChange, onCopyAgentKey, onRegenerateAgentKey }: {
  snapshot: Snapshot;
  connections: Connections;
  tunnel: TunnelInfo;
  tunnelID: string;
  tunnelKey: string;
  working: boolean;
  bridge: string;
  onCopy: (value?: string, label?: string) => void | Promise<void>;
  onTunnelIDChange: (value: string) => void;
  onTunnelKeyChange: (value: string) => void;
  onConfigureTunnel: () => Promise<void>;
  onClearTunnel: () => void;
  onEnabledChange: (enabled: boolean) => void;
  onCopyAgentKey: () => void;
  onRegenerateAgentKey: () => void;
}) {
  return (
    <>
      <TunnelCard page="agent" tunnel={tunnel} tunnelID={tunnelID} tunnelKey={tunnelKey} working={working} onTunnelIDChange={onTunnelIDChange} onTunnelKeyChange={onTunnelKeyChange} onConfigure={onConfigureTunnel} onClear={onClearTunnel} />
      <section className="card">
        <div className="card-title"><span>AGENT 服务</span><label className="switch"><input type="checkbox" aria-label="启用 Agent 服务" checked={Boolean(snapshot.agent_enabled)} disabled={working} onChange={(event) => onEnabledChange(event.target.checked)} /><span /></label></div>
        <StatRow label="服务提供商" value={stateLabel(snapshot.agent_provider?.state || "disabled")} status={snapshot.agent_provider?.state} />
        <StatRow label="Web GPT" value={stateLabel(bridge)} status={bridge} />
        <div className="triple">
          <Metric label="等待中" value={snapshot.agent?.pending ?? 0} />
          <Metric label="已领取" value={snapshot.agent?.claimed ?? 0} />
          <Metric label="已完成" value={snapshot.agent?.completed ?? 0} />
        </div>
        {snapshot.agent_enabled && <EndpointRow label="服务提供商地址" value={connections.agent_provider || "启动中…"} onCopy={() => onCopy(connections.agent_provider, "服务提供商地址已复制")} />}
        <div className="actions">
          <button disabled={working || !snapshot.agent_enabled} onClick={onCopyAgentKey}>复制 API 密钥</button>
          <button disabled={working || !snapshot.agent_enabled} onClick={onRegenerateAgentKey}>重新生成密钥</button>
        </div>
      </section>
    </>
  );
}

function TunnelCard({ page, tunnel, tunnelID, tunnelKey, working, onTunnelIDChange, onTunnelKeyChange, onConfigure, onClear }: { page: Page; tunnel: TunnelInfo; tunnelID: string; tunnelKey: string; working: boolean; onTunnelIDChange: (value: string) => void; onTunnelKeyChange: (value: string) => void; onConfigure: () => Promise<void>; onClear: () => void }) {
  const label = page === "coding" ? "Coding" : "Agent";
  return (
    <section className="card">
      <div className="card-title"><span>CHATGPT MCP 隧道</span><span className="pill small ok">{label} 独立链路</span></div>
      <TunnelPanel label={label} info={tunnel} working={working} tunnelID={tunnelID} tunnelKey={tunnelKey} onTunnelIDChange={onTunnelIDChange} onTunnelKeyChange={onTunnelKeyChange} onConfigure={onConfigure} onClear={onClear} />
    </section>
  );
}

function TunnelPanel({ label, info, working, tunnelID, tunnelKey, onTunnelIDChange, onTunnelKeyChange, onConfigure, onClear }: { label: string; info: TunnelInfo; working: boolean; tunnelID: string; tunnelKey: string; onTunnelIDChange: (value: string) => void; onTunnelKeyChange: (value: string) => void; onConfigure: () => Promise<void>; onClear: () => void }) {
  const enabled = Boolean(info.enabled);
  const state = info.state || (enabled ? "starting" : "disabled");
  return (
    <div className="tunnel-panel">
      <div className="tunnel-panel-title"><strong>{label} 插件</strong><span className={`pill small ${statusClass(state)}`}>{stateLabel(state)}</span></div>
      <input className="tunnel-input" aria-label={`${label} 隧道 ID`} value={tunnelID} onChange={(event) => onTunnelIDChange(event.target.value)} placeholder="tunnel_…" autoComplete="off" />
      <input className="tunnel-input" aria-label={`${label} Runtime API key`} type="password" value={tunnelKey} onChange={(event) => onTunnelKeyChange(event.target.value)} placeholder={info.api_key_present ? "Runtime API key 已保存" : "sk-…"} autoComplete="new-password" />
      <div className="actions"><button disabled={working || !tunnelID || !tunnelKey} onClick={() => void onConfigure()}>保存并连接 {label}</button><button disabled={working || !enabled} onClick={onClear}>断开 {label} 隧道</button></div>
    </div>
  );
}

function WorkspaceManager({ snapshot, workspaces, working, onClose, onDelete }: { snapshot: Snapshot; workspaces: string[]; working: boolean; onClose: () => void; onDelete: (repository: string) => void }) {
  return (
    <div className="overlay" role="dialog" aria-modal="true" aria-label="工作区管理">
      <div className="manager">
        <div className="manager-head"><div><p className="eyebrow">CODING 维护</p><h2>持久化工作区</h2></div><button onClick={onClose} aria-label="关闭工作区管理">×</button></div>
        <p className="manager-note">删除工作区会移除本地源码、未跟踪文件和构建缓存。下一次 coding_open 时会重新创建。</p>
        <div className="workspace-list">
          {workspaces.length === 0 && <div className="empty">暂无持久化工作区。</div>}
          {workspaces.map((repo) => <div className="workspace-item" key={repo}><code>{repo}</code><button disabled={working || (snapshot.coding?.active ?? 0) > 0} onClick={() => onDelete(repo)}>删除并重建</button></div>)}
        </div>
        {(snapshot.workspaces?.invalid_entries ?? 0) > 0 && <div className="warning">无效工作区元数据条目：{snapshot.workspaces?.invalid_entries}</div>}
      </div>
    </div>
  );
}

function ConfirmDialog({ request, working, onCancel, onConfirm }: { request: ConfirmState; working: boolean; onCancel: () => void; onConfirm: () => void }) {
  return (
    <div className="overlay confirm-overlay" role="presentation">
      <div className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="confirm-title" aria-describedby="confirm-description">
        <div className="confirm-icon">!</div>
        <p className="eyebrow">需要确认</p>
        <h2 id="confirm-title">{request.title}</h2>
        <p id="confirm-description" className="confirm-description">{request.description}</p>
        <div className="confirm-actions"><button className="cancel-button" disabled={working} onClick={onCancel}>取消</button><button className="danger-button" disabled={working} onClick={onConfirm}>{working ? "处理中…" : request.confirmLabel}</button></div>
      </div>
    </div>
  );
}

function EndpointRow({ label, value, onCopy }: { label: string; value: string; onCopy: () => void | Promise<void> }) {
  return <div className="endpoint-row"><div><span>{label}</span><code>{value}</code></div><button onClick={() => void onCopy()}>复制</button></div>;
}
function StatRow({ label, value, status }: { label: string; value: string | number; status?: string }) {
  return <div className="stat-row"><span>{label}</span><strong className={status ? statusClass(status) : ""}>{value}</strong></div>;
}
function Metric({ label, value }: { label: string; value: number }) {
  return <div className="metric"><strong>{value}</strong><span>{label}</span></div>;
}
