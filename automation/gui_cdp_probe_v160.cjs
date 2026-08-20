"use strict";

const fs = require("fs");
const path = require("path");

function option(name) {
  const index = process.argv.indexOf(name);
  if (index < 0 || index + 1 >= process.argv.length) {
    throw new Error(`missing option ${name}`);
  }
  return process.argv[index + 1];
}

function optionalOption(name) {
  const index = process.argv.indexOf(name);
  if (index < 0) return "";
  if (index + 1 >= process.argv.length) throw new Error(`missing option value ${name}`);
  return process.argv[index + 1];
}

const endpoint = option("--endpoint").replace(/\/$/, "");
optionalOption("--node-modules");
const screenshot = path.resolve(option("--screenshot"));
const expectedCommit = optionalOption("--expected-commit").trim().toLowerCase();

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

class CDPClient {
  constructor(socket) {
    this.socket = socket;
    this.nextID = 1;
    this.pending = new Map();
    socket.addEventListener("message", (event) => this.onMessage(event));
    socket.addEventListener("close", () => this.rejectAll(new Error("CDP_SOCKET_CLOSED")));
    socket.addEventListener("error", () => this.rejectAll(new Error("CDP_SOCKET_ERROR")));
  }

  onMessage(event) {
    let message;
    try { message = JSON.parse(String(event.data)); } catch (_) { return; }
    if (!message.id || !this.pending.has(message.id)) return;
    const callbacks = this.pending.get(message.id);
    this.pending.delete(message.id);
    if (message.error) callbacks.reject(new Error(`CDP_ERROR ${callbacks.method}: ${JSON.stringify(message.error)}`));
    else callbacks.resolve(message.result || {});
  }

  rejectAll(error) {
    for (const callbacks of this.pending.values()) callbacks.reject(error);
    this.pending.clear();
  }

  send(method, params = {}) {
    const id = this.nextID++;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { method, resolve, reject });
      this.socket.send(JSON.stringify({ id, method, params }));
    });
  }

  close() {
    try { this.socket.close(); } catch (_) {}
  }
}

async function fetchTargets() {
  const response = await fetch(`${endpoint}/json/list`);
  if (!response.ok) throw new Error(`CDP_TARGET_LIST_FAILED:${response.status}`);
  return await response.json();
}

async function connectWebSocket(url) {
  const socket = new WebSocket(url);
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("CDP_WS_OPEN_TIMEOUT")), 3000);
    socket.addEventListener("open", () => { clearTimeout(timer); resolve(); }, { once: true });
    socket.addEventListener("error", () => { clearTimeout(timer); reject(new Error("CDP_WS_OPEN_ERROR")); }, { once: true });
  });
  return new CDPClient(socket);
}

async function evaluate(client, expression) {
  const result = await client.send("Runtime.evaluate", {
    expression,
    awaitPromise: true,
    returnByValue: true,
    userGesture: true,
  });
  if (result.exceptionDetails) throw new Error(`CDP_EVALUATE_FAILED:${JSON.stringify(result.exceptionDetails)}`);
  return result.result ? result.result.value : undefined;
}

async function connectCWapiPage() {
  let lastError = "";
  for (let attempt = 0; attempt < 60; attempt += 1) {
    let targets = [];
    try {
      targets = await fetchTargets();
    } catch (error) {
      lastError = String(error);
      await sleep(500);
      continue;
    }
    for (const target of targets) {
      if (!target.webSocketDebuggerUrl || target.type !== "page") continue;
      let client;
      try {
        client = await connectWebSocket(target.webSocketDebuggerUrl);
        await client.send("Runtime.enable");
        await client.send("Page.enable");
        const pageInfo = await evaluate(client, `(() => ({
          title: document.title,
          url: location.href,
          body: document.body ? document.body.innerText : ""
        }))()`);
        if (pageInfo && (pageInfo.title === "CWapi" || pageInfo.body.includes("CWapi") || pageInfo.body.includes("连接 Slack"))) {
          return client;
        }
        client.close();
      } catch (error) {
        lastError = String(error);
        if (client) client.close();
      }
    }
    await sleep(500);
  }
  throw new Error(`CWAPI_PAGE_NOT_FOUND:${lastError}`);
}

async function waitForExpression(client, label, expression) {
  let lastValue;
  for (let attempt = 0; attempt < 60; attempt += 1) {
    try {
      lastValue = await evaluate(client, expression);
      if (lastValue) return lastValue;
    } catch (error) {
      lastValue = String(error);
    }
    await sleep(500);
  }
  throw new Error(`${label}_TIMEOUT:${JSON.stringify(lastValue)}`);
}

async function callApp(client, method, args = []) {
  return await evaluate(client, `(async () => {
    const app = window.go && window.go.main && window.go.main.App;
    if (!app || typeof app[${JSON.stringify(method)}] !== "function") {
      throw new Error("WAILS_BINDING_MISSING:${method}");
    }
    return await app[${JSON.stringify(method)}](...${JSON.stringify(args)});
  })()`);
}

function componentMap(observability) {
  const map = new Map();
  for (const component of observability.components || []) map.set(component.name, component);
  return map;
}

async function firstRunFormProbe(client) {
  return await evaluate(client, `(() => {
    function controlByLabel(text) {
      const labels = Array.from(document.querySelectorAll("label"));
      const label = labels.find((candidate) => (candidate.textContent || "").includes(text));
      if (!label) throw new Error("LABEL_MISSING:" + text);
      if (label.control) return label.control;
      const id = label.getAttribute("for");
      if (id) {
        const byID = document.getElementById(id);
        if (byID) return byID;
      }
      const nested = label.querySelector("input,textarea,select");
      if (nested) return nested;
      throw new Error("LABEL_CONTROL_MISSING:" + text);
    }
    function buttonByText(text) {
      const buttons = Array.from(document.querySelectorAll("button"));
      const button = buttons.find((candidate) => (candidate.textContent || "").includes(text));
      if (!button) throw new Error("BUTTON_MISSING:" + text);
      return button;
    }
    function setValue(input, value) {
      const descriptor = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(input), "value");
      if (!descriptor || typeof descriptor.set !== "function") throw new Error("INPUT_VALUE_SETTER_MISSING");
      descriptor.set.call(input, value);
      input.dispatchEvent(new Event("input", { bubbles: true }));
      input.dispatchEvent(new Event("change", { bubbles: true }));
    }

    const heading = Array.from(document.querySelectorAll("h1,h2,h3"))
      .find((element) => (element.textContent || "").includes("连接 Slack"));
    if (!heading) throw new Error("FIRST_RUN_HEADING_MISSING");
    const appToken = controlByLabel("App Token");
    const botToken = controlByLabel("Bot Token");
    const channelID = controlByLabel("Channel ID");
    const submit = buttonByText("测试连接并保存");
    if (!submit.disabled) throw new Error("FIRST_RUN_SUBMIT_SHOULD_START_DISABLED");
    setValue(appToken, "xapp-cwapi-gui-probe-not-a-real-token");
    setValue(botToken, "xoxb-cwapi-gui-probe-not-a-real-token");
    setValue(channelID, "C0123456789");
    if (appToken.value !== "xapp-cwapi-gui-probe-not-a-real-token") throw new Error("APP_TOKEN_CONTROLLED_INPUT_FAILED");
    if (botToken.value !== "xoxb-cwapi-gui-probe-not-a-real-token") throw new Error("BOT_TOKEN_CONTROLLED_INPUT_FAILED");
    if (channelID.value !== "C0123456789") throw new Error("CHANNEL_ID_CONTROLLED_INPUT_FAILED");
    if (submit.disabled) throw new Error("FIRST_RUN_SUBMIT_SHOULD_ENABLE_AFTER_INPUT");
    const root = document.querySelector("#root");
    const bounds = root ? root.getBoundingClientRect() : null;
    if (!bounds || bounds.width < 500 || bounds.height < 400) throw new Error("ROOT_BOUNDS_INVALID:" + JSON.stringify(bounds));
    return {
      heading: heading.textContent.trim(),
      root_width: Math.round(bounds.width),
      root_height: Math.round(bounds.height),
      submit_enabled_after_fill: !submit.disabled,
    };
  })()`);
}

async function main() {
  const client = await connectCWapiPage();
  await waitForExpression(client, "DOM_READY", `document.readyState === "interactive" || document.readyState === "complete"`);
  await waitForExpression(client, "ROOT_ATTACHED", `Boolean(document.querySelector("#root"))`);
  await waitForExpression(client, "WAILS_BINDINGS_READY", `Boolean(
    window.go && window.go.main && window.go.main.App &&
    window.go.main.App.RuntimeSnapshot && window.go.main.App.ReadinessSnapshot
  )`);

  const runtime = await callApp(client, "RuntimeSnapshot");
  if (runtime.version !== "1.6.0") throw new Error(`RUNTIME_VERSION_INVALID:${JSON.stringify(runtime)}`);
  if (runtime.stage !== "S2.4") throw new Error(`RUNTIME_STAGE_INVALID:${JSON.stringify(runtime)}`);
  if (expectedCommit && String(runtime.source_commit).toLowerCase() !== expectedCommit) {
    throw new Error(`RUNTIME_COMMIT_MISMATCH expected=${expectedCommit} actual=${runtime.source_commit}`);
  }

  const slack = await callApp(client, "SlackSnapshot");
  const observability = await callApp(client, "ObservabilitySnapshot");
  const readiness = await callApp(client, "ReadinessSnapshot", [10]);
  const recentMCPRequests = await callApp(client, "RecentMCPRequests", [10]);
  if (!Array.isArray(recentMCPRequests)) throw new Error("RECENT_MCP_REQUESTS_NOT_ARRAY");
  if (!readiness || !readiness.codex || !Array.isArray(readiness.recent_requests)) {
    throw new Error("READINESS_SNAPSHOT_INVALID");
  }
  if (expectedCommit && String(readiness.runtime.source_commit).toLowerCase() !== expectedCommit) {
    throw new Error(`READINESS_COMMIT_MISMATCH expected=${expectedCommit} actual=${readiness.runtime.source_commit}`);
  }

  const components = componentMap(observability);
  for (const name of ["config", "projects", "mcp-relay", "desktop", "slack", "codex", "mcp-workspace"]) {
    if (!components.has(name)) throw new Error(`OBSERVABILITY_COMPONENT_MISSING:${name}`);
  }

  const form = await firstRunFormProbe(client);
  fs.mkdirSync(path.dirname(screenshot), { recursive: true });
  const captured = await client.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: true });
  fs.writeFileSync(screenshot, Buffer.from(captured.data, "base64"));
  const screenshotBytes = fs.statSync(screenshot).size;
  if (screenshotBytes < 10000) throw new Error(`CDP_SCREENSHOT_TOO_SMALL:${screenshotBytes}`);

  const pageInfo = await evaluate(client, `(() => ({ title: document.title, url: location.href }))()`);
  const evidence = {
    schema: "cwapi.gui-cdp-probe.v1",
    title: pageInfo.title,
    url: pageInfo.url,
    heading: form.heading,
    runtime,
    slack_state: slack.state,
    slack_ready: slack.ready,
    codex_configured: readiness.codex.configured,
    codex_ready: readiness.codex.ready,
    codex_running: readiness.codex.running,
    mcp_runtime_ready: readiness.mcp_runtime_ready,
    local_ready: readiness.local_ready,
    component_states: Object.fromEntries([...components.entries()].map(([name, component]) => [name, component.state])),
    recent_mcp_request_count: recentMCPRequests.length,
    root_width: form.root_width,
    root_height: form.root_height,
    submit_enabled_after_fill: form.submit_enabled_after_fill,
    screenshot,
    screenshot_bytes: screenshotBytes,
    cdp_probe: "raw-websocket",
  };
  process.stdout.write(`CWAPI_GUI_CDP_PROBE_PASS ${JSON.stringify(evidence)}\n`);
  client.close();
}

main().catch((error) => {
  process.stderr.write(`CWAPI_GUI_CDP_PROBE_FAILED ${error && error.stack ? error.stack : String(error)}\n`);
  process.exitCode = 1;
});
