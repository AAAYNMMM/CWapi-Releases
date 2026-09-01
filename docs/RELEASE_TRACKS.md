# Release Tracks

CWapi 从 2.0.0 开始在发行仓库中维护两条隔离路线。

## `main` — CWapi 2.x

`main` 是默认主线，只保存当前 CWapi 2.x 的干净发行源码快照与发行文档。

当前 2.0.3 发行快照的开发源码来源：

```text
Repository: https://github.com/AAAYNMMM/CWapi
Commit:     8941aa5d41768993c01e7798678a485f56331691
```

发行分支不携带测试、开发进度、验证脚本、打包脚本和发布脚本。完整开发材料仍保留在开发仓库。

## `1.6.x` — Legacy

`1.6.x` 在切换 2.0 主线之前从原 `main` 原样创建，用来保留 CWapi 1.6.3 及其发行仓库历史。

1.6.x 使用旧的 Web GPT / Slack 工作流，与 2.x 的原生 MCP Coding / Agent 架构不同。旧版文档、配置和操作方式不应套用到 2.x。

## Releases 与分支

GitHub Releases 继续按版本 tag 保存二进制资产：

- `v2.0.0` 及后续 2.x（当前 `v2.0.3`）：对应 `main` 发行路线；
- `v1.6.3` 及更早版本：作为 legacy release 保留，可结合 `1.6.x` 分支查看旧版发行源码。

切换分支只影响仓库源码视图，不会删除已有 tag 或 Release 资产。
