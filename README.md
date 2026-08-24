<div align="center">

# CWapi

**Turn ChatGPT Web into a local coding agent — without a real MCP connection or Codex Agent quota.**

**让网页 ChatGPT 连接本地 Windows 开发环境，无需真正的 MCP 链路，也不消耗 Codex Agent 配额。**

[![ChatGPT](https://img.shields.io/badge/ChatGPT-Plus%20%7C%20Pro-10a37f?style=flat-square)](https://chatgpt.com/)
![MCP](https://img.shields.io/badge/MCP-Not%20Required-6f42c1?style=flat-square)
![Codex Agent](https://img.shields.io/badge/Codex%20Agent%20Quota-Not%20Used-success?style=flat-square)
![Protocol](https://img.shields.io/badge/CWapi-MCP%20v2-6f42c1?style=flat-square)
![Platform](https://img.shields.io/badge/Platform-Windows%2011-0078d4?style=flat-square)
![Install](https://img.shields.io/badge/Install-Portable-orange?style=flat-square)
[![Release](https://img.shields.io/github/v/release/AAAYNMMM/CWapi-Releases?style=flat-square&label=Release)](https://github.com/AAAYNMMM/CWapi-Releases/releases)

[快速开始](#快速开始) · [关于 MCP](#关于-mcp) · [使用前准备](#使用前准备) · [用户指南](docs/USER_GUIDE.md) · [Web GPT 入口](docs/WEB_GPT_ENTRY.md)

</div>

---

CWapi 是面向个人 Windows 开发环境的 **Web GPT → Slack → 本机开发工具** 网关。Web GPT 负责理解任务、读写 GitHub 和决定开发步骤；CWapi 负责把结构化请求绑定到指定 GitHub repository 与完整 commit，在本机执行工具，并把真实结果、日志和文件经 Slack 返回。

## 关于 MCP

**CWapi 使用 MCP 风格的消息格式来组织结构化 request / response，但 ChatGPT Web 与本机之间并没有建立真正的 MCP 连接。**

**CWapi uses MCP-style message formats for structured requests and responses, but it does not establish a real MCP connection between ChatGPT Web and the local machine.**

Slack 承载 CWapi 的结构化消息；CWapi 内部可以调用 stock MCP server，但 ChatGPT Web 不需要直接连接本机 MCP Server。

## 工作原理

```text
ChatGPT Web
   │ GitHub + Slack
   ▼
Slack control channel
   │ [CWapi/MCP/2]
   ▼
CWapi
   ├─ exact GitHub repository + 40-char commit
   ├─ isolated worktree
   ├─ stock MCP relay
   └─ local process tools
            │
            ▼
      Local Windows tools
            │
            ▼
 Slack response / Slack File
```

CWapi 不运行模型，也不启动 Codex Agent Turn 替 Web GPT 思考。

## 使用前准备

- Windows 11 x64；
- 可用的 Slack Workspace；
- ChatGPT 中连接 GitHub 和 Slack；
- **自行安装 GitHub CLI，并在 Windows 本机完成登录。**

GitHub CLI：<https://cli.github.com/>

安装后在终端执行：

```powershell
gh auth login
gh auth status
```

`gh auth status` 确认登录正常后再启动正式开发流程。CWapi portable 已包含自身需要的 Codex、MinGit、Node、Playwright MCP 与 Chromium，不需要另外安装这些运行时。

## 快速开始

1. 从 [GitHub Releases](https://github.com/AAAYNMMM/CWapi-Releases/releases) 下载 `CWapi-v1.6.1.zip`。
2. 完整解压到任意用户可写目录并运行 `CWapi.exe`，不要只复制 exe，也不要删除 `runtime/`。
3. 按 [`docs/SLACK_SETUP.md`](docs/SLACK_SETUP.md) 完成 Slack App、Socket Mode、Token 与控制频道配置。
4. 确认 GitHub CLI 已登录，并在 ChatGPT 中连接 GitHub 与 Slack。
5. 第一次告诉 Web GPT：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 的 `docs/WEB_GPT_ENTRY.md`，然后使用 CWapi v1.6.1 工作流处理我的项目。

之后直接提交开发任务即可。v1.6.1 不使用 project registry 或 `project_id`，repository request 直接携带 GitHub URL 与完整 40 位 commit。

完整的人类端安装与配置流程见 [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md)。

## v1.6.1

- MCP v2：`[CWapi/MCP/2]` / `cwapi-mcp/2`；
- GitHub repository URL + exact 40hex commit；
- 每个 repository request 使用独立 worktree；
- `process_start/status/stop` 由 Go Core 提供；
- 默认 `SAFE`，需要更高本机写权限时由用户临时切换 `FULL`；
- Slack 支持 MCP 文件、图片和大结果回传；
- portable 自带 CWapi 所需主要 runtime。

执行细节、环境发现、权限 fallback、Playwright 和等待边界统一以 [`docs/CHATGPT_WORKFLOW.md`](docs/CHATGPT_WORKFLOW.md) 为准，不在 README 重复展开。

## 文档

### 用户

- [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md)：从下载到第一次开发。
- [`docs/SLACK_SETUP.md`](docs/SLACK_SETUP.md)：Slack App 配置。
- [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md)：故障排查。

### Web GPT

- [`docs/WEB_GPT_ENTRY.md`](docs/WEB_GPT_ENTRY.md)：最小入口。
- [`docs/CHATGPT_WORKFLOW.md`](docs/CHATGPT_WORKFLOW.md)：完整执行规则。

### 协议 / 安全 / 开发

- [`docs/PROTOCOL.md`](docs/PROTOCOL.md)：MCP v2 wire contract。
- [`docs/SECURITY.md`](docs/SECURITY.md)：权限与安全边界。
- [`docs/SLACK_TRANSPORT.md`](docs/SLACK_TRANSPORT.md)：Slack transport 与文件交付。
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)：架构。
- [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md)：源码开发约束。
- [`docs/LOCAL_VALIDATION.md`](docs/LOCAL_VALIDATION.md)：维护者回归。
- [`CHANGELOG.md`](CHANGELOG.md)：版本变化。

## 便携包

```text
CWapi/
├─ CWapi.exe
├─ portable-manifest.json
├─ runtime/
└─ CWapi-data/     # 首次运行后生成
```

`CWapi-data`、用户凭据、日志、数据库、仓库和 browser profile 不属于发行 ZIP。

v1.6.1 portable manifest 对应源码 commit：

```text
c901841faeede4b851946bb35b6c1724fa1ffb74
```

## 从源码构建

开发环境与发行门禁见 [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md)、[`docs/LOCAL_VALIDATION.md`](docs/LOCAL_VALIDATION.md) 和 [`docs/RELEASE_CHECKLIST.md`](docs/RELEASE_CHECKLIST.md)。
