# Windows 日常使用与维护

本文只保留普通用户需要操作的内容。第一次配置请先看 [`USER_GUIDE.md`](USER_GUIDE.md)。

## 1. 启动

完整解压便携包后双击：

```text
CWapi.exe
```

GUI 打开后，CWapi 会自动启动自身需要的后台组件。普通用户不需要手工启动 Runner、Transport、Python、Git、Node、Codex 或浏览器 runtime。

## 2. Gmail 授权

首次使用时，在 GUI 中选择自己的 Google OAuth：

```text
credentials.json
```

CWapi 随后会打开系统默认浏览器。

用户本人在 Google 页面完成：

1. 登录
2. 选择账号
3. 查看授权
4. 点击允许

授权成功后返回 CWapi。

只要现有授权仍有效，普通 access token 刷新会自动进行。只有授权真正失效时，才需要重新打开浏览器并由用户再次手动授权。

不要手工编辑 `token.json`。

## 3. 日常使用

保持 CWapi 正在运行，然后在 ChatGPT 中使用 CWapi 工作流。

正常情况下，点击主窗口右上角 `×` 只会把 CWapi 隐藏到系统托盘，后台任务仍可继续。

需要重新打开时，从系统托盘打开 CWapi。

## 4. 完全退出

在 Windows 系统托盘找到 CWapi，选择：

```text
退出 CWapi
```

如果任务正在运行，优先等待正常结束；确实需要退出时使用 CWapi 自己的退出功能，不要在任务执行过程中直接删除程序目录。

## 5. 添加项目

在 CWapi 的项目页面添加：

- 项目名称
- 本地项目目录
- GitHub 仓库网络地址

网络地址示例：

```text
https://github.com/username/my-project.git
```

本地项目可以放在任意你有权限访问的位置。

## 6. Google Drive

如果需要 Google Drive 证据同步：

1. 安装 Google Drive for desktop
2. 登录与 ChatGPT Google Drive 插件相同的账号
3. 准备一个空白同步文件夹
4. 在 Google Drive for desktop 中添加该目录
5. 在 CWapi 设置中开启 Google Drive 同步
6. 选择同一个本地同步目录并保存

CWapi 把文件复制到同步目录，只表示已经交给 Google Drive 桌面客户端。是否真正上传到云端，以 Google Drive 客户端状态为准。

## 7. Doctor

遇到问题优先打开 CWapi 的 **Doctor**。

重点检查：

- Gmail / OAuth
- Runner
- Git
- Codex
- 项目配置
- SQLite / 状态目录
- Google Drive 同步目录

如果 Doctor 显示需要重新授权 Gmail，按照 GUI 提示重新打开浏览器，并由用户本人完成 Google 授权。

## 8. 移动 CWapi

需要换目录时：

1. 完全退出 CWapi
2. 移动整个 CWapi 文件夹
3. 不要只移动 `CWapi.exe`
4. 在新位置重新启动

CWapi 的内部便携数据应随整个目录一起移动。

## 9. 换电脑

在新电脑上：

1. 重新下载干净的正式便携包
2. 解压
3. 使用自己的 `credentials.json`
4. 重新完成 Gmail 授权
5. 配置项目
6. 重新配置 Google Drive 同步

不要把旧电脑使用过的整个用户数据目录直接当作公开安装包分发。

## 10. 更新

更新到新版本前：

1. 完全退出 CWapi
2. 从 GitHub Releases 下载正式版本
3. 按发行说明进行更新
4. 不要用来源不明的 EXE 或 ZIP 覆盖正式安装

如果新版本要求迁移设置，以该版本的发行说明和 GUI 提示为准。
