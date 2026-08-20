"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawn, spawnSync } = require("node:child_process");
const { resolveInvocation } = require("./process-invocation.cjs");
const { drainRedacted, redact } = require("./process-output.cjs");

const SERVER_VERSION = "1.6.0";
const MAX_PROCESSES = 8;
const MAX_TAIL_BYTES = 8192;
const records = new Map();
const contextArgumentNames = ["_cwapi_workspace", "_cwapi_expected_commit", "_cwapi_request_id"];

const contextProperties = {
  _cwapi_workspace: { type: "string", description: "Injected by CWapi; callers must omit." },
  _cwapi_expected_commit: { type: "string", description: "Injected by CWapi; callers must omit." },
  _cwapi_request_id: { type: "string", description: "Injected by CWapi; callers must omit." },
};

const tools = [
  {
    name: "process_start",
    description: "Start an arbitrary executable plus argv, or a legacy Python/Node entrypoint, from a CWapi-managed exact-commit workspace. Shells such as powershell.exe or cmd.exe may be used as the executable.",
    inputSchema: {
      type: "object",
      properties: {
        runtime: { type: "string", enum: ["python", "node"] },
        entrypoint: { type: "string", description: "Repository-relative .py, .js, .cjs or .mjs file." },
        command: { type: "string", description: "Executable name, absolute path, or cwd-relative path. Windows .cmd/.bat shims are supported. Do not add surrounding quotes or include credentials/secrets." },
        argv: { type: "array", maxItems: 256, items: { type: "string" }, description: "Native executable arguments are passed directly; .cmd/.bat shims use Windows command-script argument semantics." },
        cwd: { type: "string", description: "Optional repository-relative working directory using forward slashes." },
        ...contextProperties,
      },
      oneOf: [
        { required: ["runtime", "entrypoint"] },
        { required: ["command"] },
      ],
      additionalProperties: false,
    },
  },
  {
    name: "process_status",
    description: "Read status and bounded log tails for a CWapi-owned process.",
    inputSchema: {
      type: "object",
      properties: { process_id: { type: "string" }, ...contextProperties },
      required: ["process_id"],
      additionalProperties: false,
    },
  },
  {
    name: "process_stop",
    description: "Stop a CWapi-owned process in the same exact-commit workspace.",
    inputSchema: {
      type: "object",
      properties: { process_id: { type: "string" }, ...contextProperties },
      required: ["process_id"],
      additionalProperties: false,
    },
  },
];

function send(message) {
  process.stdout.write(`${JSON.stringify(message)}\n`);
}

function failure(code, message) {
  const error = new Error(message || code);
  error.code = code;
  return error;
}

function assertContext(args) {
  if (contextArgumentNames.some((name) => typeof args[name] !== "string")) {
    throw failure("PROCESS_CONTEXT_INVALID", "CWapi exact-commit process context is missing or invalid");
  }
  const workspace = args._cwapi_workspace;
  const expectedCommit = args._cwapi_expected_commit.toLowerCase();
  const requestID = args._cwapi_request_id;
  if (!path.isAbsolute(workspace) || !/^[0-9a-f]{40}$/.test(expectedCommit) || !/^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$/.test(requestID)) {
    throw failure("PROCESS_CONTEXT_INVALID", "CWapi exact-commit process context is missing or invalid");
  }
  return { workspace: path.resolve(workspace), expectedCommit, requestID };
}

function trimRecords() {
  if (records.size < 64) return;
  const terminal = [...records.values()].filter((record) => record.state !== "running" && record.state !== "starting");
  terminal.sort((left, right) => left.startedAt - right.startedAt);
  for (const record of terminal.slice(0, Math.max(0, records.size - 48))) records.delete(record.id);
}

function tail(file) {
  try {
    const stat = fs.statSync(file);
    const length = Math.min(stat.size, MAX_TAIL_BYTES);
    if (!length) return "";
    const descriptor = fs.openSync(file, "r");
    const buffer = Buffer.alloc(length);
    fs.readSync(descriptor, buffer, 0, length, stat.size - length);
    fs.closeSync(descriptor);
    return redact(buffer.toString("utf8"));
  } catch {
    return "";
  }
}

function publicRecord(record) {
  return {
    process_id: record.id,
    state: record.state,
    invocation_kind: record.invocationKind,
    ...record.publicFields,
    expected_commit: record.expectedCommit,
    started_at: record.startedAt,
    updated_at: record.updatedAt,
    exit_code: record.exitCode,
    stdout_tail: tail(record.stdoutLog),
    stderr_tail: tail(record.stderrLog),
    ...(record.error ? { error: record.error } : {}),
  };
}

async function startProcess(args) {
  const context = assertContext(args);
  const invocation = resolveInvocation(args, context.workspace);
  const active = invocation.activeKey && [...records.values()].find((record) =>
    record.workspace === context.workspace && record.activeKey === invocation.activeKey && ["starting", "running"].includes(record.state));
  if (active) throw failure("PROCESS_ALREADY_RUNNING", `entrypoint already has an active process: ${active.id}`);
  if ([...records.values()].filter((record) => ["starting", "running"].includes(record.state)).length >= MAX_PROCESSES) {
    throw failure("PROCESS_LIMIT_REACHED", `at most ${MAX_PROCESSES} processes may run at once`);
  }

  trimRecords();
  const id = `proc-${crypto.randomBytes(12).toString("hex")}`;
  const logRoot = path.resolve(process.env.CWAPI_PROCESS_LOG_ROOT || path.join(os.tmpdir(), "CWapi", "processes"));
  const stdoutLog = path.join(logRoot, `${id}.stdout.log`);
  const stderrLog = path.join(logRoot, `${id}.stderr.log`);
  try {
    fs.mkdirSync(logRoot, { recursive: true });
    fs.writeFileSync(stdoutLog, "", { flag: "a", mode: 0o600 });
    fs.writeFileSync(stderrLog, "", { flag: "a", mode: 0o600 });
  } catch {
    throw failure("PROCESS_LOG_UNAVAILABLE", "process log storage is unavailable");
  }
  const now = Date.now();
  const record = {
    id, child: null, workspace: context.workspace, expectedCommit: context.expectedCommit,
    invocationKind: invocation.kind, activeKey: invocation.activeKey, publicFields: invocation.publicFields,
    stdoutLog, stderrLog, state: "starting", exitCode: null, startedAt: now, updatedAt: now,
    stopRequested: false, spawnError: false,
  };
  records.set(id, record);
  let child;
  try {
    child = spawn(invocation.executable, invocation.arguments, {
      cwd: invocation.cwd,
      env: invocation.environment,
      windowsHide: true,
      windowsVerbatimArguments: invocation.windowsVerbatimArguments,
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (error) {
    record.state = "failed";
    record.spawnError = true;
    record.error = `process failed to start (${String(error.code || "unknown")})`;
    throw failure("PROCESS_START_FAILED", record.error);
  }
  record.child = child;
  drainRedacted(child.stdout, stdoutLog);
  drainRedacted(child.stderr, stderrLog);
  child.once("spawn", () => {
    record.state = "running";
    record.updatedAt = Date.now();
  });
  child.once("error", (error) => {
    record.spawnError = true;
    record.state = "failed";
    record.error = `process failed to start (${String(error.code || "unknown")})`;
    record.updatedAt = Date.now();
  });
  child.once("exit", (code) => {
    record.exitCode = Number.isInteger(code) ? code : null;
    if (!record.spawnError) record.state = record.stopRequested ? "stopped" : code === 0 ? "completed" : "failed";
    record.updatedAt = Date.now();
  });
  await new Promise((resolve) => setTimeout(resolve, 700));
  if (record.spawnError) {
    throw failure("PROCESS_START_FAILED", record.error || tail(stderrLog) || `process exited early with code ${record.exitCode}`);
  }
  return publicRecord(record);
}

function ownedRecord(args) {
  const context = assertContext(args);
  if (typeof args.process_id !== "string" || !/^proc-[a-f0-9]{24}$/.test(args.process_id)) {
    throw failure("PROCESS_ID_INVALID", "process_id is missing or invalid");
  }
  const record = records.get(args.process_id);
  if (!record || record.workspace !== context.workspace || record.expectedCommit !== context.expectedCommit) {
    throw failure("PROCESS_NOT_FOUND", "process is not owned by this exact-commit workspace");
  }
  return record;
}

function terminateOwnedTree(record, force) {
  if (!record.child || !record.child.pid) return;
  if (process.platform === "win32") {
    const root = process.env.SystemRoot || process.env.WINDIR || "C:\\Windows";
    const executable = path.join(root, "System32", "taskkill.exe");
    const args = ["/PID", String(record.child.pid), "/T", "/F"];
    spawnSync(executable, args, { windowsHide: true, stdio: "ignore" });
    return;
  }
  try { record.child.kill(force ? "SIGKILL" : "SIGTERM"); } catch {}
}

async function stopProcess(args) {
  const record = ownedRecord(args);
  if (!["starting", "running"].includes(record.state)) return publicRecord(record);
  record.stopRequested = true;
  terminateOwnedTree(record, false);
  const deadline = Date.now() + 3000;
  while (["starting", "running"].includes(record.state) && Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  if (["starting", "running"].includes(record.state)) {
    terminateOwnedTree(record, true);
    const forceDeadline = Date.now() + 1000;
    while (["starting", "running"].includes(record.state) && Date.now() < forceDeadline) {
      await new Promise((resolve) => setTimeout(resolve, 25));
    }
  }
  if (["starting", "running"].includes(record.state)) {
    throw failure("PROCESS_STOP_FAILED", "owned process did not terminate within the bounded stop window");
  }
  return publicRecord(record);
}

async function callTool(name, args) {
  if (!args || typeof args !== "object" || Array.isArray(args)) throw failure("PROCESS_ARGUMENTS_INVALID", "tool arguments must be an object");
  const publicArguments = name === "process_start" ? ["runtime", "entrypoint", "command", "argv", "cwd"] : ["process_status", "process_stop"].includes(name) ? ["process_id"] : null;
  if (!publicArguments) throw failure("PROCESS_TOOL_NOT_FOUND", `unknown tool: ${name}`);
  const allowedArguments = new Set([...publicArguments, ...contextArgumentNames]);
  for (const key of Object.keys(args)) {
    if (!allowedArguments.has(key)) throw failure("PROCESS_ARGUMENT_UNSUPPORTED", `unsupported process argument: ${key}`);
  }
  if (name === "process_start") return startProcess(args);
  if (name === "process_status") return publicRecord(ownedRecord(args));
  return stopProcess(args);
}

async function handle(message) {
  if (!message || message.jsonrpc !== "2.0" || typeof message.method !== "string") return;
  if (!("id" in message)) return;
  const response = { jsonrpc: "2.0", id: message.id };
  try {
    switch (message.method) {
      case "initialize":
        response.result = {
          protocolVersion: message.params?.protocolVersion || "2024-11-05",
          capabilities: { tools: { listChanged: false } },
          serverInfo: { name: "cwapi-process", version: SERVER_VERSION },
        };
        break;
      case "ping":
        response.result = {};
        break;
      case "tools/list":
        response.result = { tools };
        break;
      case "tools/call": {
        try {
          const result = await callTool(message.params?.name, message.params?.arguments || {});
          response.result = { content: [{ type: "text", text: JSON.stringify(result) }] };
        } catch (error) {
          response.result = {
            isError: true,
            content: [{ type: "text", text: JSON.stringify({ error: { code: error.code || "PROCESS_FAILED", message: error.message } }) }],
          };
        }
        break;
      }
      default:
        response.error = { code: -32601, message: `Method not found: ${message.method}` };
    }
  } catch (error) {
    response.error = { code: -32603, message: error.message || "Internal error" };
  }
  send(response);
}

let input = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", (chunk) => {
  input += chunk;
  for (;;) {
    const newline = input.indexOf("\n");
    if (newline < 0) break;
    const line = input.slice(0, newline).replace(/\r$/, "");
    input = input.slice(newline + 1);
    if (!line.trim()) continue;
    try {
      void handle(JSON.parse(line));
    } catch (error) {
      process.stderr.write(`CWAPI_PROCESS_MCP_INPUT_INVALID ${error.message}\n`);
    }
  }
});

function stopAll() {
  for (const record of records.values()) {
    if (["starting", "running"].includes(record.state) && record.child) {
      record.stopRequested = true;
      terminateOwnedTree(record, true);
    }
  }
}

process.stdin.on("end", () => { stopAll(); });
process.on("SIGTERM", () => { stopAll(); process.exit(0); });
process.on("SIGINT", () => { stopAll(); process.exit(0); });
