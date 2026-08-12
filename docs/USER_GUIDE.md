# 🚀 CWapi 新手使用教程

> 这份教程只告诉你怎么操作。第一次使用时按顺序做一遍即可。

## ✅ 1. 在 ChatGPT 中连接需要的插件

在 ChatGPT 的插件 / 应用目录连接：

- 🐙 GitHub
- 📧 Gmail
- ☁️ Google Drive

权限建议：

- **GitHub：**允许所需权限，并确保能够访问你准备开发的 GitHub 仓库
- **Gmail：**允许所需权限
- **Google Drive：**允许读取文件

📌 建议 Gmail 和 Google Drive 使用同一个 Google 账号。后面的 Google Cloud OAuth 也使用这个邮箱。

---

## 🔐 2. 获取 `credentials.json`

### 2.1 创建 Google Cloud 项目

打开 Google Cloud Console，登录刚才连接 Gmail 插件使用的 Google 账号。

创建一个新项目，例如：

```text
CWapi
```

### 2.2 启用 Gmail API

进入刚创建的项目，找到：

```text
APIs & Services
```

搜索：

```text
Gmail API
```

点击 **Enable**。

### 2.3 配置 Google Auth Platform

进入：

```text
Google Auth Platform
```

按照页面提示开始配置。

应用名称可以填写：

```text
CWapi
```

支持邮箱和联系邮箱填写自己的 Google 邮箱。

个人 Gmail 用户在需要选择 Audience 时使用：

```text
External
```

### 2.4 添加测试用户

进入：

```text
Google Auth Platform
→ Audience
→ Test users
```

点击 **Add users**。

添加：

> 📧 ChatGPT 中 Gmail 插件连接的那个邮箱。

例如：

```text
yourname@gmail.com
```

保存。

### 2.5 创建 Desktop App 凭据

进入：

```text
Google Auth Platform
→ Clients
→ Create Client
```

Application type 选择：

```text
Desktop app
```

名称可以填写：

```text
CWapi
```

创建后下载 JSON 文件，并保存为：

```text
credentials.json
```

---

## 📦 3. 下载并启动 CWapi

进入 `AAAYNMMM/CWapi-Releases` 的 GitHub Releases 页面。

下载最新版：

```text
CWapi-portable.zip
```

完整解压，例如：

```text
D:\CWapi
```

⚠️ 不要直接在 ZIP 压缩包里面运行。

打开解压目录，双击：

```text
CWapi.exe
```

---

## 🌐 4. 完成 Gmail 授权

CWapi 第一次启动时，按照界面提示添加：

```text
credentials.json
```

选择刚才从 Google Cloud 下载的文件。

CWapi 会自动打开系统默认浏览器。

在浏览器中：

1. 登录前面添加到 **Test users** 的邮箱
2. 按 Google 页面提示继续
3. 查看授权信息
4. 点击允许
5. 如果出现测试应用提示，确认是你自己创建的 CWapi OAuth 应用后继续
6. 等待授权完成

然后返回 CWapi。

✅ Gmail 授权完成。

---

## 🐙 5. 安装并登录 GitHub CLI

安装 GitHub CLI。

打开 PowerShell：

```powershell
gh auth login
```

按照提示选择：

```text
GitHub.com
HTTPS
Login with a web browser
```

浏览器打开后登录自己的 GitHub 账号并完成授权。

检查：

```powershell
gh auth status
```

能看到自己的 GitHub 账号即可。

---

## 📥 6. 把 GitHub 项目同步到本地

假设要开发的仓库是：

```text
username/my-project
```

准备一个保存项目的目录，例如：

```text
D:\Projects
```

PowerShell：

```powershell
Set-Location D:\Projects
gh repo clone username/my-project
```

完成后本地会出现：

```text
D:\Projects\my-project
```

---

## ➕ 7. 在 CWapi 中添加项目

回到 CWapi，进入 **项目**，点击 **添加项目**。

### 项目名称

例如：

```text
my-project
```

### 本地项目地址

选择刚刚同步的目录：

```text
D:\Projects\my-project
```

### 项目网络地址

填写 GitHub 仓库地址，并在最后加 `.git`：

```text
https://github.com/username/my-project.git
```

点击 **保存**。

✅ 项目已经添加到 CWapi。

---

## ☁️ 8. 安装 Google Drive for desktop

安装 Google Drive for desktop。

登录：

> ChatGPT 中 Google Drive 插件连接的同一个 Google 邮箱。

在电脑上创建一个新的空白文件夹，例如：

```text
D:\CWapi-Drive
```

在 Google Drive for desktop 中，把它添加为同步文件夹。

---

## 🔄 9. 在 CWapi 中开启 Google Drive 同步

回到 CWapi，打开 **设置**。

开启：

```text
Google Drive 同步
```

选择刚才的同步文件夹，例如：

```text
D:\CWapi-Drive
```

点击 **保存**。

---

## 🤖 10. 第一次让 GPT 使用 CWapi

保持 CWapi 正在运行。

打开 ChatGPT，先告诉 GPT：

> 连接 GitHub，读取 GitHub 仓库 `AAAYNMMM/CWapi-Releases` 中的 `docs/WEB_GPT_ENTRY.md`，了解 CWapi 的使用方法和 CWapi 工作流。

等待 GPT 读取完成。

然后告诉 GPT 你要开发哪个仓库：

> 🚀 使用 CWapi 工作流，开发 GitHub 仓库 **`username/my-project`** 项目。

有明确任务时可以直接继续写，例如：

> 🛠️ 使用 CWapi 工作流，开发 GitHub 仓库 **`username/my-project`** 项目，检查当前问题，进行修复并完成本地测试。

---

## 🎯 11. 以后怎么使用

以后通常只需要：

```text
启动 CWapi
    ↓
打开 ChatGPT
    ↓
告诉 GPT GitHub 仓库
    ↓
告诉 GPT 要完成什么
```

例如：

> 使用 CWapi 工作流，开发 GitHub 仓库 **`username/my-project`** 项目，修复当前测试失败的问题。

---

## 💡 常见问题

### ChatGPT 看不到仓库

检查 GitHub 插件是否已连接，并确认它有目标仓库的访问权限。

### CWapi 找不到项目

检查 CWapi 项目设置中的本地目录是否真实存在，并确认网络地址类似：

```text
https://github.com/username/my-project.git
```

### Gmail 授权失败

确认浏览器登录的是 Google Cloud **Test users** 中添加的邮箱。

### Google Drive 没同步

检查：

- Google Drive for desktop 是否正在运行
- 登录账号是否正确
- CWapi 中选择的目录是否就是 Google Drive 配置的同步文件夹

---

## ⚠️ 重要提醒

不要把下面的文件公开或发给别人：

```text
credentials.json
token.json
```

也不要把自己已经使用过的整个 CWapi 数据目录直接发给别人。

别人需要 CWapi 时，让对方重新下载干净的正式便携包，并使用自己的 Google、GitHub 和 Google Drive 账号。
