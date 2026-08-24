import { app } from "../wailsjs/go/models";

export type DesktopState = app.DesktopSnapshot;
export type Tone = "green" | "yellow" | "red" | "gray";
export type LatestRecord = {
  timestamp: number;
  clock: string;
  source: string;
  identity: string;
  data: string;
  tone: Tone;
};

export function statusTone(status: unknown): Tone {
  const value = String(status ?? "").toLowerCase();
  if (["healthy", "ready", "connected", "running", "completed", "delivered"].includes(value)) return "green";
  if (["starting", "connecting", "received", "claimed", "preparing_workspace", "pending", "degraded", "attention"].includes(value)) return "yellow";
  if (["failed", "error", "fatal", "blocked", "unavailable"].includes(value)) return "red";
  return "gray";
}

export function isActiveProcess(process: app.ProcessSnapshot): boolean {
  return process.state === "starting" || process.state === "running";
}

export function activeProcess(processes: app.ProcessSnapshot[]): app.ProcessSnapshot | undefined {
  return processes.find(isActiveProcess) ?? processes[0];
}

export function shortProcessID(value: string): string {
  if (value.length <= 17) return value || "—";
  return `${value.slice(0, 10)}…${value.slice(-4)}`;
}

export function shortCommit(value: string): string {
  return value ? value.slice(0, 8) : "—";
}

export function elapsedText(process: app.ProcessSnapshot, generatedAt: number): string {
  const terminal = !isActiveProcess(process);
  const end = terminal && process.updated_at ? process.updated_at : generatedAt;
  const milliseconds = Math.max(0, end - process.started_at);
  if (milliseconds < 1000) return `${milliseconds} ms`;
  const total = Math.floor(milliseconds / 1000);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  return [hours, minutes, seconds].map((part) => String(part).padStart(2, "0")).join(":");
}

function clock(timestamp: number): string {
  if (!timestamp) return "--:--:--.---";
  const value = new Date(timestamp);
  const base = [value.getHours(), value.getMinutes(), value.getSeconds()].map((part) => String(part).padStart(2, "0")).join(":");
  return `${base}.${String(value.getMilliseconds()).padStart(3, "0")}`;
}

function lastLine(value: string): string {
  const lines = String(value || "").replace(/\r/g, "").split("\n");
  for (let index = lines.length - 1; index >= 0; index--) {
    if (lines[index].trim()) return lines[index].trimEnd();
  }
  return "";
}

function dataValue(value: unknown): string {
  if (typeof value === "string" && /^[\w./:@+-]+$/.test(value)) return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try { return JSON.stringify(value); } catch { return JSON.stringify(String(value)); }
}

function executionData(event: app.ExecutionEventSnapshot): string {
  let data: Record<string, unknown> = {};
  try {
    const parsed = JSON.parse(event.data_json || "{}");
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) data = parsed;
  } catch { data = { data_json: event.data_json }; }
  const fields: Record<string, unknown> = { status: event.status, kind: event.kind };
  if (event.step_id) fields.step = event.step_id;
  if (event.duration_ms) fields.duration_ms = event.duration_ms;
  Object.assign(fields, data);
  return Object.entries(fields)
    .filter(([, value]) => value !== "" && value !== undefined && value !== null)
    .map(([key, value]) => `${key}=${dataValue(value)}`)
    .join(" ") || "state=idle";
}

function runtimeData(record: app.RuntimeLogSnapshot): string {
  let data: Record<string, unknown> = {};
  try {
    const parsed = JSON.parse(record.fields_json || "{}");
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) data = parsed;
  } catch { data = { fields_json: record.fields_json }; }
  const fields: Record<string, unknown> = { level: record.level, message: record.message };
  Object.assign(fields, data);
  return Object.entries(fields)
    .filter(([, value]) => value !== "" && value !== undefined && value !== null)
    .map(([key, value]) => `${key}=${dataValue(value)}`)
    .join(" ") || "level=info";
}

function processRecords(process?: app.ProcessSnapshot): LatestRecord[] {
  if (!process) return [];
  const stateRecord: LatestRecord = {
    timestamp: process.updated_at,
    clock: clock(process.updated_at),
    source: "state",
    identity: shortProcessID(process.process_id),
    data: `state=${process.state} backend=${process.backend}${process.exit_code === undefined ? "" : ` exit_code=${process.exit_code}`}`,
    tone: statusTone(process.state),
  };
  const stream = process.latest_stream;
  const output = stream === "stderr" ? process.stderr_tail : stream === "stdout" ? process.stdout_tail : "";
  const line = lastLine(output);
  if (!line || !process.latest_output_at) return [stateRecord];
  return [stateRecord, {
    timestamp: process.latest_output_at,
    clock: clock(process.latest_output_at),
    source: stream,
    identity: shortProcessID(process.process_id),
    data: line,
    tone: stream === "stderr" ? "red" : "gray",
  }];
}

export function buildLatestRecord(snapshot: DesktopState | null, local: LatestRecord | null): LatestRecord {
  const candidates: LatestRecord[] = local ? [local] : [];
  if (snapshot) {
    candidates.push(...processRecords(activeProcess(snapshot.processes)));
    if (snapshot.latest_execution) candidates.push({
      timestamp: snapshot.latest_execution.timestamp,
      clock: clock(snapshot.latest_execution.timestamp),
      source: "event",
      identity: snapshot.latest_execution.kind || "execution",
      data: executionData(snapshot.latest_execution),
      tone: statusTone(snapshot.latest_execution.status),
    });
    if (snapshot.latest_runtime_error) candidates.push({
      timestamp: snapshot.latest_runtime_error.timestamp,
      clock: clock(snapshot.latest_runtime_error.timestamp),
      source: "runtime",
      identity: snapshot.latest_runtime_error.component || "cwapi",
      data: runtimeData(snapshot.latest_runtime_error),
      tone: statusTone(snapshot.latest_runtime_error.level),
    });
  }
  candidates.sort((left, right) => right.timestamp - left.timestamp);
  return candidates[0] ?? { timestamp: 0, clock: clock(0), source: "state", identity: "cwapi", data: "state=idle active=0", tone: "gray" };
}
