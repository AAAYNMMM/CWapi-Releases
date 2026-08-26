export type ProbeConfig = {
  mode: "first-run" | "workbench";
  source_commit?: string;
};

export type ProbeResult = {
  mode: ProbeConfig["mode"];
  success: boolean;
  checks: string[];
  error?: string;
};

const delay = (milliseconds: number) => new Promise((resolve) => window.setTimeout(resolve, milliseconds));

async function waitFor<T>(read: () => T | null | undefined | false, label: string, timeout = 15000): Promise<T> {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = read();
    if (value) return value;
    await delay(100);
  }
  throw new Error(`GUI_PROBE_TIMEOUT:${label}`);
}

function heading(label: string): HTMLElement | undefined {
  return Array.from(document.querySelectorAll("h1,h2")).find((entry) => entry.textContent?.trim() === label) as HTMLElement | undefined;
}

async function probeFirstRun(checks: string[]) {
  await waitFor(() => heading("连接 Slack"), "first-run-heading");
  await waitFor(() => document.querySelector('[data-testid="single-page"]'), "single-page");
  for (const label of ["App Token", "Bot Token", "Channel ID"]) {
    if (!document.querySelector(`input[aria-label="${label}"]`)) throw new Error(`GUI_PROBE_FIRST_RUN_FIELD_MISSING:${label}`);
  }
  if (!document.body.textContent?.includes("v1.6.2")) throw new Error("GUI_PROBE_FIRST_RUN_VERSION_MISSING");
  checks.push("single-page", "first-run-slack", "version-1.6.2");
}

async function probeWorkbench(checks: string[]) {
  await waitFor(() => heading("执行状态"), "execution-heading");
  const shell = await waitFor(() => document.querySelector<HTMLElement>('[data-testid="single-page"]'), "single-page");
  const titlebar = await waitFor(() => document.querySelector<HTMLElement>('[data-testid="titlebar"]'), "transparent-titlebar");
  const latest = await waitFor(() => document.querySelector<HTMLElement>('[data-testid="latest-record"]'), "latest-record");
  await waitFor(() => document.querySelector('[data-testid="process-monitor"]'), "process-monitor");
  await waitFor(() => document.querySelector('[data-testid="slack-control"]'), "slack-control");
  const permission = document.querySelector('[role="switch"][aria-label="权限模式"]');
  if (!permission) throw new Error("GUI_PROBE_PERMISSION_SWITCH_MISSING");
  if (getComputedStyle(latest).overflowY !== "hidden") throw new Error("GUI_PROBE_LATEST_RECORD_SCROLLABLE");
  if (shell.getBoundingClientRect().width !== 375 || shell.getBoundingClientRect().height !== 690) throw new Error("GUI_PROBE_FIXED_SIZE_MISMATCH");
  if (getComputedStyle(titlebar).getPropertyValue("--wails-draggable").trim() !== "drag") throw new Error("GUI_PROBE_TITLEBAR_NOT_DRAGGABLE");
  for (const retired of ["控制台", "设置", "诊断", "关于", "CWapi 运行日志", "结构化执行日志", "GitHub CLI"]) {
    if (document.body.textContent?.includes(retired)) throw new Error(`GUI_PROBE_RETIRED_SURFACE_PRESENT:${retired}`);
  }
  checks.push("single-page", "fixed-375x690", "transparent-titlebar", "process-monitor", "latest-only", "permission-switch", "slack-inline");
}

export async function runGUIProbe(config: ProbeConfig): Promise<ProbeResult> {
  const checks: string[] = [];
  try {
    if (config.mode === "first-run") await probeFirstRun(checks);
    else if (config.mode === "workbench") await probeWorkbench(checks);
    else throw new Error(`GUI_PROBE_MODE_UNSUPPORTED:${String(config.mode)}`);
    return { mode: config.mode, success: true, checks };
  } catch (cause) {
    return { mode: config.mode, success: false, checks, error: String(cause) };
  }
}
