# CWapi v1.6.0 三层安全边界

安全目标是防误操作、防跑飞和保护本机数据，不做企业 RBAC。

CWapi v1.6.0 必须区分两个经常被混在一起的概念：

```text
Codex-managed execution
```

和：

```text
packaged cwapi command/process MCP
```

它们不是同一个 sandbox。

---

## 普通用户先记住这 10 件事

1. 默认使用 `safe`；只有明确需要时再开启 `full_access`。
2. `full_access` 不等于关闭所有保护。
3. Web GPT 使用的项目必须先由用户在 CWapi 中明确配置。
4. 项目调用绑定 `project_id + exact expected_commit`。
5. Slack token 保存在 Windows Credential Manager，不应出现在普通配置和日志。
6. 不要把 token、password、private key、API key 放进 MCP `command` / `argv`。
7. `cwapi-safe` / `cwapi-full-access` 主要约束 Codex-managed execution。
8. 随包 `cwapi/process_start` 启动的自由 executable 以当前 Windows 用户权限运行，不自动继承 Codex thread sandbox。
9. CWapi 不会因为 MCP 结果里出现一个本地路径，就自动读取并上传那个文件。
10. 对无法确认是否已经产生副作用的调用，CWapi 不自动 replay。

如果只想知道“日常怎样安全用”，上面十条是最重要的。

---

## 总体结构

```text
基础层：永久生效的 Codex-managed rules + CWapi 自身强制边界
安全权限：默认 Codex profile
完全访问权限：用户显式选择的 Codex profile
```

另外还有一个单独的 trusted boundary：

```text
packaged cwapi command/process MCP
```

它不能被错误描述成“自动被 safe/full_access sandbox 约束”。

---

## 1. Codex-managed 基础层

基础层不因权限模式改变，但这里只约束 Codex-managed execution；它不会自动约束任意 local stdio MCP server。

### Codex 直接强制

CWapi 在自己的 `CODEX_HOME` 写入 stock Codex execpolicy：

```text
rules/default.rules
```

其中禁止可可靠表达的危险入口，例如：

- filesystem format；
- diskpart；
- boot configuration mutation；
- generic registry editor；
- generic taskkill / Stop-Process；
- `git reset --hard`；
- Git history rewrite；
- destructive force/delete push；
- unbounded destructive `git clean` 组合。

完全访问权限仍加载这些规则。

**重要：packaged command MCP 不经过该 execpolicy。**

因此不能把上述 deny 描述成“CWapi 启动的所有命令都一定被拦截”。

### CWapi 自身强制

以下不是某个 shell command 的权限判断，而是 CWapi 自己的 transport / lifecycle / state 责任：

- Slack / Git / API secret 不进入普通日志、MCP body、resource；
- duplicate request 不重复产生副作用；
- terminal response 与 delivery 分离；
- ambiguous side-effect call 不自动 replay；
- CWapi 只结束自己拥有的 app-server process tree；
- caller 不能注入 Codex `threadId`；
- caller 不能注入 `_cwapi_workspace`、`_cwapi_expected_commit`、`_cwapi_request_id`；
- command MCP 子进程环境不包含 CWapi / Slack / Codex secret。

这些检查基于 transport / lifecycle / state，不靠 Slack parser 去猜命令语义。

---

## 2. 安全权限 `safe`

默认：

```text
permission_mode = safe
```

CWapi 选择 Codex profile：

```text
cwapi-safe
```

实现：

- profile `extends = ":workspace"`；
- 已配置项目目录加入 workspace roots；
- CWapi data root 加入 workspace roots；
- `thread/start` 直接传 `permissions = "cwapi-safe"`。

因此安全模式的 Codex-managed filesystem 写边界由 Codex permission profile 执行，而不是 CWapi 在 Slack 入站处解析命令文本。

### safe 不代表什么

`safe` **不等于**：

```text
所有本机子进程都只能访问项目目录
```

特别是 packaged command/process MCP 需要单独理解，见后文。

---

## 3. 完全访问权限 `full_access`

用户显式切换：

```text
permission_mode = full_access
```

CWapi 选择：

```text
cwapi-full-access
```

它使用 managed filesystem profile 扩大访问范围，同时：

- 保留系统目录 deny；
- 保留基础 execpolicy；
- 保留 CWapi secret / idempotency / ownership 规则；
- **不使用 `:danger-full-access`**，因为该 built-in profile 会关闭外层 filesystem sandbox。

所以 `full_access` 是扩大 Codex-managed filesystem permission，不是“取消一切安全边界”。

---

## 4. 为什么不在 Slack 文本里判断命令危险不危险

Slack 是 transport，不是本地权限引擎。

如果把权限写成 Slack parser 的字符串黑名单，会产生两个问题：

1. 换一个入口可能绕过；
2. CWapi 必须理解每一种 Tool / command 的内部语义，最终又会重造一套不完整的安全层。

因此当前设计让 Codex-managed 权限贴在真实 Codex context 上。

对于自由 command/process MCP，则明确承认它是另一条 trusted remote execution boundary，而不是假装 Slack filter 能把它变安全。

---

## 5. MCP server 边界

Codex thread profile 不会神奇地改变一个任意第三方本地进程的权限。

对于 local stdio MCP server，只有满足以下至少一项时，才能把它视为受 Codex 权限体系约束：

- server 在 Codex-managed environment / executor 中运行；
- server 明确支持 Codex sandbox-state metadata；
- server 使用 Codex permission elicitation 并正确执行结果；
- server 自身有经过验证的等价 sandbox。

否则该 server 是独立受信任程序。

新增第三方 MCP server 时，不要只因为“它是 Codex 调用的”就自动声称它受 `cwapi-safe` filesystem sandbox 保护。

---

## 6. 通用 command/process 边界

用户明确允许 Web GPT 通过 packaged `cwapi` MCP server 提交自由：

```text
command + argv
```

PowerShell / cmd 只是普通 executable。

CWapi 不维护 Python / Java / Node / 安装器 / 命令内容 allowlist。

### command 可以是什么

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
tools/build.cmd
.venv/Scripts/python.exe
```

绝对 executable 可以位于 workspace 外。

CWD 相对 executable 从 prepared exact-commit workspace 或指定真实子目录解析。

native executable 不经过 shell；`.cmd/.bat` 文件使用 Windows command-script 语义。

### 这条能力实际意味着什么

它等价于：

> 当前 Windows 用户权限下、由用户明确允许的 trusted remote command execution。

这句话必须保持清楚，不能用模糊的“safe mode”包装掉。

---

## 7. command/process 路径仍然强制什么

即使自由 executable 不受 Codex thread sandbox 自动约束，CWapi 仍然强制：

- request 绑定已配置 `project_id + expected_commit`；
- 初始工作目录必须是 detached exact-commit workspace 或其真实子目录；
- caller 不能伪造 `_cwapi_*` context；
- duplicate request 不重复启动命令；
- stdout / stderr 持续 drain、脱敏并有界返回；
- stop / shutdown 只结束该 server 启动并记录的 process tree；
- CWapi credential 不进入 app-server / MCP / command 子进程环境；
- request / process 状态可审计。

这些边界依旧非常重要，只是它们不是 filesystem sandbox。

---

## 8. command/process 路径不提供什么保证

不要声称它保证：

- command 被限制在 workspace 文件系统内；
- `cwapi-safe` / `cwapi-full-access` profile 能限制这个自由子进程；
- 命令只安装到 CWapi-data；
- 命令永远不会修改用户目录或系统配置；
- Web GPT 选择的版本一定正确；
- 安装器一定不请求管理员权限；
- CWapi 会理解并审核所有命令语义；
- CWapi 会替用户管理所有语言环境。

环境选择与安装决策属于 Web GPT / 用户职责。

---

## 9. Secret 处理

禁止把 secret 放进普通协议内容。

尤其不要放进：

```text
command
argv
Slack 普通消息
GitHub commit
artifact
截图
stdout/stderr
```

需要认证时优先使用：

- Windows Credential Manager；
- 已登录的 CLI；
- 工具自己的 credential store；
- 本机既有认证状态。

CWapi 自己的 Slack / Codex secret 不注入 command 子进程环境。

---

## 10. Project + exact commit 的安全意义

项目相关 request 需要：

```text
project_id + expected_commit
```

CWapi：

```text
project lookup
 -> repository
 -> Git mirror fetch
 -> verify expected_commit
 -> detached worktree
```

这提供的是**执行身份与版本绑定**：

```text
这个操作属于哪个用户配置项目
这个操作针对哪个 Git commit
```

它不是通用 filesystem sandbox，但能避免测试“错误项目 / 错误版本”的大量问题。

---

## 11. 文件读取与 Slack 外发

两层权限：

```text
MCP 是否取得内容
 -> CWapi outbound policy
 -> Slack message / Slack File
```

CWapi 不因为 result 中出现：

```text
C:/secret.txt
file:///...
其它 URI
```

就自行读取该路径。

只有 MCP 已经返回的：

```text
text
blob
image
resource content
```

才进入 outbound policy。

这避免“返回一个路径字符串”自动升级成“允许读取并上传该文件”。

---

## 12. Duplicate / replay 边界

- `request_id` 是业务身份；
- fingerprint 绑定 project / commit / method / params；
- same request + same fingerprint 不执行两次；
- same request + different fingerprint 是 conflict；
- ambiguous side-effect tool failure 不自动 replay；
- delivery failure 不自动导致 tool 再执行；
- 当前会话已有 terminal response / Slack file reference 可复用。

这是防止“网络抖了一下，于是同一个安装 / 删除 / 编译 / server 被重复启动”的核心边界。

---

## 13. Owned process

CWapi 只应主动结束自己拥有或明确通过 packaged process server 启动并记录的进程树。

不要通过模糊的进程名去全局 kill：

```text
所有 python.exe
所有 node.exe
所有 codex.exe
```

这样的做法可能杀掉用户自己正在运行的无关工作。

---

## 14. 用户什么时候应该用 full_access

常见原则：

### 保持 safe

如果工作主要发生在：

```text
已配置项目
CWapi 自己的数据范围
```

就保持默认。

### 考虑 full_access

如果 Codex-managed MCP / filesystem 操作确实需要访问 safe profile 之外的普通用户路径，并且用户理解这个扩大范围。

### full_access 解决不了的事情

如果问题来自 packaged command MCP 自由 executable，自由 command 本来就不是由 `cwapi-safe` filesystem profile 限制的。

不要为了“让 command 能跑”盲目切 `full_access`。

---

## 15. 验收

至少验证：

- safe thread 使用 `cwapi-safe`；
- full thread 使用 `cwapi-full-access`；
- 不出现 `:danger-full-access` 默认选择；
- project / permission 变化导致 context 重建；
- Codex 能加载两个 profile 和 base rules；
- caller-supplied threadId 被拒绝；
- caller-supplied `_cwapi_*` context 被拒绝；
- duplicate / secret / owned-process 规则不回归；
- command / argv 经 stock Codex MCP relay 到达 packaged server；
- command 初始 CWD 等于 prepared exact-commit workspace；
- short command 可返回 terminal exit / log；
- long command 可 status / stop；
- command 子进程看不到 CWapi / Slack / Codex secret 环境变量；
- absolute path / cwd-relative executable 正确解析；
- Slack outbound 不会根据 path 字符串擅自读取本地文件。

---

## 16. 相关文档

- [`USER_GUIDE.md`](USER_GUIDE.md)：普通用户使用方式
- [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)：常见权限与执行错误
- [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)：Web GPT 环境与执行流程
- [`PROTOCOL.md`](PROTOCOL.md)：request / response / discovery
- [`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)：Slack transport / file delivery