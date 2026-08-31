# CWapi 1.6.3 故障排查

[English](TROUBLESHOOTING.md) | [简体中文](TROUBLESHOOTING.zh-CN.md)

按你实际看到的现象查。Slack 配置细节见 [Slack 配置](SLACK_SETUP.zh-CN.md)，协议规则见 [ChatGPT 工作流](CHATGPT_WORKFLOW.zh-CN.md)。

## CWapi 启动失败

**现象**

`CWapi.exe` 一启动就退出、报启动/配置错误，或者 GUI 根本无法进入可用状态。

**可能原因**

portable 包不完整、目录不可写、config 无效，或者 bundled runtime / data root 校验失败。

**检查什么**

- ZIP 是否完整解压。
- 是否在 ZIP 内直接运行。
- `CWapi-data/config/cwapi.json` 与最新 runtime error。
- 产品版本是 1.6.3，但当前 `cwapi.config.v2` 的 schema version 仍然是 `1.6.1`。

**怎么修**

把 `CWapi-v1.6.3.zip` 重新完整解压到干净、当前用户可写的目录。不要手动把配置 schema version 改成 `1.6.3`，当前代码要求这里仍为 `1.6.1`。

## Slack degraded 或一直重连

**现象**

Slack 短暂 connected 后变 degraded，或者 Socket Mode 一直 reconnect。

**可能原因**

Token 无效/已撤销、缺 `connections:write`、Workspace 不一致、网络中断或 channel readiness 失败。

**检查什么**

- App Token 是否为 `xapp-...`。
- Socket Mode 是否开启。
- App-Level Token 是否有 `connections:write`。
- Bot Token 是否属于同一个 Workspace。
- Channel ID 是否正确。
- Bot 是否已经加入频道。

**怎么修**

修正 Slack App 配置；scope 变化后重新安装 App；已撤销的 token 重新生成并在 CWapi 保存。

## 收不到 ChatGPT 请求

**现象**

ChatGPT 已经往 Slack 发消息，但 CWapi 完全没有 claim request。

**可能原因**

发错频道、Bot Event 不对、frame 格式坏了，或者第一行不是 `+++`。

**检查什么**

- ChatGPT 使用的是否就是 CWapi 配置的控制频道。
- public channel 是否订阅 `message.channels`；private channel 是否订阅 `message.groups`。
- frame 是否准确从下面开始：

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
```

- JSON schema/protocol 是否为 v2。

**怎么修**

在正确频道发送完整 MCP v2 frame。修改 event subscription 后重新安装 Slack App。

## 能收请求但不能回 response

**现象**

CWapi 已经处理 request，但 Slack thread 没有 response。

**可能原因**

缺 `chat:write`、Bot Token 失效、Slack API 故障，或者 Bot 已经不在频道里。

**检查什么**

- `chat:write` 是否存在。
- Bot Token 是否有效。
- Bot 是否仍在当前配置频道。
- Slack runtime error。

**怎么修**

补 scope 并重新安装 App，恢复 Bot 频道成员关系。需要重新发动作时使用新的 request ID。

## 文件上传失败

**现象**

文本能正常返回，但截图或其它文件不出现。

**可能原因**

缺 `files:write`，底层工具只返回了路径文本，或者没有可外置的 binary result。

**检查什么**

- Bot Token 是否真的带 `files:write`。
- tool result 是否真的包含 bytes/resource content。
- Playwright 截图是否传了 `filename`。

**怎么修**

补 `files:write` 并重新安装 App。Playwright 用 `browser_take_screenshot` 时不要传 `filename`。不要指望打印一个本机路径就会自动上传。

## Bot 不在 channel

**现象**

readiness 或发消息提示 Bot 不是 channel member，或者 scope 看起来正确但频道操作仍失败。

**可能原因**

Bot 从没被邀请、后来被移除，或者配置的 Channel ID 指向另一个频道。

**检查什么**

核对 `C...` Channel ID 与实际控制频道，并看 Bot 是否在成员列表里。

**怎么修**

把 CWapi Bot 加入准确的控制频道，再做真实通信测试。

## `missing_scope`

**现象**

Slack API 返回 `missing_scope`。

**可能原因**

缺少当前动作需要的 scope，或者虽然添加了 scope，却忘了重新安装 App。

**检查什么**

public channel 基线：

```text
connections:write
channels:read
channels:history
chat:write
files:write
message.channels
```

private channel 另外需要：

```text
groups:read
groups:history
message.groups
```

**怎么修**

补上缺失 scope/event，然后重新安装 App 到 Workspace。

## Private repository 无法访问

**现象**

private GitHub repository 的 mirror fetch/clone/update 失败。

**可能原因**

当前 Windows 用户的 GitHub CLI 未登录、认证过期、没有仓库权限，credential helper 无法解析，或者 repository URL 不合法。

**检查什么**

执行：

```powershell
gh auth status
```

确认当前账号确实能访问目标仓库，并检查 URL 是否是正常 GitHub HTTPS repository URL。

**怎么修**

重新 `gh auth login`，或者给当前账号正确仓库权限。CWapi 可以调用已有 `gh auth git-credential`，但不会替你修复 GitHub 账号权限。

## Exact commit 错误或不存在

**现象**

repository prepare 因 `expected_commit` 无效、缺失或 mirror 找不到而失败。

**可能原因**

用了短 SHA、拼错、把 branch name 当 commit，commit 不属于当前 repository，或者远端没有这个对象。

**检查什么**

- `expected_commit` 是否正好 40 位 hex。
- commit 是否属于当前 repository。
- GitHub 上是否能看到同一个 commit。

**怎么修**

重新从 GitHub 解析 exact commit，然后用正确的 repository URL + 完整 commit 发新 request。

## `process invocation could not be resolved`

**现象**

`process_start` 找不到 executable/script。

**可能原因**

命令不在 CWapi 启动时冻结的 PATH 中、猜的绝对路径不对，或者 repository 脚本只写了 basename，却依赖错误 cwd 假设。

**检查什么**

- 先检查 CWapi 管理的 runtime/tool。
- 用 `where` / `Get-Command` 等只读方式确认当前 CWapi 进程到底能解析什么。
- 检查 repository-relative script path。

**怎么修**

使用真实 executable path，或明确的 repository 相对脚本路径。确实缺环境时，只在用户授权 `FULL` 后安装，或者由用户手动安装，再重新探测。

## SAFE 权限失败

**现象**

命令因为需要 SAFE 边界外权限而被阻止。

**可能原因**

invocation 需要更广 filesystem/system 权限，或者触发永久安全规则。

**检查什么**

先区分结构化 `PERMISSION_DENIED` 和普通 build/test/program failure。再看顶层命令是否属于永久禁止范围。

**怎么修**

如果任务确实需要更高权限，用户可以切 `FULL` 后按 fallback 规则重试。永久 policy block 不能靠 FULL 绕过。

## 已经 FULL，但仍然没有 System 执行

**现象**

FULL 下命令失败，但 CWapi 没签发/没接受 System Token。

**可能原因**

错误不是受认可的 sandbox permission denial、retry invocation 变了、Token 过期/已消费、Token 放错 JSON 位置，或者永久 policy 不允许。

**检查什么**

- 原 response 是否真的是 `blocked + PERMISSION_DENIED`。
- Token 是否在 request 顶层，且未超过 60 秒。
- retry 是否只换 `request_id`，repository、commit、executable、argv、cwd 全部不变。
- Token 是否已经被用过。

**怎么修**

按正确 permission flow 重试。程序自己报错时就修程序，不要试图把普通失败包装成权限绕过。

## Process 一直是 `running`

**现象**

`process_start` 返回 `process_id`，任务没有很快结束。

**可能原因**

任务本来就很长、卡在自己的工作上，或者它本来就是 server/watcher。

**检查什么**

使用新的 global request ID 调 `process_status`，查看 state 与 stdout/stderr tail。

**怎么修**

合理间隔继续查同一个 `process_id`，不要重复 `process_start`。真要结束时才调用 `process_stop`。

## 到了 3 分钟等待上限

**现象**

Web GPT 不再继续等，而是告诉你任务仍在运行。

**可能原因**

工作流明确规定单次连续等待/轮询最多 3 分钟。

**检查什么**

是否已经有稳定 `process_id` 或 request state。

**怎么修**

保留现有状态，之后用新的 status request 再查。不要拆成很多个 30 秒轮询来绕过 3 分钟累计上限。

## Playwright 页面状态丢了

**现象**

后续 browser 动作找不到之前的 page、locator 或 session 状态。

**可能原因**

不能假设两个无关 stock MCP request 共享 browser page/tab/locator/session。

**检查什么**

之前是否把连续浏览器步骤拆到了不同 request-scoped context。

**怎么修**

底层工具需要活 session 时，把 navigate/fill/click/assert/screenshot 放在能够保持状态的同一流程里；否则在新 request 明确重建页面状态。

## Screenshot 只得到路径

**现象**

response 里只有 `./screenshot.png` 一类路径，没有真正图片。

**可能原因**

截图工具被要求写文件，所以返回的是路径文本而不是 image content。

**检查什么**

`browser_take_screenshot` 是否传了 `filename`。

**怎么修**

不传 `filename` 重新调用，让 MCP result 返回真实 image bytes，再由 CWapi 上传 Slack。

## Duplicate request 行为不符合预期

**现象**

重复 Slack request 返回了旧 response，或者同 request ID 被判冲突。

**可能原因**

CWapi 会做 request fingerprint。同 request ID + 同 fingerprint 可以重投已保存 response；同 ID + 不同内容会被拒绝。

**检查什么**

比较完整 canonical request 内容和 request ID。

**怎么修**

每个新动作/状态查询都换新 request ID。只有明确重投完全相同 request 时才复用 ID。

## Workspace 出问题

**现象**

CWapi 无法 prepare/resync repository workspace，或者报告 workspace root/integrity 错误。

**可能原因**

Git/认证失败、tracked/untracked path conflict、filesystem/reparse 问题、旧 derived state 与新 commit 不兼容，或者受管 root 完整性被破坏。

**检查什么**

- Git/auth error 细节。
- ignored/untracked 是否和新 tracked tree 冲突。
- `CWapi-data/workspaces` root integrity。
- 受管 root 是否被 symlink/reparse point 替换。

**怎么修**

先修认证或 filesystem integrity。如果是项目衍生状态冲突，只清理必要的项目 build/cache 目录，并使用项目自己的 cleanup 命令。CWapi 运行时不要随手删除内部目录。

## 需要更多诊断信息

查看 GUI 最新 structured execution/runtime error 与有界 runtime log。普通日志会对常见 credential/token 形态做脱敏。排查时也不要把 Slack Token 或 System Token 复制到 issue 或公开日志里。
