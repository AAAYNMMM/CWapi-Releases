# CWapi v1.6.3 Web GPT Entry

这是 Web GPT 使用 CWapi 的最小入口。开始本机调用前，读取并遵守 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。

## 当前链路

```text
Web GPT
→ GitHub：取得 repository URL / exact commit，并处理远端写入、PR、Issue、CI 等 GitHub-native 信息
→ Slack：发送 CWapi 请求
→ CWapi：复用对应 exact commit 的 persistent workspace 做本地执行，并承担可由当前返回边界覆盖的重复源码检查/文本搜索
→ Slack：返回结果或文件
```

CWapi 不运行模型，也不要求 ChatGPT Web 与本机建立直接 MCP 连接。

当前 v1.6.3 还没有专用 `repo_read` / `repo_search` virtual tool。短文本源码检查和搜索可以通过 repository-scoped、只读 `process_start` 复用本地 workspace；完整大文件或超过 process output tail 的源码读取仍可直接使用 GitHub Connector。不要把这条当前兼容路径误写成已经存在的 filesystem MCP。

## 开工时必须知道

1. v1.6.3 不使用 project registry。repository 调用直接使用 GitHub repository URL 与完整 40 位 commit。

2. 只使用 CWapi MCP v2：

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2",...}
+++
```

3. 每个新动作使用新的 `request_id`。已经得到 `process_id` 后，后续查询原进程，不重复启动同一任务。

4. Windows path 在 MCP JSON 中优先使用 `/`。

5. 同一 repository 在当前 CWapi 进程内使用 persistent workspace。进入本地工作链后，同 repo/commit 的短文本检查与搜索优先复用这个 workspace，不要为了读取同一 tracked source 无理由反复绕回 GitHub。默认仍把工作拆成可独立验证的小步骤，不要仅为了减少 Slack 往返次数强行合并成一个大型脚本或一次 `process_start`。

6. GitHub 仍是远端源码真相和 exact commit 来源。需要新的 commit、远端写入、PR / Issue / CI / Review / Release，或者需要超过当前本地返回边界的完整源码时，继续使用 GitHub。

7. 不要自行假设本机环境或固定工具路径。环境发现、SAFE/FULL、安装依赖、浏览器测试、截图和等待规则统一按照 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md) 执行。

## 完整工作流

[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)
