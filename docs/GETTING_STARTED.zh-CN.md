# CWapi 1.6.3 快速入门

[English](GETTING_STARTED.md) | [简体中文](GETTING_STARTED.zh-CN.md)

这份文档从一台还没配置 CWapi 的 Windows 电脑开始，一直做到第一条真实 CWapi response 通过 Slack 返回。

## 1. 下载并完整解压

从 [v1.6.3 Release](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/tag/v1.6.3) 下载 `CWapi-v1.6.3.zip`，完整解压到当前用户可写目录，然后运行目录中的 `CWapi.exe`。

不要直接在 ZIP 内运行，也不要只复制一个 `CWapi.exe`。portable 包旁边还有受管 runtime 和其它运行文件。

第一次启动后，程序会在自身目录旁创建 `CWapi-data`，其中包括 `config/cwapi.json`、运行状态/日志、Git mirror/workspace、临时目录和需要的受管运行状态。Slack secret 不写进普通 JSON 配置。

## 2. 安装并登录 GitHub CLI

GitHub CLI 需要用户自行安装：[GitHub CLI](https://cli.github.com/)。安装后运行：

```powershell
gh auth login
gh auth status
```

CWapi 不额外做一套 `gh` 登录状态预检。访问 private repository 时，如果当前 Windows 用户已有可用的 `gh auth git-credential`，CWapi 会按需让 Git 使用这个 helper；认证失效就会返回真实 Git/认证错误。

## 3. 创建 Slack App

1.6.3 使用 Slack Socket Mode。最省事的方案是建立一个专用 public 控制频道。

App-Level Token scope：

```text
connections:write
```

Bot Token scopes：

```text
channels:read
channels:history
chat:write
files:write
```

Bot Event：

```text
message.channels
```

开启 Socket Mode，生成 `xapp-...` App Token；安装或重新安装 App，取得 `xoxb-...` Bot User OAuth Token；把 Bot 加入控制频道，再复制该频道的 Channel ID（`C...`）。

如果明确使用 private channel，还需要 `groups:read`、`groups:history` 和 `message.groups`。

完整步骤见 [Slack 配置](SLACK_SETUP.zh-CN.md)。

## 4. 在 CWapi 保存 Slack 配置

打开 CWapi 的 Slack 配置区域，填写：

```text
App Token   = xapp-...
Bot Token   = xoxb-...
Channel ID  = C...
```

CWapi 会先验证候选凭据，再替换当前保存的 credential pair。App/Bot Token 存在当前 Windows 用户的 Credential Manager；Channel ID 写入 `CWapi-data/config/cwapi.json`。

GUI 显示 healthy/connected 只代表基础连接通过，第一次真实读消息、发 response、上传文件仍然是检查 scopes 是否完整的最好办法。

## 5. 在 ChatGPT 连接 GitHub 和 Slack

在 ChatGPT 中连接需要使用的 GitHub 仓库权限与 Slack。Web GPT 需要通过 GitHub 获取源码和 exact commit，并通过 Slack 控制频道收发 CWapi frame。

`xapp-...` 与 `xoxb-...` 只属于 CWapi 本机配置，不需要、也不应该放进 ChatGPT prompt。

## 6. 第一次仓库请求需要什么

repository-scoped 请求至少要有：

- GitHub HTTPS repository URL；
- 完整 40 位 Git commit SHA；
- 新的 `request_id`；
- 通过已配置 Slack channel 发送的 CWapi MCP v2 frame。

CWapi 会把受管 repository workspace 准备到这个 exact commit，所以 Web GPT 必须先从 GitHub 获得准确的仓库和 commit。

## 7. 第一次通信测试

可以先做一个 global catalog 调用，例如 `mcpServerStatus/list`，params 为空。

```text
Web GPT
  -> Slack 发送 [CWapi/MCP/2] request
  -> CWapi 接收并执行
  -> CWapi 在原请求 thread 返回 MCP_RESPONSE
  -> Web GPT 读取真实 response
```

1.6.3 没有 `projects/list` 项目注册表。

## 8. 第一次 repository 测试

先做无破坏性的 repository-scoped 操作，例如只读源码搜索、版本命令或项目已有的测试命令。Web GPT 应按这个顺序：

1. 从 GitHub 得到 repository URL 和 exact commit；
2. 发送 repository-scoped request；
3. 让 CWapi 准备受管 workspace；
4. 执行命令；
5. 读取 Slack 返回结果；
6. 再继续修改、编译和测试。

正式使用时遵循 [Web GPT 入口](WEB_GPT_ENTRY.zh-CN.md) 和 [ChatGPT 工作流](CHATGPT_WORKFLOW.zh-CN.md)。

## 9. SAFE 与 FULL

普通任务保持 `SAFE`。每次 CWapi 启动都会在 runtime 创建前把权限原子重置为 `safe`。

`FULL` 只用于用户明确授权、确实需要更高本机权限的任务，而且仍受永久安全规则限制。System fallback 只有在 CWapi 识别到权限拒绝时才可能出现，需要短时、一次性的 System Token；普通程序报错不会获得系统权限。

## 10. 怎么判断配置真的好了

下面几项都成立，才算链路跑通：

- `gh auth status` 对目标仓库有效；
- CWapi 中 Slack 为 healthy/connected；
- Bot 已加入配置的控制频道；
- Web GPT 能通过 Slack 发出结构化 request；
- CWapi 能在同一 request thread 返回 response；
- repository-scoped request 真正运行在指定 exact commit 上。

遇到问题直接查 [故障排查](TROUBLESHOOTING.zh-CN.md)。
