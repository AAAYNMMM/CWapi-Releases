# CWapi v1.6.0

CWapi 是面向个人 Windows 开发环境的 **Web GPT → Slack → 本机开发工具** 桥接程序。

```text
Web GPT：理解需求、读写 GitHub、决定怎么测试
        ↓ Slack MCP request
CWapi：项目发现、exact commit、本机执行、状态、结果回传
        ↓
stock Codex app-server → configured MCP server
        ↓
MCP response / Slack File
```

CWapi 不运行模型，也不启动 Codex Agent Turn 替 Web GPT 思考。v1.6.0 的核心目标是让 Web GPT 在**正确项目、正确 Git commit**上调用本机开发能力，而不是重新实现第二套 Git / Build / Test 平台。

使用中有任何问题，可加入“小黑盒群”，链接在帖子置顶评论。

## 5 分钟开始

1. 从 [GitHub Releases](https://github.com/AAAYNMMM/CWapi-Releases/releases) 下载 `CWapi-v1.6.0.zip`。
2. 完整解压到可写目录，例如 `D:/Tools/CWapi`，运行 `CWapi.exe`。不要只复制 exe，也不要删除随包 `runtime/`。
3. 第一次使用先按 [`docs/SLACK_SETUP.md`](docs/SLACK_SETUP.md) 从零配置 Slack App、Socket Mode、Bot scopes、控制频道和三个 CWapi 参数。
4. 在 CWapi“项目”页添加要开发的 GitHub 项目；默认权限保持 `safe`。
5. 在 ChatGPT 中连接 GitHub 和 Slack。
6. 第一次告诉 Web GPT：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 的 `docs/WEB_GPT_ENTRY.md`，然后使用 CWapi v1.6.0 工作流处理我的项目。

随后就可以直接给开发任务，例如：

> 使用 CWapi 工作流开发 `username/my-project`，修改后在对应 exact commit 上完成本机测试。

完整新手流程见 [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md)。

## v1.6.0 主要能力

- Slack MCP request / response 与 Slack File 回传；
- `projects/list` / `mcpServerStatus/list` discovery；
- `project_id + expected_commit` exact-commit 执行；
- stock Codex app-server MCP relay；
- `cwapi/process_start`、`process_status`、`process_stop`；
- PATH、绝对路径、工作区相对 executable 与 `.cmd/.bat`；
- localhost Playwright E2E、DOM 验证、截图；
- Web GPT / 用户自行管理 Python、JDK、Go、Rust、SDK 等目标项目环境；
- `safe` / `full_access` Codex permission profile；
- secret、幂等、owned process、附件大小等运行边界。

这些能力的细节不在 README 重复展开，按下面的文档职责查对应文件。

## 文档怎么读

### 普通用户

- [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md)：从下载到第一次真实开发的完整顺序。
- [`docs/SLACK_SETUP.md`](docs/SLACK_SETUP.md)：**Slack 从零配置唯一教程**，包括 scopes、Socket Mode、token、Channel ID、验证。
- [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md)：按现象和错误码排障。
- [`docs/GUI.md`](docs/GUI.md)：控制台、项目、设置、诊断、关于。

### Web GPT

- [`docs/WEB_GPT_ENTRY.md`](docs/WEB_GPT_ENTRY.md)：Web GPT 唯一必读快速入口。
- [`docs/CHATGPT_WORKFLOW.md`](docs/CHATGPT_WORKFLOW.md)：环境、进程、等待、E2E、验收等完整执行规则。

### 运维 / 安全 / 协议

- [`docs/OPERATIONS.md`](docs/OPERATIONS.md)：运行、迁移、更新、恢复、日志。
- [`docs/SECURITY.md`](docs/SECURITY.md)：Codex profile 与 trusted command boundary。
- [`docs/SLACK_TRANSPORT.md`](docs/SLACK_TRANSPORT.md)：Slack transport 的技术实现；**不是配置教程**。
- [`docs/PROTOCOL.md`](docs/PROTOCOL.md)：MCP frame、discovery、request / response 格式。

### 架构 / 开发 / 发行

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- [`docs/CODEX_TOOLHOST.md`](docs/CODEX_TOOLHOST.md)
- [`docs/RUNTIME_PACKAGE.md`](docs/RUNTIME_PACKAGE.md)
- [`docs/RUNTIME_LOGGING.md`](docs/RUNTIME_LOGGING.md)
- [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md)
- [`docs/LOCAL_VALIDATION.md`](docs/LOCAL_VALIDATION.md)
- [`docs/ACCEPTANCE.md`](docs/ACCEPTANCE.md)
- [`docs/RELEASE_CHECKLIST.md`](docs/RELEASE_CHECKLIST.md)

版本变化见 [`CHANGELOG.md`](CHANGELOG.md)。

## 便携包

发行包大致结构：

```text
<安装目录>/
├─ CWapi.exe
├─ portable-manifest.json
├─ runtime/
└─ CWapi-data/     # 首次运行后生成
```

Windows 11 x64 不需要另外安装 CWapi 自己所需的 Codex、Git、Node、Playwright MCP 或 Chromium。目标项目自己的语言 / SDK 环境不由 CWapi 固定管理。

Slack App Token 与 Bot Token 成功验证后写入当前 Windows 用户的 Credential Manager，不进入普通配置、Git 或发行 ZIP。

## 从源码构建

要求 Windows 11 x64、Go 与 PowerShell：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_portable_runtime.ps1
$commit = (git rev-parse HEAD).Trim()
powershell -NoProfile -ExecutionPolicy Bypass -File automation/stage_v160_portable.ps1 -ExpectedCommit $commit -RuntimeSourceRoot .
powershell -NoProfile -ExecutionPolicy Bypass -File automation/validate_v160_portable_release.ps1 -ExpectedCommit $commit
```

发行构建启用 Wails / Go `-trimpath`，并验证 portable runtime、用户数据隔离与最终 ZIP 的可移动性。