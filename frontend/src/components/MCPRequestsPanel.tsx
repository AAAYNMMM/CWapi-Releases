import { durationText, formatTime, MCPRequestState, statusText, statusTone } from "../app_model";

function requestState(request: MCPRequestState): string {
  if (request.terminal) return request.execution_state || "completed";
  return request.execution_state || "received";
}

export function MCPRequestsPanel({ requests }: { requests: MCPRequestState[] }) {
  return (
    <section className="panel" data-testid="mcp-request-surface">
      <div className="panel-title-row">
        <div>
          <h2>最近 MCP 请求</h2>
          <p>显示真实 Slack MCP 请求、当前 Tool、耗时和结果投递状态。</p>
        </div>
        <span>{requests.length} 条</span>
      </div>
      <div className="component-list">
        {requests.slice(0, 12).map((request) => {
          const state = requestState(request);
          const operation = request.tool_name || request.method;
          return (
            <div className="diagnostic-issue" key={request.request_id} data-testid={`mcp-request-${request.request_id}`}>
              <span className={`status-dot status-dot-${statusTone(state)}`} />
              <div>
                <strong>{operation}</strong>
                <p><code>{request.request_id}</code></p>
                <small>
                  {formatTime(request.created_at)} · {durationText(request.elapsed_ms)} · 投递 {statusText(request.delivery_state)}
                </small>
              </div>
              <div className="request-actions"><span>{statusText(state)}</span></div>
            </div>
          );
        })}
        {requests.length === 0 && <div className="empty-state">还没有收到 MCP 请求。</div>}
      </div>
    </section>
  );
}
