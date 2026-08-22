# new-api Backend Project Map

## 项目定位

new-api 是 Go + Gin + GORM 实现的大模型中转站，提供用户管理、渠道管理、统一 OpenAI-compatible relay、计费和管理台。本工作区中的这个分支增加了 ClawX 桌面客户端兼容接口，用于支持 ClawX 的托管登录、设备激活、relay token、充值和更新 feed。

仓库信息：

- 本地目录：`backend-newapi/`
- origin：`https://github.com/401825317/new-api.git`
- upstream：`https://github.com/QuantumNous/new-api.git`
- 当前分支：`v1.0.0`（zz-cn 生产和管理台主线）
- 后端语言：Go，模块名 `github.com/QuantumNous/new-api`
- 前端控制台：`web/default/` 为新版 React 控制台，`web/classic/` 为经典控制台

## 先读文件

- `AGENTS.md`：new-api 原项目规则，包含 JSON 包装、跨数据库兼容、PR 规则和受保护品牌信息。
- `go.mod`：Go 版本和依赖。
- `router/clawx-router.go`：ClawX 兼容路由总入口。
- `controller/clawx.go`：ClawX bootstrap、登录、注册、设备、relay token、充值、更新 feed 逻辑。
- `controller/clawx_observability.go`：Sentry Envelope 隧道、大小限制、超时和限流。
- `model/clawx.go`：ClawX 设备、会话、激活 ticket 数据模型。
- `model/log.go` / `controller/log.go`：UClaw 请求版本诊断落库与管理员聚合查询。
- `middleware/clawx_auth.go`：ClawX access token 鉴权中间件。
- `middleware/distributor.go`：托管产物模型别名鉴权及固定上游模型替换。
- `setting/clawx_client_setting/`：ClawX 桌面客户端配置、校验和公开投影，包括模型、可观测性及产物功能开关。
- `web/default/AGENTS.md`：如果改 new-api 新版管理台前端，先读这个文件。

## 关键目录

- `router/`：HTTP 路由注册，`api-router.go` 挂载 `/api`，`relay-router.go` 挂载 `/v1` relay，`clawx-router.go` 挂载 ClawX 兼容 API。
- `controller/`：HTTP handler。ClawX 适配主要在 `controller/clawx.go`，release 管理在 `controller/clawx_release.go`。
- `model/`：GORM 模型和数据库访问，`model/main.go` 负责 AutoMigrate。
- `middleware/`：鉴权、限流、CORS 等中间件。
- `relay/`：模型请求中转核心，包括 OpenAI/Claude/Gemini 等协议转换。
- `service/`：业务服务、渠道亲和、OpenAI-compatible 辅助逻辑。
- `setting/`：系统、模型、计费、性能等配置。
- `common/`：JSON、数据库、Redis、环境变量、配额、工具函数。
- `dto/`、`types/`、`constant/`：请求/响应结构、通用类型、常量。
- `i18n/`：后端国际化。
- `web/default/`：新版管理台，React 19 + TypeScript + Rsbuild + Tailwind。
- `web/classic/`：经典管理台。
- `docs/`：部署、OpenAPI、渠道说明。

## ClawX 兼容 API

路由前缀为 `/api/clawx`：

- `GET /bootstrap`：返回 ClawX 托管分发配置，包含 service、auth、runtime、offline、skills。
- `POST /activation/check`：校验兑换/激活码，返回 activation ticket。
- `POST /verification/send-code`：发送注册验证码。
- `POST /register`：注册用户、绑定设备、创建 ClawX session。
- `POST /login`：登录用户、绑定设备、创建 ClawX session。
- `POST /auth/refresh`：刷新 access token 和 refresh token。
- `POST /auth/logout`：撤销 refresh token 对应 session。
- `POST /auth/verify`：鉴权后校验当前设备/授权状态。
- `POST /auth/unregister-device`：撤销当前设备及其 session。
- `POST /relay-token`：为当前 ClawX 设备创建或返回 relay API key。
- `GET /user/self`：返回当前用户信息。
- `GET /billing/checkout-info`：返回充值页信息。
- `POST /billing/orders`：创建充值订单。
- `POST /billing/orders/verify`：校验订单支付状态。
- `GET /updates/latest`：返回最新版本信息。
- `GET /updates/feed/:channel/*file`：返回 Electron updater feed。
- `GET /client-config`：返回 ClawX 模型策略、默认推理等级、公告、客服、`observability` 和 `features`。默认推理等级存储在 `clawx_client_setting.model_options` 的 `text.defaultThinkingLevel`，下发路径为 `modelOptions.text.defaultThinkingLevel`；该动态接口必须禁用长期 HTTP 缓存。
- `POST /observability/envelope`：受远程总开关、请求大小、IP 和安装标识限流保护的 Sentry Envelope 隧道。

管理员 API 另有 `GET /api/log/uclaw/version-stats`，按版本和构建身份聚合 UClaw consume/error 日志；它不在 `/api/clawx` 路由组内。

### 客户端配置与灰度契约

`GET /api/clawx/client-config` 的七类治理相关字段为：

- `modelOptions.text.models[].visible`：`false` 时模型仍进入托管运行时目录，但客户端普通模型选择器不展示。版本化产物别名（例如 `uclaw-artifact-v1`）应同时配置为启用且不可见。
- `modelOptions.image`：服务端下发图片模型、可用尺寸、质量与默认值；但当前桌面 Chat 输入框仍保留尺寸和质量的静态回退候选，不能宣称图片参数选择已经完全由服务端配置驱动。
- `observability`：独立于模型配置，包含 `enabled`、公开 Sentry DSN、固定隧道路径、崩溃/已处理异常/性能/产物任务采样率和每安装实例每小时事件上限。
- `features.artifacts`：包含总开关、`rolloutPercentage`、版本化 `modelAlias` 和 `policyVersion`。仅公开别名与策略版本；固定 `upstreamModel` 只保留在服务端配置中。
- `features.ecommerceMainImage`：包含独立开关、`rolloutPercentage` 和 `skillVersion`，并强制依赖 `features.artifacts.enabled`。

配置解析和语义校验采用 fail-closed：无效可观测性配置回退为关闭；无效功能配置会回退为全部关闭。Sentry DSN 不允许密码/私密 userinfo、query 或 fragment；开启产物能力时必须配置非别名形式的固定上游模型。

百分比灰度由客户端使用稳定安装 ID 分桶执行，当前服务端不按百分比或内部账号名单再次分桶。Relay 侧只对 `uclaw-artifact-v*` 保留命名空间做授权：必须精确匹配当前别名、全局产物开关已启用，并且请求 Token 关联有效 UClaw 设备；通过后才把 JSON 请求中的别名替换为固定上游模型。因此服务端授权不能证明某客户端命中了灰度桶，若需要强制灰度隔离，还需补服务端分桶或签名资格声明。

当前只有产物总能力和电商主图具备独立远程开关。HTML 实时自动预览和本地长期规则没有独立远程开关，不应在发布说明中宣称七项能力均可单独远程停止。

### 登录、注册、设备授权契约

- ClawX session、设备授权、relay token/API key 必须分开处理。
- 注册新账号可以要求激活码，并在注册事务内消费激活码、绑定首台设备、发放注册/激活额度。
- 已有账号登录时先校验账号密码；已授权设备直接登录，不再要求激活码。
- 如果账号密码正确但当前设备不存在或已撤销，且 `CLAWX_ACTIVATION_REQUIRED=true`，`POST /api/clawx/login` 返回 403，`code/errorCode=device_authorization_required`。客户端应提示用户输入激活码授权当前设备，再重试登录。
- 设备授权登录消耗激活码并绑定设备，但不创建新账号、不重复发首注册送额度。
- ClawX 兼容接口错误应返回稳定 `code` / `errorCode` / `message`，前端按错误码做本地化展示。

兼容路由：

- `/api/v1/auth/refresh`
- `/api/v1/auth/logout`
- `/api/v1/auth/me`

### 可观测性隧道边界

- `POST /api/clawx/observability/envelope` 在 `observability.enabled=false` 时返回 404，可作为服务端远程停止开关；客户端配置是拉取式，已运行的 Sentry SDK 不会因服务端开关变化而立即自停，但后续事件会被隧道拒绝。
- 单个请求体最大 5 MB，Sentry 上游请求超时 15 秒；IP 上限为每分钟 120 次，安装实例上限来自配置且服务端校验为 1–30 次/小时。`install_id` 必须是客户端原始安装 ID 的 64 位 SHA-256 十六进制摘要。
- 开启 Redis 时限流计数经 Redis Lua 脚本共享；未开启 Redis 时退化为当前 Go 进程内的滑动窗口，多实例之间不会共享计数。
- 隧道只重建 Sentry Endpoint、复制 Envelope 内容类型和可选压缩编码，并转发限流响应头；它不会记录 DSN 私密信息，也不会把 Sentry 上游错误正文返回客户端。
- Envelope 内容在客户端发送前脱敏，但隧道当前原样转发请求体，不做服务端二次解析或脱敏。客户端脱敏基于字段名、凭证模式和常见用户目录模式，不能视为对任意字符串、UNC 路径或非标准用户路径的绝对保护。
- 长期规则没有对应 zz-cn 存储字段，规则正文也不会由长期规则功能写入使用日志或 Sentry 元数据；但规则投影到客户端 `AGENTS.md` 后会成为正常模型请求的指令上下文，仍受模型 relay 的既有数据处理边界约束。

### UClaw 版本诊断

客户端只向精确配置的 UClaw Origin 注入 `X-UClaw-Client: desktop`、`X-UClaw-Version`、`X-UClaw-Commit`、`X-UClaw-Build-Id`、`X-UClaw-Platform`、`X-UClaw-Arch`、`X-UClaw-Channel`、`X-UClaw-Mode`、`X-Request-Id` 和哈希后的 `X-UClaw-Install-Id`。后端当前只把客户端标记、版本、Commit、Build ID、平台、架构、渠道、运行模式和请求 ID 写入 consume/error 日志的 `other.client_diagnostics`；安装 ID 仅用于可观测性限流，不进入该版本诊断对象。

管理员接口 `GET /api/log/uclaw/version-stats` 默认查询最近 24 小时，最大时间跨度 31 天，最多扫描最新 200,000 条候选 consume/error 日志。结果按版本、Commit、Build ID、平台、架构、渠道和运行模式分组，输出请求数、成功/错误数与比例、平均延迟和 p95；延迟依次使用 `end_to_end_upstream_response_ms`、`upstream_response_ms`、日志 `use_time`。超过扫描上限时返回 `truncated=true`，不能把该结果当作完整历史统计。

Source Map 不由 zz-cn 动态生成。客户端构建会注入 Debug ID；只有发布环境同时提供 `SENTRY_URL`、`SENTRY_AUTH_TOKEN`、`SENTRY_ORG` 和 `SENTRY_PROJECT` 时，after-pack 才创建 release、上传并校验 Source Map，全部缺失时明确跳过，部分缺失时构建失败。

## ClawX 环境变量

- `CLAWX_PUBLIC_ORIGIN`：ClawX 后端公开 origin，默认 `https://zz-cn.lingzhiwuxian.com`。
- `CLAWX_PROVIDER_BASE_URL`：模型 relay base URL，默认等于 public origin + `/v1`。
- `CLAWX_PROVIDER_KEY`：ClawX runtime provider key，默认 `lingzhiwuxian`。
- `CLAWX_PROVIDER_NAME`：显示名，默认 `零至无限`。
- `CLAWX_DEFAULT_MODEL`：默认模型，默认 `smart-latest`（智能路由）。
- `CLAWX_FALLBACK_MODELS`：逗号分隔的 fallback 模型。
- `CLAWX_MODEL_FAMILIES`：逗号分隔的模型族配置，格式 `id:name`。
- `CLAWX_REGISTRATION_ENABLED`、`CLAWX_LOGIN_ENABLED`、`CLAWX_ACTIVATION_REQUIRED`：控制注册、登录、激活要求。
- `CLAWX_OFFLINE_GRACE_SECONDS`、`CLAWX_VERIFY_MEMORY_CACHE_SECONDS`：ClawX 离线校验策略。
- `CLAWX_SKILL_MARKETPLACE_ENABLED`：是否开启远程技能市场。

## 常用命令

后端 docker dev stack：

```bash
make dev-api
```

后端本地运行：

```bash
go run main.go
```

构建前端控制台：

```bash
make build-frontend
```

运行 Go 测试：

```bash
go test ./...
```

新版管理台前端：

```bash
cd web
bun install
cd default
bun run dev
```

## 开发注意

- 业务代码不要直接使用 `encoding/json` 的 marshal/unmarshal，按 `AGENTS.md` 使用 `common/json.go` 包装。
- 数据库改动必须同时兼容 SQLite、MySQL、PostgreSQL。
- ClawX session 和 relay token 是两个概念：session 用于 `/api/clawx/*` 鉴权，relay token/API key 用于 `/v1/*` 模型中转。
- ClawX access token 通过 `Authorization: Bearer <token>` 传入，由 `middleware.ClawXAuth()` 校验。
- 修改 billing expression 前必须读 `pkg/billingexpr/expr.md`。
- 不要移除或改名 new-api/QuantumNous 相关受保护标识。
