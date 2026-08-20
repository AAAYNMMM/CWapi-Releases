# CWapi v1.6.0 公开发行状态

## Product status

v1.6.0 产品功能与真实 Web GPT/Slack 工作流已经完成验收。发行仓库当前只保留 v1.6.0 架构；历史实现只存在于 Git 历史。

```text
Web GPT
 -> Slack MCP envelope
 -> CWapi relay
 -> exact-commit Codex context
 -> stock Codex app-server
 -> configured MCP server
 -> result / Slack File
```

当前发行工作聚焦于公开源码、无隐私 portable、任意路径启动和可复现验证，不再维护旧阶段的平行实现。

## Frozen v1.6.0 boundary

- Windows 11 x64、单用户、单机、Slack 单远程通道；
- no model Turn、no autonomous Agent；
- project discovery + exact `project_id/expected_commit`；
- safe/full Codex-native permission profiles；
- packaged Codex、MinGit、Node、Playwright MCP、Chromium；
- transparent `process_start/status/stop` with executable `command + argv`；
- restart starts a fresh runtime session；
- user data、secret、日志与 browser profile 不进入 portable ZIP。

## Public release requirements

- 当前工作树不保留旧产品代码；
- README、CHANGELOG、runtime 与 security 文档统一为 v1.6.0；
- build helper 不依赖开发机固定路径；
- `CWapi.exe` 使用 `-trimpath`；
- package runtime 全部相对 executable root；
- ZIP 在不同盘符、空格路径与非 ASCII 路径中真实启动；
- privacy scan 与完整本地 gate 通过；
- package hash、source commit 和 runtime pin 可回读。

## Publication state

本地发行候选完成前不 push、不创建 tag、不创建或修改 GitHub Release。最终本地 commit 与 ZIP hash 由交付报告给出，用户确认后再进入远端发布。

## Web GPT path rule

Windows 路径进入 MCP JSON 前必须把 `\` 转为 `/`。正式请求只使用 `C:/...`、`.venv/Scripts/...` 或 `//server/share/...` 形式。
