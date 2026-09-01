# CWapi 2.0 Coding 指南

[English](CODING_GUIDE.md) | [简体中文](CODING_GUIDE.zh-CN.md)

CWapi Coding 给 Web GPT 一组很小的 MCP 工具，让它操作每个 canonical repository 对应的一份 durable 本地 Git workspace。整个任务中，负责分析、规划和决定下一步的一直是 Web GPT。

## 架构

```text
Web GPT
  ↓ 推理 + 精确工具调用
Coding MCP
  ↓
CWapi Coding service
  ↓
Durable Git workspace
  ↓
Bundled Codex app-server command/exec
  ↓
Windows 工具 / Git / build / test
```

portable 内置 Codex runtime **不是第二个 coding agent**。CWapi 只使用 app-server 初始化、Windows sandbox readiness/setup 和 model-free `command/exec`。它不创建 `thread/start` / `turn/start`，不调用 Codex 模型，不读取 Codex 账号，也不要求 Codex 登录。

## Coding 工具

```text
coding_open
coding_exec
coding_status
coding_close
```

Web GPT 不会拿到、也不需要保存公开 Coding session ID。后续所有调用都继续用同一个 canonical `repository_url` 定位 CWapi 内部 active session。

## 推荐完整流程

```text
用户给 repository URL / 任务
        ↓
coding_open
        ↓
先检查仓库和相关源码
        ↓
coding_exec 搜索 / 读取
        ↓
用精确命令修改文件
        ↓
Build / Test / Verify
        ↓
需要 Git 真相时 coding_status
        ↓
如果用户已授权且操作需要 .git 写入：切 FULL
        ↓
按要求 commit / push
        ↓
coding_status 最终检查
        ↓
coding_close
```

`coding_close` 只关闭 active handle。它不会 reset Git、clean 文件、删除未提交修改，也不会删除 durable workspace。

## `coding_open`

概念上的输入：

```text
repository_url
 target_ref
 expected_commit?  # 可选，完整 40 位 SHA
 resume?           # 默认 false
```

### `repository_url`

填需要 CWapi 管理的 GitHub repository URL。CWapi 会把 repository identity canonicalize，后续调用继续用同一个仓库 URL，不要自己发明另一套本地路径身份。

### `target_ref`

`target_ref` 必填，并且必须是有效 branch。CWapi 会 canonicalize 成 `refs/heads/<branch>`，从 `origin` fetch 该 branch，再把受管 worktree checkout 到解析出的 commit，HEAD 为 detached 状态。

例如：

```text
main
1.6.x
refs/heads/feature/example
```

当前 workspace contract 里，tag 或任意 commit 不能代替 branch `target_ref`。

### `expected_commit`

这是可选的 exact baseline guard。提供时必须是完整 40 位十六进制 SHA。

CWapi 会自己 fetch target branch 并解析远端 commit。如果结果和 `expected_commit` 不一致，open 直接报 mismatch，不会偷偷换一个版本继续干活。

Web GPT 已经知道“必须验证 GitHub 上这个具体 commit”时，建议使用。

### 第一次 open、后续 open 与 dirty workspace

`resume=false` 时：

- 第一次使用会 clone 受管 repository；
- 后续使用会验证 repository identity，再 fetch target branch；
- tracked dirty 会返回 `WORKSPACE_DIRTY`；
- 本地 commit 领先远端会返回 `WORKSPACE_LOCAL_COMMITS`；
- 历史 diverged 会返回 `WORKSPACE_DIVERGED`；
- CWapi 不会为了“看起来很干净”就擅自丢掉用户本地工作。

### `resume=true`

只有你明确想继续已有 workspace/session 时才用 `resume=true`。

CWapi 要求：

- workspace 已存在；
- repository identity 一致；
- workspace metadata 有效；
- `target_ref` 与现有 context 一致；
- 如果提供 `expected_commit`，必须和 metadata 里保存的 resolved commit 一致。

resume **不会**重新 fetch/resync，而是保留当前 workspace，返回当前 HEAD、tracked dirty，并标记 `resumed=true`。

这就是新 ChatGPT 对话能安全继续旧任务的关键。

## Active session 与 `CODING_WORKSPACE_BUSY`

一个 canonical repository 同时最多一个 active Coding session。

如果仓库已经 active：

- 兼容的 `coding_open(..., resume=true)` 会复用内部 session；
- `resume=false` 返回 `CODING_WORKSPACE_BUSY`；
- opening/closing、target ref 不兼容或 expected commit 不兼容时不会静默接管；
- 如果已经有 `coding_exec` 在跑，resume/open 状态可能显示 `busy`。

不要通过换一种 URL 拼法或另外 clone 同一个仓库来绕过。这种“绕过保护”的创意通常只会制造两份互相不知道对方存在的状态。继续使用同一个 `repository_url` 并恢复真正的 active session。

## Durable workspace 路径与生命周期

workspace 位于 portable 旁边：

```text
CWapi-data/workspaces/<sha256-derived-repository-key>/repo
```

同一 container 旁边有 `workspace.json` metadata。

Coding session close 或 ChatGPT conversation 结束都不会删除它。

Desktop maintenance 可以在用户明确想重建 workspace 时删除指定 durable workspace。删除就意味着该 workspace 中未推送/未提交的本地工作也一起消失，所以别把“清理”当成治疗焦虑的按钮。

## `coding_exec`

`coding_exec` 在 active repository workspace 中执行**一条确定的开发命令**。

概念输入：

```text
repository_url
command
argv[]
cwd?             # 可选，必须在准备好的 repository 内
timeout_seconds?
```

executable 和 arguments 分开传。不要把整条 shell 字符串塞进 `command`，除非你本来就明确要执行 `pwsh` 这类 shell。

推荐：

```text
command = rg
argv    = ["-n", "AgentAPIKey", "internal", "docs"]
```

确实需要 shell 语义时：

```text
command = pwsh
argv    = ["-NoProfile", "-Command", "Get-Content README.md | Select-Object -First 80"]
```

`cwd` 必须留在 prepared repository 里面。CWapi 会解析真实 executable 和 working directory，再交给 sandbox/toolhost。

## 先检查，再修改

好的 Coding 流程通常先用少量、信息密度高的读取，而不是发几十次“一行一行看”的请求。

例如：

```text
rg -n "symbol|error|config" src tests docs

git status --short --branch

git show HEAD:path/to/file
```

几个搜索互不依赖时，可以合理合并。文件没有变化、也没有新证据时，不要反复读同一份内容。

## 普通文本怎么读

源码、Markdown、JSON、配置、日志和其它可读文本都通过 `coding_exec` 读取。

Coding MCP 没有文件或图片传输工具，也不会产生 MCP `ImageContent` 或 `EmbeddedResource` content。

大文件优先读取相关范围、带少量上下文的搜索命中或项目自己的 query，没必要把整个文件拖进对话，只因为字节很多看起来很努力。

## 怎么修改文件

Web GPT 通过 workspace 内的精确命令/脚本修改文件。原则：

1. 先理解文件和相关调用关系；
2. 做最小且完整的修改；
3. 路径必须留在受管 repository；
4. 改完先跑最窄有效验证；
5. 有必要再扩大测试范围。

CWapi 没有另一个“AI edit model”。文件怎么改，就是 Web GPT 明确要求执行的 deterministic command/script 怎么改。

## Build / Test

通过 `coding_exec` 调项目自己的工具，例如：

```text
go test ./...
cargo test
npm test
pytest
```

到底有什么工具，以 CWapi 能看到的 Windows 环境和项目实际依赖为准。不要因为另一台电脑装了 Rust，就默认这里也凭空长出 Cargo。

portable 自带 Git runtime 和 Codex command toolhost；其它项目 runtime 取决于项目/本机环境。

## `coding_status`

需要 Git/workspace 真相时调用 `coding_status(repository_url)`，尤其是：

- 完成重要修改后；
- commit/push 之前；
- 最终交付前；
- resume/recovery 状态不确定时。

它会返回 target ref、resolved commit、current HEAD、tracking head、tracked dirty、divergence 等当前信息。

它是检查操作，不会为了把数据“刷新得漂亮”而偷偷 fetch 或改 Git。

## SAFE

普通 read/edit/build/test 使用 `SAFE`。

Coding 把 SAFE 映射到 bundled Codex sandbox 的 workspace-write 行为，允许正常修改 repository 文件，同时保护 `.git` metadata。

所以 SAFE **不是只读模式**。改源码本来就是正常工作。

## FULL

只有用户已经授权，而且操作确实需要 `.git` metadata 写入或更广 host access 时才切 `FULL`。

典型情况：

```text
git commit
git push
其它用户明确授权的 Git metadata 操作
```

SAFE/FULL 可以在 Coding session 保持 open 时热切换。已经运行中的 `coding_exec` 保持启动时的 profile；下一条 `coding_exec` 才使用新 profile。

FULL 不代表“所有规则失效”。CWapi 永久 execution policy 仍存在。除非用户明确要求，不要 force-push，也不要删除远端 ref。

## Git 操作

commit 前至少检查：

```text
git status --short
git diff --check
```

push 前确认：

- 当前 intended branch/ref；
- staged files 没有混入意外内容；
- 没有用户数据/隐私文件；
- 用户确实要求远端写入。

由于新的 non-resume prepare 会拒绝 local commits/divergence，没完成的本地 commit 应优先 resume，而不是假装这是一条全新任务重新 open。

## Private Git repository

private repository 的 clone/fetch/push 使用当前 Windows 用户已有的 Git/GitHub credential 环境。Git 认证不依赖 Codex identity。

认证失败时，检查当前 Windows 用户的 GitHub/Git credential，而不是去登录 Codex。GitHub token 也不要塞进 prompt 或仓库文件。

## 文件与图片

Coding MCP 不传输文件或图片。源码、Markdown、JSON、配置、日志等文本通过有界 `coding_exec` 精确读取；二进制文件和图片留在 workspace，不会被复制进 ChatGPT 对话。

## Close 与以后 resume

任务真正完成时：

```text
coding_status(repository_url)
coding_close(repository_url)
```

close 只会：

- 释放 active internal session owner；

不会：

- clean Git；
- reset 文件；
- 删除未提交修改；
- 删除 durable workspace。

以后可以用 `resume=true` 继续兼容的 workspace。如果你要的是全新 baseline，就先确认旧 workspace 已经干净/可覆盖，或者通过 CWapi maintenance 明确删除重建。

## 示例：完整 bug fix

```text
1. 用户：修复 https://github.com/acme/widget main 上的一个 bug。
2. Web GPT：coding_open(repository_url, target_ref="main", expected_commit=<已知 SHA>)。
3. coding_exec 用 rg/search 找到错误路径。
4. coding_exec 有界读取相关文件。
5. coding_exec 用精确脚本/命令修改。
6. coding_exec 跑最窄测试。
7. 有必要再跑更广测试。
8. coding_status。
9. 用户要求 commit/push 时，本机切 FULL。
10. coding_exec 检查 git status / diff --check，再做已授权 commit/push。
11. coding_status 做最终确认。
12. coding_close。
```

## 相关文档

- [快速入门](GETTING_STARTED.zh-CN.md)
- [Agent 指南](AGENT_GUIDE.zh-CN.md)
- [故障排查](TROUBLESHOOTING.zh-CN.md)
- [Codex Toolhost](CODEX_TOOLHOST.md)
- [Protocol](PROTOCOL.md)
