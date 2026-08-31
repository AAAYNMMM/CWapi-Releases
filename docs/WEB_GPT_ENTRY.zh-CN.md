# CWapi 1.6.3 Web GPT 入口

[English](WEB_GPT_ENTRY.md) | [简体中文](WEB_GPT_ENTRY.zh-CN.md)

这份文档是 Web GPT 开工时的短入口。完整执行规则见 [ChatGPT 工作流](CHATGPT_WORKFLOW.zh-CN.md)。

## 角色分工

```text
Web GPT
  -> 理解任务并决定下一步
  -> 通过 GitHub 处理仓库真相与源码修改
  -> 通过 Slack 发送 CWapi 控制 frame、读取 response

CWapi 1.6.3
  -> 准备本机 exact commit
  -> 执行本地工具/进程
  -> 通过 Slack 返回真实结果和文件
```

CWapi 不运行 AI 模型。所谓 MCP v2 是通过 Slack 承载的结构化 frame，不是 ChatGPT 直接连接本机 MCP。

## Repository 动作开始前

Web GPT 必须先知道：

1. GitHub HTTPS `repository_url`；
2. 完整 40 位 `expected_commit`；
3. 要执行的本机动作；
4. 一个新的 `request_id`。

tracked source、commit、branch、PR、Issue、Review、Actions/CI、Release 等 GitHub-native 信息仍以 GitHub 为准。

## MCP v2 frame

完整 frame 必须由 `+++` 开始和结束：

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2",...}
+++
```

第一行必须就是 `+++`。JSON 里的 Windows path 尽量用 `/`；必须写 `\` 时按 JSON 规则转义。

## Repository 工作流

```text
GitHub：确定 repository + exact commit
        ↓
Slack：发送 repository-scoped CWapi request
        ↓
CWapi：准备/复用 exact commit 的 persistent workspace
        ↓
CWapi：search / build / test / browser / process
        ↓
Slack：返回 response/file
        ↓
Web GPT：读取真实结果后决定下一步
```

如果 tracked source 发生修改，先通过 GitHub 提交，拿到新的 exact commit，再用这个 commit 做本机验证。

## 搜索源码

需要定位函数、类型、错误文本或配置键时，优先在已准备的 workspace 内执行小型只读搜索。返回 path、line、匹配文本和少量上下文就够了。

不要为了搜索同一个仓库又 clone/fetch 一份。定位后，需要完整文件时再通过 GitHub 精确读取或修改。

## Process 处理

使用：

```text
cwapi/process_start
cwapi/process_status
cwapi/process_stop
```

`process_start` 是 repository-scoped。如果返回 `running`，保存 `process_id`。后续每次 `process_status` / `process_stop` 都使用新的 global request ID，不要重复原始 start。

连续等待/轮询不要超过 3 分钟。超过 3 分钟还没结束，就明确告诉用户任务仍在运行以及当前状态，不要默默把等待时间叠到天荒地老。

## Persistent workspace 规则

request 只是一个执行步骤，不是 workspace 生命周期。同一个 CWapi 进程里，同 repository 的后续 request 会重新进入同一个受管 workspace。

tracked source 可能在 prepare 时被同步到 `expected_commit`；真正适合跨 request 复用的是 ignored/untracked 的衍生状态。

## SAFE / FULL

普通任务保持 `SAFE`。只有用户明确授权、任务确实需要更高本机权限时再切 `FULL`，而且永久安全规则仍然有效。

System Token 只用于 CWapi 认可的权限拒绝 fallback。不要伪造、复用过期 Token，也不要塞进嵌套 params。fallback 使用新的 `request_id`，其它 repository/commit/invocation 参数保持一致。

## 截图与文件

Playwright 截图要返回 ChatGPT 时，调用 `browser_take_screenshot` 不要传 `filename`，让 MCP result 返回真实 image content，CWapi 才能上传到 Slack。

工具只打印本机路径并不等于传文件。只有底层 MCP result 真正返回内容，CWapi 才能外置。

## 从哪里开始

第一次配置看 [快速入门](GETTING_STARTED.zh-CN.md)，完整执行规则看 [ChatGPT 工作流](CHATGPT_WORKFLOW.zh-CN.md)。
