# harbor-services 技术详细设计（Tenant / Auth / Admin）

| 属性 | 内容 |
|------|------|
| 文档版本 | v1.0 |
| 关联文档 | `intro_01.md`、`arch_01.md` |
| 参考工程 | `./wachi-auth`（行为与安全语义对齐；Go 重写） |
| 文档范围 | **租户(APP)管理**、**统一注册/登录(Auth)**、**Admin 管理后台** |
| 本期范围 | 上述三模块 P0 可落地设计；**Billing / Online Operations 本期不实现业务**，本文给出扩展契约 |
| 读者 | 中台后端 / Admin 前端 / 评审 |

---

## 0. 设计原则与变更相对 wachi-auth

### 0.1 原则

1. **Tenant 为唯一应用权威源**：从 wachi-auth 抽出 App 创建/管理，供 Auth、Billing、Ops 共用。
2. **配置按模块拆分**：App 主数据与 Auth/Billing/Ops 配置分离存储，避免「一个大 App 文档」阻碍扩展。
3. **安全语义对齐 wachi-auth**：错误码、JWT、Refresh Rotation、BYO OAuth、Admin 白名单、`require_active_app` 等保持兼容或可映射。
4. **Admin 薄、领域厚**：Admin 只做鉴权聚合与 HTTP 适配；CRUD 落在 Tenant / AuthConfig /（预留）BillingConfig / OpsConfig 应用服务。
5. **依赖倒置**：领域不依赖 Gin/Firestore；模块间通过接口协作，禁止 Billing 直接改 Auth 集合。

### 0.2 相对 wachi-auth 的关键差异

| 项 | wachi-auth | harbor-services |
|----|------------|-----------------|
| 语言/框架 | Python / FastAPI | Go / Gin |
| App 归属 | Auth 域内实体 | **独立 Tenant 模块** |
| OAuth 凭证存放 | 嵌在 `App` 实体同文档 | **`app_auth_configs`（或子文档）**，Tenant 主文档不含敏感 OAuth 字段 |
| 引导租户 ID | `wachiAdmin` | **`harborAdmin`** |
| 管理面配置 | App 更新接口混写 OAuth | Admin 分资源：`/apps`、`/auth-config`、`/billing-config`、`/ops-config` |
| `settings` | 单层 opaque dict | 保留在 Tenant 主数据作**平台级扩展袋**；模块配置走各自 config 文档 |
| Billing / Ops | 无 | 配置 API / 仓储接口 / 集合名预留 |

### 0.3 模块依赖（编译期方向）

```
admin  ──► tenant, auth (config use cases), billing(stub), ops(stub)
auth   ──► tenant          （RequireActiveApp / GetApp）
billing──► tenant          （后续；可读 Auth UserID，不反向依赖 Admin）
ops    ──► tenant          （后续）
tenant ──► （无业务模块依赖）
shared ◄── 各模块
```

---

## 1. 包结构与分层

```
harbor-services/
├── cmd/server/main.go
├── internal/
│   ├── platform/                 # config, logging, middleware, errors envelope
│   ├── shared/
│   │   ├── crypto/               # bcrypt, Fernet/AES 封装
│   │   ├── jwt/
│   │   ├── idgen/
│   │   └── cache/
│   ├── tenant/
│   │   ├── domain/
│   │   ├── application/
│   │   ├── infrastructure/       # Firestore AppRepository + CachedAppRepository
│   │   └── presentation/         # 仅内部/测试暴露时可为空；对外经 Admin
│   ├── auth/
│   │   ├── domain/
│   │   ├── application/          # register/login/oauth/refresh/...
│   │   ├── infrastructure/       # users, tokens, oauth providers
│   │   └── presentation/         # /api/v1/auth, /user, /oauth, jwks
│   ├── admin/
│   │   ├── application/          # 编排：登录校验、详情聚合
│   │   └── presentation/         # /api/v1/admin/*
│   ├── billing/                  # P0 后期 / 并行：本期可空包 + stub handler
│   └── ops/                      # P1：空包 + 501 stub
├── scripts/seed/                 # 创建 harborAdmin + 管理员账号
└── deployments/
```

每层职责同 `arch_01.md`：`presentation → application → domain ← infrastructure`。

---

## 2. 通用约定

### 2.1 响应信封

与 wachi-auth 对齐，便于客户端迁移：

```json
{
  "code": 0,
  "message": "ok",
  "data": {},
  "request_id": "req_xxx"
}
```

- 中间件注入 `X-Request-Id` / `request_id`。
- 业务错误：`code != 0`，HTTP 状态与 wachi-auth 错误码表一致（见 §8）。

### 2.2 鉴权类型

| 类型 | 用法 |
|------|------|
| 无 | 注册/登录/OAuth authorize/callback/refresh 等 |
| Bearer Access Token | `/user/*`、部分 Auth、全部 `/admin/*` |
| App Secret | Token Introspection（`Basic app_id:app_secret` 或 Header） |
| Admin | Bearer + JWT.`app_id` == `ADMIN_APP_ID` + email ∈ `ADMIN_EMAILS`（fail-closed） |

### 2.3 配置项（环境变量，节选）

| 变量 | 说明 | 默认建议 |
|------|------|----------|
| `ADMIN_APP_ID` | 管理租户 | `harborAdmin` |
| `ADMIN_EMAILS` | JSON 数组，小写邮箱 | `[]`（空则禁止一切 Admin） |
| `JWT_ALG` | `RS256`（推荐） | RS256 |
| `ACCESS_TOKEN_TTL` | 秒 | `7200` |
| `REFRESH_TOKEN_TTL` | 秒 | `2592000`（30d） |
| `ENCRYPTION_KEY` | 配置类密钥加密 | 必填（生产） |
| `GCP_PROJECT_ID` / `FIRESTORE_*` | 数据存储 | — |
| `BCRYPT_COST` | | `12` |
| `APP_CACHE_TTL_SEC` | | `300` |

---

## 3. 租户（APP）管理 — 详细设计

### 3.1 职责边界

| 负责 | 不负责 |
|------|--------|
| App 主数据 CRUD、状态机、`app_secret` 轮换 | 用户注册登录 |
| `RequireActiveApp` / `VerifyAppSecret` | OAuth 凭证明文读写（委托 AuthConfig） |
| App 列表（Admin） | Billing 商品/订单 |
| 平台级 `settings`（opaque） | Ops 业务配置内容 |

### 3.2 领域模型

#### 3.2.1 `App`（Tenant 主实体）

```go
type AppStatus string
const (
    AppStatusActive    AppStatus = "active"
    AppStatusDisabled  AppStatus = "disabled" // 对齐 wachi-auth soft-disable
)

type App struct {
    AppID         string
    AppSecretHash string
    AppName       string
    RedirectURIs  []string          // Auth OAuth 回调白名单（Tenant 持有，Auth 读取）
    Status        AppStatus
    Settings      map[string]any    // 平台扩展袋；不放模块密钥
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

func (a *App) IsActive() bool { return a.Status == AppStatusActive }
```

**设计说明**：

- `redirect_uris` 留在 Tenant：多个模块可能校验回调（Auth 必须；Billing 托管收银台回调未来也可能复用模式），避免 AuthConfig 与主数据双源。
- **不在此实体嵌入** Google/Apple 密钥（相对 wachi-auth 拆分）。
- `settings` 示例（约定键，本期可不强制执行）：`allow_register`、`token_ttl`、`feature_flags`；Billing/Ops 正式配置仍走独立文档。

#### 3.2.2 生命周期

```
create(active) ──► active ⇄ disabled
                      │
                      └── soft disable（DELETE admin API）
```

- **禁用**：`RequireActiveApp` 失败 → `2001`；已签发 Access 在下次 `get_current_user` / refresh / introspect 时因 App 非 active 失败。
- **不物理删除** App 文档，保留对账与用户数据归属。

#### 3.2.3 ID 与密钥

| 项 | 规则 |
|----|------|
| 业务 AppID | `app_` + URL-safe 随机 12 字节（对齐 wachi-auth） |
| 引导 AppID | 固定 `harborAdmin`（seed） |
| `app_secret` | 创建/轮换时生成 URL-safe 32；**仅响应一次明文**；存储 bcrypt hash |
| 校验 | `VerifyAppSecret(appID, secret)`：active + bcrypt；失败 `2002` |

### 3.3 仓储与缓存

#### 3.3.1 接口

```go
type AppRepository interface {
    Create(ctx context.Context, app *App) error
    GetByID(ctx context.Context, appID string) (*App, error)
    List(ctx context.Context, filter ListAppsFilter) ([]*App, error)
    Update(ctx context.Context, app *App) error
    // SoftDisable: status=disabled
    SoftDisable(ctx context.Context, appID string) error
}
```

Firestore：集合 `apps`，文档 ID = `app_id`。

#### 3.3.2 `CachedAppRepository`

- Key：`app:{app_id}`，TTL = `APP_CACHE_TTL_SEC`（默认 300s）。
- **仅缓存 active**；disabled / miss 不缓存（保证停用尽快生效）。
- `Update` / `SoftDisable` / secret 轮换后 **Invalidate**。
- Auth / Billing / Admin 一律经此包装读取主数据。

#### 3.3.3 应用服务（跨模块契约）

```go
// 供 Auth、Billing、Ops、中间件调用
func RequireActiveApp(ctx context.Context, repo AppRepository, appID string) (*App, error)
// 不存在或非 active → AppNotFound (2001)

func VerifyAppSecret(ctx context.Context, repo AppRepository, hasher PasswordHasher, appID, secret string) (*App, error)
// 失败 → InvalidAppSecret (2002)
```

### 3.4 Tenant 用例（由 Admin 触发）

| Use Case | 行为要点 |
|----------|----------|
| `CreateApp` | 生成 `app_id`/`app_secret`；写 `apps`；**可选**同时初始化空 `app_auth_configs` / 预留 billing/ops 占位文档 |
| `ListApps` | 默认列出 `active`；支持 `include_disabled`（Admin） |
| `GetApp` | 主数据；**不含** OAuth/Billing 密钥 |
| `UpdateApp` | 可更新 `app_name`、`redirect_uris`、`settings`、`status`；`settings` **整对象替换**（对齐 wachi-auth，文档标明） |
| `RotateAppSecret` | 新 secret，旧 hash 失效 |
| `DisableApp` | soft disable + cache invalidate |

创建成功后可同步调用各模块 `EnsureDefaultConfig(appID)`（Auth 写空配置；Billing/Ops no-op 或写 `enabled:false` 占位），保证 App 详情页各 Tab 有稳定资源。

### 3.5 为 Billing / Ops 预留的 Tenant 扩展点

| 扩展点 | 约定 |
|--------|------|
| `App.Settings` | 非敏感、跨模块展示的开关/元数据 |
| `EnsureDefaultConfig` | 创建 App 时钩子列表（注册 Billing、Ops 实现） |
| `OnAppDisabled` 领域事件（可选） | Billing 停止新单、Ops 只读等；P0 可用同步钩子接口 |
| 禁止 | 在 `apps` 文档堆积 `stripe_key`、`mor_api_key`、大量 ops JSON |

```go
type AppLifecycleHook interface {
    OnAppCreated(ctx context.Context, app *tenant.App) error
    OnAppDisabled(ctx context.Context, appID string) error
}
```

P0：Auth 注册 `OnAppCreated` → 确保 auth config 文档存在；Billing/Ops 后续实现同一接口即可，**无需改 Tenant 核心**。

---

## 4. 统一注册/登录（Auth）— 详细设计

### 4.1 职责边界

| 负责 | 不负责 |
|------|--------|
| 邮箱注册/登录、验证、重置密码 | App 主数据 CRUD |
| Google / Apple OAuth（BYO） | Billing 结账 |
| JWT 双令牌、Refresh 家族、用户资料 | Admin UI 聚合（仅提供 AuthConfig 读写用例） |
| Token Introspection、JWKS | |

**强依赖**：所有入口 `RequireActiveApp`；OAuth 与 authorize 使用 Tenant 的 `redirect_uris` + AuthConfig 凭证。

### 4.2 领域模型

#### 4.2.1 `User`

对齐 wachi-auth：

| 字段 | 说明 |
|------|------|
| `user_id` | URL-safe 16 |
| `app_id` | 租户 |
| `email` | 小写归一化；可空（纯 OAuth） |
| `email_verified` | bool |
| `password_hash` | 可空 |
| `nickname` / `avatar_url` / `phone` | 可选 |
| `status` | `unverified` / `active` / `disabled` / `deleted` |
| `global_user_id` | **预留**跨产品 SSO，本期不实现 |
| `login_fail_count` / `locked_until` | 防暴力 |
| `token_version` | 登出/改密/重置/注销时 +1 |
| `created_at` / `updated_at` / `last_login_at` | |

唯一性：`(app_id, email)` 在邮箱非空时唯一。

#### 4.2.2 `OAuthAccount`

`(app_id, provider, provider_user_id)` 唯一；`provider ∈ {google, apple}`。

#### 4.2.3 `RefreshTokenRecord`

| 字段 | 说明 |
|------|------|
| `token_id` | 文档 ID |
| `user_id` / `app_id` | |
| `token_hash` | SHA-256(refresh JWT) |
| `family_id` | 旋转家族；盗用时整族吊销 |
| `expires_at` / `revoked` / `created_at` | |

#### 4.2.4 `VerificationToken`

`token_type`: `email_verification` \| `password_reset`；存 hash；一次性。

#### 4.2.5 `AppAuthConfig`（从 App 拆出）

```go
type AppAuthConfig struct {
    AppID                        string
    GoogleClientID               *string
    GoogleClientSecretEncrypted  *string
    AppleClientID                *string
    AppleTeamID                  *string
    AppleKeyID                   *string
    ApplePrivateKeyEncrypted     *string
    UpdatedAt                    time.Time
}

func (c *AppAuthConfig) GoogleConfigured() bool { ... }
func (c *AppAuthConfig) AppleConfigured() bool  { ... }
```

Firestore：推荐集合 `app_auth_configs`，文档 ID = `app_id`（1:1）。  
敏感字段：Fernet/AES-GCM（`ENCRYPTION_KEY`）加密后入库。  
读接口对外只返回 public id + `google_configured` / `apple_configured`；**GET 永不返回解密后的 secret/私钥**（修正 wachi-auth GET 可能泄露的问题）。

### 4.3 Auth 数据集合与索引

| 集合 | Doc ID | 索引 |
|------|--------|------|
| `users` | `user_id` | `(app_id, email)`；`(app_id, status, created_at DESC)` |
| `oauth_accounts` | `account_id` | `(app_id, provider, provider_user_id)` |
| `refresh_tokens` | `token_id` | `(user_id, revoked)`；`(family_id, revoked)`；TTL `expires_at` |
| `verification_tokens` | `token_id` | `(user_id, token_type, used)`；TTL `expires_at` |
| `app_auth_configs` | `app_id` | — |

### 4.4 安全设计（对齐 wachi-auth）

#### 4.4.1 JWT

| Token | TTL | 关键 Claims |
|-------|-----|-------------|
| Access | 2h | `iss`, `sub`=user_id, `aud`=`app_id`, `app_id`, `email`, `role`, `type=access`, `tv`, `jti`, `iat`/`exp` |
| Refresh | 30d | `sub`, `app_id`, `type=refresh`, `jti`, `family`, `iat`/`exp` |

- 算法：**RS256** + `GET /.well-known/jwks.json`（`kid`）；便于业务服务本地验签。
- Access 校验路径还需：用户存在且 active、`tv` 匹配、**App active**。

#### 4.4.2 Refresh Rotation + 盗用检测

```
客户端持有 RT0
     │ refresh
     ▼
验签 RT0 → 查库 hash
  ├─ 有效未吊销 → 吊销 RT0，签发 AT1+RT1（同 family_id），存 RT1 hash
  └─ 已吊销/不存在（疑似重放）→ revoke_family(family_id) → 1007
```

#### 4.4.3 密码与锁定

- bcrypt cost 12；密码规则：≥8，含大小写与数字。
- 连续失败 5 次锁定 15 分钟 → `1006`。
- 未验证邮箱登录 → `1004`；disabled → `1005`；deleted → `1003`。

#### 4.4.4 限流（建议默认，内存或 Redis 后续）

| 维度 | 限额 |
|------|------|
| 全局 IP | 100/min |
| 注册 email+app | 5/hour |
| 登录 IP | 20/min |
| 忘记密码 email+app | 3/hour |
| Introspect app+IP | 100/min |

超限 → `1010`。

#### 4.4.5 OAuth / redirect

- `redirect_uri` **精确匹配** `App.RedirectURIs`（含尾斜杠）→ 否则 `2006`。
- Provider 未配置完整凭证 → `2007`。
- Authorize 生成 CSRF `state`（短时存储或签名 state）。

### 4.5 核心流程

#### 4.5.1 邮箱注册

```
POST /auth/register { app_id, email, password, nickname? }
  → RequireActiveApp
  → （可选）settings.allow_register == false 则拒绝
  → 邮箱占用 → 1002
  → 创建 User(status=unverified) + VerificationToken(24h)
  → 发验证邮件（Email Adapter）
  → 不返回 Token（对齐：先验证；若产品要求注册即登录可配置，默认先验证）
```

> 实现期与 wachi-auth 现行行为对齐：以参考工程 `RegisterUseCase` 为准（若其注册后不发 token，则 harbor 一致）。

#### 4.5.2 邮箱登录

```
POST /auth/login { app_id, email, password }
  → RequireActiveApp → 查用户 → 状态/锁定校验 → 验密
  → 成功：清 fail_count，发 Access+Refresh，写 RefreshTokenRecord
  → 失败：fail_count++，必要时设 locked_until → 1003/1006
```

#### 4.5.3 OAuth 登录

```
GET  /auth/oauth/{provider}/authorize?app_id&redirect_uri
POST /auth/oauth/{provider}/callback { app_id, code|id_token, redirect_uri?, state }
```

回调账号关联顺序（对齐 wachi-auth）：

1. 命中 `(app_id, provider, provider_user_id)` → 登录  
2. 否则邮箱已存在且 ACTIVE → 自动 link + 登录  
3. 否则创建 User(`active`) + OAuthAccount + 发双令牌（OAuth 用户跳过邮箱验证）

Link / Unlink：需 Bearer；解绑不可移除唯一登录手段 → `2005`；provider 已绑其他用户 → `1009`。

#### 4.5.4 `get_current_user` 中间件

```
Bearer → 验签 → type=access → 加载 User
  → tv 匹配 → status active → RequireActiveApp(payload.app_id)
  → 注入 Context
```

### 4.6 Auth HTTP API 清单（P0）

路径前缀 `/api/v1`，行为与字段对齐 `wachi-auth/README_API.md`。

| Method | Path | 鉴权 |
|--------|------|------|
| POST | `/auth/register` | 无 |
| POST | `/auth/login` | 无 |
| POST | `/auth/refresh` | 无 |
| POST | `/auth/logout` | Bearer |
| POST | `/auth/verify-email` | 无 |
| POST | `/auth/forgot-password` | 无 |
| POST | `/auth/reset-password` | 无 |
| GET | `/auth/oauth/{provider}/authorize` | 无 |
| POST | `/auth/oauth/{provider}/callback` | 无 |
| POST | `/auth/oauth/{provider}/link` | Bearer |
| DELETE | `/auth/oauth/{provider}/unlink` | Bearer |
| POST | `/oauth/introspect` | App Secret |
| GET | `/user/me` | Bearer |
| POST | `/user/me` | Bearer |
| POST | `/user/me/password` | Bearer |
| POST | `/user/me/email` | Bearer |
| GET | `/user/me/account-links` | Bearer |
| DELETE | `/user/me` | Bearer |
| GET | `/.well-known/jwks.json` | 无 |

### 4.7 AuthConfig 应用服务（供 Admin）

| Use Case | 说明 |
|----------|------|
| `GetAuthConfig` | 返回公开字段 + configured 布尔 |
| `UpdateAuthConfig` | 可选更新 Google/Apple 字段；写入前加密 secret/私钥；空值表示不修改；显式清理策略用独立 flag（如 `clear_google_secret`） |
| `EnsureAuthConfig` | App 创建钩子 |

**OAuthProviderResolver**：按 `AppAuthConfig` 解密并构建 Google/Apple Provider；无全局 Client。

### 4.8 Auth 对 Billing 的扩展契约

| 契约 | 说明 |
|------|------|
| JWT `sub` = `user_id` | Billing 订单可关联付款用户 |
| Introspect / JWKS | 业务服务与 Billing 校验终端用户 |
| `App Secret` | Billing 服务端机机调用时可复用同一 App 凭证体系，或未来发独立 `wb_` API Key（Billing 模块内，不改 Auth） |
| `global_user_id` | 预留；Billing 跨 App 聚合勿依赖本期字段 |

Auth **不**依赖 Billing 包。

---

## 5. Admin 管理后台 — 详细设计

### 5.1 职责

1. **登录**：无独立账号体系；走 Auth `POST /auth/login`，约束见下。  
2. **App 管理**：创建 / 列表 / 详情 / 更新 / 停用 / 轮换 secret。  
3. **App 详情配置**：Auth 登录配置；Billing 配置（本期可 stub）；Ops 配置（P1 stub）。

Admin 前端可独立仓库；本设计只定义 **Admin API**。

### 5.2 登录与鉴权

#### 5.2.1 登录流程（对齐 wachi-auth Admin）

```
Admin FE → POST /api/v1/auth/login
           { "app_id": "<ADMIN_APP_ID>", "email", "password" }
         ← access_token + refresh_token

后续请求 → Authorization: Bearer <access_token>
         → Admin 中间件：
              1. get_current_user
              2. payload.app_id == ADMIN_APP_ID（默认 harborAdmin）
              3. lower(email) ∈ ADMIN_EMAILS
              否则 2004 Forbidden
```

- `ADMIN_EMAILS` 为空 → **全部拒绝**（fail-closed）。
- 不提供 Admin 注册接口；账号由 `scripts/seed` 写入。
- Admin FE「只有登录页」，无业务注册。

#### 5.2.2 Seed

```
1. 确保 App{app_id=harborAdmin, status=active, ...}
2. 对 ADMIN_EMAILS 每个邮箱：
   User{app_id=harborAdmin, status=active, email_verified=true, password=ADMIN_PASSWORD}
3. EnsureAuthConfig(harborAdmin)
4. 幂等；邮箱小写
```

`ADMIN_PASSWORD` 仅 seed 环境变量，不入库明文、不进 `.env` 提交。

### 5.3 Admin API

前缀：`/api/v1/admin`，全部 `RequireAdmin`。

#### 5.3.1 App 主数据

| Method | Path | 说明 |
|--------|------|------|
| POST | `/apps` | 创建；响应含一次性 `app_secret` |
| GET | `/apps` | 列表 |
| GET | `/apps/{app_id}` | 主数据详情（无模块密钥） |
| POST | `/apps/{app_id}` | 更新 name / redirect_uris / settings / status |
| POST | `/apps/{app_id}/secret` | 轮换 secret |
| DELETE | `/apps/{app_id}` | 停用 |

**创建请求示例**：

```json
{
  "app_name": "My Product",
  "redirect_uris": ["https://myapp.com/oauth/callback", "myapp://callback"],
  "settings": { "allow_register": true }
}
```

OAuth 凭证**不在**创建接口混写（相对 wachi-auth 拆分）；创建后走 auth-config。若需兼容旧客户端，可保留可选 OAuth 字段并内部转发 `UpdateAuthConfig`（可选兼容层，默认文档以拆分为准）。

**创建响应**（节选）：`app_id`, `app_secret`, `app_name`, `redirect_uris`, `status`, `settings`, `created_at`。

#### 5.3.2 App 详情 — 模块配置（扩展核心）

| Method | Path | 模块 | 本期 |
|--------|------|------|------|
| GET/PUT | `/apps/{app_id}/auth-config` | Auth | ✅ 实现 |
| GET/PUT | `/apps/{app_id}/billing-config` | Billing | 🟡 路由注册 + Stub（501 或空配置骨架） |
| GET/PUT | `/apps/{app_id}/ops-config` | Ops | 🟡 Stub（501 或 `{ "enabled": false }`） |

**Auth Config GET 响应示例**：

```json
{
  "app_id": "app_xxx",
  "google_client_id": "xxx.apps.googleusercontent.com",
  "google_configured": true,
  "apple_client_id": null,
  "apple_team_id": null,
  "apple_key_id": null,
  "apple_configured": false,
  "updated_at": "..."
}
```

**Auth Config PUT 请求**（字段均可选）：

```json
{
  "google_client_id": "...",
  "google_client_secret": "...",
  "apple_client_id": "...",
  "apple_team_id": "...",
  "apple_key_id": "...",
  "apple_private_key": "-----BEGIN PRIVATE KEY-----..."
}
```

- 明文 secret 仅写路径接受；响应若需确认可回显「本次提交的明文」一次（对齐 wachi-auth `AppWithSecret` 写回显），**再次 GET 不再出现**。

**Billing Config Stub（契约示意，供后续实现）**：

```json
{
  "app_id": "app_xxx",
  "enabled": false,
  "default_provider": null,
  "test_mode": true,
  "providers": {},
  "updated_at": null
}
```

后续实现时在 `internal/billing` 填实，Admin 仅依赖 `BillingConfigService` 接口。

**Ops Config Stub**：

```json
{
  "app_id": "app_xxx",
  "enabled": false,
  "entries": {},
  "updated_at": null
}
```

### 5.4 Admin 详情页信息架构（前端对齐）

```
App 详情
├── 概览：app_id / name / status / redirect_uris / settings / secret 轮换
├── Auth：Google / Apple Client 配置
├── Billing：Provider 与密钥（后续）
└── Operations：远程配置（P1）
```

前端按 Tab 打对应 `*-config` API；后端模块可独立发版填实 Stub，**URL 稳定**。

### 5.5 Admin 应用层编排

```go
// admin/application/app_detail.go
type AppDetailService struct {
    Tenant   tenant.AppService
    AuthCfg  auth.AuthConfigService
    Billing  billing.ConfigService // interface; stub ok
    Ops      ops.ConfigService     // interface; stub ok
}

func (s *AppDetailService) GetDetail(ctx, appID) (*AppDetailDTO, error) {
    app := s.Tenant.Get(...)
    auth := s.AuthCfg.Get(...)
    // billing/ops: 调用接口；stub 返回空壳，不使详情失败
    return aggregate(...)
}
```

列表/创建不必强依赖 Billing/Ops；仅详情聚合时降级容忍。

### 5.6 审计（建议 P0 轻量）

对 Admin 写操作打结构化日志：`admin_email`, `action`, `app_id`, `request_id`。  
完整 `audit_logs` 集合可列为 P1，实体预留对齐 wachi-auth `AuditLog`。

---

## 6. 跨模块时序（关键路径）

### 6.1 新产品接入

```
Admin 登录(harborAdmin)
  → POST /admin/apps 创建 App
  → PUT  /admin/apps/{id}/auth-config 配置 OAuth
  → PUT  /admin/apps/{id}/billing-config   （后续）
  → 产品客户端对接 /auth/* 、服务端对接 /billing/*（后续）
```

### 6.2 C 端登录

```
Client → Auth API (app_id=业务App)
      → Tenant.RequireActiveApp
      → AuthConfig.Resolver (若 OAuth)
      → 签发 JWT(aud/app_id=业务App)
```

### 6.3 App 停用

```
Admin DELETE /admin/apps/{id}
  → Tenant.SoftDisable + cache invalidate
  → （钩子）Billing.OnAppDisabled / Ops...
  → 之后该 app_id 登录/刷新/下单均 2001 或模块等价错误
```

---

## 7. Firestore 总览（本三模块 + 预留）

| 集合 | 模块 | P0 |
|------|------|----|
| `apps` | Tenant | ✅ |
| `app_auth_configs` | Auth | ✅ |
| `users` | Auth | ✅ |
| `oauth_accounts` | Auth | ✅ |
| `refresh_tokens` | Auth | ✅ |
| `verification_tokens` | Auth | ✅ |
| `app_billing_configs` | Billing | 预留名；本期可不建或建空文档 |
| `billing_products` / `billing_orders` / `billing_webhook_events` | Billing | 后续 |
| `app_ops_configs` | Ops | 预留 |
| `audit_logs` | Platform | 可选 |

文档字段命名：JSON/Firestore 使用 `snake_case`，与 wachi-auth 对齐，降低迁移成本。

---

## 8. 错误码

沿用 wachi-auth，便于客户端复用：

| Code | HTTP | 含义 |
|------|------|------|
| 0 | 200 | 成功 |
| 1001 | 422 | 参数校验失败 |
| 1002 | 409 | 邮箱已注册 |
| 1003 | 401 | 凭证错误 |
| 1004 | 403 | 邮箱未验证 |
| 1005 | 403 | 用户已禁用 |
| 1006 | 429 | 账号锁定 |
| 1007 | 401 | Token 无效/过期/吊销/盗用 |
| 1008 | 401 | OAuth 失败 |
| 1009 | 409 | OAuth 已绑定其他用户 |
| 1010 | 429 | 限流 |
| 2001 | 404 | App 不存在或已停用 |
| 2002 | 401 | App Secret 无效 |
| 2003 | 401 | 未授权 |
| 2004 | 403 | 禁止（非 Admin） |
| 2005 | 400 | 不能解绑唯一登录方式 |
| 2006 | 400 | redirect_uri 不允许 |
| 2007 | 400 | Provider 未配置 |
| 3xxx | — | **预留 Billing** |
| 4xxx | — | **预留 Ops** |
| 9999 | 500 | 内部错误 |

---

## 9. 中间件与路由装配

```
Gin Engine
├── RequestID / Recover / AccessLog / RateLimit
├── GET /health
├── GET /.well-known/jwks.json          → auth
├── /api/v1/auth/*                      → auth handlers
├── /api/v1/user/*                      → auth + Bearer
├── /api/v1/oauth/introspect            → auth + AppSecret
└── /api/v1/admin/*
        └── RequireAdmin
            ├── /apps...                → tenant use cases
            ├── /apps/:id/auth-config   → auth config
            ├── /apps/:id/billing-config→ billing stub
            └── /apps/:id/ops-config    → ops stub
```

依赖注入：`cmd/server` 组装 Firestore 客户端、CachedAppRepository、各 UseCase、挂载路由；单测可用 memory repository（对齐 wachi-auth `DB_BACKEND=memory` 思路）。

---

## 10. 测试要点

| 层级 | 重点 |
|------|------|
| Tenant | 创建唯一性、禁用后 RequireActiveApp=2001、缓存失效、secret 轮换 |
| Auth | 注册登录、锁定、refresh 旋转与盗用、OAuth redirect 精确匹配、BYO 缺失 2007、tv 升级后旧 Access 失效 |
| Admin | 非白名单 2004、错误 app_id 登录不能进 Admin、auth-config 不回读 secret、billing/ops stub 不拖垮详情 |
| 回归 | 错误码与信封与 wachi-auth 客户端兼容抽测 |

---

## 11. 实现分期建议（本三模块）

| 迭代 | 交付 |
|------|------|
| M1 | 工程骨架 + Tenant CRUD + CachedApp + seed(`harborAdmin`) |
| M2 | Auth 邮箱链路 + JWT/JWKS + refresh 安全 |
| M3 | OAuth Google/Apple BYO + AuthConfig Admin API |
| M4 | Admin 全路由 + 详情聚合 + billing/ops stub + 限流/邮件适配 |
| M5 | 对接 Admin FE；补齐用户资料接口与 introspect |

Billing 正式开发时：实现 `billing.ConfigService` + `app_billing_configs`，替换 stub，**不改** Tenant/Auth 核心表。

---

## 12. 验收标准（相对需求）

1. **租户共用**：Auth（及后续 Billing）均通过 Tenant `app_id` 校验，无第二套 App 表。  
2. **Auth 对齐**：主路径与安全机制对齐 wachi-auth；引导租户名为 `harborAdmin`。  
3. **Admin 登录**：仅特定租户 + 白名单；无单独注册。  
4. **Admin App**：具备创建、列表、详情、管理（更新/停用/轮换密钥）。  
5. **App 详情**：可管理 Auth 三方登录配置；Billing / Ops 具备稳定 API 挂载点（stub 可接受）。  
6. **可扩展**：新增 Billing/Ops 不需修改 `App` 主实体必填字段；经 Hook + `*-config` 资源扩展。

---

## 13. 开放决策（实现前确认）

| 项 | 选项 | 建议 |
|----|------|------|
| 创建 App 是否兼容混写 OAuth 字段 | 兼容层 vs 纯拆分 | **纯拆分** + 文档引导分两步配置 |
| `settings.allow_register` 是否强制执行 | 忽略 vs 强制 | P0 **读取可选执行**，默认 true |
| Billing/Ops stub HTTP | 501 vs 200 空壳 | **200 空壳**，避免 Admin FE 分支复杂 |
| Refresh `family` claim 与库表 | 严格同族 | **同族旋转**；修复参考实现中 claim/库不一致的隐患 |
| 邮件服务 | SendGrid / SMTP / 日志黑洞 | 开发环境可 log adapter |

---

## 14. 小结

本设计将 **Tenant** 抽为共享内核，将 **Auth** 按 wachi-auth 语义在 Go 中重落地，并将 OAuth 凭证拆至 **AppAuthConfig**；**Admin** 复用 Auth 登录 + 白名单，提供 App 生命周期与分模块配置 API。Billing 与 Online Operations 通过 **lifecycle hook、独立 config 资源、错误码段与 stub 路由** 接入，保证 P0 可交付且后续扩展无需推翻租户模型。
