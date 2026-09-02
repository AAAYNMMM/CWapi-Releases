# CWapi Agent Core Protocol

CWapi Agent mode connects Web GPT to OpenAI-compatible requests issued by local software. Web GPT is the reasoning controller; CWapi owns request transport, lifecycle bookkeeping and protocol conversion.

## Bridge and requests

- `agent_open()` opens or resumes the current logical bridge. The internal bridge generation is CWapi-owned.
- `agent_exchange(...)` submits results and receives request work. Every returned request has a stable `request_id` and a `delivery` counter.
- Process every returned request independently. A response must use the matching `request_id`; never mix tool calls, results or completion state between requests in the same batch.
- `delivery > 1` is redelivery of the same request, never a new task.
- A resumed/redelivered request may include `previous_state`, `resume_reason` and `last_activity`; continue from the preserved request context rather than restarting work.
- Bridge lifetime and request lifetime are separate. A temporary bridge loss must not erase an active request.

## Event semantics

CWapi keeps protocol events distinct:

- `heartbeat`: runtime liveness only; it is maintained by CWapi and does not require a natural-language assistant message.
- `progress`: request progress for human observability; it is not heartbeat and is not completion.
- `tool_call`: a model-requested local tool invocation.
- `tool_result`: the local tool's result or structured tool error.
- `completion`: structured terminal success for the current request.
- `error`: protocol/request error information. Retryable errors do not silently terminate an otherwise recoverable request.

## Tool calls and completion

- Tool calls are executed by the local OpenAI-compatible client, not by this MCP server.
- Preserve tool-call IDs and tool-result associations exactly.
- `function.arguments` may arrive as an OpenAI JSON string or an internal JSON object. CWapi canonicalizes it once into an object and later emits the OpenAI string form once.
- Streaming tool-call argument fragments must be concatenated completely before JSON parsing.
- A tool-call parse/mapping failure is returned as a structured error so Web GPT can correct the call. It must not silently strand the request.
- A request is finished only by a structured `completion` state/event, not by matching words such as “completed” in assistant text.
- `agent_close()` closes the current bridge handle; active request state is preserved for resume until it reaches a terminal state or its request lifetime expires.

## Skills

- `load_skill(name)` loads one startup-cached global Skill by its Skill ID.
- Load only Skills listed in the startup Skill inventory. A missing Skill returns `SKILL_NOT_FOUND`.
- Loading a Skill changes neither request state nor local project state.
