import { describe, expect, it } from "vitest";
import { SlackE2EExpectation, validateMCPResources, validateMCPResponse } from "./real_slack_probe";

const toolExpectation: SlackE2EExpectation = {
  request_id: "REQ123",
  method: "mcpServer/tool/call",
  status: "completed",
};

describe("validateMCPResponse", () => {
  it("accepts a successful MCP tool result", () => {
    expect(() => validateMCPResponse({ status: "completed", result: { content: [] } }, toolExpectation)).not.toThrow();
  });

  it("rejects a completed MCP tool error", () => {
    expect(() => validateMCPResponse({ status: "completed", result: { isError: true } }, toolExpectation))
      .toThrow("SLACK_MCP_E2E_TOOL_RESULT_ERROR request=REQ123");
  });

  it("rejects a response status that disagrees with the expectation", () => {
    expect(() => validateMCPResponse({ status: "failed" }, toolExpectation))
      .toThrow("SLACK_MCP_E2E_RESPONSE_STATUS_MISMATCH request=REQ123 expected=completed actual=failed");
  });

  it("requires expected state from a multi-step tool result", () => {
    const expected = { ...toolExpectation, result_text_contains: "https://github.com/AAAYNMMM/CWapi" };
    expect(() => validateMCPResponse({ status: "completed", result: { content: [{ text: "Page URL: about:blank" }] } }, expected))
      .toThrow("SLACK_MCP_E2E_RESULT_TEXT_MISMATCH request=REQ123 expected=https://github.com/AAAYNMMM/CWapi");
    expect(() => validateMCPResponse({ status: "completed", result: { content: [{ text: "Page URL: https://github.com/AAAYNMMM/CWapi" }] } }, expected))
      .not.toThrow();
  });
});

describe("validateMCPResources", () => {
  it("rejects a blank-sized screenshot resource", () => {
    const expected = { ...toolExpectation, min_resources: 1, min_resource_bytes: 10000 };
    expect(() => validateMCPResources([{ size_bytes: 4254 }], expected))
      .toThrow("SLACK_MCP_E2E_RESOURCE_SIZE_MISMATCH request=REQ123 expected_at_least=10000");
    expect(() => validateMCPResources([{ size_bytes: 25000 }], expected)).not.toThrow();
  });
});
