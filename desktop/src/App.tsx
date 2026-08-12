import { useCallback, useEffect, useState } from "react";
import {
  backendAuthorizeGmail,
  backendCancelTask,
  backendExecutionEvents,
  backendHealth,
  backendRuntimeState,
  backendTask,
  backendTasks,
  backendWorkbench,
  desktopFrontendReady,
  desktopStatus,
  setupPickCredentials,
  type BackendHealth,
  type DesktopStatus,
  type ExecutionLiveSnapshot,
  type RuntimeStateSnapshot,
  type TaskDetail,
  type TaskSummary,
  type WorkbenchSnapshot,
} from "./ipc";
import { Workbench, type SectionId } from "./workbench";

const SECTION_KEY = "cwapi.last-section.v1";
const VALID_SECTIONS = new Set<SectionId>([
  "console",
  "projects",
  "settings",
  "diagnostics",
  "about",
]);

function initialSection(): SectionId {
  const saved = localStorage.getItem(SECTION_KEY);
  if (saved === "execution") return "console";
  return saved && VALID_SECTIONS.has(saved as SectionId) ? (saved as SectionId) : "console";
}

export default function App() {
  const [desktop, setDesktop] = useState<DesktopStatus | null>(null);
  const [health, setHealth] = useState<BackendHealth | null>(null);
  const [runtime, setRuntime] = useState<RuntimeStateSnapshot | null>(null);
  const [live, setLive] = useState<ExecutionLiveSnapshot | null>(null);
  const [tasks, setTasks] = useState<TaskSummary[]>([]);
  const [workbench, setWorkbench] = useState<WorkbenchSnapshot | null>(null);
  const [section, setSection] = useState<SectionId>(initialSection);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [taskDetail, setTaskDetail] = useState<TaskDetail | null>(null);
  const [taskLive, setTaskLive] = useState<ExecutionLiveSnapshot | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [setupBusy, setSetupBusy] = useState(false);
  const [setupMessage, setSetupMessage] = useState<string | null>(null);
  const [authBusy, setAuthBusy] = useState(false);

  useEffect(() => {
    localStorage.removeItem("cwapi.last-task.v1");
    void desktopFrontendReady().catch(() => undefined);
  }, []);

  useEffect(() => {
    localStorage.setItem(SECTION_KEY, section);
  }, [section]);

  const refreshFast = useCallback(async () => {
    const status = await desktopStatus();
    setDesktop(status);
    if (!status.backend_running) {
      setHealth(null);
      setRuntime(null);
      setLive(null);
      return;
    }
    const [nextHealth, nextRuntime, nextLive] = await Promise.all([
      backendHealth(),
      backendRuntimeState(),
      backendExecutionEvents(null, 300, 131072),
    ]);
    setHealth(nextHealth);
    setRuntime(nextRuntime);
    setLive(nextLive);
  }, []);

  const refreshWorkbench = useCallback(async () => {
    if (!desktop?.backend_running) return;
    const [taskResult, nextWorkbench] = await Promise.all([
      backendTasks(150),
      backendWorkbench(),
    ]);
    setTasks(taskResult.tasks);
    setWorkbench(nextWorkbench);
    setSelectedTaskId((current) => {
      if (current && taskResult.tasks.some((task) => task.task_id === current)) return current;
      const active = taskResult.tasks.find((task) =>
        ["claimed", "running"].includes(String(task.execution_status ?? "").toLowerCase()),
      );
      return active?.task_id ?? null;
    });
  }, [desktop?.backend_running]);

  useEffect(() => {
    let cancelled = false;
    const tick = async () => {
      try {
        await refreshFast();
        if (!cancelled) setError(null);
      } catch (cause) {
        if (!cancelled) setError(String(cause));
      }
    };
    void tick();
    const timer = window.setInterval(() => void tick(), 1000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [refreshFast]);

  useEffect(() => {
    if (!desktop?.backend_running) return;
    let cancelled = false;
    const tick = async () => {
      try {
        await refreshWorkbench();
        if (!cancelled) setError(null);
      } catch (cause) {
        if (!cancelled) setError(String(cause));
      }
    };
    void tick();
    const timer = window.setInterval(() => void tick(), 5000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [desktop?.backend_running, refreshWorkbench]);

  useEffect(() => {
    if (!desktop?.backend_running || !selectedTaskId) {
      setTaskDetail(null);
      setTaskLive(null);
      return;
    }
    let cancelled = false;
    const tick = async () => {
      try {
        const [detail, selectedLive] = await Promise.all([
          backendTask(selectedTaskId),
          backendExecutionEvents(selectedTaskId, 250, 24576),
        ]);
        if (!cancelled) {
          setTaskDetail(detail);
          setTaskLive(selectedLive);
        }
      } catch (cause) {
        if (!cancelled) setError(String(cause));
      }
    };
    void tick();
    const timer = window.setInterval(() => void tick(), 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [desktop?.backend_running, selectedTaskId]);

  const startSetup = async () => {
    setSetupBusy(true);
    setError(null);
    setSetupMessage("选择 Google OAuth 配置文件后，CWapi 会打开默认浏览器，请在 Google 页面手动完成授权。");
    try {
      const result = await setupPickCredentials();
      if (result.cancelled) {
        setSetupMessage("未选择文件，首次设置未更改。");
        return;
      }
      setSetupMessage(result.setup?.account ? `Gmail 授权完成：${result.setup.account}` : "首次设置已完成。");
      if (result.status) {
        setDesktop(result.status);
      } else {
        await refreshFast();
      }
    } catch (cause) {
      setError(String(cause));
      setSetupMessage(null);
    } finally {
      setSetupBusy(false);
    }
  };

  const reauthorize = async () => {
    setAuthBusy(true);
    setError(null);
    try {
      await backendAuthorizeGmail();
      await refreshFast();
    } catch (cause) {
      setError(String(cause));
    } finally {
      setAuthBusy(false);
    }
  };

  const cancelTask = async (taskId: string) => {
    try {
      await backendCancelTask(taskId, "用户从 CWapi 执行页面请求取消任务。");
      await refreshWorkbench();
    } catch (cause) {
      setError(String(cause));
    }
  };

  if (desktop?.setup_required) {
    return (
      <main className="setup-shell">
        <section className="setup-card">
          <div className="brand-mark brand-mark-large">CW</div>
          <h1>连接 Gmail</h1>
          <p>选择 Google OAuth 配置文件。CWapi 负责打开默认浏览器和接收回调；登录、账号选择和授权同意始终由你本人完成。</p>
          <button
            autoFocus
            className="primary-button"
            type="button"
            disabled={setupBusy}
            onClick={() => void startSetup()}
          >
            {setupBusy ? "等待浏览器授权…" : "选择 Google OAuth 配置文件"}
          </button>
          {setupMessage && <p className="setup-message">{setupMessage}</p>}
          {desktop.startup_error && <p className="error-text">{desktop.startup_error}</p>}
          {error && <p className="error-text">{error}</p>}
        </section>
      </main>
    );
  }

  return (
    <>
      <Workbench
        desktop={desktop}
        section={section}
        onSection={setSection}
        runtime={runtime}
        health={health}
        tasks={tasks}
        workbench={workbench}
        live={live}
        selectedTaskId={selectedTaskId}
        taskDetail={taskDetail}
        taskLive={taskLive}
        onSelectTask={(id) => {
          setSelectedTaskId(id);
        }}
        onCancelTask={(id) => void cancelTask(id)}
      />
      {health?.transport.authorization_required && (
        <div className="reauth-banner">
          <div>
            <strong>Gmail 授权需要重新确认</strong>
            <span>自动刷新已无法恢复，需要在默认浏览器中手动完成授权。</span>
          </div>
          <button type="button" disabled={authBusy} onClick={() => void reauthorize()}>
            {authBusy ? "等待浏览器…" : "重新授权 Gmail"}
          </button>
        </div>
      )}
      {error && <div className="global-error">{error}</div>}
    </>
  );
}
