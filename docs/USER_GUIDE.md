# CWapi v1.6.0 新手完整教程

这份文档面向第一次使用 CWapi 的普通用户。目标不是解释所有内部实现，而是让你能从下载开始，完成 Slack、项目、权限和 Web GPT 配置，并真正跑通一次本机开发任务。

如果你只想快速了解 CWapi 是什么，先看仓库根目录 [`README.md`](../README.md)。

如果你是 Web GPT，需要使用 CWapi，请优先读取 [`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md)。

---

## 1. CWapi 是干什么的

CWapi 把 Web GPT 和你的 Windows 本机开发环境连接起来。

正常链路是：

```text
Web GPT
  ├─ GitHub：查看 / 修改项目并提交代码
  └─ Slack：发送结构化 MCP 请求
                ↓
              CWapi
                ↓
       exact-commit workspace
                ↓
       stock Codex app-server
                ↓
       configured MCP server
                ↓
      本机执行结果 / Slack File
```

Web GPT 负责“想做什么”；CWapi 负责“在正确项目、正确 commit 上调用本机能力并把结果送回来”。

CWapi 不运行模型，也不替 Web GPT规划项目。

---

## 2. 使用前准备

建议准备：

- Windows 11 x64；
- 一个可以正常访问目标仓库的 GitHub 账号；
- ChatGPT 中已连接 GitHub；
- ChatGPT 中已连接 Slack；
- 一个给 CWapi 使用的 Slack App / Workspace / Channel；
- 你准备开发的 GitHub 仓库；
- 如果项目需要 Python、Java、Go、Rust、Android SDK、CUDA 等环境，本机可以已有，也可以之后由你或 Web GPT 再安装。

CWapi v1.6.0 发行包自身已经准备 CWapi 工作所需的主要 runtime，例如 Codex、Git、Node、Playwright MCP 和 Chromium。**目标项目自己的语言环境与 SDK 不由 CWapi 固定管理。**

---

## 3. 下载 CWapi

进入本仓库 GitHub Releases 页面，下载当前 v1.6.0 便携包：

```text
CWapi-v1.6.0.zip
```

完整解压到一个可写目录，例如：

```text
D:/Tools/CWapi
```

或者：

```text
C:/Users/name/Apps/CWapi
```

不要：

- 直接在 ZIP 压缩包里运行；
- 只复制 `CWapi.exe`；
- 把 `runtime` 目录单独删掉；
- 把正在运行中的 CWapi 安装目录直接移动。

正常目录大致是：

```text
CWapi/
├─ CWapi.exe
├─ portable-manifest.json
└─ runtime/
   ├─ codex/
   ├─ git/
   ├─ node/
   ├─ mcp/
   └─ browser/
```

---

## 4. 第一次启动

双击：

```text
CWapi.exe
```

首次运行后会在安装目录生成：

```text
CWapi-data/
```

这里用于保存 CWapi 自己的数据、配置、状态和日志。

Slack App Token 与 Bot Token 不写进普通配置文件，而是保存到当前 Windows 用户的 Credential Manager。

发行 ZIP 本身不应包含你的：

- Slack token；
- 项目配置；
- 任务历史；
- 数据库；
- 日志；
- 浏览器 profile；
- 私有凭据。

---

## 5. 配置 Slack

CWapi v1.6.0 使用 Slack Web API / Socket Mode 作为远程传输层。

需要准备：

```text
App Token   xapp-...
Bot Token   xoxb-...
Channel ID  C...
```

在首次启动页面或设置页面填写当前 CWapi 实例要使用的 Slack 信息，然后确认界面中的 Slack 状态正常。

### Token 保存位置

CWapi 会把 token 保存到 Windows Credential Manager。

不要把 token：

- 发到 Slack 普通消息；
- 写进 GitHub 仓库；
- 塞进 MCP `command` / `argv`；
- 复制到普通日志；
- 放进测试截图或示例配置。

### Channel ID 是什么

CWapi 使用的是 Slack Channel ID，而不是单纯的频道显示名称。格式类似：

```text
C0123456789
```

实际 ID 以你的 Slack Workspace 为准。

---

## 6. 添加项目

打开 CWapi 的“项目”页面，添加要允许 CWapi 使用的项目。

项目记录包含：

```text
display name
repository
local path
remote URL
```

例如：

```text
Display name:
My Project

Repository:
username/my-project

Local path:
D:/Projects/my-project

Remote URL:
https://github.com/username/my-project.git
```

保存后 CWapi 会维护一个内部项目 ID：

```text
prj-xxxxxxxxxxxxxxxxxxxxxxxx
```

这个 `project_id` 是机器调用用的身份证。

### 不要手写或猜 project_id

Web GPT 正常应该通过：

```text
projects/list
```

或者 `mcpServerStatus/list` 返回的 CWapi discovery 信息取得当前项目 ID。

项目被删除、重新添加、换了一套发行版数据目录后，`project_id` 可能不同。

因此不要把以前某次看到的 `project_id` 当永久常量。

---

## 7. exact commit 是什么

CWapi 的项目调用不只需要项目 ID，还需要：

```text
expected_commit
```

它必须是 Git 的完整 40 位 commit SHA，例如：

```text
0123456789abcdef0123456789abcdef01234567
```

项目相关调用会绑定：

```text
project_id + expected_commit
```

CWapi 再内部准备 detached exact-commit worktree。

这样可以避免下面这种情况：

```text
GPT 修改了 commit A
但本地目录碰巧还是 commit B
然后测试了 B
最后却宣布 A 通过
```

v1.6.0 的目标就是让“GitHub 上准备验证的版本”和“本机真正执行的版本”明确对应。

---

## 8. 权限模式怎么选

CWapi GUI 提供两个主要模式。

### safe

默认推荐。

CWapi 为 Codex-managed execution 使用：

```text
cwapi-safe
```

它主要把已配置项目和 CWapi data root 作为受管 workspace 权限范围。

第一次使用建议保持 `safe`。

### full_access

只有明确需要扩大 Codex-managed filesystem 访问时再打开。

CWapi 使用：

```text
cwapi-full-access
```

它不是“关闭一切保护”，仍保留 CWapi 自身的 secret、幂等、owned process 等边界，并且不使用裸 `:danger-full-access`。

### 一个非常重要的区别

`safe` / `full_access` 主要描述 **Codex-managed execution**。

随包 `cwapi` command/process MCP 启动的自由 executable：

```text
process_start(command + argv)
```

以当前 Windows 用户权限运行，**不会因为 thread 使用了 `cwapi-safe` 就自动进入同样的 filesystem/execpolicy sandbox**。

所以自由 command 能力应只用于你明确配置、明确允许开发的项目。

更完整说明见 [`SECURITY.md`](SECURITY.md)。

---

## 9. 在 ChatGPT 中连接 GitHub 和 Slack

Web GPT 正常需要两个外部通道。

### GitHub

用于：

- 读取项目代码；
- 查看历史；
- 修改文件；
- 提交 commit；
- 取得 exact commit SHA。

### Slack

用于：

- 给 CWapi 发送 MCP request；
- 读取 MCP response；
- 获取 CWapi 返回的 Slack File；
- 查询同一个请求或进程的后续状态。

CWapi v1.6.0 的正式产品链路不依赖 Gmail / Google Drive 作为控制通道。

---

## 10. 第一次让 Web GPT 认识 CWapi

保持 CWapi 正在运行，然后在 ChatGPT 中告诉 Web GPT：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 仓库中的 `docs/WEB_GPT_ENTRY.md`，了解 CWapi v1.6.0 当前工作流。

`WEB_GPT_ENTRY.md` 是专门给 Web GPT 的快速入口。

它会告诉 GPT：

- 先如何 discovery；
- 如何拿 `project_id`；
- 如何绑定 exact commit；
- 允许使用哪些 MCP request method；
- 如何调用 `process_start/status/stop`；
- Windows 路径怎么传；
- 文件怎么返回；
- 等待超过 3 分钟怎么办；
- 哪些旧版接口已经不能再用。

---

## 11. 第一次真正开发项目

假设项目：

```text
username/my-project
```

可以直接告诉 GPT：

> 使用 CWapi 工作流开发 GitHub 仓库 `username/my-project`，检查当前项目并完成一次本地测试。

如果有明确任务，可以直接写：

> 使用 CWapi 工作流开发 GitHub 仓库 `username/my-project`，修复当前登录页面的问题，修改后提交 GitHub，并在对应 exact commit 上完成本地测试。

之后 Web GPT 正常会做类似这些事情：

```text
GitHub 读取源码
    ↓
确定修改
    ↓
修改 / commit
    ↓
CWapi discovery
    ↓
project_id + expected_commit
    ↓
本机测试 / 编译 / 服务 / Playwright
    ↓
读取结果
    ↓
继续修复或结束
```

---

## 12. 本机环境现在由谁管理

v1.6.0 的规则是：

> **目标项目环境由用户或 Web GPT 发现、安装、选择和管理；CWapi 只负责结构化执行。**

例如一个项目需要 Python。

Web GPT 可以先检查：

```text
python
python3
py
where.exe
Get-Command
```

如果发现本机已经有：

```text
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
```

就可以直接用准确 executable。

如果没有，再根据项目需要选择安装方式。

同样适用于：

```text
Java / JDK
Node
Go
Rust
Visual Studio Build Tools
Android SDK / NDK
CUDA
其它项目工具链
```

CWapi 不负责猜“哪个版本才是你的项目想要的版本”。

---

## 13. 为什么推荐准确 executable 路径

如果机器里有多个 Python：

```text
Python 3.10
Python 3.12
项目 .venv
Microsoft Store alias
```

只写：

```text
python.exe
```

可能无法明确到底使用哪一个。

更可控的是：

```text
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
```

或者项目环境：

```text
.venv/Scripts/python.exe
```

Java 也是一样：

```text
C:/Program Files/Java/jdk-25/bin/java.exe
```

CWapi v1.6.0 的 `process_start` 已支持：

```text
PATH executable name
absolute executable path
working-directory-relative executable path
```

---

## 14. Windows 路径为什么写成 `/`

正式 MCP JSON 中推荐：

```text
C:/Users/name/Apps/Python/python.exe
```

而不是：

```text
C:\Users\name\Apps\Python\python.exe
```

原因不是 Windows 不认识反斜杠，而是：

```text
Windows path
   ↓
JSON escaping
   ↓
Slack text transport
```

连续几层字符串转义非常容易把 `\U`、`\n`、`\t` 等内容解释错。

Windows API 正常接受 `/` 形式，因此正式 Web GPT 工作流统一使用：

```text
C:/...
D:/...
.venv/Scripts/...
//server/share/...
```

不要给 `command` 字符串本身再包一层引号。

---

## 15. exact-commit workspace 里的临时文件会一直保留吗

不要这样假设。

项目调用使用的 exact-commit worktree 是临时执行上下文，调用和附件处理完成后可以被释放。

例如请求 A 执行：

```text
python -m venv .venv
```

即使成功，也不能简单假定请求 B 一定还能看到这个 `.venv`。

### 需要长期复用的环境怎么办

可以把环境安装在明确的持久位置，例如：

```text
C:/Users/name/.venvs/project/
D:/DevTools/Python312/
D:/SDK/jdk-25/
```

后续通过绝对 executable 调用。

或者由 Web GPT 根据项目要求在每次临时 workspace 中重新准备。

选择哪一种取决于项目本身，不由 CWapi 固定规定。

---

## 16. `process_start` / `status` / `stop` 是什么

随包 `cwapi` MCP server 提供三个进程工具：

```text
process_start
process_status
process_stop
```

### process_start

启动一个 executable，例如：

```text
python.exe server.py
node.exe server.mjs
go.exe test ./...
cargo.exe test
```

短命令可能直接返回 `completed`；长期服务可能返回：

```text
state = running
process_id = proc-...
```

### process_status

用已有 `process_id` 查询：

- 还在运行；
- 已完成；
- exit code；
- stdout tail；
- stderr tail。

### process_stop

用于停止由这个 `cwapi` process server 启动并记录的进程树。

例如本地测试服务器结束后，可以停止它，避免端口一直占用。

---

## 17. Playwright 可以干什么

CWapi 通过 configured Playwright MCP 可以做浏览器自动化，例如：

```text
打开 localhost 页面
填写表单
点击按钮
读取页面内容
执行 browser_evaluate
截图
```

一个常见 E2E 流程：

```text
启动本地 server
    ↓
访问 http://127.0.0.1:端口
    ↓
填写测试数据
    ↓
点击操作
    ↓
读取 DOM 结果
    ↓
截图
    ↓
停止 server
```

截图等 image content 可以由 CWapi 转成 Slack File 返回。

---

## 18. Slack File 怎么工作

短文本通常直接放在 MCP response 中。

内容较大或不是普通短文本时，CWapi 可以转为 Slack File，例如：

- 长文本；
- 日志；
- 图片；
- resource text/blob；
- 某些 compact 后仍过大的 JSON result。

当前主要限制：

```text
单个 artifact 最大 8 MiB
单次 response 最多 16 个 artifact
```

CWapi 只上传 MCP **已经返回**的内容。

如果 MCP result 里只出现：

```text
C:/Projects/example/output.log
```

CWapi 不会看到这个字符串以后自行偷偷打开那个文件并上传。

---

## 19. 长任务为什么不要重复提交

如果一个任务已经返回：

```text
process_id = proc-...
state = running
```

说明它已经启动。

Web GPT 后续应该：

```text
process_status(proc-...)
```

而不是再次提交同样的 `process_start`。

CWapi 当前会对 duplicate request 做幂等保护，但 Web GPT 正确的工作流依然应该是“查询原任务”，而不是制造一批重复编译或重复服务器。

---

## 20. Web GPT 最多等多久

对同一个外部任务或等待目标，单次回复累计最多等待：

```text
3 分钟
```

达到 3 分钟还没有 terminal result：

- Web GPT 停止继续轮询；
- 报告 request/task/process ID；
- 报告 exact commit；
- 报告最后状态；
- 本机任务可以继续运行；
- 下一轮只查询原任务；
- 不重复提交。

“停止等待”不等于“取消任务”。

---

## 21. 移动 CWapi 安装目录

CWapi 便携包不依赖启动时的当前工作目录，runtime 路径从 `CWapi.exe` 所在位置解析。

如果需要移动：

1. 正常关闭 CWapi；
2. 移动整个安装目录；
3. 不要只移动 `CWapi.exe`；
4. 重新启动；
5. 在诊断页面确认 runtime、Slack 和项目状态。

例如：

```text
D:/Tools/CWapi
```

可以整体移动到：

```text
E:/Apps/CWapi
```

前提是整个便携目录一起移动。

---

## 22. 更新 CWapi

更新发行版时建议：

1. 先正常结束当前开发任务；
2. 停止不再需要的 owned process；
3. 关闭 CWapi；
4. 下载新的正式发行包；
5. 阅读对应版本 README / CHANGELOG / 迁移说明；
6. 不要把旧安装目录里不明来源的 runtime 文件直接覆盖到新版本；
7. 用户数据如何迁移以新版本文档为准。

不要用“把两个 ZIP 随便混在一起”的方式升级。便携软件也没有义务从文件考古中理解你的意图。

---

## 23. CWapi 重启后为什么旧任务不继续显示

v1.6.0 每次启动建立新的运行会话。

当前设计：

- 不读取并重放启动前的 Slack channel history；
- 不自动恢复上一进程尚未完成的 request；
- 当前进程内 Socket reconnect 可以按 durable cursor 恢复；
- app-server 异常退出后，后续调用可以重建；
- ambiguous side-effect call 不自动 replay。

这是一种明确的防重复执行设计。

---

## 24. 日常使用最短流程

第一次配置完成以后，日常通常只需要：

```text
启动 CWapi
    ↓
确认 Slack / Codex / MCP 状态正常
    ↓
打开 ChatGPT
    ↓
告诉 GPT 要开发哪个 GitHub 仓库
    ↓
告诉 GPT 要完成什么
```

例如：

> 使用 CWapi 工作流开发 `username/my-project`，修复当前单元测试失败的问题，并在修改后的 exact commit 上完成本机测试。

或者：

> 使用 CWapi 工作流开发 `username/my-webapp`，启动本地开发服务，用 Playwright 验证登录页面并返回截图。

---

## 25. 安全提醒

不要公开或发送：

```text
Slack App Token
Slack Bot Token
API Key
password
私钥
项目 secret
CWapi-data 中的私有运行数据
```

尤其不要把 secret 放进：

```text
command
argv
普通 Slack 文本
GitHub commit
测试日志
截图
artifact
```

如果工具需要认证，优先使用本机已经配置好的 Credential Manager、登录态或该工具自己的凭据机制。

---

## 26. 出问题时看哪里

优先顺序：

1. CWapi “诊断”页面；
2. [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)；
3. [`OPERATIONS.md`](OPERATIONS.md)；
4. [`SECURITY.md`](SECURITY.md)；
5. 对 Web GPT 行为问题查看 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)；
6. 对协议问题查看 [`PROTOCOL.md`](PROTOCOL.md)。

常见错误和处理办法已经集中整理到 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)。