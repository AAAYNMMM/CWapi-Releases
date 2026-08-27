# CWapi v1.6.3 Web GPT Entry

这是 Web GPT 使用 CWapi 的最小入口。开始本机调用前，读取并遵守 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。

## 当前 v1.6.3 链路

```text
Web GPT
→ GitHub：源码搜索、完整文件读取、远端写入、取得 repository URL / exact commit，以及 PR / Issue / CI 等 GitHub-native 信息
→ Slack：发送 CWapi 请求
→ CWapi：准备/复用对应 exact commit 的 persistent workspace，执行本地 build / test / run / stock MCP
→ Slack：返回结果或文件
```

CWapi 不运行模型，也不要求 ChatGPT Web 与本机建立直接 MCP 连接。

**当前 v1.6.3 没有 `repo_search`、`repo_read` 或 filesystem MCP。** `server=cwapi` 仍只提供 `process_start/status/stop`。虽然真实链已经证明不同 repository request 会重新进入同一个 process-lifetime workspace，并且可以用只读 `process_start` 做临时文本搜索，但这只是可行性验证/调试手段，不是正式 repository search API。

因此现在不要构造不存在的 `repo_search` 请求，也不要把 `process_start` 当成文件读取接口。当前源码搜索和完整文件读取继续使用 GitHub Connector。

## 已确定的下一步：search-only 优化

后续只计划把**代码搜索定位**从 GitHub Connector 的热路径移到 CWapi，不把完整源码读取搬到 Slack。

目标链路：

```text
Web GPT
→ GitHub：取得 repository URL + exact commit
→ Slack / CWapi repo_search：在 persistent workspace 本地搜索
   ← path + line + bounded snippet
→ GitHub：精确读取已经命中的少量完整文件
→ Web GPT：分析与规划
→ GitHub：写入源码并取得新的 exact commit
→ Slack / CWapi：在新 commit 上执行 build / test / run
```

`repo_search` 正式实现后应是 Go Gateway/Core 的 repository-scoped virtual tool：不创建模型 turn，不需要 Agent，不经 Codex command backend 做搜索，不需要 FULL / System Token。默认只搜索 `expected_commit` 对应的 tracked source，返回有限的文件路径、行号和小段上下文。

这项优化只减少“GitHub search -> 猜文件 -> 再 search -> 再 fetch”的重复往返。以下工作仍然由 GitHub 负责：完整源码读取、源码写入、commit/push、branch/history、PR、Issue、Review、Actions/CI、Release 和新的 exact commit。

在 `repo_search` 真正实现、完成协议/Gate 并进入正式发布包之前，本节只是明确下一步设计，不改变 v1.6.3 当前能力。

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

5. 同一 repository 在当前 CWapi 进程内使用 persistent workspace。request terminal 只释放 repository lease，不删除 workspace；衍生物可以在同一 CWapi 进程内复用。默认仍把工作拆成可独立验证的小步骤，不要仅为了减少 Slack 往返次数强行合并成一个大型脚本或一次 `process_start`。

6. GitHub 仍是远端源码真相和 exact commit 来源。当前 v1.6.3 的源码搜索/完整读取也仍走 GitHub。未来 `repo_search` 发布后，只把搜索定位切到 CWapi，命中的完整文件仍从 GitHub 精确读取。

7. `process_start` 搜索命令只用于调试、兼容性探测或明确的本地诊断，不把它当成正式 search API，更不用于通用源码传输。

8. 不要自行假设本机环境或固定工具路径。环境发现、SAFE/FULL、安装依赖、浏览器测试、截图和等待规则统一按照 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md) 执行。

## 完整工作流

[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)
