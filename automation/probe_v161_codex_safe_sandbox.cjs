const { spawn } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const crypto = require("node:crypto");

const [codexExe, nodeExe] = process.argv.slice(2);
if (!codexExe || !nodeExe) {
  throw new Error("usage: node probe_v161_codex_safe_sandbox.cjs <codex.exe> <node.exe>");
}

const nonce = crypto.randomBytes(8).toString("hex");
const smokeOnly = process.env.CWAPI_P0_SMOKE_ONLY === "1";
const keepRoot = process.env.CWAPI_P0_KEEP_ROOT === "1";
const excludeTemp = process.env.CWAPI_P0_EXCLUDE_TEMP === "1";
const commandTimeoutMs = Number(process.env.CWAPI_P0_COMMAND_TIMEOUT_MS || 60000);
const probeTimeoutMs = Number(process.env.CWAPI_P0_PROBE_TIMEOUT_MS || 150000);
const probeRoot = path.join(path.parse(process.cwd()).root, `cwapi-p0-safe-${nonce}`);
const roots = {
  current: path.join(probeRoot, "workspaces", "git", "worktrees", "current"),
  crossTree: path.join(probeRoot, "workspaces", "git", "worktrees", "cross"),
  mirror: path.join(probeRoot, "workspaces", "git", "mirrors", "repo.git"),
  external: path.join(probeRoot, "external"),
};
const home = path.join(probeRoot, "codex-home");
for (const directory of [...Object.values(roots), home]) {
  fs.mkdirSync(directory, { recursive: true });
}

const childProbe = String.raw`
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const targets = JSON.parse(process.argv[1]);

function attempt(label, target) {
  try {
    fs.writeFileSync(target, label, "utf8");
    return { label, outcome: "written", target };
  } catch (error) {
    return {
      label,
      outcome: "denied",
      code: error && error.code ? error.code : "UNKNOWN",
      syscall: error && error.syscall ? error.syscall : "",
      target,
    };
  }
}

const direct = [
  attempt("current", path.join(targets.current, "direct.txt")),
  attempt("crossTree", path.join(targets.crossTree, "direct.txt")),
  attempt("mirror", path.join(targets.mirror, "direct.txt")),
  attempt("external", path.join(targets.external, "direct.txt")),
  attempt("temp", path.join(process.env.TEMP || process.env.TMP, targets.tempName)),
];

const nestedSource = [
  'const fs = require("node:fs");',
  'const path = require("node:path");',
  'const targets = JSON.parse(process.argv[1]);',
  'const resultPath = process.argv[2];',
  'const result = [];',
  'for (const [label, directory] of Object.entries(targets.roots)) {',
  '  const target = path.join(directory, "nested.txt");',
  '  try {',
  '    fs.writeFileSync(target, label, "utf8");',
  '    result.push({ label, outcome: "written", target });',
  '  } catch (error) {',
  '    result.push({ label, outcome: "denied", code: error.code || "UNKNOWN", syscall: error.syscall || "", target });',
  '  }',
  '}',
  'fs.writeFileSync(resultPath, JSON.stringify(result), "utf8");',
].join("\n");
const nestedResultPath = path.join(targets.current, "nested-result.json");
const nested = spawnSync(process.execPath, [
  "-e",
  nestedSource,
  JSON.stringify({ roots: targets.roots }),
  nestedResultPath,
], {
  cwd: targets.current,
  stdio: "inherit",
});
const nestedResult = fs.existsSync(nestedResultPath)
  ? fs.readFileSync(nestedResultPath, "utf8")
  : "";

process.stdout.write(JSON.stringify({
  direct,
  nested: {
    exitCode: nested.status,
    signal: nested.signal,
    stdout: nestedResult,
    stderr: "",
    error: nested.error ? {
      code: nested.error.code,
      errno: nested.error.errno,
      syscall: nested.error.syscall,
      path: nested.error.path,
    } : undefined,
  },
  runtimeExecutable: process.execPath,
  cwd: process.cwd(),
  temp: process.env.TEMP || process.env.TMP,
}));
`;

let nextId = 1;
const pending = new Map();
const notifications = [];
const notificationWaiters = new Map();
let stdoutBuffer = "";
let stderr = "";

const environment = {
  ...process.env,
  CODEX_HOME: home,
  RUST_LOG: "warn",
  LOG_FORMAT: "json",
  SBX_DEBUG: "1",
};
for (const key of Object.keys(environment)) {
  if (/^(OPENAI|CODEX|CWAPI|SLACK|GH_|GITHUB_)/i.test(key) && key !== "CODEX_HOME") {
    delete environment[key];
  }
}

let appServer;

function startAppServer() {
  stdoutBuffer = "";
  appServer = spawn(codexExe, ["app-server", "--stdio"], {
    cwd: path.dirname(codexExe),
    env: environment,
    stdio: ["pipe", "pipe", "pipe"],
    windowsHide: true,
  });
  appServer.stdout.on("data", handleStdout);
  appServer.stderr.on("data", (chunk) => { stderr += chunk.toString("utf8"); });
}

function send(method, params, notify = false) {
  const id = notify ? undefined : nextId++;
  const message = { method };
  if (id !== undefined) message.id = id;
  if (params !== undefined) message.params = params;
  appServer.stdin.write(`${JSON.stringify(message)}\n`);
  if (notify) return Promise.resolve();
  return new Promise((resolve, reject) => pending.set(id, { resolve, reject }));
}

function handleStdout(chunk) {
  stdoutBuffer += chunk.toString("utf8");
  while (stdoutBuffer.includes("\n")) {
    const boundary = stdoutBuffer.indexOf("\n");
    const line = stdoutBuffer.slice(0, boundary).trim();
    stdoutBuffer = stdoutBuffer.slice(boundary + 1);
    if (!line) continue;
    const message = JSON.parse(line);
    if (message.id !== undefined && message.method === undefined) {
      const waiter = pending.get(message.id);
      pending.delete(message.id);
      if (!waiter) continue;
      if (message.error !== undefined) waiter.reject(Object.assign(new Error("RPC_ERROR"), { rpc: message.error }));
      else waiter.resolve(message.result);
    } else if (message.id !== undefined && message.method !== undefined) {
      appServer.stdin.write(`${JSON.stringify({ id: message.id, error: { code: -32601, message: "probe has no request handlers" } })}\n`);
    } else {
      notifications.push(message);
      const waiter = notificationWaiters.get(message.method);
      if (waiter) {
        notificationWaiters.delete(message.method);
        waiter(message);
      }
    }
  }
}

function withTimeout(promise, milliseconds) {
  let timer;
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error("PROBE_TIMEOUT")), milliseconds);
  });
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timer));
}

function waitForNotification(method) {
  const existing = notifications.find((item) => item.method === method);
  if (existing) return Promise.resolve(existing);
  return new Promise((resolve) => notificationWaiters.set(method, resolve));
}

function stopAppServer() {
  return new Promise((resolve) => {
    if (appServer.exitCode !== null) return resolve();
    const timer = setTimeout(() => appServer.kill(), 1000);
    appServer.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });
    appServer.stdin.end();
  });
}

async function initializeAppServer() {
  await withTimeout(send("initialize", {
    clientInfo: { name: "cwapi-p0-probe", version: "1.6.1" },
    capabilities: { experimentalApi: true },
  }), 15000);
  await send("initialized", undefined, true);
}

startAppServer();

(async () => {
  let commandResult;
  let rpcError;
  let sandboxReadiness;
  let sandboxSetup;
  try {
    await initializeAppServer();
    sandboxReadiness = await withTimeout(send("windowsSandbox/readiness", null), 15000);
    if (sandboxReadiness.status !== "ready") {
      const completed = waitForNotification("windowsSandbox/setupCompleted");
      const started = await withTimeout(send("windowsSandbox/setupStart", {
        mode: "unelevated",
        cwd: roots.current,
      }), 15000);
      const notification = await withTimeout(completed, 30000);
      sandboxSetup = { started, completed: notification.params };
      await stopAppServer();
      startAppServer();
      await initializeAppServer();
      sandboxReadiness = await withTimeout(send("windowsSandbox/readiness", null), 15000);
    }
    const targets = {
      ...roots,
      roots,
      tempName: `cwapi-p0-temp-${nonce}.txt`,
    };
    const command = smokeOnly
      ? [path.join(process.env.SystemRoot, "System32", "cmd.exe"), "/d", "/c", "exit", "0"]
      : [nodeExe, "-e", childProbe, JSON.stringify(targets)];
    commandResult = await withTimeout(send("command/exec", {
      command,
      cwd: roots.current,
      sandboxPolicy: {
        type: "workspaceWrite",
        writableRoots: [roots.current],
        networkAccess: false,
        excludeSlashTmp: excludeTemp,
        excludeTmpdirEnvVar: excludeTemp,
      },
      timeoutMs: commandTimeoutMs,
    }), probeTimeoutMs);
  } catch (error) {
    rpcError = error.rpc || { message: error.message };
  } finally {
    await stopAppServer();
  }

  let payload;
  let nested;
  if (!smokeOnly && commandResult && commandResult.stdout) {
    try {
      payload = JSON.parse(commandResult.stdout);
      nested = JSON.parse(payload.nested.stdout);
    } catch {}
  }
  const directByLabel = Object.fromEntries((payload?.direct || []).map((item) => [item.label, item]));
  const nestedByLabel = Object.fromEntries((nested || []).map((item) => [item.label, item]));
  const deniedLabels = ["crossTree", "mirror", "external"];
  const setupSucceeded = !sandboxSetup || sandboxSetup.completed?.success === true;
  const assertions = smokeOnly ? {
    setupSucceeded,
    sandboxReady: sandboxReadiness?.status === "ready",
    commandCompleted: !rpcError && commandResult?.exitCode === 0,
  } : {
    setupSucceeded,
    sandboxReady: sandboxReadiness?.status === "ready",
    commandCompleted: !rpcError && commandResult?.exitCode === 0,
    currentWritable: directByLabel.current?.outcome === "written",
    directDenied: deniedLabels.every((label) => directByLabel[label]?.outcome === "denied"),
    childCurrentWritable: nestedByLabel.current?.outcome === "written",
    childDenied: deniedLabels.every((label) => nestedByLabel[label]?.outcome === "denied"),
    tempBehavior: excludeTemp
      ? directByLabel.temp?.outcome === "denied"
      : directByLabel.temp?.outcome === "written",
    runtimeExecutableAllowed: payload?.runtimeExecutable?.toLowerCase() === nodeExe.toLowerCase(),
  };
  const passed = Object.values(assertions).every(Boolean);
  const sandboxDirectory = path.join(home, ".sandbox");
  const sandboxLogPath = fs.existsSync(sandboxDirectory)
    ? fs.readdirSync(sandboxDirectory)
      .filter((name) => /^sandbox\..+\.log$/.test(name))
      .sort()
      .map((name) => path.join(sandboxDirectory, name))
      .at(-1)
    : undefined;
  const evidence = {
    codexExe,
    probeRoot,
    smokeOnly,
    excludeTemp,
    passed,
    assertions,
    sandboxReadiness,
    sandboxSetup,
    commandResult,
    rpcError,
    notifications: notifications.map((item) => item.method),
    appServerStderr: stderr.slice(-8192),
    sandboxLog: sandboxLogPath && fs.existsSync(sandboxLogPath)
      ? fs.readFileSync(sandboxLogPath, "utf8").slice(-8192)
      : "",
  };
  process.stdout.write(`${JSON.stringify(evidence, null, 2)}\n`);
  if (!keepRoot) fs.rmSync(probeRoot, { recursive: true, force: true });
  const tempMarker = path.join(os.tmpdir(), `cwapi-p0-temp-${nonce}.txt`);
  fs.rmSync(tempMarker, { force: true });
  process.exitCode = passed ? 0 : 1;
})().catch((error) => {
  if (!keepRoot) {
    try { fs.rmSync(probeRoot, { recursive: true, force: true }); } catch {}
  }
  throw error;
});
