# CWapi v1.6.1 用户指南

这份文档只讲 **用户第一次安装、配置和开始使用 CWapi**。Web GPT 的执行规则统一放在 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)，这里不重复环境探测、进程、Playwright 和等待细节。

## 1. 使用前准备

需要：

- Windows 11 x64；
- 一个可用的 Slack Workspace；
- ChatGPT 中可连接 GitHub 与 Slack；
- **GitHub CLI，由用户自行安装并完成本机登录。**

GitHub CLI：<https://cli.github.com/>

安装后打开 PowerShell：

```powershell
gh auth login
gh auth status
```

完成浏览器或终端登录，并确认 `gh auth status` 显示当前 GitHub 账户可用。

CWapi portable 自带 MinGit，但 **MinGit 不等于 GitHub CLI**。CWapi 使用本机已经登录的 GitHub CLI 凭据访问需要认证的 GitHub repository，因此不要跳过这一步。

## 2. 下载和启动 CWapi

1. 从 [GitHub Releases](https://github.com/AAAYNMMM/CWapi-Releases/releases) 下载 `CWapi-v1.6.1.zip`。
2. 完整解压到任意用户可写目录。
3. 不要直接在 ZIP 内运行，也不要只复制 `CWapi.exe`。
4. 运行 `CWapi.exe`。

目录大致为：

```text
CWapi/
├─ CWapi.exe
├─ portable-manifest.json
├─ runtime/
└─ CWapi-data/     # 首次运行后生成
```

portable 已包含 CWapi 自己需要的 Codex、MinGit、Node、Playwright MCP 与 Chromium。

## 3. 配置 Slack

第一次启动后，在 CWapi 界面的 Slack 区域填写：

```text
App Token   = xapp-...
Bot Token   = xoxb-...
Channel ID  = C...
```

Slack App、Socket Mode、scopes、Token 和 Channel ID 的完整创建步骤见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

Token 验证成功后保存在当前 Windows 用户的 Credential Manager，不写入普通配置文件。

## 4. 在 ChatGPT 中连接 GitHub 和 Slack

ChatGPT 里的 GitHub 连接负责读取和修改 repository；Slack 连接负责向 CWapi 控制频道发送请求并读取结果。

CWapi 自己使用的 App Token / Bot Token 不需要发送给 ChatGPT，它们只配置在本机 CWapi 中。

## 5. 第一次让 Web GPT 使用 CWapi

推荐第一次直接告诉 Web GPT：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 的 `docs/WEB_GPT_ENTRY.md`，然后使用 CWapi v1.6.1 工作流处理我的项目。

之后就可以直接给开发任务，例如：

> 使用 CWapi 工作流开发 `https://github.com/username/project`，修改后在对应 exact commit 上完成本机测试。

v1.6.1 不需要在 CWapi 中添加项目，也不使用 `project_id`。Web GPT 会直接提供 GitHub repository URL 与完整 40 位 commit。

## 6. SAFE 和 FULL

CWapi 每次启动都会恢复为 `SAFE`，日常开发默认保持 SAFE。

如果 Web GPT 确认任务需要在受控工作区之外安装软件或修改本机环境，它会要求用户临时切换 `FULL`。用户也可以选择自己手动安装缺少的软件。

`FULL` 只对当前 CWapi 运行有效，重启后自动回到 SAFE。

完整权限边界见 [`SECURITY.md`](SECURITY.md)。

## 7. 目标项目需要 Python、JDK、Rust 等怎么办

用户不需要提前为每一种项目准备统一环境。Web GPT 会按照 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md) 的规则先使用 CWapi 已管理的环境，再查找本机已有环境；确实缺少依赖时再进入 FULL 安装或用户手动安装。

因此不要把某台电脑上的 Python、Node、JDK 等固定路径写进长期配置。

## 8. 日常使用

正常开发流程大致是：

```text
给 Web GPT 开发任务
→ Web GPT 修改 GitHub 并取得 exact commit
→ 通过 Slack 调用 CWapi
→ CWapi 在本机执行编译 / 测试 / 浏览器等工具
→ Slack 返回真实结果或文件
→ Web GPT 根据结果继续修改
```

Web GPT 的 request、process、Playwright、截图、环境发现和等待规则全部以 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md) 为准。

## 9. 移动、更新和重启

- **移动 CWapi：**先退出程序，再移动整个便携目录，不要只移动 `CWapi.exe`。
- **更新版本：**完整解压新的 portable，不要混用不同版本的 `runtime/`。
- **重启：**权限恢复 SAFE；旧进程状态和短期 System Token 不跨进程恢复。
- **GitHub 登录变化：**重新运行 `gh auth status` 检查；需要重新授权时使用 `gh auth login`。

## 10. 出问题时

先确认：

```text
CWapi Core 正常
Slack 已连接
GitHub CLI 已登录
gh auth status 正常
```

然后按 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md) 定位。

不要为了排障上传整个 `CWapi-data`、Credential Manager 内容、Token 或其它无关用户数据。
