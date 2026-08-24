# CWapi v1.6.1 用户指南

这份文档只讲第一次安装、配置和开始使用 CWapi。

## 1. 使用前准备

需要：

- Windows 11 x64；
- 一个可用的 Slack Workspace；
- ChatGPT 中连接 GitHub 与 Slack；
- **GitHub CLI，由用户自行安装并完成本机登录。**

GitHub CLI：<https://cli.github.com/>

安装后执行：

```powershell
gh auth login
gh auth status
```

确认 `gh auth status` 正常后继续。

## 2. 下载和启动

1. 从 [GitHub Releases](https://github.com/AAAYNMMM/CWapi-Releases/releases) 下载 `CWapi-v1.6.1.zip`。
2. 完整解压到任意用户可写目录。
3. 运行 `CWapi.exe`。

不要直接在 ZIP 内运行，也不要只复制 `CWapi.exe`。

## 3. 配置 Slack

第一次启动后，在 CWapi 界面的 Slack 区域填写：

```text
App Token
Bot Token
Channel ID
```

完整配置步骤见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

## 4. 在 ChatGPT 中连接 GitHub 和 Slack

在 ChatGPT 中完成 GitHub 和 Slack 连接。

CWapi 使用的 Slack App Token / Bot Token 只配置在 CWapi 中，不需要发送给 ChatGPT。

## 5. 第一次使用 CWapi 工作流

第一次建议告诉 Web GPT：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 的 `docs/WEB_GPT_ENTRY.md`，然后使用 CWapi v1.6.1 工作流处理我的项目。

之后直接给开发任务，例如：

> 使用 CWapi 工作流开发 `https://github.com/username/project`，修改后在对应 exact commit 上完成本机测试。

v1.6.1 不需要先在 CWapi 中添加项目。

## 6. 权限

日常使用保持 `SAFE`。

如果任务需要安装新的本机软件或修改 SAFE 范围外的环境，Web GPT 会提示用户临时切换 `FULL`。用户也可以选择自己手动安装缺少的软件。

CWapi 重启后会重新回到 `SAFE`。

## 7. 项目缺少 Python、JDK、Rust 等环境

不需要提前把所有开发环境都装好。

Web GPT 会先尝试 CWapi 已有环境，再检查本机已经安装的环境。如果仍然缺少依赖，会提示用户切换 `FULL` 后安装，或者由用户手动安装。

## 8. 日常开发流程

```text
给 Web GPT 开发任务
→ Web GPT 修改 GitHub
→ Web GPT 通过 Slack 调用 CWapi
→ CWapi 在本机编译 / 测试 / 运行工具
→ Slack 返回真实结果或文件
→ Web GPT 根据结果继续开发
```

完整 Web GPT 工作流见 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。

## 9. 更新和移动

- 更新 CWapi 时完整解压新版本，不要混用不同版本文件。
- 移动 CWapi 时先退出程序，再移动整个目录。
- GitHub 登录失效时重新运行 `gh auth login` / `gh auth status`。
