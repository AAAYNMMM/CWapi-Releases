# CWapi v1.6.0 Release Checklist

状态：公开发行仓库本地收口中。当前任务明确禁止 push、tag 与 GitHub Release 写入；远端发布必须在用户复核本地报告后单独执行。

## Source

- [x] 旧 Python/Tauri 产品源码从当前工作树移除；
- [x] 当前 Go/Wails/React v1.6.0 源码与文档进入发行仓库；
- [x] 代码与 UI 版本统一为 `1.6.0`；
- [x] 构建 helper 不含开发机固定 cache 路径；
- [x] final source commit clean before/after；
- [x] `gofmt`、`go test ./...`、`go vet ./...`、`git diff --check`；
- [x] frontend test/typecheck/build；
- [x] documentation/no-legacy gates。

## Portable build

- [x] Wails build 使用 `-trimpath`；
- [x] runtime staging 清除 transient log/tmp/dump/trace；
- [x] manifest 声明 executable-directory install root 与 `CWapi-data` policy；
- [x] pinned Codex、MinGit、Node、Playwright MCP、Chromium 验证；
- [x] final ZIP 从 clean exact commit 构建；
- [x] ZIP SHA-256 生成并回读一致。

## Privacy

- [x] ZIP 不包含 `CWapi-data`；
- [x] 不包含 token、private key、用户配置、数据库、任务、日志；
- [x] 不包含 browser profile/session；
- [x] `CWapi.exe` 不包含构建机用户名、用户目录或源码绝对路径；
- [x] archive filename 与内容扫描通过。

## Relocation

- [x] 解压到不同盘符；
- [x] 安装路径包含空格和非 ASCII 字符；
- [x] 从无关 working directory 启动真实 GUI；
- [x] `CWapi-data` 只写入 executable 相邻目录；
- [x] relocated Codex/Git/Node 可执行；
- [x] 测试后无 owned process 或临时目录残留。

## Publication boundary

- [ ] push source；
- [ ] create tag；
- [ ] create GitHub Release；
- [ ] upload portable ZIP。

本地任务完成时，publication 项必须继续保持未勾选。
