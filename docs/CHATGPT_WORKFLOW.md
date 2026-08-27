# CWapi v1.6.3 Web GPT Workflow

CWapi 的远程调用方通过配置的 Slack channel 发送 MCP v2 frame。CWapi 不使用 project registry；每个 repository request 自带 GitHub URL 与 exact commit。

## 标准流程

1. 调用方取得目标 GitHub repository URL 与完整 40 位 commit。
2. 在 Slack 发送唯一 `request_id` 的 `[CWapi/MCP/2][MCP_REQUEST]`。
3. CWapi 在 claim 前校验 protocol、route、scope、Token 位置与参数 shape。
4. repository 调用按需配置 GitHub credential helper，获取 repository lease，准备共享 mirror 与 repository-owned process-lifetime workspace。
5. workspace tracked source 同步到 `expected_commit`；ignored/untracked derived state 不主动清除。
6. stock MCP 调用进入 request-scoped Codex context；`process_start/status/stop` 由 Go Core 直接处理。
7. CWapi 保存 terminal response，再投递到原 Slack thread；repository terminal 只释放 lease，workspace 保留到 CWapi shutdown。
8. 同 id 重投相同 fingerprint 会返回已保存 response；刷新 process 状态使用新 id。

## Process success

```text
process_start (new request id, repo+commit)
  -> acquire repository lease
  -> sync tracked source to exact commit
  -> Codex safe execution
  -> completed/running process record
  -> process_status / process_stop (new global request ids)
  -> terminal -> release repository lease
```

start 在 700ms 内完成则直接返回 terminal record，否则返回 stable process_id。长进程后续用 status 刷新；stop 最多同步等待 4 秒，owned cleanup 即使超时也继续。

同一 repository 在前一个 lease 未释放前，后续 repository task 会等待；不同 repository 可以独立并行。

### 任务拆分原则

默认把工作拆成**可独立验证、失败可直接定位的小步骤**。不要仅为了减少 Slack 往返次数，把本来独立的 build、test、copy、upload、commit 等动作强行塞进一个大型脚本或一次 `process_start`。

v1.6.3 的 persistent workspace 会在同一 CWapi 进程内保留同 repository 的有效衍生物，因此拆成多个 repository request 不会自动丢失 `target/`、`node_modules/`、`.venv/`、`build/`、`dist/` 等状态。

只有步骤确实要求共享同一进程内存、临时环境、不可重建的 session/page 状态或原子事务时，才优先合并执行。**可诊断性、正确性与简单性优先于减少往返次数。**

### Web GPT 必须理解的 workspace 模型

- **把 request 当作执行步骤，不要把 request 当作 workspace 生命周期。** 同一 repository 的后续 request 会重新进入同一个 process-lifetime workspace。
- tracked source 是 `expected_commit` 的本地投影；每个 repository request prepare 都可能重新同步 tracked 文件。不要依赖未提交的 tracked source 修改跨 request 保留；需要持久化的源码变更应先写入 GitHub，并在后续 request 使用新的 exact commit。
- ignored/untracked derived state 才是主要的跨 request 本地复用对象，例如编译缓存、依赖目录、生成文件、迁移临时目录和其它明确的辅助状态。
- 源码迁移、打包整理等任务可以拆成“准备目标 -> 选择/复制 -> 检查 -> commit -> push”等多个短 request。中间步骤失败时优先从现有 workspace 状态继续，不要无理由从头重做整个链路。
- 禁止的是为了绕过当前 repository 的 managed workspace 而重复 clone 同一 repository。跨 repository 迁移确实需要操作第二个 repository 时，可以在受控 workspace 内使用明确的临时 helper clone，或将目标 repository 作为独立 repository request 处理。

### Repository search optimization：当前事实与确定设计

这里必须区分**v1.6.3 已发布能力**和**后续已经确定的 search-only 优化方向**。发行仓库的文档不能把尚未实现的工具写成当前可调用协议。

#### v1.6.3 当前事实

当前 v1.6.3：

- `server=cwapi` 仍只支持 `process_start`、`process_status`、`process_stop`；
- **没有** `repo_search`、`repo_read`、filesystem MCP 或其它专用 repository query tool；
- 当前 stock MCP 实际只提供 Playwright，没有文件系统读取/搜索 server；
- GitHub Connector 仍负责正常的源码搜索、完整文件读取、远端写入，以及 PR / Issue / CI / Review / Release 等 GitHub-native 信息；
- repository request 仍以 GitHub HTTPS `repository_url` + 完整 40 位 `expected_commit` 作为远端源码身份与 exact-commit 真相。

2026-08-27 做过一次真实 Slack -> CWapi 链验证：第一个 repository request 在 workspace 写入临时 untracked marker 并读取 tracked `README.md`；request terminal 后，第二个新的 request 使用同一 repository / same `expected_commit`，成功重新进入同一个 process-lifetime workspace、读到该 marker，并继续看到对应 exact commit 的 tracked source。随后 marker 已清理。

这次验证只证明 persistent workspace 可以作为本地 repository 搜索的数据源，并证明同一个已经准备好的 repo/commit 没有必要为了“定位源码在哪”再次经过 GitHub Connector。它**不代表** v1.6.3 已经发布了 repository search API。

使用 `process_start` + `findstr` / `rg` / 类似命令可以作为调试、兼容性验证或临时诊断手段，但仍经过 Codex safe backend，并受 process output tail 等 process 语义约束。因此它不应成为正式搜索热路径，也不应被扩展成 CWapi 文件读取协议。

#### 已确定的优化边界：只搜索，不读取完整源码

后续 repository query 优化只需要增加一个轻量、确定性的 **`repo_search`** 能力。CWapi 不需要 Agent，也不需要实现 `repo_read`。

目标分工：

```text
Web GPT
  -> CWapi repo_search：本地 persistent workspace 搜索定位
       -> 返回 path + line + bounded snippet
  -> GitHub Connector：精确读取命中的完整文件
  -> Web GPT：理解、推理、规划
  -> GitHub：源码写入与新的 exact commit
  -> CWapi：在 exact commit 上执行 build / test / run
```

CWapi 负责的搜索阶段包括：

- 搜索函数名、类型名、变量名、错误文本、配置键和普通字符串；
- 找出相关文件路径；
- 返回命中行号；
- 返回少量上下文 snippet，帮助 Web GPT 判断哪些文件值得进一步读取。

CWapi 不负责：

- 完整源码文件读取；
- 大文件或多文件内容传输；
- 源码修改与 commit/push；
- branch / commit history；
- PR / Issue / Review / Actions / CI / Release；
- 任何需要理解自然语言意图的 Agent 行为。

搜索结束后，Web GPT 应通过 GitHub Connector 精准读取已经命中的少量文件。这样优化的是“GitHub search -> 猜文件 -> 再 search -> 再 fetch”的反复循环，而不是把 GitHub Connector 整体搬进 CWapi。

#### `repo_search` 的目标执行路径

正式实现应当是 Go Gateway/Core 的 repository-scoped virtual tool，不使用 `process_start` 包装搜索命令：

```text
Web GPT
  -> Slack MCP v2
  -> Go Gateway
  -> repository lease
  -> Prepare(repository_url, expected_commit)
  -> existing persistent workspace
  -> deterministic local text search
  -> bounded path / line / snippet result
  -> Slack MCP v2 response
```

目标约束：

- 不创建模型 turn；
- 不需要 CWapi Agent；
- 不通过 Codex command backend 做搜索；
- 不需要 FULL / System Token；
- 不为了搜索额外 clone 同一 repository；
- exact commit 已在 mirror/workspace 可用时，不因为搜索本身访问 GitHub；
- exact commit 本地不存在时，只复用现有 repository prepare 的正常 mirror fetch 行为；
- 搜索期间继续遵守 repository lease，不能在同一 workspace 的 tracked source 正被其它任务修改或切换时读取不确定状态；
- 第一版保持普通确定性文本搜索，不需要语义索引、embedding、AST Agent 或另一个 LLM。

`repo_search` 默认只针对 `expected_commit` 对应的 **tracked source**。`target/`、`node_modules/`、`.venv/`、`build/`、`dist/` 等 ignored/untracked derived state 不属于“替代 GitHub 源码搜索”的范围，默认不应进入结果。如果任务明确需要调查本地产物，继续使用普通 repository process/tool 路径。

返回必须有界。至少返回：

```text
path
line
snippet
```

并对 `max_results`、snippet 长度和总 response 大小设置上限。snippet 只用于判断相关性，不能把上下文窗口无限放大后变相实现整文件读取。

#### Web GPT 的目标往返策略

`repo_search` 正式发布后的典型流程：

```text
1. GitHub：取得 repository URL + exact commit
2. CWapi：repo_search 定位相关文件和行
3. GitHub：精确读取 1~N 个命中文件
4. Web GPT：分析并决定修改
5. GitHub：提交修改并取得新的 exact commit
6. CWapi：在新 commit 上执行 build / test / run
```

搜索结果不足时可以再发新的 `repo_search`。不要因为 CWapi 已有 workspace 就把完整源码经 Slack 搬回来，搜索优化的收益来自**减少定位阶段的远端往返**，不是把所有 repository I/O 都转移到 Slack。

在 `repo_search` 真正实现、完成协议与 Gate、并进入正式发布包之前，**当前 v1.6.3 仍继续使用 GitHub Connector 完成源码搜索和文件读取**。这一节记录的是已经确定的下一步设计与真实可行性依据，不改变当前 v1.6.3 wire contract。

### 可执行文件与运行环境解析

`process_start` 使用 CWapi 启动时冻结的执行环境。不要假定用户交互式终端中的 PATH 与 CWapi 看到的 PATH 相同，也不要把某台机器上的固定绝对路径当成通用规则。

需要 Node、Python、Git、浏览器驱动、编译器或其它外部工具时，按下面的顺序处理：

1. **先找 CWapi 管理的环境。** 优先检查当前 CWapi portable/runtime、受控 tools、已缓存并由 CWapi 管理的运行时；存在时直接使用该受控可执行文件的绝对路径。这样可以减少用户机器差异，也避免依赖交互式 PATH。
2. **CWapi 管理目录没有，再找本机已安装环境。** 使用 CWapi 当前进程实际可见的 PATH、系统 launcher、`Get-Command` / `where` 或必要的受限目录探测找到真实可执行文件，然后记录实际版本与解释器路径。不要预设 `C:/WINDOWS/py.exe`、`python.exe`、`node.exe` 或任何具体安装目录一定存在。
3. **本机也没有，不要反复尝试同一个命令。** 如果任务确实需要该依赖，告知用户缺少什么，并让用户选择：切换 CWapi 到 `FULL` 权限模式后由 Web GPT 通过 CWapi 执行安装，或者由用户手动安装。
4. **自动安装必须建立在用户明确切换/允许 FULL 权限之后。** 安装完成后重新探测实际可执行文件与版本，再继续原任务；不要假定安装程序一定把新工具加入当前 CWapi 已冻结的 PATH，必要时使用新安装文件的绝对路径或重启 CWapi 后再验证。

补充规则：

- `process invocation could not be resolved` 首先视为 executable resolution failure，不要直接归因于脚本或仓库内容；
- portable 自带的运行时优先使用 portable 内绝对路径，例如 `<portable-root>/runtime/node/node.exe`；
- repository 内脚本优先把仓库相对路径直接放进 `command`，例如 `tools/env-probe.cmd`。不要为了找到脚本先改 `cwd` 再只传 basename；
- Windows path 在 MCP JSON 中统一使用 `/`。如果必须使用 `\`，必须正确做 JSON 转义，避免在 claim 前得到 `MCP_REQUEST_JSON_INVALID`；
- 环境探测失败后应进入“受控环境 -> 本机环境 -> FULL 安装/用户手动安装”的下一层，不得用无限 PATH 猜测造成阻塞。

## 衍生资源复用规则

v1.6.3 中，同一 repository 在一个 CWapi 进程生命周期内使用同一个 persistent workspace。**复用优先于重复构建，但正确性优先于复用。**

### 可以复用

- 同一 repository workspace 内的编译物、中间文件、生成代码、依赖目录和项目内缓存；
- 同一长期进程仍然有效的 dev server、test server、watcher 等状态；
- Codex denial 后同一 invocation 的 System fallback 所使用的原 workspace；
- CWapi 自己维护的 shared bare mirror；
- 工具自身经验证仍与当前输入匹配的缓存。

### exact commit 切换

新的同 repository request 可以携带不同 `expected_commit`。CWapi 获取 repository lease 后会强制同步 tracked source 到 exact commit，并校验 HEAD；不会主动 `git clean` ignored/untracked derived state。

如果旧衍生物与新的 tracked source 冲突，或依赖 lockfile、build config、目标平台、工具链版本等已经变化，应使用项目自己的清理/重建命令。不要为了“缓存”绕过 exact commit，也不要假定无法证明有效性的旧产物仍然正确。

CWapi shutdown 或下一次 startup 会清理 process-lifetime workspace；shared mirror 保留。

## Permission fallback

```text
full_access local mode
  -> Codex first
  -> structured PERMISSION_DENIED
  -> blocked response + 60s System Token
  -> caller sends same repo/commit/process args with new request_id + Token
  -> Core re-resolves final invocation in original repository workspace
  -> binding/policy pass, one-time consume
  -> System backend process record
```

只有真实 `PERMISSION_DENIED` 才进入 System fallback；普通 `PROGRAM_FAILURE` 不升权。binding mismatch 不消费 Token，第 4 个 active Token 返回 `SYSTEM_TOKEN_LIMIT_REACHED`，不会驱逐前三个。

## Stock MCP

- status-list 是 global 且 params 为空；
- resource/tool 可 global 或 repository；
- caller 不提供 `threadId`；CWapi 管理 request-scoped context；
- `server=cwapi` 当前只支持 process_start/status/stop，不进入 stock relay；
- 不要假定两个不同 request_id 的 stock MCP 调用共享浏览器页面、tab、locator 或其它 session state。

`repo_search` 在真正实现前不属于当前 v1.6.3 工具清单；实现时必须同步更新协议、测试、acceptance 和正式发行文档，不能只改工作流说明。

### Playwright 多步操作

stock MCP 使用 request-scoped context。需要完成“打开页面 -> 填表 -> 点击 -> 断言 -> 截图”这类连续操作时，优先在一次 Playwright 调用中完成整段动作。

这是“任务拆分原则”的明确例外：如果拆成多个 request，后续 request 必须自行重新建立页面状态。不要把前一条 `browser_navigate` 成功当成下一条 `browser_fill_form` 一定还能看到同一个页面。

### 截图经 Slack 返回

需要让 ChatGPT 真正拿到截图文件时，调用 `playwright/browser_take_screenshot` **不要提供 `filename`**，例如：

```json
{
  "fullPage": true,
  "scale": "css",
  "type": "png"
}
```

这样 Playwright 可以把截图作为 MCP `type=image` content 返回；CWapi 会把返回的 image bytes 通过 Slack external file upload 投递到原请求 thread，并在 MCP response `resources` 中加入 Slack file reference。

如果提供 `filename`，Playwright 可能只返回本地文件路径。CWapi 出于安全边界不会因为 MCP 文本里出现 path/URI 就主动读取本地文件，因此“截图已保存到 `./xxx.png`”不等于 ChatGPT 已收到截图。

## 阻塞与等待规则

- 单次连续等待/轮询外部编译、Runner、Slack response 或其它异步结果累计不超过 3 分钟；
- 达到 3 分钟仍无 terminal result 时立即停止继续等待，向用户报告“任务仍在运行”与当前 process/request 状态；
- 不得通过多次 30 秒轮询把总等待时间无限延长；
- 已取得 stable `process_id` 时优先返回当前状态，后续刷新使用新的 global `process_status` request_id；
- 环境缺失不是等待条件。确认 CWapi 管理环境和本机环境都没有所需工具后，应立即进入权限/安装决策，而不是继续轮询。

## 调用方责任

### 当前 v1.6.3

- 为每个新动作/状态快照生成新 request_id；
- Windows path 在协议中使用 `/`；
- 默认采用小而可诊断的任务，不以减少 Slack 往返次数为主要优化目标；
- 当前没有正式 `repo_search`，不要构造不存在的 repository search 请求；
- 当前源码搜索和完整文件读取继续使用 GitHub Connector；`process_start` 搜索只作为调试/兼容性手段，不作为正式 search API；
- 优先复用同 repository persistent workspace 中输入一致的衍生资源，不无理由重复 build/install/start；
- 不为绕过当前 repository 的 managed workspace 而重复 clone/fetch 同一 repository；跨 repository 任务按上面的 workspace 模型处理；
- 只在 configured channel 传递短期 Token；
- 不把 Slack response 里的 Token 复制到 issue、日志或长期文档；
- System fallback 前确认错误确实属于 sandbox permission denial，而不是普通程序失败；
- 安装新依赖前确认用户已经选择 `FULL` 权限下由 Web GPT 安装，或选择自行手动安装；
- 需要二进制 MCP 结果时让 tool 返回 bytes/image/resource content，不要求 CWapi 根据文本路径自行读取本机文件。

### `repo_search` 实现并正式发布后

- 同 repo/commit 的代码搜索定位优先走 CWapi `repo_search`；
- 根据 path/line/snippet，再通过 GitHub Connector 精确读取完整源码文件；
- 不把 CWapi 搜索扩展成通用文件读取或大内容传输；
- 新 exact commit、远端写入和 GitHub-native 元数据继续由 GitHub 负责；
- Web GPT 继续负责理解、推理与规划，CWapi 只执行确定性搜索和本地任务。