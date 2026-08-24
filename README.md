<div align="center">

# CWapi

**Turn ChatGPT Web into a local Windows development agent through Slack.**

**让网页 ChatGPT 通过 Slack 调用本机开发环境，并绑定到准确的 GitHub commit。**

[![Release](https://img.shields.io/github/v/release/AAAYNMMM/CWapi-Releases?style=flat-square&label=Release)](https://github.com/AAAYNMMM/CWapi-Releases/releases)
![Protocol](https://img.shields.io/badge/Protocol-CWapi%20MCP%20v2-6f42c1?style=flat-square)
![Platform](https://img.shields.io/badge/Platform-Windows%2011-0078d4?style=flat-square)
![Install](https://img.shields.io/badge/Install-Portable-orange?style=flat-square)

[5 分钟开始](#5-分钟开始) · [v1.6.1](#v161-主要变化) · [工作原理](#工作原理) · [Web GPT 入口](docs/WEB_GPT_ENTRY.md) · [用户指南](docs/USER_GUIDE.md)

</div>

---

CWapi 是一个面向个人 Windows 开发环境的 **Web GPT → Slack → 本机工具** 网关。Web GPT 负责理解任务、读取和修改 GitHub；CWapi 负责把结构化请求绑定到指定 GitHub repository 和完整 40 位 commit，在本机准备隔离 worktree、调用 stock MCP 或进程工具，并把真实结果、日志和文件传回 Slack。

CWapi 不需要让 ChatGPT Web 直接连接本机 MCP Server，也不启动 Codex Agent Turn 替 Web GPT 思考。

## 工作原理

```text
ChatGPT Web
   │ GitHub + Slack
   ▼
Slack control channel
   │ [CWapi/MCP/2]
   ▼
CWapi Go Gateway
   ├─ exact GitHub repository + 40-char commit
   ├─ request-unique detached worktree
   ├─ stock Codex MCP relay
   └─ process_start / process_status / process_stop
            │
            ▼
      Local Windows tools
            │
            ▼
 Slack response / Slack File
            │
            └──────────────► ChatGPT Web
```

## 5 分钟开始

1. 从 [GitHub Releases](https://github.com/AAAYNMMM/CWapi-Releases/releases) 下载 `CWapi-v1.6.1.zip`。
2. 完整解压到任意用户可写目录，然后运行 `CWapi.exe`。不要只复制 exe，也不要删除随包 `runtime/`。
3. 按 [`docs/SLACK_SETUP.md`](docs/SLACK_SETUP.md) 创建 Slack App，配置 Socket Mode、Bot scopes、App Token、Bot Token 和控制频道。
4. 在 ChatGPT 中连接 GitHub 与 Slack。
5. 第一次告诉 Web GPT：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 的 `docs/WEB_GPT_ENTRY.md`，然后使用 CWapi v1.6.1 工作流处理我的项目。

之后可以直接给开发任务，例如：

> 使用 CWapi 工作流开发 `https://github.com/username/project`，修改后在对应 exact commit 上完成本机测试。

v1.6.1 已删除 project registry。Web GPT 不需要先在 CWapi 中登记项目，也不使用 `project_id`；repository 请求直接携带 GitHub URL 与完整 40 位 `expected_commit`。

## v1.6.1 主要变化

- 协议升级为 `[CWapi/MCP/2]` / `cwapi-mcp/2`，v1 不再兼容；
- repository identity 改为 GitHub HTTPS URL + exact 40hex commit；
- 每个 repository request 使用独立 detached worktree，共享 mirror；
- `process_start/status/stop` 由 Go Core 直接实现，不再使用旧 Node CWapi process MCP；
- stock MCP 使用 request-scoped ephemeral context；不同 request 不应假定共享浏览器状态；
- `safe` 为默认权限，每次程序重启都会恢复为 `safe`；
- `full_access` 仍先走 Codex，只有结构化权限拒绝才签发 60 秒、一次性 System Token fallback；
- Slack 已支持 MCP image/blob/大文本的 external file upload；
- Playwright 截图要传给 ChatGPT 时，`browser_take_screenshot` 不应指定 `filename`，让工具直接返回 MCP image content；
- 外部环境发现顺序固定为 **CWapi 自管 runtime → 本机已有环境 → FULL 自动安装或用户手动安装**；
- 外部任务单次连续等待/轮询累计最多 3 分钟，到上限必须返回当前状态而不是继续无限等待。

完整执行规则见 [`docs/CHATGPT_WORKFLOW.md`](docs/CHATGPT_WORKFLOW.md)。

## 权限模型

CWapi v1.6.1 只有两个用户权限模式：

```text
safe
  -> 默认模式
  -> repository process 只允许受控边界内写入

full_access
  -> 本次运行临时启用
  -> Codex-first
  -> 只有真实 PERMISSION_DENIED 才进入 System Token fallback
```

`full_access` 不跨程序重启保留。System Token 最多同时 3 个、60 秒过期、一次性使用，并绑定 repository、commit 和最终 invocation。

协议与边界分别见 [`docs/PROTOCOL.md`](docs/PROTOCOL.md) 和 [`docs/SECURITY.md`](docs/SECURITY.md)。

## 本机运行环境

CWapi 自己需要的 Codex、MinGit、Node、Playwright MCP 与 Chromium 已包含在 portable runtime 中。目标项目额外需要的 Python、JDK、Go、Rust、SDK 等按以下顺序处理：

```text
CWapi 自管环境
→ 本机已经安装的环境
→ 都没有：用户切换 FULL 后由 Web GPT 安装，或用户手动安装
→ 安装后重新探测真实 executable/version
```

不要在工作流里固定某台电脑上的 Python、Node 或其它安装路径。CWapi 的 PATH 是启动时冻结的快照，新安装工具必要时使用实际绝对路径，或重启 CWapi 后重新验证。

## Slack 文件与截图

CWapi 可以把 MCP 已经返回的 image/audio/blob/resource/大文本上传为 Slack File。它不会因为普通文本里出现 `./image.png` 或其它本地路径就擅自读取文件。

Playwright 截图需要真正传给 ChatGPT 时推荐：

```json
{
  "fullPage": true,
  "scale": "css",
  "type": "png"
}
```

不要指定 `filename`。详细说明见 [`docs/SLACK_TRANSPORT.md`](docs/SLACK_TRANSPORT.md)。

## 便携包

```text
CWapi/
├─ CWapi.exe
├─ portable-manifest.json
├─ runtime/
└─ CWapi-data/     # 首次运行后生成
```

`CWapi-data`、Slack Token、Credential Manager 内容、日志、数据库、仓库和 browser profile 不属于发行 ZIP。

v1.6.1 portable manifest 的源代码 commit 为：

```text
c901841faeede4b851946bb35b6c1724fa1ffb74
```

本公开仓库同步该 v1.6.1 源码线，并额外保留面向公开发行的用户文档；测试 fixture 中与开发机有关的路径/Slack ID 已替换为匿名示例，不影响生产代码语义。

## 文档入口

- [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md)：第一次安装和使用。
- [`docs/SLACK_SETUP.md`](docs/SLACK_SETUP.md)：Slack App 从零配置。
- [`docs/WEB_GPT_ENTRY.md`](docs/WEB_GPT_ENTRY.md)：Web GPT 快速入口。
- [`docs/CHATGPT_WORKFLOW.md`](docs/CHATGPT_WORKFLOW.md)：完整 Web GPT 执行规则。
- [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md)：故障排查。
- [`docs/PROTOCOL.md`](docs/PROTOCOL.md)：MCP v2 wire contract。
- [`docs/SECURITY.md`](docs/SECURITY.md)：安全与权限边界。
- [`docs/SLACK_TRANSPORT.md`](docs/SLACK_TRANSPORT.md)：Slack transport 与文件交付。
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)：系统架构。
- [`docs/LOCAL_VALIDATION.md`](docs/LOCAL_VALIDATION.md)：维护者回归方法。
- [`CHANGELOG.md`](CHANGELOG.md)：版本变化。

## 从源码构建

开发环境要求 Windows、Go、Node/npm 与 Wails 相关工具。基础检查：

```powershell
go test ./...
cd frontend
npm ci
npm test
npm run build
```

正式 portable 的 staging、runtime、GUI、Slack 与 privacy gate 见 [`docs/LOCAL_VALIDATION.md`](docs/LOCAL_VALIDATION.md) 和 [`docs/RELEASE_CHECKLIST.md`](docs/RELEASE_CHECKLIST.md)。
