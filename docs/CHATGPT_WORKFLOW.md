# CWapi v1.6.1 Web GPT Workflow

CWapi 的远程调用方通过配置的 Slack channel 发送 MCP v2 frame。CWapi 不使用 project registry；每个 repository request 自带 GitHub URL 与 exact commit。

## 标准流程

1. 调用方取得目标 GitHub repository URL 与完整 40 位 commit。
2. 在 Slack 发送唯一 `request_id` 的 `[CWapi/MCP/2][MCP_REQUEST]`。
3. CWapi 在 claim 前校验 protocol、route、scope、Token 位置与参数 shape。
4. repository 调用检查 gh readiness，准备共享 mirror 和 request-unique detached worktree。
5. stock MCP 调用进入 request-scoped ephemeral Codex thread；process tool 由 Go Core 直接处理。
6. CWapi 保存 terminal response，再投递到原 Slack thread。
7. 同 id 重投相同 fingerprint 会返回已保存 response；刷新 process 状态使用新 id。

## Process success

```text
process_start (new request id, repo+commit)
  -> Codex safe execution
  -> completed process record
  -> process_status / process_stop (new global request ids)
```

start 在 700ms 内完成则直接返回 terminal record，否则返回 stable process_id。长进程后续用 status 刷新；stop 最多同步等待 4 秒，owned cleanup 即使超时也继续。

### 可执行文件与运行环境解析

`process_start` 使用 CWapi 启动时冻结的执行环境。不要假定用户交互式终端中的 PATH 与 CWapi 看到的 PATH 相同，也不要把某台机器上的固定绝对路径当成通用规则。

需要 Node、Python、Git、浏览器驱动、编译器或其它外部工具时，按下面的顺序处理：

1. **先找 CWapi 管理的环境。** 优先检查当前 CWapi portable/runtime、受控 tools、已缓存并由 CWapi 管理的运行时；存在时直接使用该受控可执行文件的绝对路径。这样可以减少用户机器差异，也避免依赖交互式 PATH。
2. **CWapi 管理目录没有，再找本机已安装环境。** 使用 CWapi 当前进程实际可见的 PATH、系统 launcher、`Get-Command` / `where` 或必要的受限目录探测找到真实可执行文件，然后记录实际版本与解释器路径。不要预设 `C:/WINDOWS/py.exe`、`python.exe`、`node.exe` 或任何具体安装目录一定存在。
3. **本机也没有，不要反复尝试同一个命令。** 如果任务确实需要该依赖，告知用户缺少什么，并让用户选择：切换 CWapi 到 `FULL` 权限模式后由 Web GPT 通过 CWapi 执行安装，或者由用户手动安装。
4. **自动安装必须建立在用户明确切换/允许 FULL 权限之后。** 安装完成后重新探测实际可执行文件与版本，再继续原任务；不要假定安装程序一定把新工具加入当前 CWapi 已冻结的 PATH，必要时使用新安装文件的绝对路径或重启 CWapi 后再验证。

补充规则：

- `process invocation could not be resolved` 首先视为可执行文件解析失败，不要直接归因于脚本或仓库内容；
- portable 自带的运行时优先使用 portable 内绝对路径，例如 `<portable-root>/runtime/node/node.exe`；
- repository 内脚本优先把仓库相对路径直接放进 `command`，例如 `tools/env-probe.cmd`。不要为了找到脚本先改 `cwd` 再只传 basename；
- Windows path 在 MCP JSON 中统一使用 `/`。如果必须使用 `\`，必须正确做 JSON 转义，避免在 claim 前得到 `MCP_REQUEST_JSON_INVALID`；
- 环境探测失败后应进入“受控环境 -> 本机环境 -> FULL 安装/用户手动安装”的下一层，不得用无限 PATH 猜测造成阻塞。

## 衍生资源复用规则

v1.6.1 可以复用已经产生且仍然有效的衍生资源，但复用范围受 request-unique worktree 生命周期限制。**复用优先于重复构建，但正确性优先于复用。**

### 可以复用

- **同一个仍存活的 repository worktree 内的资源。** 编译物、中间文件、生成代码、依赖目录、项目内缓存等，只要仍位于当前存活 worktree 中且输入没有变化，应优先复用，不要无理由重复生成。
- **同一个 `process_start` 执行链中的产物。** 如果 `build -> test -> package -> run` 依赖同一批编译结果，优先用一个仓库脚本或一个 `process_start` 完成连续步骤，让后续步骤直接使用前面生成的资源；不要为了每一步都新开 repository request 而主动丢失前一步产物。
- **仍为 `starting` / `running` 的长期进程或服务。** dev server、test server、watcher、localhost 服务等已经启动且与当前 commit/config 相符时，后续浏览器或客户端检查应复用现有服务。启动同类新进程前先用 `process_status` 确认旧进程是否仍可用。
- **System Token fallback 保留的原 dirty tree。** FULL 模式下 Codex 因权限被拒并进入同一 invocation 的 System fallback 时，CWapi 会保留原 worktree；不要仅因为 backend 从 Codex 切换到 System 就重新构建已经存在且仍有效的中间资源。
- **CWapi 自己维护的 Git mirror。** repository mirror 会由 CWapi 自动复用。调用方不应为了“缓存 Git”自行额外 clone/fetch 一份仓库。

### 不允许假定可复用

- **不同 repository request 之间的 worktree 文件不能直接复用。** 每个新的 repository request 使用新的 request-unique detached worktree；原 process 进入 terminal 状态后，其 worktree cleanup 会删除其中的编译物、`dist/`、`target/`、`.venv/`、`node_modules/`、临时索引等未提交资源。
- 即使 `repository_url` 和 `expected_commit` 完全相同，也不能因为上一条 request 曾生成过某个路径，就在下一条 request 中直接引用该旧路径。
- 重发相同 `request_id` 只会按幂等规则返回已保存 response，不代表旧 worktree 会重新开放，也不是衍生资源缓存机制。
- stock MCP 的不同 request 同样不能假定共享 repository 文件状态、浏览器页面、tab、locator 或其它 session state。
- 本机工具自己的全局缓存如果碰巧可读，可以由工具自然命中，但它不属于 CWapi 保证的复用资源；不得依赖未验证的全局缓存来跳过必要的构建或校验。

### 必须失效并重新生成

出现以下任一情况，应把相关衍生资源视为失效：

- `expected_commit` 改变，或相关源码已经变化；
- dependency lockfile、build config、编译参数、目标平台、工具链版本或影响结果的环境发生变化；
- 原拥有该资源的 process 已进入 `completed` / `failed` / `stopped`，因此对应 worktree 已进入 cleanup；
- 无法证明资源对应当前输入，或复用可能掩盖测试/构建正确性问题。

不得为了制造跨请求缓存而主动切换 FULL，把编译物或缓存散落到任意用户目录。用户明确需要长期保存某个产物时，应把“持久化产物”作为单独任务处理；v1.6.1 本身没有 CWapi 管理的跨 repository-request build artifact cache。

## Permission fallback

```text
full_access local mode
  -> Codex structured PERMISSION_DENIED
  -> blocked response + 60s System Token
  -> caller sends same repo/commit/process args with new request_id + Token
  -> Core re-resolves final invocation in original dirty tree
  -> binding/policy pass, one-time consume
  -> System backend process record
```

binding mismatch 不消费 Token，修正后仍必须换新 request_id。第 4 个 active Token 返回 `SYSTEM_TOKEN_LIMIT_REACHED`，不会驱逐前三个。

## Stock MCP

- status-list 是 global 且 params 为空；
- resource/tool 可 global 或 repository；
- caller 不提供 `threadId`；CWapi 管理 ephemeral context；
- `server=cwapi` 只支持 process_start/status/stop，不进入 stock relay；
- 不要假定两个不同 request_id 的 stock MCP 调用共享浏览器页面、tab、locator 或其它 session state。

### Playwright 多步操作

stock MCP 使用 request-scoped ephemeral context。需要完成“打开页面 -> 填表 -> 点击 -> 断言 -> 截图”这类连续操作时，优先在一次 Playwright 调用中完成整段动作，例如一次 `browser_run_code_unsafe` 内导航、交互和断言。

如果拆成多个 request，则后续 request 必须自行重新建立所需页面状态。不要把前一条 `browser_navigate` 成功当成下一条 `browser_fill_form` 一定还能看到同一个页面。

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

- 为每个新动作/状态快照生成新 request_id；
- Windows path 在协议中使用 `/`；
- 优先复用当前仍存活且输入一致的衍生资源，不无理由重复 build/install/start；
- 不把已结束 request 的 worktree 文件当作跨请求缓存；
- 只在 configured channel 传递短期 Token；
- 不把 Slack response 里的 Token 复制到 issue、日志或长期文档；
- System fallback 前确认该直接命令确实需要当前 Windows 用户权限；
- 安装新依赖前确认用户已经选择 `FULL` 权限下由 Web GPT 安装，或选择自行手动安装；
- 需要二进制 MCP 结果时让 tool 返回 bytes/image/resource content，不要求 CWapi 根据文本路径自行读取本机文件。
