# CWapi 2.0

CWapi 是一个 Windows 桌面桥接程序，为 Web GPT 提供两条彼此隔离的本地链路：

```text
CODING  Web GPT -> Coding MCP -> durable Git workspace -> private Codex command/exec -> GitHub
AGENT   Agent Web GPT -> Agent MCP -> broker -> localhost OpenAI-compatible software
```

Coding 与 Agent 可以并发运行，使用独立 token、独立 MCP server、独立 Tunnel 配置和完全隔离的 tool catalog。

> 本仓库是 **发行仓库**。`main` 只保存 CWapi 2.x 的干净发行源码快照与发行文档，不保存测试、构建/打包脚本或开发过程记录。实际开发仓库为 [`AAAYNMMM/CWapi`](https://github.com/AAAYNMMM/CWapi)。

## 发行路线

| 路线 | 分支 | 当前版本 | 用途 |
| --- | --- | --- | --- |
| 主线 | `main` | `2.0.0` | CWapi 2.x，新架构 |
| 旧版支线 | `1.6.x` | `1.6.3` | 保留 1.6 系列，不再与 2.x 混合 |

CWapi 2.0 与 1.6.3 的通信方式和内部结构差异很大，请不要把两条路线的配置、文档或工作流混用。

## 下载

正式便携版请从本仓库的 [Releases](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases) 下载。

当前主线：`CWapi-v2.0.0.zip`

2.0.0 发行包对应开发仓库源码提交：

```text
d904ae80428c90717e050a151c65fa35b6b83c63
```

## CWapi 2.0 提供什么

### Coding MCP

```text
coding_open
coding_exec
coding_status
coding_attachment
coding_close
```

- Web GPT 是 Coding 链唯一的 agent；
- 每个 canonical repository 使用一个 durable workspace；
- 可用完整 `expected_commit` 做 exact baseline guard；
- `resume=true` 可在新对话中继续同一仓库的现有 workspace；
- `coding_exec` 只执行 Web GPT 给出的精确 command/argv；
- bundled Codex 只作为 model-free `command/exec` 工具宿主，不创建 Codex thread/turn，不调用 Codex agent；
- SAFE/FULL 分别映射 Codex `workspace-write` / `danger-full-access`；
- `coding_attachment` 只用于受支持的 raster image，普通源码、JSON、日志等文本通过 `coding_exec` 读取。

### Agent MCP + OpenAI-compatible Provider

```text
agent_open
agent_exchange
agent_close
```

本地 Provider：

```text
GET  /v1/models
POST /v1/chat/completions
```

Agent 链让本地 OpenAI-compatible 软件把任务交给 Web GPT，再把工具调用结果返回本地软件。CWapi 负责有界请求队列、bridge 生命周期、结果关联与图片中转，不把普通文件扩展成 MCP 文件资源。

## 本地端点与 Tunnel

CWapi 只监听 loopback：

```text
http://127.0.0.1:<mcp-port>/mcp/coding/<coding-token>
http://127.0.0.1:<mcp-port>/mcp/agent/<agent-token>
http://127.0.0.1:<agent-port>/v1
```

这些地址只供同一台电脑上的本地客户端使用。ChatGPT Web 不能直接访问 `127.0.0.1`。

如果要把 Coding 或 Agent MCP 接入 ChatGPT，请使用 OpenAI Secure MCP Tunnel。portable 已内置官方 `tunnel-client`。Coding 与 Agent 应分别使用自己的 Tunnel 配置；如果两条链都接入 ChatGPT，应创建两个 Tunnel，避免路由混用。

## 快速开始

1. 下载并完整解压 `CWapi-v2.0.0.zip`。
2. 运行 `CWapi.exe`。
3. 在 GUI 中按需要配置 Coding / Agent 以及对应 Tunnel。
4. 在 ChatGPT 中添加对应 MCP 应用。
5. 首次连接后，CWapi 会通过 MCP server instructions 告诉 Web GPT 当前链路的操作规则。

程序首次启动会在自身旁边创建：

```text
CWapi-data/config/cwapi.json
CWapi-data/workspaces/<repository-hash>/repo
```

## Portable 内容

正式 Windows portable 只包含：

```text
CWapi.exe
portable-manifest.json
runtime/codex/current/...
runtime/git/...
runtime/tunnel/current/tunnel-client.exe
```

2.0.0 锁定的运行时：

- OpenAI Codex `0.150.1`
- MinGit `2.55.0.windows.4`
- OpenAI `tunnel-client 0.0.10`

Node、Wails、测试工具、构建脚本和用户数据不会进入 portable。

## 隐私与凭据

2.0.0 发行包在发布前通过 portable privacy gate。包内不包含 `CWapi-data`、Git 仓库元数据、浏览器 profile、日志数据库、`.env`、token/credentials 文件、私钥或构建机用户路径；`portable-manifest.json` 标记 `user_data_included: false`。

Tunnel Runtime API key 保存在 Windows Credential Manager，不写入普通配置文件、profile、日志或发行 ZIP。

## 文档

- [`docs/OPERATIONS.md`](docs/OPERATIONS.md)：安装、连接与运行维护
- [`docs/CHATGPT_WORKFLOW.md`](docs/CHATGPT_WORKFLOW.md)：Coding / Agent 的 Web GPT 工作流
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)：2.0 架构
- [`docs/PROTOCOL.md`](docs/PROTOCOL.md)：MCP 与 Provider 协议
- [`docs/GUI.md`](docs/GUI.md)：桌面界面
- [`docs/CODEX_TOOLHOST.md`](docs/CODEX_TOOLHOST.md)：私有 Codex toolhost 边界
- [`docs/SECURITY.md`](docs/SECURITY.md)：安全边界与凭据处理
- [`docs/RUNTIME_LOGGING.md`](docs/RUNTIME_LOGGING.md)：运行状态与日志边界
- [`docs/RELEASE_TRACKS.md`](docs/RELEASE_TRACKS.md)：2.x / 1.6.x 双路线说明

## 源码说明

本仓库 `main` 的源码是正式发行快照，不作为开发工作区使用。为避免把开发过程混入发行面，本分支不携带：

- Git 开发过程文档；
- 单元测试 / 集成测试文件；
- `automation/` 验证与打包脚本；
- `scripts/` runtime 安装/发布脚本；
- 本地验证、验收、Release Checklist 等开发文档。

需要查看完整开发历史、测试和构建链，请前往 [`AAAYNMMM/CWapi`](https://github.com/AAAYNMMM/CWapi)。
