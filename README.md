<div align="center">

# CWapi

**Turn ChatGPT Web into a local coding agent — without a real MCP connection or Codex Agent quota.**

**让网页 ChatGPT 连接本地 Windows 开发环境，无需真正的 MCP 链路，也不消耗 Codex Agent 配额。**

[![ChatGPT](https://img.shields.io/badge/ChatGPT-Plus%20%7C%20Pro-10a37f?style=flat-square)](https://chatgpt.com/)
![MCP](https://img.shields.io/badge/MCP-Not%20Required-6f42c1?style=flat-square)
![Codex Agent](https://img.shields.io/badge/Codex%20Agent%20Quota-Not%20Used-success?style=flat-square)
![Platform](https://img.shields.io/badge/Platform-Windows%2011-0078d4?style=flat-square)
![Install](https://img.shields.io/badge/Install-Portable-orange?style=flat-square)
[![Release](https://img.shields.io/github/v/release/AAAYNMMM/CWapi-Releases?style=flat-square&label=Release)](https://github.com/AAAYNMMM/CWapi-Releases/releases)

[功能](#功能) · [使用前准备](#使用前准备) · [快速开始](#快速开始) · [用户指南](docs/USER_GUIDE.md) · [Web GPT 工作流](docs/WEB_GPT_ENTRY.md)

</div>

---

CWapi 让普通 ChatGPT Web 会话通过 Slack 调用本机 Windows 开发环境。Web GPT 负责理解需求和修改 GitHub，CWapi 负责在对应 GitHub commit 上运行本机工具，再把真实结果和文件返回给 Web GPT。

## 关于 MCP

**CWapi 使用 MCP 风格的消息格式来组织结构化 request / response，但 ChatGPT Web 与本机之间并没有建立真正的 MCP 连接。**

**CWapi uses MCP-style message formats for structured requests and responses, but it does not establish a real MCP connection between ChatGPT Web and the local machine.**   

<img width="373" height="688" alt="Snipaste_2026-08-25_02-45-09" src="https://github.com/user-attachments/assets/38654765-d920-43d4-ac45-f026bdd4b17b" />


## 功能

- 让 ChatGPT Web 调用本机编译、测试、脚本和开发工具；
- 在指定 GitHub repository 的准确 commit 上执行任务；
- 支持 localhost 网页与 Playwright 浏览器测试；
- 支持截图、文件和较大结果经 Slack 返回；
- 支持长进程启动、查询和停止；
- 默认 `SAFE` 权限，必要时可由用户临时切换 `FULL`；
- 自动优先使用 CWapi 自带或管理的运行环境，再使用本机已有环境；
- 不使用 Codex Agent Turn 替 Web GPT 思考。

## 工作流

```text
ChatGPT Web
   │ GitHub + Slack
   ▼
Slack control channel
   ▼
CWapi
   │ exact repository + commit
   ▼
Local Windows tools
   │
   ▼
Slack response / File
   ▼
ChatGPT Web
```

Web GPT 的完整执行规则见 [`docs/CHATGPT_WORKFLOW.md`](docs/CHATGPT_WORKFLOW.md)。

## 使用前准备

需要：

- Windows 11 x64；
- 一个 Slack Workspace；
- ChatGPT 中连接 GitHub 和 Slack；
- **用户自行安装 GitHub CLI，并在本机完成登录。**

GitHub CLI：<https://cli.github.com/>

安装后执行：

```powershell
gh auth login
gh auth status
```

确认 `gh auth status` 正常后即可使用 CWapi。

## 快速开始

1. 从 [GitHub Releases](https://github.com/AAAYNMMM/CWapi-Releases/releases) 下载 `CWapi-v1.6.1.zip`。
2. 完整解压后运行 `CWapi.exe`。
3. 按 [`docs/SLACK_SETUP.md`](docs/SLACK_SETUP.md) 完成 Slack 配置。
4. 确认 GitHub CLI 已登录，并在 ChatGPT 中连接 GitHub 和 Slack。
5. 第一次告诉 Web GPT：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 的 `docs/WEB_GPT_ENTRY.md`，然后使用 CWapi v1.6.1 工作流处理我的项目。

之后可以直接给开发任务，例如：

> 使用 CWapi 工作流开发 `https://github.com/username/project`，修改后在对应 exact commit 上完成本机测试。

v1.6.1 不需要在 CWapi 中添加项目。

## 权限

日常使用保持 `SAFE` 即可。

如果任务需要安装新的本机软件或修改 SAFE 范围外的环境，Web GPT 会提示用户临时切换 `FULL`，或者用户也可以选择手动安装。

CWapi 重启后会重新回到 `SAFE`。

## 文档

- [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md)：第一次安装和使用。
- [`docs/SLACK_SETUP.md`](docs/SLACK_SETUP.md)：Slack 配置。
- [`docs/WEB_GPT_ENTRY.md`](docs/WEB_GPT_ENTRY.md)：Web GPT 开工入口。
- [`docs/CHATGPT_WORKFLOW.md`](docs/CHATGPT_WORKFLOW.md)：完整工作流逻辑。
