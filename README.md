# CWapi

CWapi 是一个面向 Windows 的本地受控执行与审计工具。ChatGPT 负责理解需求、读取或修改 GitHub 项目并做业务决策，CWapi 负责在本机按受控规则执行任务并返回结果。

# 任何问题聊天进小黑盒群，链接在帖子置顶评论

## 🚀 快速开始

第一次使用请先阅读：

- [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md)：从零开始安装、授权、添加项目并第一次使用 CWapi
- [`docs/WEB_GPT_ENTRY.md`](docs/WEB_GPT_ENTRY.md)：让网页 ChatGPT 使用 CWapi 时需要读取的唯一必读入口
- [`docs/OPERATIONS.md`](docs/OPERATIONS.md)：日常启动、退出、OAuth、项目、Drive 和 Doctor
- [`docs/SECURITY.md`](docs/SECURITY.md)：凭据、项目代码和本地执行的安全注意事项

正式便携版请从本仓库的 **GitHub Releases** 下载。

## 📦 Windows 便携版

正常使用形式：

```text
CWapi.exe
CWapi-data/
```

下载 `CWapi-portable.zip` 后完整解压，再双击 `CWapi.exe`。

CWapi v1.5.1 便携版已经包含 CWapi 自身需要的 Python、Git、Node、Go Transport、Codex、Playwright MCP 和 Chromium runtime。普通用户不需要为了运行 CWapi 另外安装这些运行组件。

首次 Gmail 配置需要用户自己提供 Google OAuth `credentials.json`，随后 CWapi 会打开系统默认浏览器，由用户本人完成 Google 登录和授权。

## 🤖 让 ChatGPT 使用 CWapi

先让 ChatGPT 连接 GitHub、Gmail 和 Google Drive，然后让它读取：

```text
docs/WEB_GPT_ENTRY.md
```

正常任务可以直接告诉 ChatGPT：

> 使用 CWapi 工作流，开发 GitHub 仓库 `username/my-project` 项目。

需要修复或测试时，在后面继续写具体任务即可。

## 🔐 提醒

不要公开或分享：

- `credentials.json`
- `token.json`
- 使用过的 `CWapi-data` 用户数据目录
- 项目中的私有密钥、API Key 或其他凭据

给其他人使用 CWapi 时，应让对方重新下载干净的正式便携包并使用自己的账号与凭据。

## 📚 关于这个仓库

本仓库用于公开 CWapi 产品源码、必要构建声明、用户使用文档和 GPT 使用 CWapi 所需文档。

开发过程记录、内部验收记录、测试工作资料、真实账号信息和私有运行数据不属于公开发行内容。
