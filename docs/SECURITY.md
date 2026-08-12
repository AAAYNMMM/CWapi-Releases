# CWapi 用户安全说明

## 1. 不要公开凭据

以下文件属于用户私有数据：

```text
credentials.json
token.json
```

不要：

- 提交到 GitHub
- 上传到公开网盘
- 发给其他人
- 放进公开 Issue、日志或截图

怀疑 Google 授权泄露时，应在 Google 账号中撤销相关授权，再重新完成 OAuth。

## 2. Google OAuth 必须由用户本人完成

CWapi 可以打开系统默认浏览器并处理授权结果，但 Google 页面中的：

- 登录
- 账号选择
- 授权确认

必须由用户本人操作。

如果原有授权已经失效，需要重新打开浏览器并再次手动授权。

## 3. 只添加可信项目

CWapi 可以运行项目自己的测试、构建脚本和其他已允许操作。

因此只把自己信任的 GitHub 仓库添加到 CWapi。

pytest、Cargo build script、项目脚本等都可能执行仓库代码。CWapi 的受控 worktree 能减少对日常项目目录的影响，但它不是完整的操作系统沙箱。

对完全不可信的代码，应使用虚拟机或其他更强隔离环境。

## 4. 不要随意扩大权限

CWapi 使用固定 Action Registry、项目配置和参数校验限制可执行操作。

不要为了“省事”修改安全设置，让未知项目拥有不必要的本机权限。

`local_command` 属于受控 PowerShell 能力，不等于允许任意系统命令。

## 5. 不要分享使用过的 CWapi 数据目录

使用后的 CWapi 目录可能包含：

- OAuth 授权数据
- 本地配置
- 任务状态
- 日志
- 结果
- 项目相关状态

给别人安装 CWapi 时，让对方重新下载干净的正式发行包，并使用自己的账号完成配置。

## 6. Google Drive 不是秘密存储

CWapi 可以把受控结果复制到用户选择的 Google Drive 同步目录。

不要主动让项目把密码、token、API Key 或其他秘密写进日志或 artifact。

文件进入本地 Drive 同步目录不等于 CWapi 已确认云端上传完成。

## 7. GitHub 项目版本

执行任务前，ChatGPT/CWapi 会使用目标 GitHub 仓库的精确 commit。

代码变化后应使用新的远程 commit 和新的请求，不要拿旧任务的成功结果证明新代码已经通过。

## 8. 普通用户不要手工修改内部文件

正常使用通过 CWapi GUI 配置：

- Gmail
- 项目
- Google Drive
- 用户可见安全设置

不要为了排障随意编辑 CWapi 内部 SQLite、token、runtime 文件或临时状态。

遇到问题优先使用 **Doctor** 和 GUI 提示。
