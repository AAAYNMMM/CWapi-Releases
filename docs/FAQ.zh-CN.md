# CWapi 1.6.3 常见问题

[English](FAQ.md) | [简体中文](FAQ.zh-CN.md)

## CWapi 1.6.3 是什么？

它是旧版 GitHub + Slack 路线。Web GPT 负责理解任务、通过 GitHub 处理仓库真相，再经 Slack 向本机 CWapi 发送结构化请求；CWapi 在 Windows 本机执行开发任务，并把真实结果或文件通过 Slack 返回。

## 1.6.3 和 2.x 有什么区别？

它们已经是两条独立产品线。1.6.3 使用 GitHub + Slack Socket Mode，通过 Slack 承载 MCP v2 风格 frame；2.x 是另一套 MCP/Tunnel 架构，配置和工作流都不同。不要把 1.6.x Slack 配置直接复制到 2.x。详见 [版本选择指南](VERSION_GUIDE.zh-CN.md)。

## 为什么 1.6.3 要用 Slack？

Slack 负责 Web GPT 与本机 CWapi 之间的远程控制和结果传输。Socket Mode 让 CWapi 不需要暴露公网 Events API endpoint 也能接收事件。

## 需要 MCP 吗？

不需要 ChatGPT 直接连接本机 MCP。1.6.3 只是用 MCP v2 风格协议组织 payload，真正传输走的是 Slack。

## 所谓 MCP v2 frame，是不是 ChatGPT 直接连 MCP？

不是。`[CWapi/MCP/2]` 在 1.6.3 里表示 Slack 消息中的结构化 frame，不表示 ChatGPT 打开了到本机的直接 MCP transport。

## 需要 GitHub CLI 吗？

要走受支持的 private repository 认证路径，需要用户安装 GitHub CLI，并先执行 `gh auth login`。之后 CWapi 可以按需使用当前 Windows 用户已有的 `gh auth git-credential` helper 做非交互 Git 认证。

## 为什么必须 exact commit？

CWapi 要把本机受管 workspace 准备到确定的仓库状态。完整 40 位 `expected_commit` 可以防止本机实际执行在另一个 branch tip 或 checkout 上，而 Web GPT 还以为自己验证的是刚才那个版本。

## persistent workspace 是什么？

同一个 CWapi 进程生命周期内，同一 repository 复用一个受管 workspace。普通 request 结束后只释放 repository lease，不删 workspace，因此兼容的 ignored/untracked 依赖、构建产物和缓存可以继续使用。

## workspace 保存多久？

保存到当前 CWapi 进程退出。正常 shutdown 会删除 repository workspace；下次启动也会清理上一进程残留的 stale workspace。shared bare Git mirror 会保留。

## SAFE 和 FULL 有什么区别？

`SAFE` 是正常模式，每次程序启动都会恢复到 SAFE。它把写入/执行约束在 CWapi 受管边界和永久规则内。`FULL` 是用户临时授权模式，只在出现 CWapi 认可的 sandbox permission denial 后，才可能允许受控的 System fallback；永久安全规则并不会消失。

## FULL 为什么还有 System Token？

因为 FULL 不是“以后所有命令都直接 System”。`process_start` 仍然先走 safe backend。只有明确权限拒绝时，CWapi 才可能签发短时、一次性 Token，而且绑定原 repository、commit、executable、argv、cwd。调用方必须换新 request ID 重试同一个 invocation。

## 截图怎么返回 ChatGPT？

Playwright 使用 `browser_take_screenshot` 时不要传 `filename`。这样 MCP result 才会带真实 image bytes，CWapi 才能把它外置成 Slack File 返回到 request thread。

## 普通文件怎么返回？

底层 MCP result 必须真的返回可外置的 bytes/resource content。程序输出一个本机文件路径并不会让 CWapi 自动去读这个路径再上传。

## 为什么 Web GPT 3 分钟后就停止等待？

工作流规定单次连续等待/轮询最多 3 分钟，避免一个对话无限卡在“再查一次”。任务还在运行时，Web GPT 应报告当前状态，之后再用新的 status request 查询同一个 `process_id`。

## 3 分钟后任务还没结束怎么办？

保留稳定的 `process_id`，不要重启任务。以后用新的 global request ID 调 `process_status`；只有真的要结束任务时才调用 `process_stop`。

## private repository 怎么认证？

CWapi 使用非交互 Git，并可以配置隔离的 credential helper 去调用当前 Windows 用户已有的 `gh auth git-credential`。它不会修改 global Git config。GitHub CLI 登录或仓库权限不够时，会返回真实 Git/认证错误。

## Slack token 保存在哪里？

Slack App Token 与 Bot Token 保存在当前 Windows 用户 Credential Manager；Slack Channel ID 保存在 `CWapi-data/config/cwapi.json`。

## 为什么配置文件里还能看到 `1.6.1`？

那是**配置 schema 版本**，不是产品版本。产品正式版本是 `1.6.3`，但当前 `cwapi.config.v2` 的 schema version 仍然是 `1.6.1`。如果擅自把配置里的这个值改成 1.6.3，当前代码反而会拒绝它。

## 1.6 配置可以直接迁移到 2.x 吗？

不可以。通信架构和配置模型都变了。建议把两条路线解压到不同目录，2.x 按 `main` 分支自己的文档重新配置。

## CWapi 会运行 AI 模型吗？

不会。Web GPT 负责推理；CWapi 负责准备本机执行环境、运行工具/进程、传输真实结果。
