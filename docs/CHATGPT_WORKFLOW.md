# CWapi v1.6.3 Web GPT Workflow

CWapi 的远程调用方通过配置的 Slack channel 发送 MCP v2 frame。CWapi 不使用 project registry；每个 repository request 自带 GitHub URL 与 exact commit。

## 0. MCP v2 frame

消息必须完整包含 opening/closing frame：

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2",...}
+++
```

第一行不是 `+++` 时，当前 `DecodeProtocol` 不会把文本识别为完整 MCP v2 frame。Windows path 在 MCP JSON 中优先使用 `/`；若必须使用 `\`，必须按 JSON 规则转义。

## 1. 标准流程

1. 调用方取得目标 GitHub repository URL 与完整 40 位 commit。
2. 在 Slack 发送唯一 `request_id` 的 MCP v2 frame。
3. CWapi 在 claim 前校验 protocol、route、scope、Token 位置与参数 shape。
4. repository 调用按需配置 GitHub credential helper，获取 repository lease，准备共享 mirror 与 repository-owned process-lifetime workspace。
5. workspace tracked source 被同步到 `expected_commit`；ignored/untracked derived state 不主动清除。
6. stock MCP 调用进入 request-scoped Codex context；`process_start/status/stop` 由 Go Core 直接处理。
7. CWapi 保存 terminal response，再投递到原 Slack thread。
8. repository request terminal 后只释放 repository lease，workspace 保留到 CWapi shutdown；相同 `request_id` 重投相同 fingerprint 返回已保存 response。

## 2. Process success

```text
process_start (new request id, repo+commit)
  -> acquire repository lease
  -> sync tracked source to exact commit
  -> Codex safe execution
  -> completed/running process record
  -> process_status / process_stop (new global request ids)
  -> terminal -> release repository lease
```

start 在 700ms 内完成则直接返回 terminal record，否则返回 stable `process_id`。长进程后续用新的 global `process_status` request_id 刷新；stop 最多同步等待 4 秒，owned cleanup 即使超时也继续。

同一 repository 在前一个 lease 未释放前，后续 repository task 会等待；不同 repository 可以独立并行。

### 任务拆分与往返

默认把工作拆成**可独立验证、失败可直接定位的小步骤**。不要仅为了减少 Slack 往返次数，把本来独立的 build、test、copy、upload、commit 等动作强行塞进一个大型脚本或一次 `process_start`。

v1.6.3 的 persistent workspace 会在同一 CWapi 进程内保留同 repository 的有效衍生物，因此拆成多个 repository request 不会自动丢失 `target/`、`node_modules/`、`.venv/`、`build/`、`dist/` 等状态。

只有步骤确实要求共享同一进程内存、临时环境、不可重建的 session/page 状态或原子事务时，才优先合并执行。**可诊断性、正确性与简单性优先于减少往返次数。**

### Web GPT 必须理解的 workspace 模型

- **把 request 当作执行步骤，不要把 request 当作 workspace 生命周期。** 同一 repository 的后续 request 会重新进入同一个 process-lifetime workspace。
- tracked source 是 `expected_commit` 的本地投影；每个 repository request prepare 都可能重新同步 tracked 文件。不要依赖未提交的 tracked source 修改跨 request 保留；需要持久化的源码变更应先写入 GitHub，并在后续 request 使用新的 exact commit。
- ignored/untracked derived state 才是主要的跨 request 本地复用对象，例如编译缓存、依赖目录、生成文件、迁移临时目录和其它明确的辅助状态。
- 源码迁移、打包整理等任务可以拆成“准备目标 -> 选择/复制 -> 检查 -> commit -> push”等多个短 request。中间步骤失败时优先从现有 workspace 状态继续，不要无理由从头重做整个链路。
- 禁止的是为了绕过当前 repository 的 managed workspace 而重复 clone 同一 repository。跨 repository 迁移确实需要操作第二个 repository 时，可以在受控 workspace 内使用明确的临时 helper clone，或将目标 repository 作为独立 repository request 处理。

### 仓库源码搜索

需要在仓库中定位函数、类型、变量、错误文本、配置键或其它源码位置时，优先由 Web GPT 根据当前问题生成**只读搜索命令或短脚本**，通过 repository-scoped `process_start` 在该 repository 的 persistent workspace 中执行。

搜索结果只用于定位，优先返回：

```text
path
line
匹配文本
少量上下文
```

拿到命中路径后，需要完整上下文时再通过 GitHub Connector 精确读取相关文件。不要通过搜索脚本大量打印完整源码。

搜索脚本遵守以下规则：

- 只读，不修改 tracked source，也不混入 build、test、install、format、commit、cleanup 等其它动作；
- 能限定 `internal/`、`src/`、`tests/`、文件类型或其它范围时优先限定范围；
- 默认排除 `target/`、`node_modules/`、`.venv/`、`build/`、`dist/`、缓存等 derived state，除非任务明确需要调查这些内容；
- 同一问题需要多个相关关键词时，可以在一次短脚本中批量搜索并分段输出，减少 Slack 往返；
- 优先使用当前 CWapi 运行环境已经验证可用的搜索方式，例如 `rg`、PowerShell `Select-String`、`findstr` 或其它只读命令；
- 已经进入对应 `expected_commit` 的 workspace 后，搜索脚本不要再次 clone/fetch 同一 repository，也不要为了搜索访问 GitHub。

推荐的代码理解流程：

```text
1. GitHub：取得 repository URL + exact commit
2. CWapi：执行 Web GPT 生成的只读搜索脚本，定位相关 path / line / snippet
3. GitHub：精确读取真正需要的少量文件
4. Web GPT：分析并决定修改
5. GitHub：提交修改并取得新的 exact commit
6. CWapi：在新 commit 上执行 build / test / run
```

## 3. 可执行文件与运行环境解析

`process_start` 使用 CWapi 启动时冻结的执行环境。不要假定用户交互式终端中的 PATH 与 CWapi 看到的 PATH 相同，也不要把某台机器上的固定绝对路径当成通用规则。

需要 Node、Python、Git、浏览器驱动、编译器或其它外部工具时：

1. **先找 CWapi 管理的环境。** 优先检查当前 portable/runtime、受控 tools、package manifest 和 `mcpServerStatus/list`。
2. **再找 CWapi 当前进程真实可见的本机环境。** 使用 verified `process_start`、`where` / `Get-Command` 或必要的受限目录探测。
3. **两层都没有时才判断为未验证/缺失。** 不要无限猜 PATH。
4. **安装只在用户明确选择 FULL 后进行。** 安装完成后重新探测实际路径与版本；不要假定新安装程序已经修改 CWapi 启动时冻结的 PATH。

补充规则：

- `process invocation could not be resolved` 首先视为 executable resolution failure；
- portable 自带 runtime 优先使用 portable 内绝对路径；
- repository 内脚本优先把仓库相对路径直接放进 `command`；
- 不要为了找到脚本先改 `cwd` 再只传 basename；
- Windows path 在 MCP JSON 中统一优先使用 `/`。

## 4. Persistent workspace 与衍生资源复用

v1.6.3 的关键变化是：**同一 repository 在一个 CWapi 进程生命周期内只拥有一个 persistent workspace。** request terminal 不再删除该 workspace。

### 可以复用

- 同一 repository workspace 内的 `target/`、`node_modules/`、`.venv/`、`build/`、`dist/`、编译器增量缓存与生成文件；
- 同一长期进程仍然有效的 server/watcher 状态；
- 同一 repository 的 shared bare mirror；
- Codex denial 后同一 invocation 的 System fallback 所使用的原 workspace；
- 工具自身经验证仍与当前输入匹配的缓存。

### exact commit 切换

下一条同 repository request 可以携带不同 `expected_commit`。CWapi 在获得 repository lease 后：

- 若 mirror 已有 commit，不重复 fetch；否则 fetch/prune；
- 强制同步 tracked source 到 exact commit；
- 校验 HEAD 精确匹配；
- 不主动执行 `git clean`，因此 ignored/untracked derived state 继续存在。

如果旧衍生物与新的 tracked source 发生路径冲突或导致 Git checkout/reset 无法安全完成，workspace prepare 应失败并返回稳定错误；CWapi 不偷偷删除衍生物来“帮忙修好”。Web GPT 根据日志决定运行项目自己的清理命令。

### 什么时候必须重建/清理

- dependency lockfile、build config、编译参数、目标平台、工具链版本改变；
- 项目测试或编译明确证明衍生物损坏/不兼容；
- tracked/untracked path conflict 阻止 exact commit 同步；
- 无法证明缓存与当前输入一致。

清理使用项目自己的命令，例如 `cargo clean`、删除特定 build/cache 目录、重建 `.venv` 或重新 `npm ci`。v1.6.3 不新增 `workspace_clean` 协议。

## 5. Permission fallback

```text
full_access local mode
  -> Codex first
  -> structured PERMISSION_DENIED
  -> blocked response + 60s System Token
  -> caller sends same repo/commit/process args
     with NEW request_id + top-level system_token
  -> Core re-resolves final invocation in original repository workspace
  -> binding/permanent policy pass
  -> one-time consume
  -> System backend
```

必须遵守：

- 只有 `blocked + PERMISSION_DENIED` 才进入 System fallback；
- 普通 `PROGRAM_FAILURE` 不签发 Token；
- Token 最多 3 个 active、每个 60 秒、一次性；
- binding mismatch 不消费 Token；
- Token retry 必须保持 repository、commit、command、argv、cwd 一致，只换新的 request_id；
- Token 过期后重新从 Codex attempt 开始，不伪造/复用过期 Token；
- permanent policy 对 Codex/System 都不可绕过；
- System backend failure 不递归签发第二枚 Token。

项目 Gate 若在顶层程序内部检测到明确的嵌套 Windows sandbox denial，可以把该已知 denial 映射成 CWapi 认可的 cooperative blocked exit code；普通单元测试失败不得这样处理。

## 6. Process status

`process_start` 返回 `state=running` 时：

- 保存 `process_id`；
- 每次 `process_status` 使用新的 global request_id；
- 不重复发送原 `process_start`；
- terminal state 为 `completed`、`failed` 或 `stopped`；
- repository lease 由 process runtime 持有到真正 terminal，再释放。

## 7. Stock MCP

- `mcpServerStatus/list` 是 global 且 params 为空；
- resource/tool 可 global 或 repository；
- caller 不提供 `threadId`，CWapi 管理 request-scoped context；
- `server=cwapi` 只支持 `process_start/status/stop`，不进入 stock relay；
- 不要假定两个不同 stock MCP request 共享浏览器 page/tab/locator/session state。

### Playwright 多步操作

需要完成“导航 -> 填表 -> 点击 -> 断言 -> 截图”时，优先在一次 Playwright 调用内完成连续动作。若拆成多个 request，后续 request 必须自行重建页面状态。

### 截图经 Slack 返回

调用 `browser_take_screenshot` 时不要提供 `filename`，让 MCP result 返回真正的 image content。CWapi 会把 bytes 外置为 Slack File。普通文本中的 `./image.png` 不会触发 CWapi 主动读取本地文件。

## 8. Workspace operational error

repository prepare/release/startup sweep/shutdown cleanup/prune 的网络、认证、Git 或 filesystem failure：

- MCP/Slack terminal response 保持稳定 error code/category；
- structured execution 继续保存 request terminal truth；
- 同时进入现有 runtime error logger，供 GUI 最新错误候选显示；
- 必须经过已有 secret redaction，不能复制 credential/System Token/敏感 env。

## 9. 阻塞与等待规则

- 单次连续等待/轮询外部编译、Runner、Slack response 或其它异步结果累计不超过 **3 分钟**；
- 达到 3 分钟仍无 terminal result 时立即停止继续等待，向用户报告“任务仍在运行”与当前 process/request 状态；
- 不得通过多次 30 秒轮询把累计等待无限延长；
- 已取得 stable `process_id` 时只用新的 global `process_status` request_id 查询，不重启任务；
- 环境缺失不是等待条件。

## 10. 调用方责任

- 每个新动作/状态快照使用新的 request_id；
- frame 第一行必须是 `+++`；
- Windows path 在 JSON 中优先使用 `/`；
- 代码理解需要仓库搜索时，优先由 Web GPT 生成只读搜索脚本，通过 repository-scoped `process_start` 在 persistent workspace 内执行；
- 搜索输出保持简短，以 path / line / 小 snippet 为主；完整源码文件按需通过 GitHub Connector 精确读取；
- 搜索脚本不得为了定位源码再次 clone/fetch 同一 repository，也不得混入无关写入/build/test 操作；
- 同一 repository 优先复用 persistent workspace 的有效衍生物，不无理由重复 install/build；
- 不为绕过当前 repository 的 managed workspace 而重复 clone/fetch 同一 repository；跨 repository 任务按上面的 workspace 模型处理；
- 只在 configured channel 传递短期 Token；
- 不把 Token 复制到 issue、日志或长期文档；
- FULL fallback 前确认错误确实属于 sandbox permission denial，而不是普通程序失败；
- 需要二进制 MCP 结果时让 tool 返回 bytes/image/resource content，不要求 CWapi 根据文本路径自行读取本机文件。