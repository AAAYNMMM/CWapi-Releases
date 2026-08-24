# CWapi v1.6.1 Web GPT Entry

这是 Web GPT 使用 CWapi 的**最小入口**。开始实际本机调用前，读取并遵守 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。协议字段需要查阅时再看 [`PROTOCOL.md`](PROTOCOL.md)。

## 当前链路

```text
Web GPT
→ GitHub：读写源码并取得 exact commit
→ Slack：发送 CWapi MCP v2 request
→ CWapi：准备 exact-commit worktree 并调用本机能力
→ Slack：返回 response / File
```

CWapi 不运行模型，也不要求 ChatGPT Web 与本机建立直接 MCP 连接。

## 开工时必须知道的规则

1. **没有 project registry。** repository 调用直接使用：

```text
repository_url  = https://github.com/owner/repo
expected_commit = 完整 40 位 Git SHA
```

2. **只使用 MCP v2。** 请求 frame 为：

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2",...}
+++
```

3. **每个新动作使用新的 request_id。** 已得到 `process_id` 后，刷新状态使用新的 global `process_status` request，不重复 `process_start`。

4. **Windows path 在 MCP JSON 中优先使用 `/`。** 不要把未转义反斜杠直接放进 JSON。

5. **不要自行假设本机环境或固定工具路径。** 环境发现、SAFE/FULL、安装依赖、Playwright、截图和外部等待规则全部以 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md) 为准。

## 常用文档

- [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)：Web GPT 完整执行规则。
- [`PROTOCOL.md`](PROTOCOL.md)：MCP v2 wire contract。
- [`SECURITY.md`](SECURITY.md)：权限边界。
- [`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)：Slack 文件交付。
- [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)：错误排查。
