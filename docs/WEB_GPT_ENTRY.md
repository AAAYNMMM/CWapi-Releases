# CWapi v1.6.1 Web GPT Entry

这是 **Web GPT 使用 CWapi v1.6.1 的快速入口**。完整规则见 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)，协议细节见 [`PROTOCOL.md`](PROTOCOL.md)。

## 1. 当前链路

```text
Web GPT
  -> GitHub：读取源码、修改、取得 exact commit
  -> Slack：发送 CWapi MCP v2 frame
  -> CWapi：校验 / exact-commit worktree / process / delivery
  -> stock MCP 或 Go Core process tools
  -> Slack response / Slack File
```

CWapi 不运行模型，也不要求 ChatGPT Web 与本机建立直接 MCP 连接。

## 2. 不再有 project registry

v1.6.1 已删除 `projects/list`、`project_id` 和 GUI 项目 CRUD。

repository request 直接携带：

```text
repository_url  = https://github.com/owner/repo
expected_commit = 完整 40 位 Git SHA
```

二者必须成对出现。CWapi 根据 repository identity 维护共享 mirror，并为每个 repository request 创建独立 detached worktree。

## 3. 当前协议

正式请求：

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2",...}
+++
```

v1.6.1 只接受 MCP v2。不要再发送 `[CWapi/MCP/1]` 或旧 v1 schema。

允许的远程方法只有：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

第一次需要确认 stock MCP catalog 时，可先做 global：

```text
method = mcpServerStatus/list
params = {}
```

## 4. 本机命令

CWapi virtual process tools：

```text
server = cwapi
process_start
process_status
process_stop
```

`process_start` 是 repository-scoped，外层 request 必须提供 repository URL + exact commit。

如果返回：

```text
state = running
process_id = proc-...
```

后续使用新的 request id 调 global `process_status` 查询同一个 process；不要重复 `process_start`。

## 5. 外部运行环境

需要 Python、Node、JDK、Go、Rust、SDK 等目标项目环境时：

```text
先找 CWapi 管理的 runtime/tools/cache
→ 没有再找本机已经安装、且 CWapi 实际可见的环境
→ 两边都没有：停止猜路径
→ 用户切换 FULL 后由 Web GPT 安装，或用户手动安装
→ 安装后重新探测真实 executable/version
```

不要固定某台机器的 Python/Node 路径。CWapi 的 PATH 是启动时冻结的快照，新安装程序必要时直接使用实际绝对路径或重启 CWapi 后再验证。

Windows path 在 MCP JSON 中优先使用 `/`。

## 6. Playwright

stock MCP context 是 request-scoped ephemeral。不同 request 不应假定共享页面、tab、locator 或其它浏览器状态。

连续的：

```text
navigate -> fill -> click -> assert -> screenshot
```

优先在一次 Playwright 调用中完成；如果拆成多个 request，后续 request 自行重新建立页面状态。

### 截图传回 Slack

需要 ChatGPT 真正拿到截图时，调用 `browser_take_screenshot` **不要传 `filename`**：

```json
{
  "fullPage": true,
  "scale": "css",
  "type": "png"
}
```

这样工具可以返回 MCP `type=image` content，CWapi 会自动上传为 Slack File。

如果指定 `filename` 后只返回 `./image.png`，那只是本机路径；CWapi 不会根据普通文本路径擅自读取本地文件。

## 7. 权限

每次 CWapi 启动默认 `SAFE`。

```text
SAFE
  -> 默认受控执行

FULL
  -> 本次运行临时启用
  -> 仍先走 Codex
  -> 真实结构化 PERMISSION_DENIED 时才签发短期 System Token fallback
```

System Token 最多同时 3 个、60 秒、一次性使用，并绑定 repository/commit/final invocation。

安装新软件或需要扩大本机写权限时，由用户明确切换 FULL 后再执行。

## 8. 等待规则

对同一个外部编译、进程、Slack response 或其它异步结果，单次连续等待/轮询累计最多 **3 分钟**。

到 3 分钟仍没有 terminal result：

```text
立即停止继续等待
→ 报告“任务仍在运行”
→ 给出 request/process id 与最后状态
→ 下一轮查询原任务
→ 不重复提交
```

缺少环境不是等待条件。确认工具不存在后直接进入 FULL 安装或用户手动安装分支。

## 9. 更多细节

- 完整 Web GPT 工作流：[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)
- MCP v2：[`PROTOCOL.md`](PROTOCOL.md)
- 权限：[`SECURITY.md`](SECURITY.md)
- Slack 文件与截图：[`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)
- 故障：[`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)

Slack App Token / Bot Token 只填入本机 CWapi，不要通过普通消息交给 Web GPT。
