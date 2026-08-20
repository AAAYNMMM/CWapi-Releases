# CWapi v1.6.0 运行与维护

这份文档描述日常运行、安装目录、项目、环境、进程、日志、恢复、迁移和卸载。第一次安装先看 [`USER_GUIDE.md`](USER_GUIDE.md)。

## 1. 便携目录

正式发行包完整解压，例如 `D:/Tools/CWapi/`：

```text
CWapi/
├─ CWapi.exe
├─ portable-manifest.json
├─ runtime/{codex,git,node,mcp,browser}/
└─ CWapi-data/   # 首次运行后生成
```

不要只复制 `CWapi.exe`。CWapi 不依赖启动时 current working directory，随包 runtime 从 executable 所在目录解析。

## 2. 用户数据与凭据

普通数据写入 `<安装目录>/CWapi-data/`。Slack App Token / Bot Token 写入当前 Windows 用户 Credential Manager，不应进入普通 config、SQLite 普通字段、Git、argv、MCP body、artifact 或日志。发行 ZIP 也不应携带真实用户 token、数据库、项目配置、浏览器 profile 或运行日志。

## 3. Startup

正常启动：加载 config → 建立新运行会话 → 加载 Credential Manager → 初始化 Slack → 验证 packaged stock Codex → 启动 Wails UI。app-server 按需启动；readiness / GUI 状态查询不制造模型 Turn。

每次应用启动都是新运行会话，不自动读取并执行启动前 Slack history，也不恢复上一进程未完成 request。

## 4. 第一次 MCP 调用

CWapi 读取 permission mode / projects，生成或刷新自己的 `CODEX_HOME`，启动 `codex app-server --stdio`，initialize API，并为请求建立 ephemeral MCP context。项目请求再准备 exact-commit worktree，`thread/start` 传入正确 `cwd + permissions`，然后转发 MCP。

正常 relay 不调用 `turn/start`。

## 5. 项目生命周期

项目维护 display name、repository、local path、remote URL 和 CWapi 自己的 `project_id`。Web GPT 通过 discovery 获取 ID，不应猜。

项目 request 必须带 `project_id + expected_commit`：

```text
configured project → Git mirror fetch → verify commit
→ detached worktree → MCP call → artifact handling → release worktree
```

项目 / permission fingerprint 变化后，后续调用建立新的必要 context，通常不要求手工重启应用。

## 6. Context reuse

长期 app-server 连接在配置允许时复用。没有 project 的 status context 可复用；exact-worktree context 按调用释放。fingerprint 变化时等待现有调用结束，再切换旧 client/context，避免设置变化后旧 thread 继续工作的 split-brain。

## 7. Slack 运行状态

运行中主要看：Slack connected/disconnected、目标 Workspace / Channel、request 是否收到、response 是否投递、Slack File external upload 是否成功、最近 transport error。

Slack 是 transport，不是 filesystem / command 权限引擎。

## 8. 环境管理

目标项目自己的 Python、Java/JDK、Node、Go、Rust、SDK 等环境由 Web GPT / 用户发现、安装、选择和管理。CWapi 负责 exact-commit context、process lifecycle、日志与交付。

常用发现：`where.exe`、`Get-Command`、`py.exe -0p`、`<tool> --version`。长期复用环境可放明确持久目录，例如 `C:/Users/name/.venvs/project/`、`D:/SDK/jdk-25/`，再通过 absolute executable 调用。

不要假定前一次 exact-commit worktree 中临时生成的环境会跨请求保留。

## 9. command path forms

`process_start.command` 支持：PATH executable、absolute executable、working-directory-relative executable，例如：

```text
python.exe
C:/Program Files/Git/cmd/git.exe
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
.venv/Scripts/python.exe
tools/build.cmd
```

正式 MCP JSON 中 Windows 路径统一使用 `/`，不要给 executable 字符串额外套引号。

## 10. Process lifecycle

随包 `cwapi` server 提供 `process_start/status/stop`。短任务可直接 `completed + exit_code`；长期 server 常返回 `running + process_id`。后续查同一个 `process_id`，测试完成后再 stop。

主动 stop 后 exit code 不一定为 0；判断停止优先看 `state=stopped` 和真实端口 / 服务状态。

## 11. 本地 Web 服务

典型：

```text
process_start server → stdout ready
→ Playwright localhost → test / DOM verify / screenshot
→ process_stop
```

启动后 `ERR_CONNECTION_REFUSED`：检查 process 是否 running、stdout 是否 ready、stderr、端口、绑定地址。stop 后再次访问出现该错误通常说明服务已真正关闭。

## 12. Slack File delivery

```text
MCP 已返回内容 → outbound policy → Slack external upload
→ file reference → compact MCP_RESPONSE
```

当前单 artifact 最大 8 MiB、单 response 最多 16 个。CWapi 不根据 result 中的 path/URI 自行读取本地文件，因此 filesystem read 和 Slack upload 是两层权限。

## 13. Logs

主要保留 structured MCP execution/delivery log 与 CWapi runtime log。原则：有界、不默认上传整份大日志、按 request/process 读取必要范围、stdout/stderr 返回有界 tail、secret 不进入普通日志。

排障优先记录：source commit、request ID、project ID、expected commit、method/server/tool、status/error code、process ID、必要 stdout/stderr tail。

## 14. Recovery

- app-server 退出：后续调用重建。
- 同一进程 Slack Socket 断线：从 durable cursor 后继续，当前会话 terminal response 可保留。
- ambiguous side-effect tool failure：不自动 replay，先调查真实状态。
- duplicate same request：当前会话不执行第二次。
- CWapi 应用重启：新会话，不拾取上一进程未完成 request，不重放启动前 Slack history。

## 15. Shutdown

正常退出：停止 Slack、释放 Codex process scope、结束 CWapi 自己拥有的 app-server process tree、关闭 state store。packaged process server 只管理自己启动并记录的进程树。

不要通过模糊进程名全局 kill 用户自己启动的 Python/Node/Codex 等无关进程。

## 16. 移动便携目录

结束任务 → stop 不需要继续的 owned process → 退出 CWapi → 移动**整个目录** → 重新启动 → 诊断页确认 runtime、Slack、Codex、MCP。不要只移动 executable。

## 17. 更新发行版

关闭旧版本并下载新的正式发行包，阅读新 README / CHANGELOG / 迁移说明，使用新版本自己的 runtime。不要把新旧 `runtime/` 目录随意混合，也不要默认所有版本都支持“直接覆盖 ZIP”。

## 18. 卸载

退出 CWapi，确认不再需要 `CWapi-data` 中的项目配置 / 日志 / 状态后删除便携目录；如果需要彻底清理，再从 Windows Credential Manager 删除 CWapi 保存的 Slack 凭据。不要误删独立存放的项目仓库或外部 SDK / 环境。

## 19. Security operations

Codex-managed 权限问题检查：当前 `permission_mode`、生成的 `CODEX_HOME/config.toml`、`rules/default.rules`、`thread/start` profile、目标 MCP server 是否真的运行于该 sandbox 边界。

packaged command/process MCP 是单独 trusted remote execution boundary，不要误套 Codex thread sandbox 假设。详细见 [`SECURITY.md`](SECURITY.md)。

## 20. 诊断页顺序

优先看：CWapi version/source commit → Slack state → Codex executable/version/SHA → app-server/MCP readiness → permission mode → active/terminal request → 最近 error。然后按错误去 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md) 查处理方法。