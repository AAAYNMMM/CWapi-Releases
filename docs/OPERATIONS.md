# CWapi v1.6.0 运行与维护

这份文档描述 CWapi 的日常运行、安装目录、项目、环境、进程、日志、恢复与迁移。普通用户第一次安装请先看 [`USER_GUIDE.md`](USER_GUIDE.md)。

---

## 1. 便携安装目录

正式发行包应完整解压，例如：

```text
D:/Tools/CWapi/
```

目录大致为：

```text
CWapi/
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

不要只复制 `CWapi.exe`。发行 runtime 与 executable 必须作为完整便携目录一起移动。

CWapi 不依赖启动时的 current working directory，runtime 路径从 executable 所在目录解析。

---

## 2. 用户数据与凭据

运行后普通用户数据写入：

```text
<安装目录>/CWapi-data/
```

Slack App Token / Bot Token 存储在当前 Windows 用户的 Credential Manager。

它们不应进入：

- 普通配置文件；
- SQLite 普通字段；
- Git；
- MCP body；
- command / argv；
- artifact；
- runtime log。

发行 ZIP 本身不应携带任何真实用户 token、项目数据、数据库、日志或浏览器 profile。

---

## 3. Startup

正常启动流程：

1. 加载 CWapi config；
2. 建立新的运行会话并清理上一进程的 request/log/error 会话 state；
3. 加载 Windows Credential Manager 中的凭据；
4. 初始化 Slack transport；
5. 验证 packaged stock Codex executable；
6. 启动 Wails UI；
7. app-server 保持按需启动。

readiness / GUI 状态查询不需要制造 Codex model Turn。

---

## 4. First MCP call

第一次真正需要 MCP 能力时：

1. 读取当前 permission mode 和项目配置；
2. 生成或刷新 CWapi 自己的 `CODEX_HOME`；
3. 启动 `codex app-server --stdio`；
4. initialize stock / experimental app-server API；
5. 建立带权限的 ephemeral MCP context thread；
6. 项目请求准备 exact-commit worktree；
7. `thread/start` 使用正确 `cwd + permissions`；
8. 转发 stock MCP request。

正常 relay 不调用 `turn/start`，也不启动模型 Turn。

---

## 5. Project lifecycle

项目页维护：

```text
display_name
repository
local_path
remote_url
project_id
```

`project_id` 由 CWapi 管理，不应由 Web GPT 猜测。

项目变化后通常不需要手工重启 CWapi。后续 request 会根据新的 project / permission fingerprint 建立所需 context。

### exact-commit execution

项目请求必须带：

```text
project_id + expected_commit
```

CWapi 执行：

```text
configured project
 -> Git mirror fetch
 -> verify exact commit
 -> detached worktree
 -> MCP call
 -> artifact handling
 -> release worktree
```

这套 worktree 是执行上下文，不是 Web GPT 的长期环境目录。

---

## 6. Context reuse

permission profile + project roots fingerprint 未变化时，CWapi 尽量复用长期 app-server 连接。

没有 project 的 status context 可以复用；exact-worktree context 按调用生命周期释放。

当 permission / project fingerprint 变化时，CWapi 等待现有调用结束，再关闭旧 client/context 并建立新的必要 context，避免设置已变化但旧 thread 仍继续工作的 split-brain。

---

## 7. Slack 运行状态

Slack transport 使用：

```text
App Token
Bot Token
Channel ID
```

运行中重点观察：

- Slack connected / disconnected；
- 当前 workspace / channel 是否正确；
- 是否收到新的 MCP request；
- response 是否成功投递；
- Slack File 是否完成 external upload；
- 最近 transport error。

Slack 只是 transport，不承担本地 filesystem / command 权限判断。

---

## 8. 项目环境管理

v1.6.0 不固定管理目标项目自己的 Python / Java / JDK / Go / Rust / SDK。

Web GPT 或用户负责：

```text
发现环境
安装环境
选择版本
选择安装位置
确认 executable
```

CWapi 负责：

```text
exact-commit context
process_start/status/stop
日志与状态
结果交付
```

### 环境发现

常用：

```text
where.exe
Get-Command
<tool> --version
py.exe -0p
```

### 环境位置

适合长期复用的环境可以放在明确持久目录，例如：

```text
C:/Users/name/.venvs/project/
D:/DevTools/Python312/
D:/SDK/jdk-25/
```

然后通过 absolute executable 调用。

不要假定 exact-commit 临时 worktree 中前一个请求生成的环境会跨请求保留。

---

## 9. command path forms

`process_start.command` 当前支持：

```text
PATH executable name
absolute executable path
working-directory-relative executable path
```

例子：

```text
python.exe
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
C:/Program Files/Git/cmd/git.exe
tools/build.cmd
.venv/Scripts/python.exe
node_modules/.bin/tool.cmd
```

Windows 路径进入 MCP JSON 前统一使用 `/`。

不要把 executable 本身再包一层引号。

---

## 10. Process lifecycle

随包 `cwapi` MCP server：

```text
process_start
process_status
process_stop
```

### 短任务

例如版本查询或快速编译可能直接返回：

```text
state = completed
exit_code = 0
```

### 长任务

例如本地 server：

```text
state = running
process_id = proc-...
```

后续只使用同一个 process ID：

```text
process_status(proc-...)
```

测试完成：

```text
process_stop(proc-...)
```

### stop 的 exit code

主动终止长期进程后 exit code 不一定是 0。

判断 stop 是否成功优先看：

```text
state = stopped
```

并结合端口 / 服务实际状态。

---

## 11. 本地 Web 服务运维

典型：

```text
process_start server
 -> stdout ready marker
 -> Playwright localhost
 -> test
 -> screenshot / DOM result
 -> process_stop
```

如果启动后出现：

```text
ERR_CONNECTION_REFUSED
```

先查：

- process 是否仍 running；
- stdout 是否出现 ready；
- stderr 是否有崩溃；
- 端口是否正确；
- server 是否绑定 localhost / 127.0.0.1。

如果是在 stop 后重新访问出现 `ERR_CONNECTION_REFUSED`，通常正好说明服务已停止。

---

## 12. Slack File delivery

外发链：

```text
MCP 已返回内容
 -> CWapi outbound policy
 -> Slack external file upload
 -> file reference
 -> compact MCP_RESPONSE
```

当前限制：

```text
单个 artifact 最大 8 MiB
单次 response 最多 16 个 artifact
```

CWapi 不会因为 result 里出现本地 path / URI 就自行读取该路径。

这意味着：

```text
filesystem read permission
```

和：

```text
Slack upload permission
```

是两层独立边界。

---

## 13. Logs

主要保留：

- structured MCP execution / delivery log；
- CWapi runtime log。

原则：

- 日志有界；
- 默认不把整份大日志发 Slack；
- 排障按 request / process 选择必要范围；
- stdout / stderr 只返回有界 tail；
- secret 应经过 redaction / environment isolation，不应进入普通日志。

排障优先记录：

```text
source_commit
request_id
project_id
expected_commit
method
server/tool
status
error.code
process_id
必要的 stdout_tail / stderr_tail
```

---

## 14. Recovery

### app-server 退出

后续调用重新建立 app-server / context。

### Slack Socket 暂时断线

同一个 CWapi 进程内保留当前会话 terminal response，并从 durable cursor 后继续接收新消息。

### ambiguous tool failure

如果一个有副作用的 tool 无法确认到底执行没执行，**不自动 replay**。

先调查状态，再决定下一步。

### duplicate request

当前运行会话内同 request ID + 同 fingerprint 不启动第二次。

### CWapi 应用重启

重启建立新的运行会话：

- 不拾取上一进程未完成 request；
- 不重放启动前 Slack history；
- 不把旧的 ambiguous side effect 再执行一次。

---

## 15. Shutdown

正常退出：

1. 停止 Slack；
2. 释放 Codex process scope；
3. 只结束 CWapi 拥有的 app-server process tree；
4. 关闭 state store；
5. 对仍由 packaged process server 管理的进程按其 lifecycle 处理。

用户自己在系统里启动的其它进程不属于 CWapi owned process，CWapi 不应把“关软件”变成“扫荡全系统进程”。

---

## 16. 移动便携目录

移动步骤：

1. 结束当前开发任务；
2. 停止不需要继续运行的 owned process；
3. 正常退出 CWapi；
4. 移动整个目录；
5. 重新启动；
6. 打开诊断页确认 runtime 路径、Slack、Codex、MCP 状态。

CWapi 下一次启动从新的 executable location 重新解析：

```text
runtime/codex
runtime/git
runtime/node
runtime/mcp
runtime/browser
```

---

## 17. 更新发行版

建议流程：

1. 正常结束旧版本当前工作；
2. 记录需要保留的用户配置；
3. 关闭旧版本；
4. 下载新正式发行包；
5. 阅读新版本 README / CHANGELOG / 迁移说明；
6. 使用新版本自己的 runtime；
7. 不把新旧 `runtime/` 目录随意混合；
8. 用户数据迁移遵循对应新版本文档。

不要假设所有版本都可以靠直接覆盖 ZIP 实现升级。

---

## 18. 卸载

CWapi 是便携式应用。

完整卸载通常包括：

1. 退出 CWapi；
2. 确认不再需要其中的项目配置、日志和状态；
3. 删除安装目录；
4. 如需彻底清理，再从 Windows Credential Manager 删除 CWapi 保存的 Slack 凭据。

删除目录前不要误删你自己另行存放的项目仓库或外部环境。

---

## 19. Security operations

排查 Codex-managed 权限问题时看：

1. 当前 `permission_mode`；
2. 生成的 `CODEX_HOME/config.toml`；
3. `rules/default.rules`；
4. `thread/start` 是否选择正确 profile；
5. 目标 MCP server 是否真正运行于该 sandbox 边界。

排查 packaged command MCP 时不要误套 Codex thread sandbox 假设。

自由 executable 的真实边界见 [`SECURITY.md`](SECURITY.md)。

---

## 20. 诊断页建议查看顺序

出现问题时优先：

```text
CWapi version / source commit
Slack state
Codex executable / version / SHA
app-server readiness
MCP catalog readiness
permission mode
active / terminal request
最近 error
```

再根据错误去 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md) 查对应处理方法。

不要从一条 Slack 文本猜整套权限和 runtime 状态。