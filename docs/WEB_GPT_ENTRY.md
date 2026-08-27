# CWapi v1.6.3 Web GPT Entry

这是 Web GPT 使用 CWapi 的最小入口。开始本机调用前，读取并遵守 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。

## 当前 v1.6.3 链路

```text
Web GPT
→ GitHub：取得 repository URL / exact commit，并处理完整文件读取、远端写入、PR、Issue、CI 等 GitHub-native 信息
→ Slack：发送 CWapi repository 请求
→ CWapi：准备/复用对应 exact commit 的 persistent workspace，执行 Web GPT 生成的只读搜索脚本或其它本地 build / test / run / stock MCP
→ Slack：返回搜索结果、执行结果或文件
→ Web GPT：根据本地搜索命中的 path / line / snippet，再去 GitHub 精确读取真正需要的完整源码文件
```

CWapi 不运行模型，也不要求 ChatGPT Web 与本机建立直接 MCP 连接。

## 仓库搜索：现在就使用，不新增工具

**v1.6.3 没有 `repo_search`、`repo_read` 或 filesystem MCP，也不需要为仓库搜索再修改 CWapi 二进制或发布新版本。**

代码搜索直接复用现有 repository-scoped `process_start`：

1. Web GPT 理解当前开发问题，决定要搜索的关键词、目录和文件类型；
2. Web GPT 自己生成一个确定性的只读搜索命令/短脚本；
3. 通过 Slack MCP v2 把这个 `process_start` 发给 CWapi，并携带 `repository_url + expected_commit`；
4. CWapi 获取 repository lease，准备/复用该 exact commit 的 persistent workspace；
5. Codex safe backend 在该 workspace 内执行搜索脚本；
6. 搜索结果只返回必要的 `path + line + 少量 snippet`；
7. Web GPT 根据这些定位结果，通过 GitHub Connector 精确读取少量命中的完整文件。

典型链路：

```text
GitHub：repository URL + exact commit
        ↓
Web GPT：生成只读搜索脚本
        ↓
Slack / CWapi process_start
        ↓
persistent workspace 本地搜索
        ↓
path + line + snippet
        ↓
GitHub：精准 fetch 命中文件
        ↓
Web GPT：分析 / 修改
        ↓
GitHub：写入并取得新 exact commit
        ↓
CWapi：build / test / run
```

这项优化替代的是 GitHub Connector 的**宽泛代码搜索/定位阶段**，不是完整文件读取。完整源码仍从 GitHub 精确读取；远端写入、commit、branch/history、PR、Issue、Review、Actions/CI、Release 也继续由 GitHub 负责。

搜索脚本必须保持只读并限制输出。不要把完整源码通过 `process_start` 大量打印回 Slack；public process record 的 stdout/stderr tail 有长度边界，这条路径的目标只是快速告诉 Web GPT“相关代码在哪”。

同一个问题需要多个相关搜索词时，可以把这些只读搜索合并进一个短脚本，一次返回多组结果，以减少 Slack 往返；不要把 build、test、写入、commit 等有副作用步骤混进搜索脚本。

如果 `rg` 不存在，Web GPT 应选择当前环境已经验证可用的 PowerShell `Select-String`、`findstr`、本地 Git 搜索能力或其它只读方法。requested exact commit 已经准备在 workspace 时，搜索脚本不应再次 clone/fetch 或访问 GitHub。

## 已验证的 persistent workspace 依据

2026-08-27 的真实 Slack -> CWapi 测试已经确认：一个 repository request terminal 后，第二个新的 request 使用同一 repository / same `expected_commit` 会重新进入同一个 process-lifetime workspace；测试中第一个 request 留下的临时 untracked marker 被第二个 request 成功读到，同时 tracked source 仍对应声明的 exact commit。测试 marker 随后已清理。

因此，进入 CWapi 本地开发链后，仓库搜索可以直接利用已有 persistent workspace，不需要为了定位源码反复绕回 GitHub 做整仓搜索。

## 开工时必须知道

1. v1.6.3 不使用 project registry。repository 调用直接使用 GitHub repository URL 与完整 40 位 commit。

2. 只使用 CWapi MCP v2：

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2",...}
+++
```

3. 每个新动作使用新的 `request_id`。已经得到 `process_id` 后，后续查询原进程，不重复启动同一任务。

4. Windows path 在 MCP JSON 中优先使用 `/`。

5. 同一 repository 在当前 CWapi 进程内使用 persistent workspace。request terminal 只释放 repository lease，不删除 workspace；衍生物可以在同一 CWapi 进程内复用。

6. 需要代码搜索定位时，优先由 Web GPT 自己写只读搜索脚本，通过 repository-scoped `process_start` 搜本地 workspace；再根据命中路径去 GitHub 精确读取完整文件。

7. 搜索脚本不得额外 clone/fetch 同一 repository，不得混入修改、build、test、install、commit 等副作用操作，除非当前任务本来就是执行这些独立步骤。

8. GitHub 仍是远端源码真相和 exact commit 来源；CWapi 本地搜索只是减少定位阶段的网络/工具往返。

9. 不要自行假设本机环境或固定工具路径。环境发现、SAFE/FULL、安装依赖、浏览器测试、截图和等待规则统一按照 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md) 执行。

## 完整工作流

[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)
