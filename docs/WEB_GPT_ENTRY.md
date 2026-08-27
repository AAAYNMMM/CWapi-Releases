# CWapi v1.6.3 Web GPT Entry

这是 Web GPT 使用 CWapi 的最小入口。开始本机调用前，读取并遵守 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。

## 当前链路

```text
Web GPT
→ GitHub：取得 repository URL / exact commit，并处理完整文件读取、远端写入、PR、Issue、CI 等 GitHub-native 信息
→ Slack：发送 CWapi repository 请求
→ CWapi：准备/复用对应 exact commit 的 persistent workspace，执行本地搜索、build、test、run 或 stock MCP
→ Slack：返回结果
```

CWapi 不运行模型，也不要求 ChatGPT Web 与本机建立直接 MCP 连接。

## 开工时必须知道

1. repository 调用直接使用 GitHub repository URL 与完整 40 位 commit。

2. 只使用 CWapi MCP v2：

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2",...}
+++
```

3. 每个新动作使用新的 `request_id`。已经得到 `process_id` 后，后续查询原进程，不重复启动同一任务。

4. Windows path 在 MCP JSON 中优先使用 `/`。

5. 同一 repository 在当前 CWapi 进程内使用 persistent workspace。request terminal 只释放 repository lease，不删除 workspace；衍生物可以在同一 CWapi 进程内复用。

6. 需要在仓库中搜索代码时，由 Web GPT 根据当前问题生成只读搜索命令或短脚本，通过 repository-scoped `process_start` 在 persistent workspace 中执行。搜索结果以文件路径、行号、匹配文本和少量上下文为主；需要完整源码时，再通过 GitHub Connector 精确读取命中的文件。

7. 搜索脚本保持只读，尽量限定目录和文件类型，排除无关的依赖、构建和缓存目录；同一问题的多个相关关键词可以合并到一次搜索中。已经进入对应 exact commit 的 workspace 后，不要为了搜索再次 clone/fetch 同一 repository。

8. GitHub 仍是远端源码真相和 exact commit 来源；完整文件读取、源码写入、commit、branch/history、PR、Issue、Review、Actions/CI、Release 等仍通过 GitHub 处理。

9. 不要自行假设本机环境或固定工具路径。环境发现、SAFE/FULL、安装依赖、浏览器测试、截图和等待规则统一按照 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md) 执行。

## 完整工作流

[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)
