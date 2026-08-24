# CWapi v1.6.1 Web GPT Entry

这是 Web GPT 使用 CWapi 的最小入口。开始本机调用前，读取并遵守 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。

## 当前链路

```text
Web GPT
→ GitHub：读写源码并取得 exact commit
→ Slack：发送 CWapi 请求
→ CWapi：在对应 commit 上调用本机能力
→ Slack：返回结果或文件
```

CWapi 不运行模型，也不要求 ChatGPT Web 与本机建立直接 MCP 连接。

## 开工时必须知道

1. v1.6.1 不使用 project registry。repository 调用直接使用 GitHub repository URL 与完整 40 位 commit。

2. 只使用 CWapi MCP v2：

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2",...}
+++
```

3. 每个新动作使用新的 `request_id`。已经得到 `process_id` 后，后续查询原进程，不重复启动同一任务。

4. Windows path 在 MCP JSON 中优先使用 `/`。

5. 不要自行假设本机环境或固定工具路径。环境发现、SAFE/FULL、安装依赖、浏览器测试、截图和等待规则统一按照 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md) 执行。

## 完整工作流

[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)
