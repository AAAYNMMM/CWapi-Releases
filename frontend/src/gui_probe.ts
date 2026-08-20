type ProbeConfig = {
  mode: "first-run" | "workbench";
  project_path?: string;
  source_commit?: string;
};

type ProbeResult = {
  mode: ProbeConfig["mode"];
  success: boolean;
  checks: string[];
  error?: string;
};

const sleep = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms));

function safeDOMState(): string {
  const main = document.querySelector("main");
  const mainClass = main?.className || "missing";
  const headings = Array.from(document.querySelectorAll("h1,h2"))
    .map((entry) => entry.textContent?.trim() || "")
    .filter(Boolean)
    .slice(0, 10)
    .join("|");
  const alert = document.querySelector('[role="alert"]')?.textContent?.trim().slice(0, 300) || "";
  const loading = main?.querySelector("p")?.textContent?.trim().slice(0, 200) || "";
  return `main=${mainClass};headings=${headings || "none"};alert=${alert || "none"};status=${loading || "none"}`;
}

async function waitFor<T>(read: () => T | null | undefined | false, label: string, timeout = 20000): Promise<T> {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = read();
    if (value) return value as T;
    await sleep(100);
  }
  throw new Error(`GUI_PROBE_TIMEOUT:${label};${safeDOMState()}`);
}

function buttons(): HTMLButtonElement[] {
  return Array.from(document.querySelectorAll("button"));
}

function button(text: string): HTMLButtonElement | null {
  return buttons().find((entry) => entry.textContent?.trim() === text) ?? null;
}

function heading(text: string): HTMLElement | null {
  return Array.from(document.querySelectorAll("h1,h2")).find((entry) => entry.textContent?.trim() === text) as HTMLElement | null;
}

function input(label: string): HTMLInputElement | null {
  return document.querySelector(`input[aria-label="${label}"]`);
}

function setInput(element: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
  if (!setter) throw new Error("GUI_PROBE_INPUT_SETTER_MISSING");
  setter.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
  element.dispatchEvent(new Event("change", { bubbles: true }));
}

async function fill(label: string, value: string) {
  const element = await waitFor(() => input(label), `input:${label}`);
  setInput(element, value);
  await waitFor(() => element.value === value, `input-value:${label}`);
}

async function click(text: string) {
  const target = await waitFor(() => button(text), `button:${text}`);
  target.click();
  await sleep(100);
}

async function probeFirstRun(config: ProbeConfig, checks: string[]) {
  await waitFor(() => heading("连接 Slack"), "first-run-heading", 30000);
  checks.push("first-run-heading");
  const appToken = await waitFor(() => input("App Token"), "app-token");
  const botToken = await waitFor(() => input("Bot Token"), "bot-token");
  const channel = await waitFor(() => input("Channel ID"), "channel-id");
  const submit = await waitFor(() => button("测试连接并保存"), "slack-submit");
  if (!submit.disabled) throw new Error("GUI_PROBE_SLACK_SUBMIT_INITIAL_STATE");
  checks.push("first-run-controls");

  setInput(appToken, "xapp-cwapi-gui-probe-not-a-real-token");
  setInput(botToken, "xoxb-cwapi-gui-probe-not-a-real-token");
  setInput(channel, "C0123456789");
  await waitFor(() => !submit.disabled, "slack-submit-enabled");
  checks.push("controlled-inputs");

  if (document.body.textContent?.includes("Gmail")) throw new Error("GUI_PROBE_GMAIL_TEXT_PRESENT");
  if (config.source_commit && !document.body.textContent?.includes("CWapi v1.6.0")) throw new Error("GUI_PROBE_VERSION_TEXT_MISSING");
}

async function probeWorkbench(config: ProbeConfig, checks: string[]) {
  await waitFor(() => document.querySelector("main.app-shell"), "workbench-shell", 30000);
  for (const name of ["控制台", "项目", "设置", "诊断", "关于"]) await waitFor(() => button(name), `nav:${name}`);
  await click("控制台");
  await waitFor(() => heading("控制台"), "console-heading");
  checks.push("navigation");

  await waitFor(() => heading("组件运行状态"), "component-status");
  for (const label of ["CWapi 桌面程序", "Go Core / MCP Relay", "Slack Transport", "Codex MCP Toolhost"]) {
    if (!document.body.textContent?.includes(label)) throw new Error(`GUI_PROBE_COMPONENT_MISSING:${label}`);
  }
  if (document.body.textContent?.includes("Go Core Runner")) throw new Error("GUI_PROBE_LEGACY_RUNNER_LABEL_PRESENT");
  checks.push("mcp-console-shape");

  const requestSurface = document.querySelector('[data-testid="mcp-request-surface"]');
  if (!requestSurface) throw new Error("GUI_PROBE_MCP_REQUEST_SURFACE_MISSING");
  checks.push("mcp-request-surface");

  const structured = document.querySelector('[data-testid="structured-log-surface"]');
  const runtime = document.querySelector('[data-testid="runtime-log-surface"]');
  if (!structured) throw new Error("GUI_PROBE_STRUCTURED_LOG_SURFACE_MISSING");
  if (!runtime) throw new Error("GUI_PROBE_RUNTIME_LOG_SURFACE_MISSING");
  if (structured === runtime) throw new Error("GUI_PROBE_LOG_SURFACES_NOT_SEPARATE");
  checks.push("separate-log-surfaces");

  await click("项目");
  await waitFor(() => heading("项目"), "projects-heading");
  const projectPath = config.project_path?.trim();
  if (!projectPath) throw new Error("GUI_PROBE_PROJECT_PATH_REQUIRED");
  const repository = "AAAYNMMM/CWapi-GuiProbe";

  await click("＋ 添加项目");
  await fill("Display name", "CWapi GUI Probe");
  await fill("Local path", projectPath);
  await fill("Remote URL", `https://github.com/${repository}.git`);
  if (document.querySelector('input[aria-label="Allowed actions"]')) throw new Error("GUI_PROBE_ACTIONS_FIELD_VISIBLE");
  await click("保存");
  await waitFor(() => document.body.textContent?.includes(repository), "project-added");
  checks.push("project-add");

  const projectRow = await waitFor(() => Array.from(document.querySelectorAll("article.project-row")).find((entry) => entry.textContent?.includes(repository)) as HTMLElement | undefined, "project-row");
  const edit = Array.from(projectRow.querySelectorAll("button")).find((entry) => entry.textContent?.trim() === "编辑") as HTMLButtonElement | undefined;
  if (!edit) throw new Error("GUI_PROBE_PROJECT_EDIT_MISSING");
  edit.click();
  await waitFor(() => button("保存"), "project-save");
  await fill("Display name", "CWapi GUI Probe Edited");
  await click("保存");
  await waitFor(() => document.body.textContent?.includes("CWapi GUI Probe Edited"), "project-edited");
  checks.push("project-edit");

  const editedRow = await waitFor(() => Array.from(document.querySelectorAll("article.project-row")).find((entry) => entry.textContent?.includes(repository)) as HTMLElement | undefined, "project-edited-row");
  const remove = Array.from(editedRow.querySelectorAll("button")).find((entry) => entry.textContent?.trim() === "删除") as HTMLButtonElement | undefined;
  if (!remove) throw new Error("GUI_PROBE_PROJECT_DELETE_MISSING");
  remove.click();
  await waitFor(() => !document.body.textContent?.includes(repository), "project-deleted");
  checks.push("project-delete");

  await click("设置");
  await waitFor(() => heading("设置"), "settings-heading");
  await waitFor(() => heading("Slack"), "slack-settings-heading");
  await waitFor(() => button("更换 Slack 配置"), "slack-settings-button");
  if (document.body.textContent?.includes("Gmail")) throw new Error("GUI_PROBE_GMAIL_SETTINGS_PRESENT");
  if (document.body.textContent?.includes("允许 Actions")) throw new Error("GUI_PROBE_ACTIONS_TEXT_PRESENT");
  checks.push("settings");

  await click("诊断");
  await waitFor(() => heading("诊断"), "diagnostics-heading");
  checks.push("diagnostics");

  await click("关于");
  await waitFor(() => heading("关于"), "about-heading");
  if (!document.body.textContent?.includes("版本 1.6.0")) throw new Error("GUI_PROBE_ABOUT_VERSION_MISSING");
  if (config.source_commit && !document.body.textContent?.includes(config.source_commit)) throw new Error("GUI_PROBE_ABOUT_COMMIT_MISSING");
  checks.push("about");

  await click("控制台");
  await waitFor(() => heading("控制台"), "console-return");
  await waitFor(() => document.querySelector('[data-testid="mcp-request-surface"]'), "mcp-request-surface-return");
  checks.push("console-return");
}

export async function runGUIProbe(config: ProbeConfig): Promise<ProbeResult> {
  const checks: string[] = [];
  try {
    if (config.mode === "first-run") await probeFirstRun(config, checks);
    else if (config.mode === "workbench") await probeWorkbench(config, checks);
    else throw new Error(`GUI_PROBE_MODE_UNSUPPORTED:${String(config.mode)}`);
    return { mode: config.mode, success: true, checks };
  } catch (cause) {
    return { mode: config.mode, success: false, checks, error: String(cause) };
  }
}
