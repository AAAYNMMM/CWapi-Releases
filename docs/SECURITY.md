# CWapi v1.6.0 三层安全边界

安全目标是防误操作、防跑飞和保护本机数据，不做企业 RBAC。最重要的是区分：**Codex-managed execution** 与 **packaged `cwapi` command/process MCP**，它们不是同一个 sandbox。

## 普通用户先记住这 10 件事

1. 默认使用 `safe`，只有明确需要时再开启 `full_access`。
2. `full_access` 不等于关闭所有保护。
3. Web GPT 使用的项目必须先由用户在 CWapi 中配置。
4. 项目调用绑定 `project_id + exact expected_commit`。
5. Slack token 存 Windows Credential Manager，不应进入普通配置和日志。
6. 不把 token/password/private key/API key 放进 `command` / `argv`。
7. `cwapi-safe/full-access` 主要约束 Codex-managed execution。
8. `cwapi/process_start` 启动的自由 executable 以当前 Windows 用户权限运行，不自动继承 Codex thread sandbox。
9. CWapi 不会因为 MCP result 出现一个本地路径就自动读取并上传文件。
10. 无法确认是否已经产生副作用的调用不自动 replay。

## 1. 总体结构

```text
基础层：Codex-managed rules + CWapi 自身强制边界
safe：默认 Codex permission profile
full_access：用户显式扩大 Codex-managed filesystem 权限
另有：packaged cwapi command/process trusted boundary
```

## 2. Codex-managed 基础层

CWapi 在自己的 `CODEX_HOME/rules/default.rules` 写 stock Codex execpolicy，禁止可可靠表达的危险入口，例如 filesystem format、diskpart、boot configuration mutation、generic registry editor、generic taskkill/Stop-Process、`git reset --hard`、history rewrite、destructive force/delete push、无界 destructive `git clean` 组合。

`full_access` 仍加载这些规则。但 packaged command MCP **不经过该 execpolicy**，所以不能把这些 deny 描述成“CWapi 启动的所有命令都一定被拦截”。

## 3. CWapi 自身强制边界

这些属于 transport/lifecycle/state，而不是命令黑名单：

- Slack/Git/API secret 不进入普通日志、MCP body、resource。
- duplicate request 不重复产生副作用。
- terminal response 与 delivery 分离。
- ambiguous side-effect call 不自动 replay。
- caller 不能注入 Codex `threadId` 或 `_cwapi_*` context。
- command 子进程环境不包含 CWapi/Slack/Codex secret。
- CWapi 只结束自己拥有或明确记录的 process tree。

## 4. `safe`

默认 `permission_mode=safe`，CWapi 选择 `cwapi-safe`：profile 基于 managed workspace sandbox，已配置项目与 CWapi data root 进入 workspace roots，`thread/start` 直接传对应 permissions。

因此 `safe` 的 filesystem 写边界由 Codex-managed profile 执行，不是在 Slack parser 里猜命令内容。

**`safe` 不表示所有任意本机子进程都只能访问项目目录。** packaged command/process MCP 另见第 8 节。

## 5. `full_access`

用户显式切换后使用 `cwapi-full-access`。它扩大 Codex-managed filesystem permission，同时保留系统目录 deny、基础 execpolicy、CWapi secret/idempotency/ownership 规则，并且不使用裸 `:danger-full-access` 关闭外层 sandbox。

所以 `full_access` 是“扩大 Codex-managed 权限”，不是“取消所有保护”。

## 6. 为什么不在 Slack 文本里做命令黑名单

Slack 是 transport，不是本地权限引擎。把权限放在 Slack parser 会造成：换入口可能绕过；CWapi 被迫理解所有 Tool/command 内部语义；最后又重造一套不完整 safety layer。

当前设计让 Codex-managed 权限贴在真实 Codex context 上；对自由 command MCP 则明确承认它是单独 trusted execution boundary。

## 7. 任意 local MCP server 的边界

Codex thread profile 不会自动改变第三方本地进程权限。只有 server 真正在 Codex-managed executor 中、明确支持 sandbox-state/permission elicitation，或自己实现经过验证的等价 sandbox，才能声称受对应权限体系约束。

否则它是独立受信任程序，不能靠“是 Codex 调的”这句话自动获得 sandbox。

## 8. Packaged command/process MCP

用户明确允许 Web GPT 通过 packaged `cwapi` server 提交自由 `command + argv`。PowerShell/cmd 只是普通 executable；CWapi 不维护 Python/Java/Node/安装器/命令内容 allowlist。

支持：

```text
PATH executable name
absolute executable path
working-directory-relative executable path
```

例如：

```text
python.exe
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
C:/Program Files/Java/jdk-25/bin/java.exe
.venv/Scripts/python.exe
tools/build.cmd
```

native executable 的 argv 直接传入；`.cmd/.bat` 使用 Windows command-script 语义。绝对 executable 可以位于 workspace 外。

这条能力等价于：**当前 Windows 用户权限下、由用户明确允许的 trusted remote command execution。**

## 9. command/process 仍强制什么

- request 绑定已配置 `project_id + expected_commit`。
- 初始 cwd 必须是 detached exact-commit workspace 或其真实子目录。
- caller 不能伪造 `_cwapi_*` context。
- duplicate request 不重复启动命令。
- stdout/stderr 持续 drain、脱敏并有界返回。
- stop/shutdown 只结束该 server 启动并记录的 process tree。
- CWapi credential 不进入 app-server/MCP/command 子进程环境。
- request/process 状态可审计。

这些很重要，但它们不是 filesystem sandbox。

## 10. command/process 不保证什么

它不保证 command 被限制在 workspace；不保证 `cwapi-safe/full-access` 能限制自由子进程；不保证安装器只修改 CWapi-data、不请求管理员权限或不改系统；不保证 CWapi 能理解所有命令语义、替用户判断版本，或管理所有语言环境。

环境选择与安装决策属于 Web GPT / 用户职责。

## 11. Secret

禁止把 secret 放进 `command`、`argv`、普通 Slack 消息、GitHub commit、artifact、截图、stdout/stderr。需要认证时优先使用 Windows Credential Manager、已登录 CLI、工具自己的 credential store 或本机既有认证状态。

CWapi 自己的 Slack/Codex secret 不注入 command 子进程环境。

## 12. Project + exact commit 的意义

```text
project_id → configured repository → mirror fetch
→ verify expected_commit → detached worktree
```

它提供的是**执行身份与版本绑定**：这个操作属于哪个用户配置项目、针对哪个 Git commit。它不是通用 filesystem sandbox，但能避免测试错项目/错版本。

## 13. 文件读取与 Slack 外发

```text
MCP 是否取得内容 → MCP 已返回内容
→ CWapi outbound policy → Slack message / Slack File
```

CWapi 不因为 result 中出现 `C:/secret.txt`、`file://...` 等 URI 就自行读取文件。只有 MCP 已经返回的 text/blob/image/resource content 才进入 outbound policy。

## 14. Duplicate / replay

`request_id` 是业务身份，fingerprint 绑定 project/commit/method/params。same request + same fingerprint 不执行两次；same ID + different fingerprint 是 conflict；ambiguous side-effect failure 不自动 replay；delivery failure 不让 tool 再执行；当前会话已有 terminal response / Slack file reference 可复用。

## 15. Owned process

CWapi 只主动结束自己拥有或通过 packaged process server 启动并记录的进程树。不要用模糊进程名全局 kill 所有 `python.exe`、`node.exe`、`codex.exe`，那会伤到用户自己的无关工作。

## 16. 什么时候用 full_access

工作主要发生在已配置项目与 CWapi data 范围时保持 `safe`。只有 Codex-managed MCP/filesystem 操作确实需要 safe profile 外的普通用户路径，并且用户理解扩大范围时再考虑 `full_access`。

如果问题来自 packaged command MCP，自由 executable 本来就不是由 `cwapi-safe` filesystem profile 限制；不要为了“让 command 能跑”盲目切 full_access。

## 17. 验收重点

至少验证：safe/full thread 使用正确 profile；不出现 `:danger-full-access` 默认选择；project/permission 变化重建 context；caller threadId / `_cwapi_*` 注入被拒绝；duplicate/secret/owned-process 不回归；command/argv 经 stock relay 到 packaged server；初始 CWD 为 prepared exact-commit workspace；short command 可 terminal、long command 可 status/stop；command 子进程看不到 CWapi/Slack/Codex secret；absolute/cwd-relative executable 正确解析；Slack outbound 不根据 path 字符串擅自读取文件。

相关：[`USER_GUIDE.md`](USER_GUIDE.md)、[`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)、[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)、[`PROTOCOL.md`](PROTOCOL.md)、[`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)。