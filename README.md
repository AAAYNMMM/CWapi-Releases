# CWapi v1.6.0

CWapi 是面向个人 Windows 开发环境的 **Web GPT → Slack → 本机开发工具** 桥接程序。

Web GPT 负责理解需求、读取和修改 GitHub 项目、选择测试方式与本机开发环境；CWapi 负责把结构化 MCP 请求安全地送到用户配置的项目与 **exact commit** 上下文中，调用本机 stock Codex app-server / MCP server，再把结果、日志或文件返回 Slack。

简单说：

```text
你在 Web GPT 里描述开发任务
        ↓
GPT 在 GitHub 查看 / 修改代码并得到精确 commit
        ↓
GPT 通过 Slack 请求 CWapi 使用本机能力
        ↓
CWapi 在这个 exact commit 上执行测试、启动服务、调用 Playwright 等
        ↓
结果 / 日志 / 截图返回 Slack
        ↓
GPT 根据真实本机结果继续开发
```

CWapi **不运行模型，也不消耗 Codex Agent Turn 来代替 Web GPT 思考**。它更像一条本地执行与回传通道：GPT 是决策者，CWapi 负责项目上下文、执行路由、状态、幂等和结果交付。

使用中有任何问题，可加入“小黑盒群”，链接在帖子置顶评论。

本仓库是公开发行仓库。当前工作树只保留 v1.6.0 的代码、文档与可复现构建配置；旧版本源码仍可在 Git 历史中查看。

## v1.6.0 能做什么

当前正式链路支持：

- 通过 Slack 接收 Web GPT 的 MCP 请求并返回结果；
- 发现 CWapi 中已经配置的项目与 `project_id`；
- 把项目调用绑定到精确的 40 位 Git `expected_commit`；
- 为每次项目调用准备 detached exact-commit worktree；
- 通过 stock Codex app-server 调用配置的 MCP server；
- 通过随包 `cwapi` MCP server 启动、查询和停止本机进程；
- Web GPT 自己发现、安装、选择和管理 Python、Java、JDK、SDK、Go、Rust、Node 等目标项目环境；
- 直接调用 PATH executable、绝对 executable 路径或工作区相对 executable；
- 使用 Playwright 访问本机开发服务器、操作页面、读取 DOM、截图；
- 把长文本、日志、图片和 MCP resource 内容按策略返回为 Slack File；
- 提供默认 `safe` 与用户显式选择的 `full_access` Codex permission profile；
- 对 duplicate request、secret、owned process、ambiguous side-effect replay 等关键边界做保护。

CWapi 不重新实现第二套 Git / Build / Test / File 平台，也不提供旧版 `workspace.open`、`test.run`、`automation.run` 等自定义大工具合同。

## 5 分钟快速开始

第一次使用建议直接按下面顺序做。

### 1. 下载发行版

从 [GitHub Releases](https://github.com/AAAYNMMM/CWapi-Releases/releases) 下载：

```text
CWapi-v1.6.0.zip
```

完整解压到任意可写目录，例如：

```text
D:/Tools/CWapi
C:/Users/name/Apps/CWapi
```

不要只复制 `CWapi.exe`，也不要直接在 ZIP 里运行。

### 2. 启动 CWapi

运行：

```text
CWapi.exe
```

首次运行后用户数据写入：

```text
<安装目录>/CWapi-data/
```

Slack App Token 与 Bot Token 保存在当前 Windows 用户的 Credential Manager，不写进普通配置、日志或发行 ZIP。

### 3. 配置 Slack

CWapi 当前 Slack transport 使用：

```text
App Token    xapp-...
Bot Token    xoxb-...
Channel ID   C...
```

在首次启动或设置页面完成 Slack 配置并确认连接状态正常。

### 4. 添加项目

在“项目”页面添加允许 CWapi 使用的项目。项目记录包括：

```text
display name
GitHub repository identity，例如 owner/repository
本地项目路径
remote URL
```

CWapi 会为项目生成自己的：

```text
project_id = prj-...
```

Web GPT 不应猜这个 ID，而应通过 CWapi discovery / `projects/list` 获取。

### 5. 选择权限模式

默认推荐：

```text
safe
```

只有确实需要扩大 Codex-managed filesystem 权限时再显式启用：

```text
full_access
```

注意：随包 `cwapi` command/process MCP 启动的自由 executable 以当前 Windows 用户权限运行，并不自动继承 Codex thread 的 filesystem/execpolicy sandbox。完整边界见 [`docs/SECURITY.md`](docs/SECURITY.md)。

### 6. 在 ChatGPT 连接 GitHub 和 Slack

Web GPT 需要：

- GitHub：读取、修改并提交目标仓库；
- Slack：向 CWapi 控制频道发送 MCP request，并读取 CWapi response / Slack File。

### 7. 第一次让 Web GPT 认识 CWapi

建议先告诉 Web GPT：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 仓库中的 `docs/WEB_GPT_ENTRY.md`，了解 CWapi v1.6.0 当前工作流。

然后就可以直接给开发任务，例如：

> 使用 CWapi 工作流开发 GitHub 仓库 `username/my-project`，检查当前问题，修复后完成本地测试。

更完整的新手流程见 [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md)。

## 一次正常开发实际发生什么

假设 Web GPT 修改了项目并得到 commit：

```text
0123456789abcdef0123456789abcdef01234567
```

后续本机调用会绑定：

```text
project_id + expected_commit
```

CWapi 内部再完成：

```text
project lookup
   ↓
Git mirror fetch
   ↓
验证 expected_commit
   ↓
detached exact-commit worktree
   ↓
stock Codex thread/start(cwd + permissions)
   ↓
MCP tool call
   ↓
response / Slack File
```

因此“GPT 在 GitHub 修改的是哪个版本”和“本机实际测试的是哪个版本”可以明确对应，而不是拿某个碰巧存在的本地工作树宣布测试通过。

## 本机环境由 Web GPT 管理

v1.6.0 不再要求 CWapi 固定管理目标项目自己的语言与 SDK 环境。

发行包自身已经带有 CWapi 工作所需的 Codex、Git、Node、Playwright MCP 和 Chromium 等 runtime；但目标项目可能需要的这些环境由用户或 Web GPT 决定：

```text
Python / venv
Java / JDK
Go
Rust
Node 版本
Android SDK / NDK
Visual Studio Build Tools
CUDA
其它 portable toolchain
```

推荐流程：

```text
Web GPT 检查本机已有环境
        ↓
已有 → 选择准确 executable
没有 → 选择合适安装方式和安装位置
        ↓
把准确 command + argv 交给 CWapi
        ↓
CWapi 在 exact-commit workspace 上执行
```

例如：

```text
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
C:/Program Files/Git/cmd/git.exe
D:/SDK/jdk-25/bin/java.exe
.venv/Scripts/python.exe
node_modules/.bin/tool.cmd
```

### Windows 路径约定

进入 MCP JSON 前统一把 `\` 转为 `/`：

```text
C:/Projects/example/.venv/Scripts/python.exe
//server/share/tool.exe
```

`C:\\...` 只作为兼容输入，不是正式 Web GPT 工作流格式。这样可以避免 JSON / Slack 文本中的反斜杠转义问题。

### exact-commit worktree 是临时执行上下文

项目调用使用的 detached worktree 会在调用和附件处理完成后释放，因此不要假定“请求 A 在临时 worktree 创建的 `.venv`”一定能在“请求 B”继续存在。

需要跨请求复用的环境，更适合安装在明确的持久位置，再通过绝对 executable 路径调用；或者由 Web GPT 在每次需要时重新准备。

## 进程工具

随包 `cwapi` MCP server 提供：

```text
process_start
process_status
process_stop
```

常见用途：

```text
启动 Python / Node 本地服务
运行测试入口
执行编译器 / SDK
启动需要稍后检查状态的进程
测试完成后停止由 CWapi 启动的服务
```

`process_start` 支持：

```text
PATH executable name
absolute executable path
working-directory-relative executable path
```

native executable 的 argv 直接传入；Windows `.cmd/.bat` 作为 command-script 执行。

## 文件、日志和截图

CWapi 的文件外发原则是：**只处理 MCP 已经返回的内容**。

```text
MCP read/tool permission
        ↓
MCP 已返回 text / blob / image
        ↓
CWapi outbound policy
        ↓
Slack message / Slack File
```

当前主要限制：

- 单个 artifact 最大 8 MiB；
- 单次 MCP response 最多 16 个 artifact；
- 超限明确失败，不静默截断；
- CWapi 不会因为结果里出现 `C:/...`、`file://...` 或其它 URI 就自行读取那个路径；
- delivery 失败不会让有副作用的 MCP tool 被自动重放。

## 便携目录

```text
<任意安装目录>/
├─ CWapi.exe
├─ portable-manifest.json
├─ runtime/
│  ├─ codex/
│  ├─ git/
│  ├─ node/
│  ├─ mcp/
│  └─ browser/
└─ CWapi-data/          # 首次运行后生成
```

CWapi 不依赖启动时的当前工作目录。关闭 CWapi 后可以移动整个安装目录，下一次启动会从新的 executable 目录重新解析随包 runtime。

Windows 11 x64 正常使用发行包时，不需要另外安装 CWapi 自身使用的 Codex、Git、Node、Playwright MCP 或 Chromium。

## 重启与任务状态

CWapi 每次启动建立新的运行会话：

- 不从启动前的 Slack channel history 自动拾取旧任务；
- 当前进程里的 duplicate request 不重复执行；
- 同一进程 Slack 断线重连时从 durable cursor 之后继续；
- app-server 异常退出后，后续调用可重新建立；
- ambiguous side-effect call 不自动 replay。

因此如果一个本机长任务还在运行，不要因为 Web GPT 停止等待就重复提交。正常做法是继续查询原来的 `process_id` / request 状态。

## 文档导航

### 第一次使用

- [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md)：从下载到第一次完整开发的逐步教程
- [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md)：常见错误、故障现象与处理方法
- [`docs/GUI.md`](docs/GUI.md)：控制台、项目、设置、诊断、关于页面说明

### Web GPT

- [`docs/WEB_GPT_ENTRY.md`](docs/WEB_GPT_ENTRY.md)：Web GPT 唯一必读的快速入口
- [`docs/CHATGPT_WORKFLOW.md`](docs/CHATGPT_WORKFLOW.md)：完整工作流、环境管理、等待与验收规则

### 使用与安全

- [`docs/OPERATIONS.md`](docs/OPERATIONS.md)：安装、迁移、项目、环境、进程、日志与恢复
- [`docs/SECURITY.md`](docs/SECURITY.md)：Codex profile、command MCP、secret 与权限边界
- [`docs/SLACK_TRANSPORT.md`](docs/SLACK_TRANSPORT.md)：Slack transport、凭据、幂等和文件外发

### 架构与开发

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- [`docs/PROTOCOL.md`](docs/PROTOCOL.md)
- [`docs/CODEX_TOOLHOST.md`](docs/CODEX_TOOLHOST.md)
- [`docs/RUNTIME_PACKAGE.md`](docs/RUNTIME_PACKAGE.md)
- [`docs/RUNTIME_LOGGING.md`](docs/RUNTIME_LOGGING.md)
- [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md)
- [`docs/LOCAL_VALIDATION.md`](docs/LOCAL_VALIDATION.md)
- [`docs/ACCEPTANCE.md`](docs/ACCEPTANCE.md)
- [`docs/RELEASE_CHECKLIST.md`](docs/RELEASE_CHECKLIST.md)

版本变化见 [`CHANGELOG.md`](CHANGELOG.md)。

## 从源码构建

要求 Windows 11 x64、Go 与 PowerShell。Node、Wails 和发行 runtime 由固定 lock 与脚本准备到仓库相对的 ignored 目录，不依赖开发机固定路径。

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_portable_runtime.ps1

$commit = (git rev-parse HEAD).Trim()
powershell -NoProfile -ExecutionPolicy Bypass -File automation/stage_v160_portable.ps1 `
  -ExpectedCommit $commit `
  -RuntimeSourceRoot .

powershell -NoProfile -ExecutionPolicy Bypass -File automation/validate_v160_portable_release.ps1 `
  -ExpectedCommit $commit
```

发行构建启用 Wails/Go `-trimpath`。staging 会移除 runtime 临时日志，并拒绝用户数据、数据库、浏览器 profile 和凭据类文件。最终 portable release validation 会在不同路径条件下启动真实 GUI，并验证 runtime 版本与数据落点。