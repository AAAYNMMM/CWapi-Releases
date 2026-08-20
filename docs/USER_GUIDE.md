# CWapi v1.6.0 新手完整教程

这份文档只负责回答：**第一次使用 CWapi，从下载开始应该按什么顺序做。**

专项内容：Slack 从零配置见 [`SLACK_SETUP.md`](SLACK_SETUP.md)；Web GPT 规则见 [`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md) / [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)；故障见 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)；安全见 [`SECURITY.md`](SECURITY.md)；运维见 [`OPERATIONS.md`](OPERATIONS.md)。

## 1. CWapi 是什么

```text
Web GPT：理解需求、读写 GitHub、决定测试方式
        ↓ Slack MCP request
CWapi：项目 / exact commit / 本机执行 / 状态 / 回传
        ↓
stock Codex app-server → configured MCP server
        ↓
结果 / 日志 / Slack File
```

CWapi 不运行模型。它负责把 Web GPT 的本机操作绑定到正确项目和正确 Git commit。

## 2. 下载与第一次启动

准备：Windows 11 x64、可访问目标仓库的 GitHub 账号、Slack Workspace，以及 ChatGPT 中可用的 GitHub / Slack 连接。

1. 从 GitHub Releases 下载 `CWapi-v1.6.0.zip`。
2. 完整解压到可写目录，例如 `D:/Tools/CWapi`。
3. 不要在 ZIP 内运行，也不要只复制 `CWapi.exe`。
4. 双击 `CWapi.exe`。

目录大致为：

```text
CWapi/
├─ CWapi.exe
├─ portable-manifest.json
├─ runtime/
└─ CWapi-data/     # 首次运行后生成
```

发行包已经包含 CWapi 自己需要的 Codex、Git、Node、Playwright MCP、Chromium。目标项目自己的 Python、Java/JDK、Go、Rust、Android SDK、CUDA 等环境由用户或 Web GPT 管理。

## 3. 从零配置 Slack

完整说明见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。第一次可以直接按下面清单完成。

### 3.1 创建 App

打开 `https://api.slack.com/apps`，选择 **Create New App → From scratch**，App Name 可填 `CWapi`，选择目标 Workspace。

### 3.2 Bot scopes

进入 **OAuth & Permissions → Bot Token Scopes**，推荐专用 public 控制频道，并添加：

```text
channels:read
channels:history
chat:write
files:write
```

### 3.3 Events

进入 **Event Subscriptions → Enable Events → Subscribe to bot events**，添加：

```text
message.channels
```

### 3.4 Socket Mode 与 App Token

进入 **Socket Mode → Enable Socket Mode**。

再到 **Basic Information → App-Level Tokens → Generate Token and Scopes**，添加 `connections:write`，生成并保存：

```text
App Token = xapp-...
```

### 3.5 安装 App 与 Bot Token

进入 **OAuth & Permissions → Install to Workspace → Allow**，复制：

```text
Bot User OAuth Token = xoxb-...
```

以后修改 scopes 后要 **Reinstall to Workspace**。

### 3.6 控制频道与 Channel ID

创建专用频道，例如 `#cwapi-control`，并把 CWapi App / Bot 加入频道。

在浏览器打开频道，地址通常类似：

```text
https://app.slack.com/client/T01234567/C0123456789
```

其中 `C0123456789` 就是 CWapi 要的 Channel ID。

### 3.7 填入 CWapi

打开 **CWapi → 设置 → Slack → 更换 Slack 配置**，填写 App Token、Bot Token、Channel ID，然后点击 **验证并保存**。

成功后设置页应显示正确 Workspace / Channel，控制台的 Slack Transport 应进入 connected / healthy。

如果使用 private channel，需要额外 `groups:read`、`groups:history` 和 `message.groups`，见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

## 4. 添加项目

打开“项目”页面，点击“添加项目”。当前 GUI 主要填写：项目名称、本地路径、Git 地址。例如：

```text
项目名称：My Project
本地路径：D:/Projects/my-project
Git 地址：https://github.com/username/my-project.git
```

保存后 CWapi 会维护自己的 `project_id = prj-...`。用户通常不用手工抄给 Web GPT；Web GPT 应通过 `projects/list` 或 `mcpServerStatus/list` discovery 获取当前真实值。

## 5. 权限先保持 safe

第一次使用保持 **安全权限 / safe** 即可。只有明确需要扩大 Codex-managed filesystem 权限时才切换 `full_access`。

随包 `cwapi` command/process MCP 启动的自由 executable 以当前 Windows 用户权限运行，不自动继承 Codex thread 的 safe/full_access sandbox。完整边界只看 [`SECURITY.md`](SECURITY.md)。

## 6. 在 ChatGPT 中连接 GitHub 和 Slack

正常工作流需要：GitHub 用于读取 / 修改源码并取得 exact commit；Slack 用于向 CWapi 控制频道发送 MCP request 并读取 response / Slack File。

CWapi 自己的 `xapp-...` / `xoxb-...` 不要交给 ChatGPT。ChatGPT 中的 Slack 连接和 CWapi Slack App 是两套独立授权。

第一次建议让 Web GPT 读取：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 仓库中的 `docs/WEB_GPT_ENTRY.md`，了解 CWapi v1.6.0 当前工作流。

## 7. 第一次真实开发任务

之后可以直接说：

> 使用 CWapi 工作流开发 GitHub 仓库 `username/my-project`，检查当前问题，修改后在对应 exact commit 上完成本机测试。

正常过程：

```text
GitHub 读代码
→ 修改并 commit
→ discovery project_id
→ project_id + expected_commit
→ 本机测试 / 编译 / Playwright
→ 读取真实结果
→ 继续修复或结束
```

最终测试必须对应当前准备发布 / 使用的 exact commit。

## 8. 目标项目环境怎么办

只记住一条：**CWapi 不替项目决定 Python / JDK / Go / Rust / SDK；Web GPT 或用户先发现已有环境，缺失时再安装，然后把准确 executable 交给 CWapi。**

例如：

```text
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
C:/Program Files/Java/jdk-25/bin/java.exe
C:/Program Files/Git/cmd/git.exe
.venv/Scripts/python.exe
node_modules/.bin/tool.cmd
```

Windows 路径进入 MCP JSON 时统一使用 `/`。

环境发现、安装位置、exact-commit 临时 worktree、`process_start/status/stop`、长期 server 和 Playwright 的完整规则见 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。

## 9. 怎么判断任务真的通过

不要只看“命令发出去了”。至少要有真实结果，例如测试命令 exit / 输出、编译产物、服务状态、localhost 实际访问、DOM / `browser_evaluate` 业务结果、截图或必要日志。

Web E2E 常见顺序：

```text
process_start localhost server
→ Playwright navigate
→ fill / click
→ browser_evaluate 验证结果
→ screenshot（需要时）
→ process_stop
```

## 10. 移动、重启和更新

**移动：**关闭 CWapi 后移动整个便携目录，不要只移动 `CWapi.exe`。

**重启：**每次启动建立新的 CWapi 运行会话；不会自动拾取启动前的旧 Slack request，也不会自动恢复上一进程未完成任务。

**更新：**下载新发行包并阅读对应版本 README / CHANGELOG。不要把不同版本的 `runtime/` 随意混用。详细运维见 [`OPERATIONS.md`](OPERATIONS.md)。

## 11. 出问题时

先看 **CWapi 诊断页 → [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)**。Slack 专属问题直接看 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

不要为了排障上传整个 `CWapi-data`、Credential Manager 内容或包含 secret 的日志。