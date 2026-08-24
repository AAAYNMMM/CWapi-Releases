// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import { app } from "../wailsjs/go/models";
import { ConfigureSlack, DesktopSnapshot, StopProcess, UpdatePermissionMode } from "../wailsjs/go/main/App";

vi.mock("../wailsjs/go/main/App", () => ({
  ConfigureSlack: vi.fn(), DesktopSnapshot: vi.fn(), StopProcess: vi.fn(), UpdatePermissionMode: vi.fn(),
}));

const runtime = new app.RuntimeSnapshot({
  version: "1.6.1", source_commit: "0123456789abcdef0123456789abcdef01234567",
  architecture: "go-core+wails-v2+react-typescript", core: "go", desktop: "wails-v2",
  platform: "windows/amd64", stage: "v1.6.1",
});

function slack(configured = true) {
  return new app.SlackSnapshot({
    configured, ready: configured, state: configured ? "ready" : "setup_required",
    detail: "", credential_store: "windows_credential_manager", app_token_present: configured,
    bot_token_present: configured, channel_id: configured ? "C12345678" : "", channel_name: "cwapi-control",
    team: "CWapi Team", team_id: "T123", user: "cwapi", user_id: "U123", bot_id: "B123",
    socket_ready: configured, recent_index_size: 1,
  });
}

function codex() {
  return new app.CodexSnapshot({
    configured: true, ready: true, running: true, version: "0.144.4-cwapi.1",
    executable_path: "E:\\CWapi\\runtime\\codex\\current\\bin\\codex.exe",
    executable_sha256: "51398051c2332b6afe08dc3b9dbb4056085c197f35ca57a307ee303d450cada5",
    browser_mcp_ready: true, node_path: "", browser_path: "",
  });
}

function process(state = "running") {
  return new app.ProcessSnapshot({
    process_id: "proc-0123456789abcdef01234567", state, backend: "codex", repository: "owner/repo",
    expected_commit: "abcdef0123456789abcdef0123456789abcdef01", working_directory: ".",
    started_at: 1000, updated_at: state === "running" ? 2000 : 4500,
    exit_code: state === "running" ? undefined : 0, stdout_tail: "line one\ncurrent=32 total=80",
    stderr_tail: "", latest_stream: "stdout", latest_output_at: state === "running" ? 4000 : 4400,
  });
}

function desktop(configured = true, processState = "running") {
  return new app.DesktopSnapshot({
    generated_at: 5000, runtime,
    config: new app.ConfigSnapshot({ schema: "cwapi.config.v2", version: "1.6.1", config_path: "E:\\CWapi-data\\config\\cwapi.json", permission_mode: "safe", slack: { channel_id: configured ? "C12345678" : "" } }),
    slack: slack(configured), codex: codex(), processes: configured ? [process(processState)] : [],
    latest_execution: new app.ExecutionEventSnapshot({ id: 1, timestamp: 3000, task_id: "REQ", step_id: "process", kind: "mcp.request", status: "running", message: "ignored", duration_ms: 12, data_json: '{"server":"cwapi","tool":"process_start"}' }),
    latest_runtime_error: undefined,
    components: [new app.ComponentSnapshot({ name: "mcp-relay", state: "healthy", detail: "ready", updated_at: 1 })],
  });
}

function config(permissionMode: "safe" | "full_access") {
  return new app.ConfigSnapshot({
    schema: "cwapi.config.v2", version: "1.6.1", config_path: "E:\\CWapi-data\\config\\cwapi.json",
    permission_mode: permissionMode, slack: { channel_id: "C12345678" },
  });
}

describe("CWapi v1.6.1 compact desktop", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(DesktopSnapshot).mockResolvedValue(desktop());
    vi.mocked(UpdatePermissionMode).mockResolvedValue(config("full_access"));
    vi.mocked(StopProcess).mockResolvedValue(process("stopped"));
  });

  afterEach(() => cleanup());

  it("renders one data monitor and only the newest non-scrollable record", async () => {
    render(<App />);
    expect(await screen.findByText("执行状态")).toBeTruthy();
    expect(screen.getByText("RUNNING")).toBeTruthy();
    expect(screen.getByText("current=32 total=80")).toBeTruthy();
    expect(screen.getByTestId("latest-record").className).toContain("latest-viewport");
    expect(screen.queryByText("CWapi 运行日志")).toBeNull();
    expect(screen.queryByText("结构化执行日志")).toBeNull();
    expect(screen.queryByRole("navigation")).toBeNull();
    expect(screen.queryByText("关于")).toBeNull();
  });

  it("shows the newest backend error as raw key-value data", async () => {
    const value = desktop();
    value.latest_runtime_error = new app.RuntimeLogSnapshot({
      id: 3, timestamp: 6000, level: "error", component: "gateway",
      message: "request.dispatch: timeout", fields_json: '{"operation":"request.dispatch"}', fingerprint: "fp",
    });
    vi.mocked(DesktopSnapshot).mockResolvedValue(value);
    render(<App />);
    expect(await screen.findByText("runtime")).toBeTruthy();
    expect(screen.getByText(/level=error message="request.dispatch: timeout" operation=request.dispatch/)).toBeTruthy();
  });

  it("stops only the displayed active process", async () => {
    render(<App />);
    await screen.findByText("RUNNING");
    fireEvent.click(screen.getByRole("button", { name: /STOP/ }));
    await waitFor(() => expect(StopProcess).toHaveBeenCalledWith("proc-0123456789abcdef01234567"));
  });

  it("keeps permission mutation in the same page", async () => {
    render(<App />);
    const control = await screen.findByRole("switch", { name: "权限模式" });
    expect(control.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(control);
    await waitFor(() => expect(UpdatePermissionMode).toHaveBeenCalledWith("full_access"));
    expect(await screen.findByText("FULL")).toBeTruthy();
  });

  it("retains completed short commands without showing STOP", async () => {
    vi.mocked(DesktopSnapshot).mockResolvedValue(desktop(true, "completed"));
    render(<App />);
    expect(await screen.findByText("COMPLETED")).toBeTruthy();
    expect(screen.getByText("exit_code")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /STOP/ })).toBeNull();
  });

  it("keeps Slack setup inside the same fixed shell", async () => {
    vi.mocked(DesktopSnapshot).mockResolvedValue(desktop(false));
    vi.mocked(ConfigureSlack).mockResolvedValue(slack());
    render(<App />);
    expect(await screen.findByText("连接 Slack")).toBeTruthy();
    expect(screen.getByTestId("single-page")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("App Token"), { target: { value: "xapp-test" } });
    fireEvent.change(screen.getByLabelText("Bot Token"), { target: { value: "xoxb-test" } });
    fireEvent.change(screen.getByLabelText("Channel ID"), { target: { value: "C12345678" } });
    fireEvent.click(screen.getByRole("button", { name: "测试连接并保存" }));
    await waitFor(() => expect(ConfigureSlack).toHaveBeenCalledTimes(1));
  });

  it("waits through CORE_NOT_STARTED before loading the desktop", async () => {
    vi.mocked(DesktopSnapshot).mockRejectedValueOnce(new Error("CORE_NOT_STARTED")).mockResolvedValue(desktop());
    render(<App />);
    expect(await screen.findByText("执行状态")).toBeTruthy();
    await waitFor(() => expect(DesktopSnapshot).toHaveBeenCalledTimes(2));
    expect(screen.queryByText(/CORE_NOT_STARTED/)).toBeNull();
  });
});
