# new-api Backend Project Map

## 项目定位

new-api 是 Go + Gin + GORM 实现的大模型中转站，提供用户管理、渠道管理、统一 OpenAI-compatible relay、计费和管理台。本工作区中的这个分支增加了 ClawX 桌面客户端兼容接口，用于支持 ClawX 的托管登录、设备激活、relay token、充值和更新 feed。

仓库信息：

- 本地目录：`backend/`
- origin：`https://github.com/401825317/new-api.git`
- upstream：`https://github.com/QuantumNous/new-api.git`
- 当前分支：`feature/clawx-newapi-adapter`
- 后端语言：Go，模块名 `github.com/QuantumNous/new-api`
- 前端控制台：`web/default/` 为新版 React 控制台，`web/classic/` 为经典控制台

## 先读文件

- `AGENTS.md`：new-api 原项目规则，包含 JSON 包装、跨数据库兼容、PR 规则和受保护品牌信息。
- `go.mod`：Go 版本和依赖。
- `router/clawx-router.go`：ClawX 兼容路由总入口。
- `controller/clawx.go`：ClawX bootstrap、登录、注册、设备、relay token、充值、更新 feed 逻辑。
- `model/clawx.go`：ClawX 设备、会话、激活 ticket 数据模型。
- `middleware/clawx_auth.go`：ClawX access token 鉴权中间件。
- `setting/clawx_client_setting/`：ClawX 桌面客户端轻量配置，包括重要公告和帮助与客服多联系人二维码。
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
- `GET /client-config`：返回 ClawX 客户端重要公告和帮助与客服多联系人二维码配置。当前是轻量配置能力，不做用户私信、服务端已读或实时长连接。

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

## ClawX 环境变量

- `CLAWX_PUBLIC_ORIGIN`：ClawX 后端公开 origin，默认 `https://zz-cn.lingzhiwuxian.com`。
- `CLAWX_PROVIDER_BASE_URL`：模型 relay base URL，默认等于 public origin + `/v1`。
- `CLAWX_PROVIDER_KEY`：ClawX runtime provider key，默认 `lingzhiwuxian`。
- `CLAWX_PROVIDER_NAME`：显示名，默认 `灵智无限`。
- `CLAWX_DEFAULT_MODEL`：默认模型，默认 `qwen-latest`。
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
