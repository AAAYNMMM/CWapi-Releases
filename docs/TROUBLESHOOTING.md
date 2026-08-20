# CWapi v1.6.0 故障排查

这份文档按“你看到了什么现象”来排查，不要求先理解 CWapi 的全部内部架构。

推荐顺序：

```text
先看 CWapi 诊断页
   ↓
再看本页对应现象
   ↓
需要时再查 OPERATIONS / SECURITY / PROTOCOL
```

---

## 1. CWapi 启动不了

先确认：

- Windows 11 x64；
- 已完整解压发行 ZIP；
- `CWapi.exe` 与 `runtime/` 仍在同一个完整便携目录；
- 不要直接在 ZIP 中运行；
- 当前目录可写；
- 没有把新旧版本的 runtime 文件混在一起。

正常目录大致应包含：

```text
CWapi.exe
portable-manifest.json
runtime/
```

首次运行后还会出现：

```text
CWapi-data/
```

如果你刚移动过安装目录，先确认移动的是整个目录，而不是只复制 `CWapi.exe`。

---

## 2. Slack 显示未连接 / CWapi 没有收到消息

先检查三个配置：

```text
App Token   xapp-...
Bot Token   xoxb-...
Channel ID  C...
```

再检查：

- CWapi GUI 中 Slack 状态；
- 当前 Slack Workspace 是否就是配置 token 对应的 Workspace；
- Web GPT 是否把请求发到 CWapi 配置的那个 Channel；
- Channel ID 是否正确，而不是只填了频道显示名称；
- Slack App / Bot 在目标频道是否具备正常工作的权限与可见性。

CWapi 每次启动建立新的运行会话，不会自动回头扫描并执行启动前已经存在的旧频道消息。

所以如果你先发请求、后启动 CWapi，不应期待旧消息被自动拾取。

---

## 3. Web GPT 不知道 project_id

不要让 GPT 猜。

正常调用：

```text
projects/list
```

参数为空对象：

```json
{}
```

也可以先调用：

```text
mcpServerStatus/list
```

当前 CWapi discovery 会返回：

- `source_commit`；
- request methods；
- 已配置项目；
- `project_id`；
- `display_name`；
- `repository`；
- process tools。

如果项目列表为空，先去 CWapi “项目”页面添加项目。

---

## 4. `MCP_PROCESS_CONTEXT_REQUIRED`

当前代码中的含义是：

> CWapi process tools 需要项目上下文，但请求没有提供可用的 `project_id + expected_commit`。

正确做法：

1. 调 `projects/list` 获取真实 `project_id`；
2. 从 GitHub 获取要执行版本的完整 40 位 commit SHA；
3. 在外层 MCP request 同时提供：

```text
project_id
expected_commit
```

不要把这两个字段塞进 `process_start.arguments`。

---

## 5. `MCP_PROJECT_CONTEXT_INCOMPLETE`

含义：

> `project_id` 和 `expected_commit` 只提供了其中一个。

项目相关请求要求二者成对出现。

正确：

```text
project_id = prj-...
expected_commit = 40 位 SHA
```

错误：

```text
只有 project_id
```

或者：

```text
只有 expected_commit
```

---

## 6. `MCP_PROJECT_ID_INVALID`

含义：

> `project_id` 格式不符合当前协议。

当前项目 ID 格式类似：

```text
prj-0123456789abcdef01234567
```

不要：

- 自己生成；
- 从别的 CWapi 安装实例复制；
- 假定删除后重新添加项目还会得到同一个 ID。

重新调用：

```text
projects/list
```

取得当前实例真实 ID。

---

## 7. `MCP_EXPECTED_COMMIT_INVALID`

含义：

> `expected_commit` 不是完整 40 位 Git SHA。

不要使用短 SHA，例如：

```text
2a45c3b
```

应使用：

```text
2a45c3b0438725e764ccdffa8fa45acf58da585c
```

最终测试和验收尤其不要拿旧 commit 的结果证明新 commit 已通过。

---

## 8. 项目找到了，但 exact commit 准备失败

检查：

- GitHub 仓库 identity / remote URL 是否正确；
- commit 是否真的存在于目标 repository；
- 本机网络能否访问 GitHub；
- 仓库是否需要当前本机 Git 凭据；
- Web GPT 是否在提交后拿到了真正的新 40 位 SHA；
- 是否误用了另一个仓库的 commit。

CWapi 会通过 Git mirror fetch + exact commit verification 准备 detached worktree。

它不会因为本地某个目录“看起来像对的代码”就跳过 exact-commit 验证。

---

## 9. `MCP_CWAPI_TOOL_UNAVAILABLE`

当前随包 `cwapi` process server 只公开：

```text
process_start
process_status
process_stop
```

如果调用了其它 `cwapi` process tool 名称，就会失败。

先用 `mcpServerStatus/list` 读取当前真实 catalog，不要照抄旧版本工具名。

---

## 10. `MCP_PROCESS_ARGUMENTS_INVALID`

含义：

> `process_start` / `process_status` / `process_stop` 的 `arguments` 不是合法 object，或不符合当前工具 schema。

Web GPT 应先从 MCP catalog 读取当前 tool schema，再构造参数。

不要依赖旧版本截图或旧文档里的参数格式。

---

## 11. `MCP_PROCESS_CONTEXT_MANAGED`

CWapi 会自己注入：

```text
_cwapi_workspace
_cwapi_expected_commit
_cwapi_request_id
```

caller 不允许自己提供这些字段。

Web GPT 只需要提供正常业务参数，例如：

```text
command
argv
cwd
```

以及外层：

```text
project_id
expected_commit
```

---

## 12. `PROCESS_COMMAND_NOT_FOUND`

含义通常是：

> Web GPT 指定的 executable 找不到。

检查三种命令形式。

### PATH 名称

例如：

```text
python.exe
git.exe
node.exe
```

确认该命令真的在 CWapi 当前 Windows 用户环境可解析的 PATH 中。

### 绝对路径

例如：

```text
C:/Program Files/Git/cmd/git.exe
```

确认路径存在，并且目标是实际 executable / command script。

### 工作区相对路径

例如：

```text
tools/build.cmd
.venv/Scripts/python.exe
```

确认这个文件确实存在于**当前这次 exact-commit workspace**。

注意：上一次请求临时创建的 `.venv` 不保证下一次请求仍存在。

---

## 13. Python 明明装了，为什么 `python.exe` 找不到

Windows 上可能出现：

```text
python 不在 PATH
但 py.exe 存在
```

或者安装路径是：

```text
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
```

Web GPT 应先做环境发现，再选择准确 executable。

例如：

```text
where.exe python
where.exe py
py.exe -0p
```

如果已经找到真实 Python，就优先直接使用绝对路径或明确 launcher。

CWapi 不负责替 Web GPT 猜 Python 安装位置。

---

## 14. Java / JDK / Go / Rust / SDK 找不到怎么办

处理原则和 Python 一样：

```text
先发现
   ↓
确认版本
   ↓
已有就用准确 executable
   ↓
没有再安装
   ↓
把 command + argv 交给 CWapi
```

常见绝对路径可能带空格，例如：

```text
C:/Program Files/Java/jdk-25/bin/java.exe
C:/Program Files/Git/cmd/git.exe
```

v1.6.0 支持带空格的绝对 executable 路径，不需要为了避开空格把整个命令塞进一层 shell 字符串。

---

## 15. 为什么 Windows 路径推荐 `/`

正式 MCP JSON 推荐：

```text
C:/Users/name/Tools/python.exe
```

而不是：

```text
C:\Users\name\Tools\python.exe
```

因为 Windows 反斜杠进入 JSON 和 Slack 文本层以后容易触发转义。

比如：

```text
\n
\t
\U
```

都可能让字符串变得难以判断。

CWapi 正式 Web GPT 工作流统一采用：

```text
C:/...
D:/...
.venv/Scripts/...
//server/share/...
```

不要在 `command` 字符串外再手工加引号。

---

## 16. `PROCESS_START_FAILED`

这是“进程没有成功启动”的上层错误。

需要继续看：

- `command_path`；
- `command_resolution`；
- stderr；
- 系统错误信息；
- executable 是否存在；
- executable 是否能在当前 Windows 用户权限下运行。

如果错误底层是 `ENOENT`，通常表示启动阶段没有找到目标 executable 或其启动所需路径。

先确认实际环境，不要直接把问题归咎于项目脚本。

---

## 17. 进程返回 `running`，这算失败吗

不算。

长期服务正常就应该可能返回：

```text
state = running
process_id = proc-...
```

例如：

```text
python server.py
node server.mjs
```

后续使用：

```text
process_status(process_id)
```

查询。

测试结束时再：

```text
process_stop(process_id)
```

---

## 18. `process_stop` 后 exit_code 为什么可能不是 0

主动停止长期进程时，操作系统层面可能表现为非正常自然退出，因此 exit code 不一定是 0。

判断 stop 是否成功应优先看：

```text
state = stopped
```

以及服务是否真的不再监听端口。

不要只看到 `exit_code = 1` 就断言 stop 失败。

---

## 19. localhost 返回 `ERR_CONNECTION_REFUSED`

分两种情况。

### 启动后立即访问就拒绝

检查：

- server process 是否仍是 `running`；
- stdout 是否出现 ready 信息；
- 端口是否正确；
- server 是否绑定 `127.0.0.1` / localhost；
- 项目是否启动后马上崩溃；
- stderr 是否有异常。

### `process_stop` 之后访问被拒绝

这反而通常是**正常现象**，说明服务已经真的停止。

---

## 20. Playwright 能打开页面，但功能结果不对

不要只验证“页面能打开”。

完整 E2E 至少应按业务逻辑继续：

```text
navigate
  ↓
fill / click
  ↓
browser_evaluate 或页面读取
  ↓
验证真实结果
```

例如按钮点击成功不等于计算结果正确。

Web GPT 应读取 DOM / 状态值确认功能结果。

---

## 21. `browser_evaluate` 报 JavaScript 错误

检查：

- `function` 是否是合法 JavaScript；
- CSS selector 是否正确；
- 页面是否已经加载；
- 元素是否存在；
- 是否把 JSON / Slack 转义问题误带进 JavaScript 字符串。

尽量保持 function 简短，例如：

```js
() => ({
  result: document.querySelector('#result')?.textContent,
  title: document.title
})
```

`?.` 可以避免元素不存在时直接抛出属性访问错误。

---

## 22. 截图为什么没有出现在普通文本里

图片不是普通短文本。

Playwright 返回 image content 后，CWapi outbound policy 会把它作为 Slack File 交付。

正常 MCP response 的 `resources` 会记录类似：

```text
uri
media_type
sha256
size_bytes
```

当前限制：

```text
单个 artifact 最大 8 MiB
单次 response 最多 16 个 artifact
```

---

## 23. 为什么 CWapi 不根据文件路径自动上传文件

这是刻意的权限边界。

如果 MCP 结果只返回：

```text
C:/Projects/example/report.log
```

CWapi 不会自行读取这个路径。

外发链路是：

```text
MCP 已经取得并返回内容
   ↓
CWapi outbound policy
   ↓
Slack File
```

这样“允许 MCP 读文件”和“允许 CWapi 把文件传出去”不会被一个普通路径字符串偷偷合并成同一种权限。

---

## 24. 为什么重复发送同一个 request 没有执行第二次

这是幂等保护。

CWapi 当前 fingerprint 包含：

```text
project_id
expected_commit
method
canonical params
```

同一 `request_id` + 同 fingerprint 不重复执行。

同一个 `request_id` 如果换了不同 fingerprint，则属于冲突，不应被当成“修改原任务”。

要执行一个新动作，应生成新的 request ID。

---

## 25. Web GPT 等了 3 分钟后为什么停了

这是工作流规则，不是任务被取消。

对同一个外部任务：

```text
单轮累计等待上限 = 3 分钟
```

3 分钟仍未完成时，Web GPT 应：

- 停止本轮继续轮询；
- 报告当前 request / task / process ID；
- 报告 exact commit；
- 报告最后状态；
- 下一轮继续查原任务；
- 不重复提交。

本机任务可以继续运行。

---

## 26. CWapi 重启后旧 request 为什么不见了

v1.6.0 每次启动建立新的运行会话。

设计上：

- 不重放启动前 Slack history；
- 不自动恢复上一进程未完成 request；
- 当前进程内 duplicate / terminal response 可以复用；
- 同一进程 Slack Socket reconnect 可以继续 durable cursor 之后的消息。

这是为了避免在应用重启后把无法确认状态的副作用任务再次执行。

---

## 27. safe 模式为什么还是能运行自由 command

因为这里有两个不同的权限边界。

### Codex-managed execution

受：

```text
cwapi-safe / cwapi-full-access
Codex filesystem profile
Codex execpolicy
```

影响。

### packaged `cwapi` command/process MCP

自由 executable 以当前 Windows 用户权限运行，不自动继承上述 Codex thread sandbox。

所以不要把：

```text
safe
```

理解成“机器上的所有任意子进程都被锁在项目目录”。

完整说明见 [`SECURITY.md`](SECURITY.md)。

---

## 28. full_access 是不是等于完全关闭保护

不是。

`full_access` 扩大的是 Codex-managed filesystem profile。

CWapi 仍保留：

- secret 隔离；
- idempotency；
- owned process；
- delivery 规则；
- 基础系统目录 / execpolicy 设计；
- caller 不能注入 Codex threadId。

同时，packaged command MCP 的自由 executable 本来就是独立 trusted remote execution boundary，不应和 `full_access` 混为一谈。

---

## 29. 本机命令需要 token / password 怎么办

不要把 secret 直接放进：

```text
command
argv
Slack message
GitHub commit
artifact
```

优先使用：

- Windows Credential Manager；
- 已存在的 CLI 登录态；
- 工具自己的凭据存储；
- 本机受控环境变量机制，前提是不会被普通日志和 artifact 泄露。

CWapi 自己的 Slack / Codex secret 不会注入 command 子进程环境。

---

## 30. 还不能定位问题怎么办

收集这些信息通常足够：

```text
CWapi version
source_commit
当前 permission mode
project display name
project_id
expected_commit
request_id
MCP method
server / tool
terminal status
error.code
error.message
process_id（如果有）
stdout_tail / stderr_tail（必要的小范围）
```

不要为了排障直接上传整份用户数据目录、Credential Manager 内容或所有日志。

进一步参考：

- [`OPERATIONS.md`](OPERATIONS.md)
- [`SECURITY.md`](SECURITY.md)
- [`PROTOCOL.md`](PROTOCOL.md)
- [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)
- [`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)