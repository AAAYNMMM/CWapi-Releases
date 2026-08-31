# CWapi 1.6.3 用户指南

[English](USER_GUIDE.md) | [简体中文](USER_GUIDE.zh-CN.md)

这份文档讲第一次配置完成以后，日常到底怎么用 1.6.3。

## 日常工作方式

```text
你把开发任务交给 Web GPT
        ↓
Web GPT 通过 GitHub 读取/修改源码并取得 exact commit
        ↓
Web GPT 通过 Slack 发送 CWapi MCP v2 frame
        ↓
CWapi 准备本机 exact-commit workspace
        ↓
执行 build / test / browser / process
        ↓
CWapi 通过 Slack 返回文本或文件
        ↓
Web GPT 根据真实结果继续
```

负责推理的是 Web GPT，CWapi 不运行模型。

## Repository 请求

仓库级任务必须同时携带：

```text
repository_url
expected_commit
```

repository URL 必须是 GitHub HTTPS 仓库地址，commit 必须是完整 40 位 SHA。CWapi 会为这个 exact commit 准备 detached 的受管 workspace，并校验最终 HEAD。

如果 Web GPT 修改了 tracked source，可靠流程是：

1. 先通过 GitHub 修改并提交；
2. 取得新的 exact commit；
3. 后续 CWapi request 使用这个新 commit；
4. 再在本机进行 build/test。

不要依赖未提交的 tracked 文件跨 request 保留。每次 repository prepare 都可能把 tracked source 重新同步到指定 commit。

## Persistent workspace

1.6.3 在当前 CWapi 进程生命周期内，为同一个 repository 复用一个 workspace。普通 request terminal 后只释放 repository lease，不删除 workspace。

因此下面这些 ignored / untracked 状态可以在多个 request 之间继续复用：

- `target/`
- `node_modules/`
- `.venv/`
- `build/`
- `dist/`
- 编译缓存和生成文件

这不等于“缓存永远不会坏”。lockfile、工具链、build config、目标平台或源码布局变化后，如果实际 build/test 证明旧衍生物不兼容，就用项目自己的 cleanup / reinstall 命令处理。

CWapi 正常退出时会删除 repository workspace；下次启动也会清理上一进程异常残留的 workspace。shared bare mirror 会继续保留。

## 搜索源码

Web GPT 需要定位函数、错误文本、类型、配置键或文件时，优先在现有 repository workspace 中执行短小、只读的搜索命令。

输出尽量只保留：

```text
path
line
匹配文本
少量上下文
```

已经有受管 workspace 后，不要为了搜索同一个仓库再 clone 一份。定位到文件后，再通过 GitHub 精确读取或修改。

## Process 工具

1.6.3 提供：

```text
cwapi/process_start
cwapi/process_status
cwapi/process_stop
```

`process_start` 是 repository-scoped，会在准备好的 workspace 中启动任务。很快结束的任务可能直接返回 terminal 结果；长任务会返回稳定的 `process_id`。

如果进程还在运行：

- 保存 `process_id`；
- 每次 `process_status` 使用新的 global request ID；
- 不要重复发原来的 `process_start`；
- 真要停止时再用 `process_stop`。

公开状态只有 `starting`、`running`、`completed`、`failed`、`stopped`。

## 3 分钟连续等待上限

Web GPT 单次连续等待或轮询 build、Runner、Slack response、长进程等结果，累计不要超过 3 分钟。

3 分钟后仍未 terminal，就应该告诉用户“任务仍在运行”，并带上当前 request/process 状态。后续继续查原来的 `process_id`，不要因为等烦了就把任务重启一遍。

## SAFE

普通读取、编译、测试优先保持 `SAFE`。safe 模式把写入范围限制在受管执行目录，以及允许的 global MCP 临时目录。

每次启动 CWapi 都会自动把权限重置成 `safe`，上一轮 `FULL` 不会恢复。

## FULL 与 System fallback

`FULL` 是用户临时授权的运行模式，不是“关闭所有安全限制”。永久执行规则仍然存在。

对 `process_start`，CWapi 仍然先走 safe backend。只有返回 CWapi 认可的结构化权限拒绝时，才可能签发 System Token。这个 Token：

- 60 秒有效；
- 一次性；
- 绑定原 repository、commit、executable、argv、cwd；
- 只能放在 fallback request 顶层。

重试时换新的 `request_id`，其它 invocation 参数必须保持一致。普通 build/test 失败不会拿到 Token。

## 截图与文件

CWapi 可以把 MCP result 中真实的二进制内容通过 Slack external upload flow 外置成 Slack File。

Playwright 截图如果要返回 ChatGPT，调用 `browser_take_screenshot` 时**不要传 `filename`**。这样 Playwright 才能把真实 image content 放进 MCP result，CWapi 再自动上传到 Slack。

如果工具只打印 `./image.png` 之类的文本路径，那只是字符串。CWapi 不会看到一个路径就擅自读取本机任意文件。

其它文件也遵循同样原则：底层 MCP result 必须真正返回可外置的 bytes/resource content。

## Private repository 认证

CWapi 使用非交互 Git 准备仓库。private repository 可以按需使用当前 Windows 用户已有的 `gh auth git-credential` helper。

CWapi 不修改 global Git config，也不会把广泛的 Git/GitHub secret/debug 环境变量继承给子进程。

## 移动或更新 portable 目录

`CWapi-data` 与程序目录绑定。只移动程序、不移动 `CWapi-data`，新位置会使用自己的本地数据目录。

更新 1.6.3 时不要把不同版本文件胡乱覆盖在一起。需要保留同一套 1.6.x 本地数据时，先退出 CWapi，再整体移动 portable 目录。

## 继续阅读

- [快速入门](GETTING_STARTED.zh-CN.md)
- [Slack 配置](SLACK_SETUP.zh-CN.md)
- [Web GPT 入口](WEB_GPT_ENTRY.zh-CN.md)
- [ChatGPT 工作流](CHATGPT_WORKFLOW.zh-CN.md)
- [常见问题](FAQ.zh-CN.md)
- [故障排查](TROUBLESHOOTING.zh-CN.md)
