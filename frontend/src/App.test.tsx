// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import { app } from "../wailsjs/go/models";
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

type DesktopReturn = Awaited<ReturnType<typeof DesktopSnapshot>>;
type DiagnosticsReturn = Awaited<ReturnType<typeof DiagnosticsSnapshot>>;

vi.mock("../wailsjs/go/main/App", () => ({
  AddProject: vi.fn(),
  ConfigureSlack: vi.fn(),
  DesktopSnapshot: vi.fn(),
  DiagnosticsSnapshot: vi.fn(),
  RemoveProject: vi.fn(),
  ResolveDesktopError: vi.fn(),
  UpdatePermissionMode: vi.fn(),
  UpdateProject: vi.fn(),
}));

const runtime = new app.RuntimeSnapshot({
  version: "1.6.0",
  source_commit: "0123456789abcdef0123456789abcdef01234567",
  architecture: "go-core+wails-v2+react-typescript",
  core: "go",
  desktop: "wails-v2",
  platform: "windows/amd64",
  stage: "S2.4",
});

function project(displayName: string): app.ProjectSnapshot {
  return new app.ProjectSnapshot({
    id: "prj-0123456789abcdef01234567",
    display_name: displayName,
    repository: "AAAYNMMM/CWapi-test",
    local_path: "E:\\Projects\\CWapi-test",
    remote_url: "https://github.com/AAAYNMMM/CWapi-test.git",
  });
}

function configSnapshot(projects: app.ProjectSnapshot[] = [], permissionMode = "safe"): app.ConfigSnapshot {
  const value = new app.ConfigSnapshot({
    schema: "cwapi.config.v1",
    version: "1.6.0",
    config_path: "E:\\CWapi-data\\config\\cwapi.json",
    slack: { channel_id: "C12345678" },
    projects,
  });
  return Object.assign(value, { permission_mode: permissionMode });
}

function slackSnapshot(configured = true): app.SlackSnapshot {
  return new app.SlackSnapshot({
    configured,
    ready: configured,
    state: configured ? "healthy" : "setup_required",
    detail: configured ? "Socket Mode connected" : "Slack credentials and channel are not ready",
    credential_store: "windows_credential_manager",
    app_token_present: configured,
    bot_token_present: configured,
    channel_id: configured ? "C12345678" : "",
    channel_name: configured ? "cwapi-control" : "",
    team: configured ? "CWapi Team" : "",
    team_id: configured ? "T123" : "",
    user: configured ? "cwapi" : "",
    user_id: configured ? "U123" : "",
    bot_id: configured ? "B123" : "",
    socket_ready: configured,
    recent_index_size: 3,
  });
}

function codexSnapshot(): app.CodexSnapshot {
  return new app.CodexSnapshot({
    configured: true,
    ready: true,
    running: true,
    version: "0.144.4-cwapi.1",
    executable_path: "E:\\CWapi\\runtime\\codex\\current\\bin\\codex.exe",
    executable_sha256: "51398051c2332b6afe08dc3b9dbb4056085c197f35ca57a307ee303d450cada5",
    browser_mcp_ready: false,
    process_mcp_ready: true,
    node_path: "",
    browser_path: "",
  });
}

function mcpRequests(): app.MCPRequestSnapshot[] {
  return [new app.MCPRequestSnapshot({
    request_id: "REQ-DEMO",
    source_message_id: "C123:1.000",
    method: "mcpServer/tool/call",
    tool_name: "browser_navigate",
    execution_state: "completed",
    delivery_state: "delivered",
    terminal: true,
    created_at: 1000,
    updated_at: 1250,
    elapsed_ms: 250,
  })];
}

function observability(activeError = true): app.ObservabilitySnapshot {
  return new app.ObservabilitySnapshot({
    state_path: "E:\\CWapi-data\\state\\cwapi.db",
    state_schema: "3",
    structured_execution: [{
      id: 1,
      timestamp: 1000,
      task_id: "REQ-DEMO",
      step_id: "step-001",
      kind: "mcp.tool",
      status: "completed",
      message: "structured-step-finished",
      duration_ms: 250,
      data_json: "{}",
    }],
    runtime_logs: [{
      id: 2,
      timestamp: 2000,
      level: "info",
      component: "slack",
      message: "runtime-socket-reconnected",
      fields_json: `{"state":"connected","detail":"Socket Mode connected"}`,
      fingerprint: "",
    }],
    errors: [{
      fingerprint: "fingerprint-1",
      component: "desktop",
      operation: "snapshot",
      message: "persistent failure",
      count: 4,
      first_seen: 1000,
      last_seen: 2000,
      active: activeError,
    }],
    components: [
      { name: "mcp-relay", state: "healthy", detail: "MCP relay ready", updated_at: 1000 },
      { name: "codex", state: "healthy", detail: "Codex app-server ready", updated_at: 1000 },
    ],
  });
}

function desktopSnapshot(options: { configured?: boolean; projects?: app.ProjectSnapshot[]; activeError?: boolean } = {}): DesktopReturn {
  const configured = options.configured ?? true;
  return {
    generated_at: 3000,
    runtime,
    config: configSnapshot(options.projects ?? []),
    slack: slackSnapshot(configured),
    codex: codexSnapshot(),
    mcp_requests: mcpRequests(),
    observability: observability(options.activeError ?? true),
  } as unknown as DesktopReturn;
}

describe("v1.6 simple MCP desktop", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    localStorage.clear();
    vi.mocked(DesktopSnapshot).mockResolvedValue(desktopSnapshot());
    vi.mocked(DiagnosticsSnapshot).mockResolvedValue({
      generated_at: 3000,
      version: "1.6.0",
      source_commit: runtime.source_commit,
      architecture: runtime.architecture,
      platform: runtime.platform,
      stage: "S2.4",
      config_path: "E:\\CWapi-data\\config\\cwapi.json",
      state_path: "E:\\CWapi-data\\state\\cwapi.db",
      state_schema: "3",
      slack: slackSnapshot(true),
      codex: codexSnapshot(),
      mcp_requests: mcpRequests(),
      components: observability().components,
    } as unknown as DiagnosticsReturn);
    vi.mocked(ResolveDesktopError).mockResolvedValue(observability(false));
  });

  afterEach(() => cleanup());

  it("uses the simple Slack first-run flow", async () => {
    vi.mocked(DesktopSnapshot)
      .mockResolvedValueOnce(desktopSnapshot({ configured: false }))
      .mockResolvedValue(desktopSnapshot({ configured: true }));
    vi.mocked(ConfigureSlack).mockResolvedValue(slackSnapshot(true));

    render(<App />);
    expect(await screen.findByText("连接 Slack")).toBeTruthy();
    expect(document.body.textContent).not.toContain("Gmail");

    fireEvent.change(screen.getByLabelText("App Token"), { target: { value: "xapp-sensitive-test" } });
    fireEvent.change(screen.getByLabelText("Bot Token"), { target: { value: "xoxb-sensitive-test" } });
    fireEvent.change(screen.getByLabelText("Channel ID"), { target: { value: "C12345678" } });
    fireEvent.click(screen.getByRole("button", { name: "测试连接并保存" }));

    await waitFor(() => expect(ConfigureSlack).toHaveBeenCalledTimes(1));
    expect(await screen.findByRole("heading", { name: "控制台" })).toBeTruthy();
    expect(document.body.textContent).not.toContain("xapp-sensitive-test");
    expect(document.body.textContent).not.toContain("xoxb-sensitive-test");
  });

  it("shows MCP runtime state and keeps the two logs in one shared grid", async () => {
    render(<App />);
    expect(await screen.findByRole("heading", { name: "控制台" })).toBeTruthy();
    expect(screen.getByText("组件运行状态")).toBeTruthy();
    expect(screen.getByText("CWapi 桌面程序")).toBeTruthy();
    expect(screen.getByText("Go Core / MCP Relay")).toBeTruthy();
    expect(screen.getByText("Slack Transport")).toBeTruthy();
    expect(screen.getByText("Codex app-server / MCP Relay")).toBeTruthy();
    expect(screen.getByText("安全项目进程 MCP")).toBeTruthy();
    expect(screen.getByTestId("mcp-request-REQ-DEMO")).toBeTruthy();
    expect(document.body.textContent).not.toContain("Go Core Runner");

    const grid = screen.getByTestId("console-log-grid");
    const structured = screen.getByTestId("structured-log-surface");
    const runtimeSurface = screen.getByTestId("runtime-log-surface");
    expect(grid.contains(structured)).toBe(true);
    expect(grid.contains(runtimeSurface)).toBe(true);
    expect(within(structured).getByText("structured-step-finished")).toBeTruthy();
    expect(within(structured).queryByText("runtime-socket-reconnected")).toBeNull();
    expect(within(runtimeSurface).getByText("已连接")).toBeTruthy();
    expect(within(runtimeSurface).getByText("runtime-socket-reconnected · Socket Mode connected")).toBeTruthy();
    expect(within(runtimeSurface).queryByText("structured-step-finished")).toBeNull();
  });

  it("saves only project facts and exposes no Action policy", async () => {
    vi.mocked(AddProject).mockResolvedValue(configSnapshot([project("CWapi test")]));
    vi.mocked(UpdateProject).mockResolvedValue(configSnapshot([project("CWapi renamed")]));
    vi.mocked(RemoveProject).mockResolvedValue(configSnapshot());

    render(<App />);
    await screen.findByRole("heading", { name: "控制台" });
    fireEvent.click(screen.getByRole("button", { name: "项目" }));
    expect(await screen.findByText("还没有添加项目")).toBeTruthy();
    expect(document.body.textContent).not.toContain("Allowed Actions");
    expect(document.body.textContent).not.toContain("允许 Actions");

    fireEvent.click(screen.getByRole("button", { name: "添加项目" }));
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "CWapi test" } });
    fireEvent.change(screen.getByLabelText("Local path"), { target: { value: "E:\\Projects\\CWapi-test" } });
    fireEvent.change(screen.getByLabelText("Remote URL"), { target: { value: "https://github.com/AAAYNMMM/CWapi-test.git" } });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(await screen.findByText("CWapi test")).toBeTruthy();
    expect(within(screen.getByTestId("project-prj-0123456789abcdef01234567")).getByText("prj-0123456789abcdef01234567")).toBeTruthy();
    expect(AddProject).toHaveBeenCalledTimes(1);
    const addCommand = vi.mocked(AddProject).mock.calls[0][0];
    expect(addCommand.repository).toBe("AAAYNMMM/CWapi-test");
    expect(Object.keys(addCommand).sort()).toEqual(["display_name", "local_path", "remote_url", "repository"].sort());

    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "CWapi renamed" } });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    expect(await screen.findByText("CWapi renamed")).toBeTruthy();
    expect(UpdateProject).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "删除" }));
    expect(await screen.findByText("还没有添加项目")).toBeTruthy();
    expect(RemoveProject).toHaveBeenCalledTimes(1);
  });

  it("exposes the two mutable permission modes without restoring Action policy", async () => {
    vi.mocked(UpdatePermissionMode).mockResolvedValue(configSnapshot([], "full_access"));

    render(<App />);
    await screen.findByRole("heading", { name: "控制台" });
    fireEvent.click(screen.getByRole("button", { name: "设置" }));
    expect(await screen.findByRole("heading", { name: "设置" })).toBeTruthy();
    expect(screen.getByText("界面")).toBeTruthy();
    expect(screen.getByText("权限")).toBeTruthy();
    expect(screen.getByText("Slack")).toBeTruthy();
    expect(screen.getByRole("radio", { name: /安全权限/ }).getAttribute("aria-checked")).toBe("true");
    fireEvent.click(screen.getByRole("radio", { name: /完全访问权限/ }));
    await waitFor(() => expect(UpdatePermissionMode).toHaveBeenCalledWith("full_access"));
    await waitFor(() => expect(screen.getByRole("radio", { name: /完全访问权限/ }).getAttribute("aria-checked")).toBe("true"));
    expect(screen.getByText("允许全盘访问，基础层永久保护仍生效")).toBeTruthy();
    expect(screen.getByRole("button", { name: "更换 Slack 配置" })).toBeTruthy();
    expect(document.body.textContent).not.toContain("全局允许 Actions");
    expect(document.body.textContent).not.toContain("Capability");
  });

  it("renders compact component diagnostics without component comments and resolves errors", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "控制台" });
    fireEvent.click(screen.getByRole("button", { name: "诊断" }));

    const componentGrid = await screen.findByTestId("diagnostic-component-grid");
    expect(within(componentGrid).getByText("mcp-relay")).toBeTruthy();
    expect(within(componentGrid).getByText("codex")).toBeTruthy();
    expect(within(componentGrid).queryByText("MCP relay ready")).toBeNull();
    expect(within(componentGrid).queryByText("Codex app-server ready")).toBeNull();

    const surface = await screen.findByTestId("error-aggregate-surface");
    expect(within(surface).getByText("persistent failure")).toBeTruthy();
    expect(within(surface).getByText(/出现 4 次/)).toBeTruthy();
    fireEvent.click(within(surface).getByRole("button", { name: "标记已解决" }));

    await waitFor(() => expect(ResolveDesktopError).toHaveBeenCalledWith("fingerprint-1"));
    await waitFor(() => expect(within(surface).getByText("当前没有活动错误。")).toBeTruthy());
  });
});
