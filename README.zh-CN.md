# CWapi 1.6.3

[English](README.md) | [简体中文](README.zh-CN.md)

[![Release](https://img.shields.io/github/v/release/AAAYNMMM/chatgpt-work-api-Releases?filter=v1.6.3&style=flat-square&label=Release)](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/tag/v1.6.3)
![Windows](https://img.shields.io/badge/Windows-11%20x64-0078d4?style=flat-square)
![Transport](https://img.shields.io/badge/Transport-GitHub%20%2B%20Slack-4A154B?style=flat-square)

**CWapi 1.6.x 是旧版 GitHub + Slack 路线，当前正式版本为 `1.6.3`。**

CWapi 1.6.3 让 ChatGPT Web 通过 GitHub 获取和修改源码，再用 Slack 作为控制与结果传输通道，在你的 Windows 电脑上执行真实的编译、测试、脚本、浏览器和进程任务。负责理解需求、分析结果和决定下一步的是 Web GPT，CWapi 本身不运行 AI 模型。

> 如果你准备使用 CWapi 2.x，请前往 [`main` 分支](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/main)。2.x 已经是另一套 MCP / Tunnel / Coding / Agent 架构，不要把 1.6.x 的 Slack 配置直接搬过去。

## 工作方式

```text
ChatGPT Web
   │ GitHub + Slack
   ▼
Slack 控制频道
   ▼
CWapi 1.6.3
   ▼
本机 Windows 开发工具
   ▼
Slack 返回结果 / 文件
```

ChatGPT 通过 GitHub 读取或修改远端仓库，取得准确的 40 位 commit SHA，然后在指定 Slack 频道发送结构化 CWapi 请求。CWapi 把本机受管 workspace 同步到这个 exact commit，再执行 build、test、浏览器测试、脚本或长进程，结果通过 Slack 返回。

1.6.3 的消息格式是**通过 Slack 传输的 MCP v2 风格 frame**。它不是 ChatGPT 直连本机 MCP，也不需要 OpenAI Secure MCP Tunnel。

## 为什么还会有人选择 1.6.3

- 保留 Web GPT 负责推理、本机负责真实执行的工作方式。
- 每次仓库任务绑定 exact commit，不靠模糊的本地 checkout 状态。
- 同一个 CWapi 进程内复用 repository persistent workspace，保留仍有效的依赖、构建产物和缓存。
- 支持 `process_start`、`process_status`、`process_stop` 管理长任务。
- Playwright 截图和工具产生的文件可以经 Slack 返回。
- 日常保持 `SAFE`，确实需要更高本机权限时再临时使用 `FULL`。
- Slack App/Bot Token 不写入普通 JSON 配置，而是保存在 Windows Credential Manager。

## 版本怎么选

| 路线 | 通信方式 | 主要用途 |
| --- | --- | --- |
| **1.6.x** | GitHub + Slack Socket Mode | 旧版 Web GPT 本地开发工作流 |
| **2.x** | MCP + OpenAI Secure MCP Tunnel | 当前独立架构，详见 `main` |

准备迁移前先看 [版本选择指南](docs/VERSION_GUIDE.zh-CN.md)。

## 5 分钟快速开始

1. 下载 [`CWapi-v1.6.3.zip`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/tag/v1.6.3)，完整解压，不要在 ZIP 里直接运行。
2. 启动解压目录中的 `CWapi.exe`。
3. 安装 [GitHub CLI](https://cli.github.com/)，执行 `gh auth login`，再用 `gh auth status` 确认登录正常。
4. 创建 Slack App，开启 Socket Mode，配置所需 scopes，并建立专用控制频道。完整步骤见 [Slack 配置](docs/SLACK_SETUP.zh-CN.md)。
5. 在 CWapi 中填写 App Token、Bot Token、Channel ID 并保存。
6. 在 ChatGPT 中连接 GitHub 与 Slack。
7. 把目标仓库交给 Web GPT，并让它遵循 [Web GPT 入口](docs/WEB_GPT_ENTRY.zh-CN.md) 与 [ChatGPT 工作流](docs/CHATGPT_WORKFLOW.zh-CN.md)。
8. 确认第一条 CWapi response 能在对应 Slack 请求线程中返回。

第一次从零配置建议直接按 [快速入门](docs/GETTING_STARTED.zh-CN.md) 做。

## 需要先理解的几个概念

### GitHub 是远端源码真相

仓库级请求同时携带 `repository_url` 与 `expected_commit`。commit 必须是完整 40 位 Git SHA，并且能在受管 mirror 中取得。需要让 tracked 源码跨请求长期保留时，应先通过 GitHub 提交修改，再让后续 CWapi 请求使用新的 exact commit。

### Persistent workspace

同一个 CWapi 进程内，同一 repository 会复用一个受管 workspace。新的仓库请求可以把 tracked source 强制同步到新的 exact commit，但不会主动清掉兼容的 ignored / untracked 衍生物。正常退出时会清理 repository workspace；shared Git mirror 会保留。

### SAFE 与 FULL

每次启动都会回到 `SAFE`。`FULL` 也不能绕过永久安全规则。只有 Codex safe backend 返回 CWapi 认可的权限拒绝时，才可能签发一次性、短时有效并绑定原调用参数的 System Token，用于同一 invocation 的 System fallback。

### 长进程

`process_start` 用于仓库级启动。如果返回 `running`，保存 `process_id`，后续用新的全局 request ID 调 `process_status`；需要结束时用 `process_stop`。Web GPT 单次连续等待不要超过 3 分钟。超过 3 分钟任务还没结束，就应明确告诉用户“任务仍在运行”和当前状态，而不是继续无止境轮询。

## 常见用途

- 在用户真实 Windows 环境里编译、测试 GitHub 项目。
- 启动 localhost 服务并执行浏览器自动化。
- 在已准备好的 workspace 中做只读源码搜索，再通过 GitHub 精确读取或修改命中的文件。
- 把截图或生成文件通过 Slack 返回 ChatGPT。
- 让长时间 build/server 继续运行，同时由 Web GPT 分开查询状态。

## 安全与隐私

Slack App/Bot Token 保存在当前 Windows 用户的 Credential Manager；`CWapi-data/config/cwapi.json` 只保存 Slack Channel ID 等非 secret 配置。配置的 Slack 频道本身就是远程控制信任边界。

private Git repository 可以使用当前 Windows 用户已有的 `gh auth git-credential` helper。CWapi 不会修改全局 Git 配置。

更完整的边界见 [安全说明](docs/SECURITY.md)。

## 文档

- [快速入门](docs/GETTING_STARTED.zh-CN.md)
- [用户指南](docs/USER_GUIDE.zh-CN.md)
- [Slack 配置](docs/SLACK_SETUP.zh-CN.md)
- [Web GPT 入口](docs/WEB_GPT_ENTRY.zh-CN.md)
- [ChatGPT 工作流](docs/CHATGPT_WORKFLOW.zh-CN.md)
- [常见问题](docs/FAQ.zh-CN.md)
- [故障排查](docs/TROUBLESHOOTING.zh-CN.md)
- [版本选择指南](docs/VERSION_GUIDE.zh-CN.md)
- [Protocol](docs/PROTOCOL.md)
- [Security](docs/SECURITY.md)
- [Architecture](docs/ARCHITECTURE.md)

## 开发仓库与发行仓库

当前仓库面向发行和用户文档；开发源码位于 [`AAAYNMMM/CWapi`](https://github.com/AAAYNMMM/CWapi)。`1.6.x` 发行分支有意排除了测试、构建脚本、开发历史等发行用户不需要的内容。
