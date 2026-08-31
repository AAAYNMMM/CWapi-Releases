# Changelog

## 2.0.0 — 2026-08-31

CWapi 2.0 正式成为发行主线。

主要变化：

- 新增彼此隔离的 Coding MCP 与 Agent MCP；
- Coding 采用 durable repository workspace，可按 repository URL 恢复，并支持 exact commit guard；
- Web GPT 直接负责规划与编码，bundled Codex 仅作为 model-free `command/exec` 工具宿主；
- Agent 提供 localhost OpenAI-compatible `/v1/models` 与 `/v1/chat/completions`，把本地软件请求桥接给 Web GPT；
- Coding / Agent 使用独立 token、独立 MCP server、独立 tool catalog 与独立 OpenAI Secure MCP Tunnel 配置；
- `coding_attachment` 与 Agent inline attachment 边界收缩为 raster image；普通文本和文件不作为通用 MCP 文件资源传输；
- 配置升级为 `cwapi.config.v3 / 2.0.0`；
- portable 内置 OpenAI Codex `0.150.1`、MinGit `2.55.0.windows.4` 与 OpenAI `tunnel-client 0.0.10`；
- 发行包不包含用户数据，并通过 portable privacy gate；
- 发行仓库改为双路线：`main` 维护 2.x，`1.6.x` 保留 1.6.3 及旧版历史。

发行包对应开发仓库源码提交：

```text
d904ae80428c90717e050a151c65fa35b6b83c63
```

## 1.6.x

1.6.3 及更早发行仍保留在 `1.6.x` 分支和已有 GitHub Releases 中。2.x 与 1.6.x 不共享同一工作流或配置结构。
