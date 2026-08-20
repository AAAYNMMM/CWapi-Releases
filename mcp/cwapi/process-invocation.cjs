"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");

const MAX_COMMAND_BYTES = 32768;
const MAX_ARGUMENTS = 256;
const MAX_ARGUMENT_BYTES = 32768;
const MAX_ARGUMENTS_BYTES = 131072;

function failure(code, message) {
  const error = new Error(message || code);
  error.code = code;
  return error;
}

function inside(root, target) {
  const relative = path.relative(root, target);
  return relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative);
}

function resolveWorkspacePath(workspace, value, kind) {
  if (typeof value !== "string") throw failure(`PROCESS_${kind}_INVALID`, `${kind.toLowerCase()} must be a string`);
  if (!value || value.length > 512 || value.includes("\\") || value.includes("\0") || path.posix.isAbsolute(value)) {
    throw failure(`PROCESS_${kind}_INVALID`, `${kind.toLowerCase()} must be a bounded repository-relative path using forward slashes`);
  }
  const clean = path.posix.normalize(value);
  if (clean !== value || clean === ".." || clean.startsWith("../")) {
    throw failure(`PROCESS_${kind}_INVALID`, `${kind.toLowerCase()} escapes the exact-commit workspace`);
  }
  const absolute = path.resolve(workspace, ...clean.split("/"));
  if (!inside(workspace, absolute)) throw failure(`PROCESS_${kind}_INVALID`, `${kind.toLowerCase()} escapes the exact-commit workspace`);
  let workspaceReal;
  let absoluteReal;
  try {
    workspaceReal = fs.realpathSync(workspace);
    absoluteReal = fs.realpathSync(absolute);
  } catch {
    throw failure(`PROCESS_${kind}_NOT_FOUND`, `${kind.toLowerCase()} does not exist in the exact-commit workspace`);
  }
  if (!inside(workspaceReal, absoluteReal)) {
    throw failure(`PROCESS_${kind}_INVALID`, `${kind.toLowerCase()} resolves outside the exact-commit workspace`);
  }
  return { relative: clean, absolute: absoluteReal };
}

function resolveEntrypoint(workspace, value, runtime) {
  const entrypoint = resolveWorkspacePath(workspace, value, "ENTRYPOINT");
  const extension = path.posix.extname(entrypoint.relative).toLowerCase();
  if (runtime === "python" && extension !== ".py") {
    throw failure("PROCESS_ENTRYPOINT_TYPE_INVALID", "python runtime requires a .py entrypoint");
  }
  if (runtime === "node" && ![".js", ".cjs", ".mjs"].includes(extension)) {
    throw failure("PROCESS_ENTRYPOINT_TYPE_INVALID", "node runtime requires a .js, .cjs or .mjs entrypoint");
  }
  let stat;
  try { stat = fs.statSync(entrypoint.absolute); } catch { throw failure("PROCESS_ENTRYPOINT_NOT_FOUND", "entrypoint is unavailable"); }
  if (!stat.isFile()) throw failure("PROCESS_ENTRYPOINT_NOT_FILE", "entrypoint is not a regular file");
  return entrypoint;
}

function resolveWorkingDirectory(workspace, value) {
  if (value === undefined) {
    try { return { relative: ".", absolute: fs.realpathSync(workspace) }; }
    catch { throw failure("PROCESS_WORKING_DIRECTORY_NOT_FOUND", "exact-commit workspace is unavailable"); }
  }
  const directory = resolveWorkspacePath(workspace, value, "WORKING_DIRECTORY");
  let stat;
  try { stat = fs.statSync(directory.absolute); } catch { throw failure("PROCESS_WORKING_DIRECTORY_NOT_FOUND", "working directory is unavailable"); }
  if (!stat.isDirectory()) throw failure("PROCESS_WORKING_DIRECTORY_NOT_DIRECTORY", "working directory is not a directory");
  return directory;
}

function executableFor(runtime) {
  if (runtime === "node") return process.execPath;
  if (runtime === "python") return process.env.CWAPI_PYTHON || (process.platform === "win32" ? "python.exe" : "python3");
  throw failure("PROCESS_RUNTIME_UNSUPPORTED", "runtime must be python or node");
}

function resolveLegacyInvocation(args, workspace) {
  if (args.runtime !== "python" && args.runtime !== "node") {
    throw failure("PROCESS_RUNTIME_UNSUPPORTED", "runtime must be python or node");
  }
  const entrypoint = resolveEntrypoint(workspace, args.entrypoint, args.runtime);
  let hash;
  try { hash = crypto.createHash("sha256").update(fs.readFileSync(entrypoint.absolute)).digest("hex"); }
  catch { throw failure("PROCESS_ENTRYPOINT_READ_FAILED", "entrypoint could not be read"); }
  return {
    kind: "runtime_entrypoint",
    executable: executableFor(args.runtime),
    arguments: [entrypoint.absolute],
    cwd: workspace,
    environment: args.runtime === "python" ? { ...process.env, PYTHONUNBUFFERED: "1" } : process.env,
    activeKey: `${args.runtime}:${entrypoint.relative}`,
    publicFields: { runtime: args.runtime, entrypoint: entrypoint.relative, entrypoint_sha256: hash },
  };
}

function commandName(command) {
  const parts = command.split(/[\\/]/);
  return parts[parts.length - 1] || command;
}

function hasPathSyntax(command) {
  return path.isAbsolute(command) || command.includes("/") || command.includes("\\");
}

function resolveCommandExecutable(command, cwd) {
  if (!hasPathSyntax(command)) {
    if (process.platform === "win32" && /^[A-Za-z]:/.test(command)) {
      throw failure("PROCESS_COMMAND_PATH_INVALID", "drive-relative command paths are not supported; use C:/path/tool.exe or C:\\path\\tool.exe");
    }
    return { executable: command, publicPath: command, resolution: "path_lookup" };
  }
  const wasAbsolute = path.isAbsolute(command);
  const candidate = wasAbsolute ? path.resolve(command) : path.resolve(cwd, command);
  let resolved;
  try { resolved = fs.realpathSync(candidate); }
  catch { throw failure("PROCESS_COMMAND_NOT_FOUND", "command path does not exist"); }
  let stat;
  try { stat = fs.statSync(resolved); }
  catch { throw failure("PROCESS_COMMAND_NOT_FOUND", "command path is unavailable"); }
  if (!stat.isFile()) throw failure("PROCESS_COMMAND_NOT_FILE", "command path is not a regular file");
  return {
    executable: resolved,
    publicPath: wasAbsolute ? resolved : path.normalize(command),
    resolution: wasAbsolute ? "absolute_path" : "working_directory_relative",
  };
}

function quoteCommandScriptToken(value) {
  return `"${value.replaceAll('"', '""')}"`;
}

function resolveCommandSpawn(target, argv, environment) {
  const extension = path.extname(target.executable).toLowerCase();
  if (process.platform !== "win32" || ![".cmd", ".bat"].includes(extension)) {
    return { executable: target.executable, arguments: argv, environment, executableKind: "native" };
  }
  const windowsRoot = environment.SystemRoot || environment.WINDIR || "C:\\Windows";
  const commandInterpreter = environment.ComSpec || environment.COMSPEC || path.join(windowsRoot, "System32", "cmd.exe");
  const commandLine = `"${[target.executable, ...argv].map(quoteCommandScriptToken).join(" ")}"`;
  return {
    executable: commandInterpreter,
    arguments: ["/d", "/s", "/v:off", "/c", commandLine],
    environment,
    windowsVerbatimArguments: true,
    executableKind: "windows_command_script",
  };
}

function resolveCommandInvocation(args, workspace) {
  if (typeof args.command !== "string" || !args.command.trim() || args.command !== args.command.trim() || args.command.includes("\0") ||
      args.command.includes('"') || Buffer.byteLength(args.command, "utf8") > MAX_COMMAND_BYTES) {
    throw failure("PROCESS_COMMAND_INVALID", `command must be a trimmed, unquoted executable name or path up to ${MAX_COMMAND_BYTES} bytes`);
  }
  const argv = args.argv === undefined ? [] : args.argv;
  if (!Array.isArray(argv) || argv.length > MAX_ARGUMENTS) {
    throw failure("PROCESS_ARGV_INVALID", `argv must be an array with at most ${MAX_ARGUMENTS} strings`);
  }
  let totalBytes = 0;
  for (const value of argv) {
    if (typeof value !== "string" || value.includes("\0") || Buffer.byteLength(value, "utf8") > MAX_ARGUMENT_BYTES) {
      throw failure("PROCESS_ARGV_INVALID", `each argv value must be a string up to ${MAX_ARGUMENT_BYTES} bytes without NUL`);
    }
    totalBytes += Buffer.byteLength(value, "utf8");
  }
  if (totalBytes > MAX_ARGUMENTS_BYTES) {
    throw failure("PROCESS_ARGV_INVALID", `combined argv must not exceed ${MAX_ARGUMENTS_BYTES} bytes`);
  }
  const directory = resolveWorkingDirectory(workspace, args.cwd);
  const target = resolveCommandExecutable(args.command, directory.absolute);
  const spawn = resolveCommandSpawn(target, argv, process.env);
  const fingerprint = crypto.createHash("sha256").update(JSON.stringify([target.publicPath, argv, directory.relative])).digest("hex");
  return {
    kind: "command_argv",
    executable: spawn.executable,
    arguments: spawn.arguments,
    cwd: directory.absolute,
    environment: spawn.environment,
    windowsVerbatimArguments: spawn.windowsVerbatimArguments === true,
    activeKey: null,
    publicFields: {
      command_name: commandName(target.executable),
      command_path: target.publicPath,
      command_resolution: target.resolution,
      executable_kind: spawn.executableKind,
      argv_count: argv.length,
      working_directory: directory.relative,
      invocation_sha256: fingerprint,
    },
  };
}

function resolveInvocation(args, workspace) {
  const hasCommand = Object.hasOwn(args, "command");
  const hasLegacy = Object.hasOwn(args, "runtime") || Object.hasOwn(args, "entrypoint");
  if (hasCommand && hasLegacy) {
    throw failure("PROCESS_INVOCATION_CONFLICT", "use either command/argv or runtime/entrypoint, not both");
  }
  if (hasCommand) return resolveCommandInvocation(args, workspace);
  if (Object.hasOwn(args, "argv") || Object.hasOwn(args, "cwd")) {
    throw failure("PROCESS_COMMAND_REQUIRED", "argv and cwd require command");
  }
  if (hasLegacy) return resolveLegacyInvocation(args, workspace);
  throw failure("PROCESS_INVOCATION_REQUIRED", "process_start requires command or runtime/entrypoint");
}

module.exports = { resolveInvocation };
