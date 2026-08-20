# CWapi v1.6.0 三层安全边界

安全目标是防误操作、防跑飞和保护本机数据，不做企业 RBAC。

```text
基础层：永久生效
安全权限：默认
完全访问权限：显式开启
```

## 1. Codex-managed 基础层

基础层不因权限模式改变，但只约束 Codex-managed execution；它不会自动约束 local stdio MCP server。

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

完全访问权限仍加载这些规则。packaged command MCP 不经过该 execpolicy，因此不能把这些 deny 描述成通用命令路径的保证。

### CWapi 自身强制

以下不是“命令权限”，继续由 CWapi 自己保证：

- Slack/Git/API secret 不进入普通日志、MCP body、resource；
- duplicate request 不重复产生副作用；
- terminal response 与 delivery 分离；
- ambiguous side-effect call 不自动 replay；
- CWapi 只结束自己拥有的 app-server process tree；
- caller 不能注入 Codex `threadId`。
- command MCP 子进程环境不包含 CWapi/Slack/Codex secret；

这些检查基于 transport/lifecycle/state，不解析 Slack 中的命令内容。

## 2. 安全权限

默认 `permission_mode = safe`。

CWapi 选择 Codex profile：

```text
cwapi-safe
```

实现：

- profile `extends = ":workspace"`；
- 已配置项目目录加入 workspace roots；
- CWapi data root 加入 workspace roots；
- thread/start 直接传 `permissions = "cwapi-safe"`。

因此安全模式的本地 filesystem 写边界由 Codex managed permission profile 执行，而不是 CWapi 在 Slack 入站处猜测命令。

## 3. 完全访问权限

用户显式切换 `permission_mode = full_access` 后，CWapi 选择：

```text
cwapi-full-access
```

它使用 managed filesystem profile 扩大访问范围，同时：

- 保留系统目录 deny；
- 保留基础 execpolicy；
- 保留 CWapi secret/idempotency/ownership 规则；
- **不使用 `:danger-full-access`**，因为该 built-in profile 会关闭外层 filesystem sandbox。

## 4. 为什么不在 Slack 拦截命令

Slack 是 transport，不是本地权限引擎。把权限做在 Slack parser 会产生两个问题：

1. 换一个入口就可能绕过；
2. CWapi 必须理解每一种 Tool/command 的内部语义，等于重造 Codex safety layer。

当前设计让权限贴在 Codex context 上，与真正执行位置绑定。

## 5. MCP server 边界

这里必须保持精确：Codex thread profile 不会神奇地改变一个任意第三方本地进程的权限。

对于 local stdio MCP server，只有满足以下至少一项时才能把它视为受 Codex 权限体系约束：

- server 在 Codex-managed environment/executor 中运行；
- server 明确支持 Codex sandbox-state metadata；
- server 使用 Codex permission elicitation 并正确执行结果；
- 或 server 自身有经过验证的等价 sandbox。

否则该 server 只能被视为独立受信任程序，不能靠 Slack filter 来补锅。

## 6. 通用 command/process 边界

用户已明确允许 Web GPT 通过 packaged `cwapi` MCP server 提交自由 executable `command + argv`。PowerShell 或 cmd 通过将其 executable 与参数显式传入使用，不存在语言、安装器或命令内容 allowlist。

绝对 executable 路径可以位于 workspace 外；CWD 相对路径从 prepared workspace 或指定子目录解析。直接路径只做存在性/regular-file 校验，不形成文件系统 sandbox。native executable 不经过 shell；`.cmd/.bat` 文件本身属于 Windows command script，使用 `cmd.exe` 并遵循其参数语义。

仍然强制：

- 请求绑定已配置 `project_id + expected_commit`；
- 初始工作目录必须是 detached exact-commit workspace 或其真实子目录；
- duplicate request 不重复启动命令；
- stdout/stderr 持续 drain、脱敏并有界返回；
- stop/shutdown 只结束该 server 启动并记录的 process tree；
- CWapi credential 不进入 app-server/MCP/command 子进程环境。

不提供的保证：

- command 被限制在 workspace 文件系统内；
- `cwapi-safe` / `cwapi-full-access` profile 能限制该子进程；
- 命令只安装到 CWapi-data、只使用非管理员权限或不修改系统；
- CWapi 理解、审核或复现 Web GPT 选择的版本和安装方式。

因此该能力等价于当前 Windows 用户权限下的 trusted remote command execution。caller 不得在 command/argv 中携带 token、password 或其它 secret；需要认证的安装器应使用本机已配置的凭据机制。

## 7. 验收

至少验证：

- safe thread 使用 `cwapi-safe`；
- full thread 使用 `cwapi-full-access`；
- 不出现 `:danger-full-access` 默认选择；
- project/permission 变化导致 context 重建；
- Codex 能加载两个 profile 和 base rules；
- caller-supplied threadId 被拒绝；
- duplicate/secret/owned-process 规则不回归。
- command/argv 经 stock Codex MCP relay 到达 packaged server；
- command 的初始 CWD 等于 prepared exact-commit workspace；
- short command 可返回 terminal exit/log，long command 可 status/stop；
- command 子进程看不到 CWapi/Slack/Codex secret 环境变量。
