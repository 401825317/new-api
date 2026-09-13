# Aliyun Guardrail OpenAI Adapter

这个小服务把阿里云 AI 安全护栏 `MultiModalGuard` 转换成 Sub2API 提示词审计要求的 OpenAI-compatible Qwen3Guard 响应格式。

对外接口：

- `GET /v1/models`
- `POST /v1/chat/completions`
- `GET /healthz`

核心映射：

- `suggestion=pass` -> `Safety: Safe\nCategories: None`
- `suggestion=review` -> `Safety: Controversial\nCategories: ...`
- `suggestion=block` -> `Safety: Unsafe\nCategories: ...`

降级策略：

- 阿里云 Guardrail 正常时，以阿里云结果为准。
- 阿里云欠费、超时、网络异常或返回异常时，默认不向 Sub2API 返回 503。
- 降级时先跑本地高置信高危规则；命中则返回 `Safety: Unsafe`，未命中则返回 `Safety: Safe`。
- 降级事件会写 `guardrail_degraded` 日志，只记录错误码、request_id 和本地规则命中结果，不记录提示词原文。

必要环境变量：

- `ALIYUN_ACCESS_KEY_ID`
- `ALIYUN_ACCESS_KEY_SECRET`

可选环境变量：

- `ALIYUN_ENDPOINT`，默认 `green-cip.eu-central-1.aliyuncs.com`
- `ALIYUN_SERVICE`，默认 `query_security_check_cb`
- `ADAPTER_MODEL`，默认 `aliyun-multimodalguard-qwen3guard`
- `ADAPTER_API_KEY`，设置后要求调用方使用 `Authorization: Bearer ...`
- `FAIL_OPEN_ON_GUARDRAIL_UNAVAILABLE`，默认 `true`，阿里云不可用时走本地兜底并返回标准审计结果
- `LOCAL_FALLBACK_ENABLED`，默认 `true`，启用本地高危规则兜底
- `LOCAL_FALLBACK_DEFAULT_SAFETY`，默认 `Safe`，本地兜底未命中时返回的安全等级
- `SIMULATE_GUARDRAIL_UNAVAILABLE`，默认关闭，仅用于本地/临时验证降级路径
- `PORT`，默认 `8080`

注意：不要把 AK/SK 或 `ADAPTER_API_KEY` 写入仓库，部署时通过平台环境变量注入。
