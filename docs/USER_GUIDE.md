# CWapi v1.6.0 新手完整教程

这份文档只负责回答一件事：**第一次使用 CWapi，从下载开始应该按什么顺序做。**

更细的专项内容分别放在：

- Slack 从零配置：[`SLACK_SETUP.md`](SLACK_SETUP.md)
- Web GPT 执行规则：[`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md) / [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)
- 故障排查：[`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)
- 安全边界：[`SECURITY.md`](SECURITY.md)
- 运行维护：[`OPERATIONS.md`](OPERATIONS.md)

这样避免每份文档都重新讲一遍 exact commit、环境、进程和权限。

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

安装步骤：

1. 从本仓库 GitHub Releases 下载 `CWapi-v1.6.0.zip`。
2. 完整解压到可写目录，例如 `D:/Tools/CWapi`。
3. 不要在 ZIP 内运行，也不要只复制 `CWapi.exe`。
4. 双击 `CWapi.exe`。

正常目录大致是：

```text
CWapi/
├─ CWapi.exe
├─ portable-manifest.json
├─ runtime/
└─ CWapi-data/     # 首次运行后生成
```

发行包已经包含 CWapi 自己需要的 Codex、Git、Node、Playwright MCP、Chromium 等 runtime。目标项目自己的 Python、Java/JDK、Go、Rust、Android SDK、CUDA 等环境由用户或 Web GPT 管理。

## 3. 从零配置 Slack

如果你还没有专门给 CWapi 使用的 Slack App，按下面顺序做。完整截图级说明和故障解释见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

### 3.1 创建 App

打开：

```text
https://api.slack.com/apps
```

选择：

```text
Create New App
→ From scratch
→ App Name: CWapi
→ 选择你的 Workspace
```

### 3.2 添加 Bot scopes

进入：

```text
OAuth & Permissions
→ Bot Token Scopes
```

推荐使用专用 public 控制频道，并添加：

```text
channels:read
channels:history
chat:write
files:write
```

### 3.3 开启事件

进入：

```text
Event Subscriptions
→ Enable Events
→ Subscribe to bot events
```

添加：

```text
message.channels
```

### 3.4 开启 Socket Mode

进入：

```text
Socket Mode
→ Enable Socket Mode
```

然后在：

```text
Basic Information
→ App-Level Tokens
→ Generate Token and Scopes
```

添加：

```text
connections:write
```

生成并保存：

```text
App Token = xapp-...
```

### 3.5 安装 App 并取得 Bot Token

进入：

```text
OAuth & Permissions
→ Install to Workspace
→ Allow
```

复制：

```text
Bot User OAuth Token = xoxb-...
```

以后如果修改 scopes，要 **Reinstall to Workspace**。

### 3.6 创建控制频道并邀请 Bot

推荐创建：

```text
#cwapi-control
```

把 CWapi App / Bot 加入这个频道。CWapi 会检查 bot 是否确实是频道成员。

### 3.7 找到 Channel ID

在浏览器打开控制频道，地址通常类似：

```text
https://app.slack.com/client/T01234567/C0123456789
```

其中：

```text
C0123456789
```

就是 Channel ID。

### 3.8 填入 CWapi

打开：

```text
CWapi
→ 设置
→ Slack
→ 更换 Slack 配置
```

填写：

```text
App Token   xapp-...
Bot Token   xoxb-...
Channel ID  C...
```

点击：

```text
验证并保存
```

成功后设置页应显示正确 Workspace / Channel，控制台的 Slack Transport 应进入 connected / healthy。

如果使用 private channel，需要额外配置 `groups:read`、`groups:history` 和 `message.groups`，详见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

## 4. 添加项目

打开“项目”页面，点击“添加项目”。当前 GUI 主要填写：

```text
项目名称
本地路径
Git 地址
```

例如：

```text
项目名称：My Project
本地路径：D:/Projects/my-project
Git 地址：https://github.com/username/my-project.git
```

保存后 CWapi 会维护自己的：

```text
project_id = prj-...
```

用户通常不用手工抄这个 ID 给 Web GPT。Web GPT 应通过 `projects/list` 或 `mcpServerStatus/list` discovery 获取当前真实值。

## 5. 权限先保持 safe

第一次使用保持：

```text
安全权限 / safe
```

即可。

只有明确需要扩大 **Codex-managed filesystem 权限**时才切换 `full_access`。

注意：随包 `cwapi` command/process MCP 启动的自由 executable 以当前 Windows 用户权限运行，不自动继承 Codex thread 的 safe/full_access sandbox。这个边界的完整解释只放在 [`SECURITY.md`](SECURITY.md)，不在这里重复展开。

## 6. 在 ChatGPT 中连接 GitHub 和 Slack

正常工作流需要：

```text
GitHub：读取 / 修改源码并取得 exact commit
Slack：向 CWapi 控制频道发送 MCP request 并读取 response / Slack File
```

CWapi 自己的 `xapp-...` / `xoxb-...` 不要交给 ChatGPT。ChatGPT 中的 Slack 连接和 CWapi Slack App 是两套独立授权。

第一次建议让 Web GPT 读取：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 仓库中的 `docs/WEB_GPT_ENTRY.md`，了解 CWapi v1.6.0 当前工作流。

## 7. 第一次真实开发任务

之后可以直接说：

> 使用 CWapi 工作流开发 GitHub 仓库 `username/my-project`，检查当前问题，修改后在对应 exact commit 上完成本机测试。

正常过程大致是：

```text
GitHub 读代码
→ 修改并 commit
→ discovery project_id
→ project_id + expected_commit
→ 本机测试 / 编译 / Playwright
→ 读取真实结果
→ 继续修复或结束
```

这里最重要的是：**最终测试必须对应当前准备发布 / 使用的 exact commit。**

## 8. 目标项目环境怎么办

只记住一条：

> CWapi 不替项目决定 Python / JDK / Go / Rust / SDK；Web GPT 或用户先发现已有环境，缺失时再安装，然后把准确 executable 交给 CWapi。

例如：

```text
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
C:/Program Files/Java/jdk-25/bin/java.exe
C:/Program Files/Git/cmd/git.exe
.venv/Scripts/python.exe
node_modules/.bin/tool.cmd
```

Windows 路径进入 MCP JSON 时统一使用 `/`。

环境发现、安装位置、exact-commit 临时 worktree、`process_start/status/stop`、长期 server 和 Playwright 的完整规则集中放在 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。

## 9. 怎么判断任务真的通过

不要只看“命令发出去了”。至少要有真实结果：

```text
测试命令 exit / 输出
编译产物
服务 running / stopped 状态
localhost 实际访问结果
DOM / browser_evaluate 业务结果
截图或必要日志
```

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

### 移动

关闭 CWapi 后移动整个便携目录，不要只移动 `CWapi.exe`。

### 重启

每次启动建立新的 CWapi 运行会话；不会自动拾取启动前的旧 Slack request，也不会自动恢复上一进程未完成任务。

### 更新

下载新发行包并阅读对应版本 README / CHANGELOG。不要把不同版本的 `runtime/` 随意混用。

详细运维见 [`OPERATIONS.md`](OPERATIONS.md)。

## 11. 出问题时

先看：

```text
CWapi 诊断页
→ TROUBLESHOOTING.md
```

Slack 专属问题直接看 [`SLACK_SETUP.md`](SLACK_SETUP.md)；其它按职责查对应专项文档。

不要为了排障上传整个 `CWapi-data`、Credential Manager 内容或包含 secret 的日志。