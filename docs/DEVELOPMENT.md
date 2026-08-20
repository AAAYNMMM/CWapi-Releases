# CWapi v1.6.0 开发规则

最高优先级：**简单、稳定、高效**。

## Architecture contract

- Web GPT：需求、GitHub 修改、结果分析；
- CWapi：Slack relay、request/delivery state、Codex lifecycle、权限选择、日志与恢复；
- stock Codex app-server：permission profile、sandbox/execpolicy、MCP runtime；
- MCP server：具体工具能力。

禁止重新建立 custom `cwapi-dev`、workspace/Git/Test/Build/Automation/File Tool 平台或私有 patched Codex runtime。packaged `cwapi` MCP server 只实现透明 command/process lifecycle，不理解语言、包管理器或构建语义。

## MCP boundary

允许 remote relay：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

内部 context：`thread/start` + `ephemeral=true`。正常 relay 不调用 `turn/start`。

## Permission development

产品语义保留：基础层、安全权限、完全访问权限。

- safe -> `cwapi-safe`；
- full_access -> `cwapi-full-access`；
- full 不得退化成 `:danger-full-access`；
- 可由 Codex 直接约束的 filesystem/network/exec policy 放在 Codex config/rules；
- Slack 不做命令内容过滤；
- `cwapi/process_start` 允许自由 `command + argv`，但必须绑定 configured project 与 exact commit；
- command 子进程环境不得继承 CWapi/Slack/Codex secret；
- secret/idempotency/process ownership 属于 CWapi 自身职责。

## Stability

优先保证：

- Slack reconnect 只补当前运行会话游标后的消息；
- 当前运行会话内 duplicate/idempotency；
- 当前运行会话内 terminal response persistence/redelivery；
- 应用重启不恢复或展示上一会话任务；
- app-server crash/recreate；
- ambiguous side-effect call 不 replay；
- owned-process-only cleanup；
- bounded logs；
- secret redaction。

## Code modularity

- 人工维护 production/frontend/automation 目标 `<=400` 行；
- 硬上限 `500` 行；
- 接近目标时按职责拆分；
- generated/vendor 可例外，但不放手写业务逻辑；
- 可取消/超时操作传播 `context.Context`；
- shared mutable state 有明确 owner；
- config/protocol/state 强类型。

## Documentation modularity

- 当前 Markdown 目标 `<=250` 行，硬上限 `350`；
- 一个文档维护一个事实职责；
- 历史架构放 Git history/CHANGELOG；
- 当前文档不得继续描述已删除 custom Tool 平台为现状。

## Frontend

- UI 不保存第二份 authoritative state；
- secrets 不持久化到 frontend；
- 只通过 typed Go bindings 写配置；
- 显示 Slack、stock Codex、MCP relay 和真实 request 状态；
- 没有 backend cancel contract 就不显示“已取消”。

## Validation

按改动范围运行必要检查。最终 same-commit Gate 至少覆盖 Go、frontend/Wails、stock app-server relay、permission profiles、packaged hash、真实 Slack。

没有真实 Gate 结果，不宣布完成。
