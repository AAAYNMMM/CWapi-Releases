# CWapi v1.6.1 新手完整教程

这份文档只回答：**第一次使用 CWapi，从下载开始应该按什么顺序做。**

Slack 从零配置见 [`SLACK_SETUP.md`](SLACK_SETUP.md)；Web GPT 规则见 [`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md) / [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)；故障见 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)。

## 1. CWapi 是什么

```text
Web GPT：理解需求、读写 GitHub、决定测试方式
        ↓ Slack MCP v2 request
CWapi：exact repository / commit / 本机执行 / 状态 / 回传
        ↓
stock Codex MCP 或 Go process tools
        ↓
结果 / 日志 / Slack File
```

CWapi 不运行模型，也不要求 ChatGPT Web 与本机建立直接 MCP 连接。

## 2. 下载与第一次启动

准备：Windows 11 x64、Slack Workspace、ChatGPT 中可用的 GitHub / Slack 连接。private repository 建议安装并登录 GitHub CLI，让本机 Git I/O 可以使用当前 Windows 用户已有的 GitHub 凭据。

1. 从 GitHub Releases 下载 `CWapi-v1.6.1.zip`。
2. 完整解压到任意用户可写目录。
3. 不要在 ZIP 内运行，也不要只复制 `CWapi.exe`。
4. 双击 `CWapi.exe`。

目录大致为：

```text
CWapi/
├─ CWapi.exe
├─ portable-manifest.json
├─ runtime/
└─ CWapi-data/     # 首次运行后生成
```

portable 已包含 CWapi 自己需要的 Codex、MinGit、Node、Playwright MCP 与 Chromium。

## 3. 配置 Slack

第一次启动后，在单页 GUI 底部的 Slack 区域打开配置 sheet，填写：

```text
App Token   = xapp-...
Bot Token   = xoxb-...
Channel ID  = C...
```

完整创建 App、Socket Mode、scopes 和 token 的步骤见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

Token 验证后保存在当前 Windows 用户的 Credential Manager，不写进普通配置文件。

## 4. 权限保持 SAFE

CWapi 每次启动都会把权限恢复为 `SAFE`。第一次使用保持 SAFE 即可。

只有任务明确需要在受控 worktree 之外安装或修改本机环境时，才由用户临时切换 `FULL`。`FULL` 不跨 CWapi 重启保留。

## 5. v1.6.1 不需要添加项目

v1.6.1 已删除 project registry、项目列表和 GUI 项目 CRUD。

repository request 直接携带：

```text
repository_url   = https://github.com/owner/repo
expected_commit  = 完整 40 位 Git commit
```

CWapi 自己维护共享 mirror，并为每个 repository request 创建独立 detached worktree。

## 6. 在 ChatGPT 中连接 GitHub 和 Slack

GitHub 用于读取 / 修改源码并取得 exact commit；Slack 用于向 CWapi 控制频道发送 MCP v2 request，并读取 response / Slack File。

CWapi 自己使用的 `xapp-...` / `xoxb-...` 不要交给 ChatGPT。ChatGPT 中的 Slack 连接和 CWapi Slack App 是两套独立授权。

第一次建议告诉 Web GPT：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 的 `docs/WEB_GPT_ENTRY.md`，了解 CWapi v1.6.1 工作流。

## 7. 第一次真实开发任务

之后可以直接说：

> 使用 CWapi 工作流开发 `https://github.com/username/my-project`，修改后在对应 exact commit 上完成本机测试。

正常过程：

```text
GitHub 读代码
→ 修改并得到新 commit
→ repository_url + expected_commit
→ CWapi 准备 exact-commit worktree
→ 本机测试 / 编译 / Playwright
→ 读取真实结果
→ 继续修复或结束
```

旧 commit 的测试结果不能证明新 commit 已通过。

## 8. Python / Node / JDK 等环境怎么办

不要固定某台机器的安装路径。Web GPT 应按以下顺序发现环境：

```text
1. CWapi portable/runtime 或 CWapi 管理的 tools/cache
2. 用户本机已经安装、且 CWapi 实际可见的环境
3. 两边都没有：
   - 用户切换 FULL，由 Web GPT 通过 CWapi 安装
   - 或用户手动安装
4. 安装后重新探测真实 executable 与 version
```

CWapi 的 PATH 是启动时冻结的快照。刚安装的软件如果没有进入当前 PATH，可以直接使用实际绝对路径，或重启 CWapi 后重新验证。

MCP JSON 中 Windows 路径统一优先使用 `/`。

## 9. process 生命周期

本机命令通过：

```text
process_start
process_status
process_stop
```

`process_start` 在约 700ms 内完成会直接返回 terminal record；长进程会返回稳定 `process_id`。后续状态查询使用新的 global request id 调 `process_status`，不要重复 `process_start`。

## 10. Playwright 与截图

stock MCP 使用 request-scoped ephemeral context。连续的 navigate / fill / click / assert / screenshot 最好在一次 Playwright 调用中完成；拆成不同 MCP request 时，不应假定浏览器页面状态会自动继承。

需要把截图真正传回 ChatGPT 时，`browser_take_screenshot` 不要指定 `filename`：

```json
{
  "fullPage": true,
  "scale": "css",
  "type": "png"
}
```

这样截图可以作为 MCP image content 返回，CWapi 再自动上传为 Slack File。只返回 `./image.png` 之类本地路径，不代表 ChatGPT 已经收到图片。

## 11. 等待边界

Web GPT 对同一个外部编译、进程或 Slack response 的连续等待/轮询累计最多 3 分钟。

3 分钟仍未结束时，应报告“任务仍在运行”、当前 request/process id 和状态；下一轮继续查询原任务，不重复提交。

缺少 Python、Node、编译器等环境不是等待条件。确认不存在后直接进入 FULL 安装或用户手动安装分支。

## 12. 移动、重启和更新

- **移动：**关闭 CWapi 后移动整个便携目录，不要只移动 exe。
- **重启：**运行中的 `FULL` 会恢复为 SAFE；旧 process registry / System Token 不跨进程恢复。
- **更新：**完整解压新版本，不要混用不同版本的 `runtime/`。

## 13. 出问题时

先看单页 GUI 中的 Core / Slack / Codex / process / latest record，再按 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md) 定位。

不要为了排障上传整个 `CWapi-data`、Credential Manager 内容、Token 或无关日志。
