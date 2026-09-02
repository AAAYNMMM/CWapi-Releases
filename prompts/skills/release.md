# Release

Description: 用于版本更新、发布准备、发行检查和必要文档同步。

## Rules

- 发布前确认版本号在实际使用位置保持一致。
- 更新必要的 changelog、README 或发行文档，但不做无关文档重写。
- 优先对 exact commit 执行适用的构建、打包和隐私/候选审计；记录验证所对应的 source commit。
- 如果 candidate 验证后 tracked source 又发生变化，旧 candidate 证据视为过期，应针对新的 exact commit 重新构建/审计。
- 已验证 commit 原样 fast-forward 到目标分支时，同一 commit 的验证证据仍然适用，无需因分支名变化机械重跑。
- 发布前确认工作区只包含预期改动，远程提交、合并、tag、Release 或上传严格按当前任务授权范围执行。
