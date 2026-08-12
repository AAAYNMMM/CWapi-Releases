import { invoke } from "@tauri-apps/api/core";
import {
  beginBlockingOperation,
  endBlockingOperation,
  publishDesktopStatus,
} from "./blocking-events";

export interface DesktopStatus {
  app_root: string;
  data_root: string;
  config_path: string | null;
  backend_running: boolean;
  backend_pid: number | null;
  backend_url: string | null;
  backend_started_at_epoch_ms: number | null;
  setup_required: boolean;
  startup_error: string | null;
}

export interface BackendHealth {
  schema: string;
  status: string;
  pid: number;
  runner_id: string;
  started_at: string;
  transport: {
    state: string;
    pid: number | null;
    url: string | null;
    version: string | null;
    authentication_enabled: boolean;
    authorization_required: boolean;
  };
}

export type RuntimeStateName =
  | "unavailable" | "healthy" | "working" | "waiting"
  | "retrying" | "warning" | "failed" | "stopped";

export interface RuntimeLamp {
  color: "gray" | "green" | "blue" | "yellow" | "red";
  animation: "static" | "chase";
  fill: "empty" | "solid";
}

export interface RuntimeComponent {
  name: string;
  state: RuntimeStateName;
  enabled: boolean;
  detail: string;
  lamp: RuntimeLamp;
  task_id?: string | null;
  pid?: number | null;
  generation?: number;
  startup_count?: number;
  active_operations?: number;
  authorization_required?: boolean;
}

export interface RuntimeStateSnapshot {
  schema: "cwapi.runtime.state.v1" | string;
  generated_at: string;
  overall: string;
  current_task_id: string | null;
  components: Record<string, RuntimeComponent>;
}

export interface LiveEvent {
  id: string;
  source: string;
  type: string;
  status: string | null;
  timestamp: string | null;
  message: string;
  data: Record<string, unknown>;
}

export interface LiveStream {
  id: string;
  step_id: string;
  stream: "stdout" | "stderr" | string;
  path: string;
  size_bytes: number;
  start_offset: number;
  end_offset: number;
  text: string;
  truncated: boolean;
}

export interface ExecutionLiveSnapshot {
  schema: "cwapi.execution.live.v1" | string;
  task_id: string | null;
  task?: Record<string, unknown>;
  events: LiveEvent[];
  streams: LiveStream[];
  trace_current: {
    updated_at?: string | null;
    active?: { function?: string; file?: string; line?: number } | null;
  } | null;
  bounded?: { max_events: number; tail_bytes: number };
}

export interface TaskSummary {
  task_id: string;
  repository: string;
  expected_commit: string;
  actual_commit?: string | null;
  execution_status: string;
  result_status: string;
  received_at: string;
  finished_at?: string | null;
  last_error?: string | null;
  progress_status?: string | null;
  source_draft_id?: string | null;
  workspace_path?: string | null;
  artifact_path?: string | null;
  cancel_requested?: boolean;
  actions?: string[];
  step_count?: number;
}

export interface TaskDetail {
  task: Record<string, unknown>;
  task_payload?: Record<string, unknown> | null;
  steps: Array<Record<string, any>>;
  outbox?: Record<string, unknown> | null;
  result_payload?: Record<string, unknown> | null;
  artifact?: Record<string, unknown> | null;
  workspaces?: Array<Record<string, unknown>>;
}

export interface ProjectSnapshot {
  repository: string;
  name: string;
  path: string;
  remote_url: string;
  python_executable: string;
  cargo_executable: string;
  default_test_paths: string[];
  allow_dependency_check: boolean;
  allowed: boolean;
  git: Record<string, any>;
  workspaces: Array<Record<string, unknown>>;
}

export interface WorkbenchSnapshot {
  schema: "cwapi.workbench.v1" | string;
  generated_at: string;
  projects: ProjectSnapshot[];
  capabilities: Array<Record<string, unknown>>;
  environment: {
    paths: Record<string, unknown>;
    tools: Record<string, any>;
    drive_enabled: boolean;
  };
  configuration: Record<string, unknown>;
  diagnostics: Array<{ name: string; ok: boolean; detail: string }>;
  other: Record<string, any>;
}

export interface SetupResult {
  cancelled: boolean;
  already_configured?: boolean;
  setup?: {
    schema: string;
    account: string;
    config_path: string;
    credentials_present: boolean;
    token_present: boolean;
  };
  status?: DesktopStatus;
}

export interface ManagementSnapshot {
  schema: string;
  generated_at: string;
  active_task_id: string | null;
  accepting_new_tasks: boolean;
  restart_required: boolean;
  pending: { config: boolean; capability_policy: boolean; blocks_new_tasks: boolean };
  config: { revision: string; editable: Record<string, any>; read_only: Record<string, unknown> };
  capability_policy: { revision: string; editable: Record<string, any>; effective_network: Record<string, any>; path: string };
}

export interface DoctorSnapshot {
  schema: string;
  generated_at: string;
  overall: string;
  authorization: Record<string, any>;
  checks: Array<{ name: string; ok: boolean; detail: string; category: string }>;
}

const inFlightReads = new Map<string, Promise<unknown>>();

function requestKey(command: string, args?: Record<string, unknown>): string {
  return `${command}:${JSON.stringify(args ?? {})}`;
}

function invokeShared<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  const key = requestKey(command, args);
  const existing = inFlightReads.get(key) as Promise<T> | undefined;
  if (existing) return existing;

  const pending = invoke<T>(command, args);
  inFlightReads.set(key, pending);
  const cleanup = () => {
    if (inFlightReads.get(key) === pending) inFlightReads.delete(key);
  };
  pending.then(cleanup, cleanup);
  return pending;
}

async function invokeBlocking<T>(
  message: string,
  command: string,
  args?: Record<string, unknown>,
): Promise<T> {
  const id = beginBlockingOperation(message);
  try {
    return await invoke<T>(command, args);
  } finally {
    endBlockingOperation(id);
  }
}

export const desktopStatus = async () => {
  const status = await invokeShared<DesktopStatus>("desktop_status");
  publishDesktopStatus(status);
  return status;
};
export const desktopFrontendReady = () => invoke("desktop_frontend_ready");
export const backendHealth = () => invokeShared<BackendHealth>("backend_health");
export const backendTasks = (limit = 100) => invokeShared<{ tasks: TaskSummary[] }>("backend_tasks", { limit });
export const backendTask = (taskId: string) => invokeShared<TaskDetail>("backend_task", { taskId });
export const backendCurrentExecution = () => invokeShared("backend_current_execution");
export const backendRuntimeState = () => invokeShared<RuntimeStateSnapshot>("backend_runtime_state");
export const backendExecutionEvents = (
  taskId?: string | null,
  limit = 300,
  tailBytes = 32768,
) => invokeShared<ExecutionLiveSnapshot>("backend_execution_events", {
  taskId: taskId ?? null,
  limit,
  tailBytes,
});
export const backendWorkbench = () => invokeShared<WorkbenchSnapshot>("backend_workbench");
export const backendConfig = () => invokeShared("backend_config");
export const backendValidateConfig = () => invokeBlocking("正在检查配置…", "backend_validate_config");
export const backendProcesses = () => invokeShared("backend_processes");
export const desktopProcesses = () => invokeShared("desktop_processes");
export const backendAuthorizeGmail = () => invokeBlocking("等待浏览器完成 Gmail 授权…", "backend_authorize_gmail");
export const setupPickCredentials = () => invokeBlocking<SetupResult>("正在配置 Gmail，请在浏览器中完成授权…", "setup_pick_credentials");
export const desktopPickDirectory = () => invokeBlocking<{ cancelled: boolean; path: string | null }>("正在等待文件夹选择…", "desktop_pick_directory");
export const desktopReplaceGmailCredentials = () => invokeBlocking<Record<string, any>>("正在更换 OAuth 配置并等待浏览器授权…", "desktop_replace_gmail_credentials");
export const desktopRevealPath = (path: string) => invokeBlocking<{ opened: boolean }>("正在打开位置…", "desktop_reveal_path", { path });
export const desktopRequestExit = () => invoke("desktop_request_exit");
export const backendCancelTask = (taskId: string, reason?: string) => invokeBlocking("正在请求取消任务…", "backend_cancel_task", { taskId, reason });
export const backendManagement = () => invokeShared<ManagementSnapshot>("backend_management");
export const backendDoctor = () => invokeBlocking<DoctorSnapshot>("正在检查系统…", "backend_doctor");
export const backendValidateSettings = (kind: string, editable: Record<string, any>) => invokeBlocking<{ valid: boolean; diff: Array<Record<string, unknown>> }>("正在检查设置…", "backend_validate_settings", { payload: { kind, editable } });
export const backendSaveSettings = (kind: string, revision: string, editable: Record<string, any>) => invokeBlocking<Record<string, any>>("正在应用设置…", "backend_save_settings", { payload: { kind, revision, editable } });
export const backendMaintenance = (action: string, taskId?: string | null) => invokeBlocking<Record<string, any>>(
  action === "cleanup" ? "正在清理临时文件…" : "正在执行维护操作…",
  "backend_maintenance",
  { action, taskId: taskId ?? null },
);
export const backendGmailStatus = () => invokeShared<Record<string, any>>("backend_gmail_status");
export const desktopRestartBackend = () => invokeBlocking<DesktopStatus>("正在重新启动后台服务…", "desktop_restart_backend");
export const desktopRemoveGmailAuthorization = () => invokeBlocking<Record<string, any>>("正在移除 Gmail 授权并重新启动后台服务…", "desktop_remove_gmail_authorization");
