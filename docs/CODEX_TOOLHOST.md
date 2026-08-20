# CWapi v1.6.0 Codex app-server / MCP Relay

文件名保留用于已有文档链接；当前实现不再是私有 custom Toolhost。

## Runtime

CWapi 使用 stock Codex CLI：

```text
codex app-server --stdio
```

固定运行时：

```text
version = 0.144.4-cwapi.1
source ref = rust-v0.144.4
source commit = 8c68d4c87dc54d38861f5114e920c3de2efa5876
SHA-256 = 51398051c2332b6afe08dc3b9dbb4056085c197f35ca57a307ee303d450cada5
```

Codex fork 分支的 source tree 已恢复到 stock baseline；CWapi 不依赖 custom `cwapi-dev` server 或 patched Git/Test/Build/File tools。

## MCP surface

CWapi relay 只接受：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

调用者不能提供 `threadId`。CWapi 自己创建并注入 context thread ID。

随包 `cwapi` MCP server 提供：

```text
process_start(command, argv, cwd?)
process_start(runtime, entrypoint)  # 兼容旧调用
process_status(process_id)
process_stop(process_id)
```

`command + argv` 不做语言或包管理器识别；PowerShell/cmd 作为普通 executable 调用。`command` 可由 PATH、绝对路径或 CWD 相对路径解析，直接路径必须存在且是 regular file；Windows `.cmd/.bat` shim 使用 `cmd.exe`。调用必须带 configured `project_id + expected_commit`，初始 CWD 位于 detached workspace。该 local MCP server 以当前 Windows 用户权限运行，不继承 Codex thread filesystem/execpolicy sandbox。

## Context thread

创建参数：

```json
{
  "ephemeral": true,
  "cwd": "<resolved local context>",
  "permissions": "cwapi-safe | cwapi-full-access"
}
```

正常 MCP relay 不调用 `turn/start`，所以不会为了转发一次 MCP tool 再启动模型 Turn。

## Permissions

`CODEX_HOME/config.toml` 由 CWapi 生成两个 Codex-native profile：

- `cwapi-safe`：基于 `:workspace`，workspace roots 来自已配置项目和 CWapi data root；
- `cwapi-full-access`：managed root write，加永久系统目录 deny，不使用 `:danger-full-access`。

`CODEX_HOME/rules/default.rules` 保存基础层可由 Codex execpolicy 强制的禁止规则，例如磁盘格式化、diskpart、危险 Git history mutation 和任意 process kill 入口。

## MCP server trust

Codex permission profile 直接约束 Codex-managed execution。对于 local stdio MCP server，是否真正受相同 filesystem/exec 边界约束取决于该 server 的启动环境与协议支持。

因此：

- packaged/启用的 MCP server 必须有明确来源；
- 新增本地 MCP server 前必须验证 sandbox/environment；
- 支持 `codex/sandbox-state-meta` 或 permission elicitation 的 server 优先；
- 不能把“thread 有 profile”误写成“任何 MCP server 都自动被 sandbox”。

## Lifecycle

- 一个 app-server connection 长期复用；
- 无 project 的全局 MCP context thread 在 permission 配置稳定时复用；
- exact-worktree context thread 按调用租用，并在 workspace 清理前 `thread/unsubscribe`；
- config 变化时等待现有 lease 结束，再重建 app-server context；
- app-server crash 后下次调用重建；
- MCP timeout/cancel 会结束 owned app-server process tree，避免后台调用继续占用已释放 workspace；
- ambiguous tool failure 不自动 replay。
