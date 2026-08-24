# Pinned Codex runtime

CWapi v1.6.1 portable 使用：

```text
version: 0.144.4-cwapi.1
source commit: 8c68d4c87dc54d38861f5114e920c3de2efa5876
codex.exe SHA-256: 51398051c2332b6afe08dc3b9dbb4056085c197f35ca57a307ee303d450cada5
```

binary 不提交到 Git；由 `scripts/install_codex_runtime.ps1` 或已有 portable runtime 安装到 `runtime/codex/current/`。启动 app-server/command backend 前都会校验 hash。

该 runtime 必须支持 model-free `command/exec`、Windows sandbox readiness/setup、per-execution workspace root、structured exit、stdout/stderr 和 Job Object lifecycle。最终 gate 见 `automation/validate_v161_codex_runtime.ps1`。
