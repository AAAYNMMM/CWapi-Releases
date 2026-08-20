# CWapi 项目指导方针

CWapi 是个人使用的 Windows 开发辅助工具。长期原则只有：

```text
简单
稳定
高效
```

## 简单

- 工作流保持 `GPT -> MCP -> Result -> GPT`。
- Web GPT 负责思考与 GitHub 修改。
- CWapi 只负责 Slack、状态、路由、Codex lifecycle 和最少必要校验。
- 本地 MCP 执行交给 stock Codex app-server 与其配置的 MCP server。
- 不再维护第二套 Git/Test/Build/File Tool 平台；只保留一个透明的 command/process lifecycle MCP 执行端。
- 不为个人单机环境提前建设 RBAC、多租户或复杂策略编排。

## 稳定

- Slack 断线可恢复，只补当前进程游标后的新消息；应用重启不拾取旧任务；
- stock Codex app-server 退出后可重建；
- duplicate request 在当前进程会话内不重复产生副作用；
- terminal result 与 Slack delivery 在当前进程会话内分离保存；
- 无法确认完成状态的副作用调用不自动 replay；
- CWapi 只结束自己拥有的进程树；
- secret 不进入普通日志、MCP body 或 artifact。

## 高效

- 一个长期复用的 app-server 连接；
- 一个按需创建、无模型 Turn 的 MCP context thread；
- 权限/项目配置未变化时复用 context；
- 大日志按需读取，不把整份输出刷进 Slack；
- GUI 状态查询不主动制造昂贵执行。

## 三层安全边界

### 基础层

Codex-managed execution 的禁止规则写入其 exec policy / filesystem policy；幂等、CWapi secret 环境隔离、owned-process、delivery 等 CWapi 自身职责继续由 CWapi 强制执行。

基础层不是 Slack 指令过滤器。CWapi 不解析消息内容去猜某条命令危险不危险。

packaged command MCP 是用户明确接受的 trusted remote execution boundary：自由 `command + argv` 以当前 Windows 用户权限运行，不继承 Codex thread sandbox。CWapi 只绑定 configured project、exact commit、请求状态、日志和 owned process，不管理语言环境或过滤命令语义。

### 安全权限

默认模式。CWapi 选择 Codex profile `cwapi-safe`：

- 基于 Codex managed workspace sandbox；
- 已配置项目目录可写；
- CWapi 自己的数据目录可写；
- 其它普通用户路径不作为 workspace write root。

### 完全访问权限

用户显式开启。CWapi 选择 `cwapi-full-access`：

- 使用 Codex managed filesystem profile 扩大访问；
- 仍保留系统目录 deny；
- 仍加载基础 exec policy；
- 不使用裸 `:danger-full-access` 关闭 sandbox。

## MCP 边界

Codex profile 不是给任意本地程序施魔法。packaged command MCP 明确不受该 profile 的 filesystem/execpolicy sandbox；新增其它 local stdio MCP server 时也必须确认其真实边界。

## 版本边界

v1.6.0：单用户、单机 Windows、一个 CWapi runtime、一个 stock Codex app-server、Slack 单远程通道。

不属于 v1.6.0：多用户、多机器、复杂 RBAC、自定义 MCP 执行平台、私有 patched Codex runtime。
