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

必要环境变量：

- `ALIYUN_ACCESS_KEY_ID`
- `ALIYUN_ACCESS_KEY_SECRET`

可选环境变量：

- `ALIYUN_ENDPOINT`，默认 `green-cip.eu-central-1.aliyuncs.com`
- `ALIYUN_SERVICE`，默认 `query_security_check_cb`
- `ADAPTER_MODEL`，默认 `aliyun-multimodalguard-qwen3guard`
- `ADAPTER_API_KEY`，设置后要求调用方使用 `Authorization: Bearer ...`
- `PORT`，默认 `8080`

注意：不要把 AK/SK 或 `ADAPTER_API_KEY` 写入仓库，部署时通过平台环境变量注入。
