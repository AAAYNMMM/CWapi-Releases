# CWapi v1.6.0 新手完整教程

这份文档面向第一次使用 CWapi 的普通用户，目标是从下载开始，完成 Slack、项目、权限和 Web GPT 配置，并跑通一次真实本机开发任务。Web GPT 本身优先读取 [`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md)。

## 1. CWapi 是什么

```text
Web GPT：理解需求、读写 GitHub、选择环境和测试方式
        ↓ Slack MCP request
CWapi：项目 / exact commit / 本机执行 / 状态 / 回传
        ↓
stock Codex app-server → configured MCP server
        ↓
结果 / 日志 / Slack File
```

CWapi 不运行模型，也不替 Web GPT规划项目。它负责把 Web GPT 的本机操作绑定到正确项目和正确 Git commit。

## 2. 使用前准备

建议准备：Windows 11 x64；可访问目标仓库的 GitHub 账号；ChatGPT 中连接 GitHub 与 Slack；给 CWapi 使用的 Slack App / Workspace / Channel；要开发的 GitHub 仓库。

发行包自身包含 CWapi 工作所需的主要 runtime，例如 Codex、Git、Node、Playwright MCP、Chromium。**目标项目自己的 Python、Java/JDK、Go、Rust、Android SDK、CUDA 等环境由用户或 Web GPT 管理。**

## 3. 下载与第一次启动

1. 从本仓库 GitHub Releases 下载 `CWapi-v1.6.0.zip`。
2. 完整解压到可写目录，例如 `D:/Tools/CWapi` 或 `C:/Users/name/Apps/CWapi`。
3. 不要直接在 ZIP 中运行，不要只复制 `CWapi.exe`，不要删除 `runtime/`。
4. 双击 `CWapi.exe`。

首次运行后目录大致是：

```text
CWapi/
├─ CWapi.exe
├─ portable-manifest.json
├─ runtime/{codex,git,node,mcp,browser}/
└─ CWapi-data/
```

`CWapi-data/` 保存 CWapi 自己的数据、配置、状态和日志。Slack token 不写普通配置，而是写入当前 Windows 用户的 Credential Manager。

## 4. 配置 Slack

当前 transport 使用：

```text
App Token   xapp-...
Bot Token   xoxb-...
Channel ID  C...
```

在首次启动或设置页面填写，并确认 Slack 状态正常。Channel ID 是 `C...` 形式的真实频道 ID，不只是频道显示名称。

不要把 token 放进 GitHub、普通 Slack 消息、MCP `command` / `argv`、日志、截图或 artifact。详细 transport 说明见 [`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)。

## 5. 添加项目

打开“项目”页面，填写项目 display name、GitHub repository identity、本地项目路径和 remote URL，例如：

```text
Display name: My Project
Repository: username/my-project
Local path: D:/Projects/my-project
Remote URL: https://github.com/username/my-project.git
```

保存后 CWapi 维护内部 `project_id = prj-...`。Web GPT 应通过 `projects/list` 或 `mcpServerStatus/list` discovery 获取当前真实 ID，不要手写、猜测或长期缓存旧安装实例的 ID。

项目调用还必须带完整 40 位 `expected_commit`：

```text
project_id + expected_commit
```

CWapi 会 fetch Git mirror、验证 commit、建立 detached worktree，再让 MCP 在该 exact commit 上工作。因此不能用“某个本地目录看起来是最新代码”代替 same-commit 证据。

## 6. 权限模式

### safe
默认推荐。Codex-managed execution 使用 `cwapi-safe`，已配置项目和 CWapi data root 作为 managed workspace roots。

### full_access
用户显式开启。Codex-managed filesystem permission 扩大，但仍保留 CWapi 自身 secret、幂等、owned process 等边界，并不使用裸 `:danger-full-access` 作为默认实现。

### 最重要的区别
随包 `cwapi` command/process MCP 启动的自由 executable 以当前 Windows 用户权限运行，**不自动继承 Codex thread 的 safe/full_access filesystem / execpolicy sandbox**。所以 `safe` 不能理解成“所有本机子进程都被锁在项目目录”。详细见 [`SECURITY.md`](SECURITY.md)。

## 7. 在 ChatGPT 中连接并第一次使用

Web GPT 正常需要 GitHub 和 Slack：GitHub 用于读取、修改、提交代码并取得 exact commit；Slack 用于给 CWapi 发 MCP request、读 response 和 Slack File。v1.6.0 正式链路不再使用 Gmail / Google Drive 作为产品控制通道。

第一次建议告诉 Web GPT：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 仓库中的 `docs/WEB_GPT_ENTRY.md`，了解 CWapi v1.6.0 当前工作流。

之后直接给任务，例如：

> 使用 CWapi 工作流开发 GitHub 仓库 `username/my-project`，修复当前问题，修改后在对应 exact commit 上完成本地测试。

正常过程：

```text
GitHub 读代码 → 修改 / commit → discovery project_id
→ project_id + expected_commit → 本机测试 / 编译 / Playwright
→ 读取真实结果 → 继续修复或结束
```

## 8. 目标项目环境怎么处理

v1.6.0 规则：**环境由用户 / Web GPT 发现、安装、选择和管理；CWapi 只负责结构化执行。**

Web GPT 应先看项目 README、lockfile、`pyproject.toml`、`package.json`、`go.mod`、`Cargo.toml`、Gradle/Maven、CI 等，确认所需版本，再检查本机已有环境。不要因为 `python` 不在 PATH 就立刻安装第二份 Python。

常见发现方式：`where.exe`、`Get-Command`、`py.exe -0p`、`<tool> --version`。找到满足要求的环境后优先使用准确 executable：

```text
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
C:/Program Files/Java/jdk-25/bin/java.exe
C:/Program Files/Git/cmd/git.exe
D:/SDK/tool.exe
.venv/Scripts/python.exe
node_modules/.bin/tool.cmd
```

`process_start.command` 支持 PATH 名称、绝对 executable 路径、working-directory-relative 路径。Windows 路径进入 MCP JSON 前统一把 `\` 转成 `/`，并且不要给 `command` 值再加一层引号。

exact-commit worktree 是临时执行上下文；请求 A 在其中创建的 `.venv` 不保证请求 B 继续存在。跨请求复用的环境更适合放在明确持久位置，再通过绝对 executable 调用；或者按项目需要重新创建。

完整环境 / 进程规则见 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。

## 9. 进程工具

随包 `cwapi` MCP server 提供：

```text
process_start
process_status
process_stop
```

短任务可能直接返回 `completed + exit_code`；长期 server 常返回：

```text
state = running
process_id = proc-...
```

后续只查询同一个 `process_id`，不要重复提交同一个 `process_start`。测试完成后使用 `process_stop(process_id)`。

主动 stop 后 exit code 不一定为 0；判断是否停止优先看 `state = stopped`，并结合端口 / 服务真实状态。

## 10. Playwright 与 Web E2E

configured Playwright MCP 可用于 localhost 页面：navigate、填表、点击、读取页面、`browser_evaluate`、截图。

推荐完整流程：

```text
process_start 本地服务
→ 确认 running / ready
→ browser_navigate localhost
→ fill / click
→ browser_evaluate 验证真实结果
→ screenshot（需要时）
→ process_stop
```

不要把“页面能打开”或“按钮能点击”当成业务已经通过；最终应读取实际 DOM / 状态 / 输出。

## 11. 日志、图片和 Slack File

CWapi 只外发 **MCP 已经返回的内容**：短文本 inline；长文本/日志、image、resource text/blob 可作为 Slack File。

当前主要限制：单个 artifact 最大 8 MiB，单次 response 最多 16 个 artifact；超限明确失败，不静默截断。CWapi 不会因为 result 里出现 `C:/file.log`、`file://...` 等字符串就自行打开对应本地文件。

## 12. 长任务与 3 分钟等待规则

对同一个外部任务，Web GPT 单次回复累计等待最多 3 分钟。达到上限仍无 terminal result 时：停止本轮轮询；报告 request/task/process ID、exact commit 和最后状态；本机任务可继续运行；下一轮只查询原任务；不重复提交。停止等待不等于 cancel。

## 13. 移动、重启与更新

### 移动
先结束任务、停止不再需要的 owned process、关闭 CWapi，然后移动**整个**便携目录。下一次启动会从新的 executable location 重新解析随包 runtime。

### 重启
每次启动建立新运行会话：不自动拾取启动前 Slack history，不恢复上一进程未完成 request；同一进程内 duplicate request 不重复执行，Socket reconnect 从 durable cursor 后继续。

### 更新
关闭旧版本，下载新的正式发行包，阅读新 README / CHANGELOG / 迁移说明，使用新版本自己的 runtime。不要把两个版本的 `runtime/` 目录随意混在一起。

详细运维见 [`OPERATIONS.md`](OPERATIONS.md)。

## 14. 安全提醒

不要公开或发送：Slack App Token、Bot Token、API key、password、private key、项目 secret、`CWapi-data` 私有运行数据。尤其不要放进 `command`、`argv`、GitHub commit、普通 Slack 文本、测试日志、截图或 artifact。

需要认证时优先使用 Windows Credential Manager、已登录 CLI、工具自己的 credential store 或本机已有认证状态。

## 15. 出问题时怎么查

优先顺序：

1. CWapi “诊断”页面：version/source commit、Slack、Codex、MCP、permission mode、最近错误。
2. [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)：常见现象 / 错误码。
3. [`OPERATIONS.md`](OPERATIONS.md)：运行、迁移、恢复、日志。
4. [`SECURITY.md`](SECURITY.md)：权限 / trusted command boundary。
5. [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)：Web GPT 环境和执行流程。
6. [`PROTOCOL.md`](PROTOCOL.md)：frame / request / response / discovery。