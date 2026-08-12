# CWapi 网页 GPT 极短入口

本页是网页 GPT 正常使用 CWapi 的唯一必读入口。先发现状态与能力，再提交业务决策；不要预读完整协议，也不要手写 canonical TASK。

## 1. 固定调用方式

每次调用都新建一封“未发送”的 Gmail Draft，收件人为当前 CWapi Gmail 账号：

- Subject 固定为 `[CWapi/1][REQUEST][PENDING][AUTO]`；
- 正文只放一个 UTF-8 JSON object；
- 不发送邮件，不编辑已处理的 REQUEST；
- 从 `[CWapi/1][RESPONSE][READY|FAILED][request_id]` Draft 读取调用结果。

REQUEST 不提供 `schema`、`request_id`、创建时间、runner、channel 或 workspace。`tasks.create` 也不提供 `task_id`；这些机械字段全部由 backend 生成。

## 2. Context：每个新会话先做一次

```json
{"operation":"context.get"}
```

读取 RESPONSE 中的 `needs_attention`、runtime、projects、recent tasks 和 `control_plane.features`。只调用 `available=true` 且 transports 包含 `gmail` 的 feature；Runner 或目标项目不可用时先处理 context 给出的 attention，不要猜测内部配置。

## 3. Discovery：只发现当前要用的能力

常见流程优先发现 preset；需要自定义步骤时再发现 action：

```json
{"operation":"presets.list"}
```

```json
{"operation":"actions.list"}
```

只对已经选中的一个条目请求详情：

```json
{"operation":"presets.get","name":"python_full_validation"}
```

```json
{"operation":"actions.get","name":"pytest"}
```

返回的 discovery metadata 用于选择和填写参数；真正授权仍由 Action Registry、Task Builder 和正式 validator 决定。不要提交任意 executable、shell、argv、cwd 或 env。

## 4. Create：只提交业务决策

先从 GitHub 确认目标 remote 的精确 40 位 commit SHA。preset 示例：

```json
{"operation":"tasks.create","repository":"username/my-project","expected_commit":"0123456789abcdef0123456789abcdef01234567","preset":"python_full_validation"}
```

单 action 示例：

```json
{"operation":"tasks.create","repository":"username/my-project","expected_commit":"0123456789abcdef0123456789abcdef01234567","action":"pytest","arguments":{"paths":["tests"],"extra_args":["-q"]}}
```

也可使用 discovery 明确允许的 `steps` 或 `preset_parameters`。三种模式互斥；不要自己生成 TASK 的时间、step_id、target runner 或 workspace。READY RESPONSE 返回 backend 生成的 `task_id`；FAILED RESPONSE 按结构化 error 的 `recommended_next_action` 修正。

## 5. Summary：普通成功到此结束

等待 `[CWapi/1][SUMMARY][READY|ATTENTION][task_id]` Draft；需要重新查询时：

```json
{"operation":"results.summary","task_id":"GPT..."}
```

当 `status="completed"` 且 `needs_attention=false` 时，接受该 SUMMARY 作为正常结论，不读取 full RESULT、日志或本地路径。完整 `cwapi.result.v1`、manifest、stdout/stderr 和 artifact 仍由 backend 保存用于审计。

## 6. Attention：只展开被指出的证据

当 `needs_attention=true` 时，先读 `error.code`、`category`、`retryable`、`affected_step`、`details_required` 和 `recommended_next_action`。只在 `details_required=true` 或该动作明确要求时调用 `results.get`：

```json
{"operation":"results.get","task_id":"GPT...","detail":"step","step_id":"step-001"}
```

`detail` 只能是 `full_result`、`step`、`logs` 或 `manifest`；优先 step，随后按需 logs/manifest，最后才取 full_result。不要把路径、命令或文件名当成请求参数，也不要因 SUMMARY 投递或查询失败而重跑已完成 action。

修正失败原因后必须创建新 REQUEST；是否重试、查看哪个 step 以及是否需要人工决定，以结构化 error 的 `recommended_next_action` 为准。
