import { ReadinessSnapshot, RecentSlackProtocol } from "../wailsjs/go/main/App";

export type SlackE2EExpectation = {
  request_id: string;
  method?: "projects/list" | "mcpServerStatus/list" | "mcpServer/resource/read" | "mcpServer/tool/call";
  server?: string;
  tool_name?: string;
  project_id?: string;
  require_exact_commit?: boolean;
  status?: string;
  delivery_state?: string;
  min_request_messages?: number;
  min_events?: number;
  min_resources?: number;
  min_resource_bytes?: number;
  result_text_contains?: string;
  require_response?: boolean;
  require_codex_running?: boolean;
};

export type RealSlackProbeConfig = {
  mode: "real-slack";
  source_commit: string;
  timeout_seconds?: number;
  expectations: {
    schema: "cwapi.slack-mcp-e2e.expectations.v1";
    requests: SlackE2EExpectation[];
  };
};

type ProtocolMessage = Awaited<ReturnType<typeof RecentSlackProtocol>>[number];

type CompletedRequest = Record<string, unknown> & {
  request_id: string;
  request_messages: number;
  event_messages: number;
  response_messages: number;
  resource_count: number;
};

type RealSlackProbeResult = {
  mode: "real-slack";
  success: boolean;
  checks: string[];
  evidence?: Record<string, unknown>;
  error?: string;
};

const sleep = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms));

function exactSubject(family: string, requestID: string): string {
  return `[CWapi/MCP/1][${family}][${requestID}]`;
}

function subjectMessages(messages: Awaited<ReturnType<typeof RecentSlackProtocol>>, family: string, requestID: string): ProtocolMessage[] {
  const subject = exactSubject(family, requestID);
  return messages.filter((message) => message.subject === subject);
}

function countSubject(messages: Awaited<ReturnType<typeof RecentSlackProtocol>>, family: string, requestID: string): number {
  return subjectMessages(messages, family, requestID).length;
}

function parseBody(message: ProtocolMessage | undefined): Record<string, unknown> | undefined {
  if (!message?.body) return undefined;
  try {
    const parsed = JSON.parse(message.body);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as Record<string, unknown> : undefined;
  } catch {
    return undefined;
  }
}

export function validateMCPResponse(response: Record<string, unknown>, expected: SlackE2EExpectation): void {
  const expectedStatus = expected.status || "completed";
  if (response.status !== expectedStatus) {
    throw new Error(`SLACK_MCP_E2E_RESPONSE_STATUS_MISMATCH request=${expected.request_id} expected=${expectedStatus} actual=${response.status || "missing"}`);
  }
  const result = response.result;
  if (expected.method === "mcpServer/tool/call" && result && typeof result === "object" && !Array.isArray(result) &&
      (result as Record<string, unknown>).isError === true) {
    throw new Error(`SLACK_MCP_E2E_TOOL_RESULT_ERROR request=${expected.request_id}`);
  }
  if (expected.result_text_contains && !JSON.stringify(result).includes(expected.result_text_contains)) {
    throw new Error(`SLACK_MCP_E2E_RESULT_TEXT_MISMATCH request=${expected.request_id} expected=${expected.result_text_contains}`);
  }
}

export function validateMCPResources(resources: Array<Record<string, unknown>>, expected: SlackE2EExpectation): void {
  const minimumResources = Math.max(0, Number(expected.min_resources || 0));
  if (resources.length < minimumResources) {
    throw new Error(`SLACK_MCP_E2E_RESOURCE_COUNT_MISMATCH request=${expected.request_id} expected=${minimumResources} actual=${resources.length}`);
  }
  const minimumBytes = Math.max(0, Number(expected.min_resource_bytes || 0));
  if (minimumBytes > 0 && !resources.some((resource) => Number(resource.size_bytes || 0) >= minimumBytes)) {
    throw new Error(`SLACK_MCP_E2E_RESOURCE_SIZE_MISMATCH request=${expected.request_id} expected_at_least=${minimumBytes}`);
  }
}

function requestMatches(
  actual: Awaited<ReturnType<typeof ReadinessSnapshot>>["recent_requests"][number],
  expected: SlackE2EExpectation,
): boolean {
  if (actual.request_id !== expected.request_id) return false;
  if (expected.method && actual.method !== expected.method) return false;
  if (expected.tool_name && actual.tool_name !== expected.tool_name) return false;
  return true;
}

function validateRequestEnvelope(
  messages: Awaited<ReturnType<typeof RecentSlackProtocol>>,
  expected: SlackE2EExpectation,
  sourceCommit: string,
): boolean {
  const request = parseBody(subjectMessages(messages, "MCP_REQUEST", expected.request_id)[0]);
  if (!request) return false;
  if (request.request_id !== expected.request_id) throw new Error(`SLACK_MCP_E2E_REQUEST_BODY_ID_MISMATCH request=${expected.request_id}`);
  if (expected.method && request.method !== expected.method) throw new Error(`SLACK_MCP_E2E_REQUEST_METHOD_MISMATCH request=${expected.request_id}`);
  if (expected.project_id && request.project_id !== expected.project_id) throw new Error(`SLACK_MCP_E2E_PROJECT_MISMATCH request=${expected.request_id}`);
  if (expected.require_exact_commit && String(request.expected_commit || "").toLowerCase() !== sourceCommit) {
    throw new Error(`SLACK_MCP_E2E_EXACT_COMMIT_MISMATCH request=${expected.request_id} expected=${sourceCommit} actual=${request.expected_commit || ""}`);
  }
  if (expected.server || expected.tool_name) {
    const params = request.params;
    if (!params || typeof params !== "object" || Array.isArray(params)) throw new Error(`SLACK_MCP_E2E_PARAMS_INVALID request=${expected.request_id}`);
    const values = params as Record<string, unknown>;
    if (expected.server && values.server !== expected.server) throw new Error(`SLACK_MCP_E2E_SERVER_MISMATCH request=${expected.request_id}`);
    if (expected.tool_name && values.tool !== expected.tool_name) throw new Error(`SLACK_MCP_E2E_TOOL_MISMATCH request=${expected.request_id}`);
  }
  return true;
}

function terminalReady(
  actual: Awaited<ReturnType<typeof ReadinessSnapshot>>["recent_requests"][number],
  expected: SlackE2EExpectation,
): boolean {
  if (!actual.terminal) return false;
  const expectedStatus = expected.status || "completed";
  const expectedDelivery = expected.delivery_state || "delivered";
  if (actual.execution_state !== expectedStatus) {
    throw new Error(`SLACK_MCP_E2E_STATUS_MISMATCH request=${actual.request_id} expected=${expectedStatus} actual=${actual.execution_state}`);
  }
  if (actual.delivery_state !== expectedDelivery) {
    if (actual.delivery_state === "pending") return false;
    throw new Error(`SLACK_MCP_E2E_DELIVERY_MISMATCH request=${actual.request_id} expected=${expectedDelivery} actual=${actual.delivery_state}`);
  }
  return true;
}

function responseResources(messages: Awaited<ReturnType<typeof RecentSlackProtocol>>, requestID: string): Array<Record<string, unknown>> {
  const responses = subjectMessages(messages, "MCP_RESPONSE", requestID);
  const response = parseBody(responses[responses.length - 1]);
  if (!response) return [];
  const resources = response.resources;
  if (!Array.isArray(resources)) return [];
  return resources.filter((entry): entry is Record<string, unknown> => Boolean(entry) && typeof entry === "object" && !Array.isArray(entry));
}

function transportReady(messages: Awaited<ReturnType<typeof RecentSlackProtocol>>, expected: SlackE2EExpectation, sourceCommit: string): boolean {
  const requestCount = countSubject(messages, "MCP_REQUEST", expected.request_id);
  const responses = subjectMessages(messages, "MCP_RESPONSE", expected.request_id);
  const responseCount = responses.length;
  const eventCount = countSubject(messages, "MCP_EVENT", expected.request_id);
  if (requestCount < Math.max(1, Number(expected.min_request_messages || 1))) return false;
  if (!validateRequestEnvelope(messages, expected, sourceCommit)) return false;
  if (expected.require_response !== false && responseCount < 1) return false;
  const response = parseBody(responses[responses.length - 1]);
  if (expected.require_response !== false && !response) return false;
  if (response) validateMCPResponse(response, expected);
  if (eventCount < Math.max(0, Number(expected.min_events || 0))) return false;
  validateMCPResources(responseResources(messages, expected.request_id), expected);
  return true;
}

function completedEvidence(
  actual: Awaited<ReturnType<typeof ReadinessSnapshot>>["recent_requests"][number],
  messages: Awaited<ReturnType<typeof RecentSlackProtocol>>,
): CompletedRequest {
  const resources = responseResources(messages, actual.request_id);
  return {
    ...actual,
    request_id: actual.request_id,
    request_messages: countSubject(messages, "MCP_REQUEST", actual.request_id),
    event_messages: countSubject(messages, "MCP_EVENT", actual.request_id),
    response_messages: countSubject(messages, "MCP_RESPONSE", actual.request_id),
    resource_count: resources.length,
    resources,
  };
}

function validateConfig(config: RealSlackProbeConfig) {
  if (config.mode !== "real-slack") throw new Error("SLACK_MCP_E2E_MODE_INVALID");
  if (!/^[0-9a-fA-F]{40}$/.test(config.source_commit || "")) throw new Error("SLACK_MCP_E2E_COMMIT_INVALID");
  if (config.expectations?.schema !== "cwapi.slack-mcp-e2e.expectations.v1") throw new Error("SLACK_MCP_E2E_EXPECTATIONS_INVALID");
  if (!Array.isArray(config.expectations.requests) || config.expectations.requests.length === 0) throw new Error("SLACK_MCP_E2E_EXPECTATIONS_EMPTY");
  const ids = new Set<string>();
  for (const expected of config.expectations.requests) {
    if (!expected.request_id || ids.has(expected.request_id)) throw new Error(`SLACK_MCP_E2E_REQUEST_ID_INVALID:${expected.request_id || "missing"}`);
    if (expected.require_exact_commit && !expected.project_id) throw new Error(`SLACK_MCP_E2E_PROJECT_REQUIRED:${expected.request_id}`);
    ids.add(expected.request_id);
  }
}

export async function runRealSlackProbe(config: RealSlackProbeConfig): Promise<RealSlackProbeResult> {
  const checks: string[] = [];
  try {
    validateConfig(config);
    const expectedCommit = config.source_commit.toLowerCase();
    const timeoutSeconds = Math.max(30, Math.min(1800, Number(config.timeout_seconds || 300)));
    const deadline = Date.now() + timeoutSeconds * 1000;
    let readiness: Awaited<ReturnType<typeof ReadinessSnapshot>> | undefined;

    while (Date.now() < deadline) {
      readiness = await ReadinessSnapshot(100);
      if (String(readiness.runtime?.source_commit || "").toLowerCase() !== expectedCommit) {
        throw new Error(`SLACK_MCP_E2E_COMMIT_MISMATCH expected=${expectedCommit} actual=${readiness.runtime?.source_commit || ""}`);
      }
      if (readiness.slack?.ready && readiness.slack?.socket_ready && readiness.local_ready) break;
      await sleep(500);
    }
    if (!readiness?.slack?.ready || !readiness.slack.socket_ready) {
      throw new Error(`SLACK_MCP_E2E_SLACK_NOT_READY state=${readiness?.slack?.state || "unknown"} detail=${readiness?.slack?.detail || ""}`);
    }
    if (!readiness.local_ready || !readiness.mcp_runtime_ready || !readiness.codex?.ready) {
      throw new Error(`SLACK_MCP_E2E_LOCAL_RUNTIME_NOT_READY detail=${readiness.detail || ""}`);
    }
    checks.push("readiness");

    const completed = new Map<string, CompletedRequest>();
    while (Date.now() < deadline && completed.size < config.expectations.requests.length) {
      const current = await ReadinessSnapshot(100);
      const requests = Array.isArray(current.recent_requests) ? current.recent_requests : [];
      const protocolMessages = await RecentSlackProtocol("[CWapi/MCP/1]", 500);
      if (!Array.isArray(protocolMessages)) throw new Error("SLACK_MCP_E2E_PROTOCOL_INDEX_INVALID");

      for (const expected of config.expectations.requests) {
        if (completed.has(expected.request_id)) continue;
        const matchingRecords = requests.filter((entry) => entry.request_id === expected.request_id);
        if (matchingRecords.length > 1) {
          throw new Error(`SLACK_MCP_E2E_DUPLICATE_STATE_RECORD request=${expected.request_id} count=${matchingRecords.length}`);
        }
        const actual = matchingRecords.find((entry) => requestMatches(entry, expected));
        if (!actual || !terminalReady(actual, expected) || !transportReady(protocolMessages, expected, expectedCommit)) continue;
        if (expected.require_codex_running && !current.codex?.running) {
          throw new Error(`SLACK_MCP_E2E_CODEX_NOT_RUNNING request=${expected.request_id}`);
        }
        completed.set(expected.request_id, completedEvidence(actual, protocolMessages));
      }
      if (completed.size < config.expectations.requests.length) await sleep(500);
    }

    if (completed.size !== config.expectations.requests.length) {
      const missing = config.expectations.requests.filter((entry) => !completed.has(entry.request_id)).map((entry) => entry.request_id);
      throw new Error(`SLACK_MCP_E2E_TIMEOUT missing=${missing.join(",")}`);
    }

    const finalReadiness = await ReadinessSnapshot(100);
    checks.push("requests", "exact-commit", "transport", "terminal-results", "slack-files");
    return {
      mode: "real-slack",
      success: true,
      checks,
      evidence: {
        schema: "cwapi.slack-mcp-e2e.result.v1",
        source_commit: finalReadiness.runtime.source_commit,
        slack_state: finalReadiness.slack.state,
        slack_socket_ready: finalReadiness.slack.socket_ready,
        codex_ready: finalReadiness.codex.ready,
        codex_running: finalReadiness.codex.running,
        mcp_runtime_ready: finalReadiness.mcp_runtime_ready,
        requests: [...completed.values()],
      },
    };
  } catch (cause) {
    return { mode: "real-slack", success: false, checks, error: String(cause) };
  }
}
