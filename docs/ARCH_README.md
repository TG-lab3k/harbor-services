# harbor-services 总体技术架构

| 属性 | 内容 |
|------|------|
| 文档版本 | v1.0 |
| 关联文档 | `intro_01.md` |
| 参考工程 | `./wachi-auth`（Auth / Admin 登录逻辑）、`./wachi-billing`（支付能力域） |
| 文档性质 | 总体技术架构（平台级） |
| 本期范围 | **P0**：租户(APP)、Auth、Billing、Admin；**P1** Online Operations 本期不开发，架构预留扩展点 |

---

## 1. 定位与目标

### 1.1 一句话定义

**harbor-services** 是面向独立开发者多产品场景的 **多租户轻量级 BaaS 中台**：以统一租户（APP）为枢纽，提供 **Auth（注册/登录）+ Billing（收款）+ Admin（运营管理）**，并预留 Online Operations，减少各产品重复建设通用能力、缩短上线周期。

### 1.2 要解决的问题

| 痛点 | 中台价值 |
|------|----------|
| 每个新产品重复做登录 / 支付 / 后台 | 一次对接中台，新产品只接 REST API |
| Auth 与 Billing 各自维护「应用」模型 | 租户（APP）独立抽离，多模块共用同一套租户主数据 |
| 管理入口分散 | 统一 Admin：登录 + App 全生命周期 + 各模块配置入口 |
| 后期要加运营配置等能力 | 模块化边界清晰，P1 可插拔扩展 |

### 1.3 架构目标

| 目标 | 说明 |
|------|------|
| **多租户隔离** | 以 `app_id` 为租户边界，数据、凭证、配置、鉴权全链路隔离 |
| **模块可组合** | Tenant 为共享内核；Auth / Billing / Ops / Admin 边界清晰、可独立演进 |
| **轻量可运维** | Go + Gin + Firestore + Cloud Run，无服务器、按量扩缩，适合独立开发者成本模型 |
| **可扩展** | 本期只交付 P0，但领域模型、路由、存储与 Admin 详情页为 P1（Online Operations）预留挂载点 |
| **能力收敛自参考实现** | Auth / Admin 登录对齐 `wachi-auth`；Billing 能力域对齐 `wachi-billing` 的统一收款中台思路 |

---

## 2. 技术选型

| 层级 | 选型 | 说明 |
|------|------|------|
| 语言 | Go | 与 Billing 规划一致，利于 Cloud Run 冷启动与并发 |
| Web 框架 | Gin | REST API、中间件生态成熟 |
| API 风格 | REST + JSON | 版本化前缀，如 `/api/v1/...` |
| 数据库 | Google Firestore | NoSQL、免运维、按读写计费；本地可用 Emulator |
| 部署 | Google Cloud Run | 容器化无服务器，按请求扩缩 |
| 容器 | Docker | 统一本地 / CI / Cloud Run 镜像 |
| 密钥与敏感配置 | 环境变量 +（可选）Secret Manager | OAuth secret、API Key、加密密钥等 |
| 观测 | 结构化日志（stdout）→ Cloud Logging | 请求 ID、`app_id`、模块标签贯穿 |

> **说明**：`wachi-auth` 现为 Python/FastAPI；harbor-services 整体采用 **Go 重写/收敛**，业务语义与安全模型对齐参考实现，而非语言级复用。

---

## 3. 总体架构

### 3.1 逻辑架构

```
                    ┌─────────────────────────────┐
                    │     Admin Console (FE)      │
                    │  特定租户 + 白名单管理员登录   │
                    └──────────────┬──────────────┘
                                   │ REST
         业务产品 A/B/C ───────────┼──────────────────┐
              │ REST / Webhook     │                  │
              ▼                    ▼                  ▼
┌─────────────────────────────────────────────────────────────────┐
│                     harbor-services (Cloud Run)                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────┐ │
│  │  Tenant     │  │    Auth     │  │   Billing   │  │  Admin  │ │
│  │  (APP)      │◄─┤  注册/登录   │  │  收款网关    │  │  管理面 │ │
│  │  共享内核    │  │  OAuth/JWT  │  │  MoR/Stripe │  │  API    │ │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └────┬────┘ │
│         │                │                │               │     │
│         │         ┌──────┴────────────────┴───────────────┘     │
│         │         │         Online Operations (P1 预留)          │
│         │         └─────────────────────────────────────────────│
│  ┌──────┴──────────────────────────────────────────────────────┐│
│  │              Shared Kernel / Platform                       ││
│  │  鉴权中间件 · 限流 · 加密 · 缓存 · 错误码 · 审计日志          ││
│  └─────────────────────────────┬───────────────────────────────┘│
└────────────────────────────────┼────────────────────────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │   Google Firestore      │
                    │  apps / users / tokens  │
                    │  billing_* / ops_* ...  │
                    └─────────────────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                  ▼
         Google/Apple      Creem/Paddle/...      (P1) 配置源
           OAuth               MoR Providers
```

### 3.2 部署架构

```
开发者本地                     GCP
┌──────────────────┐          ┌────────────────────────────────┐
│ Docker Compose   │          │ Artifact Registry (镜像)        │
│  API + Firestore  │  push    │              │                 │
│  Emulator        │ ───────► │         Cloud Run              │
└──────────────────┘          │    (harbor-services 服务)       │
                              │              │                 │
                              │         Firestore              │
                              │    (+ Secret Manager 可选)      │
                              └────────────────────────────────┘
```

- **单服务多模块**：本期以 **一个 Cloud Run Service / 一个进程** 承载全部 P0 模块，降低运维复杂度；模块在代码内按包边界隔离。
- **水平扩展**：Cloud Run 无状态扩缩；会话态落在 JWT + Firestore（Refresh Token 等），不依赖本机内存强一致（进程内缓存仅作短 TTL 加速，可失效）。
- **环境**：至少区分 `dev` / `staging` / `prod`；Billing 另建议 `test` / `live` 业务环境隔离（密钥与数据逻辑分离）。

### 3.3 请求分层（代码架构）

采用 **清晰分层 + 依赖倒置**（对齐 `wachi-auth` 的 DDD 四层思路，落在 Go 包结构上）：

```
cmd/server                 # 进程入口、依赖装配
internal/
  presentation/            # Gin Router / Middleware / DTO / HTTP 适配
  application/             # Use Case（编排），无框架细节
  domain/                  # 实体、值对象、领域错误、仓储接口
  infrastructure/          # Firestore、OAuth、MoR Adapter、加密、缓存
  shared/                  # 跨模块：鉴权、错误码、request id、配置
```

**依赖方向**：`presentation → application → domain ← infrastructure`。基础设施实现 domain 定义的接口，业务不直接依赖 Firestore/GCP SDK 细节。

---

## 4. 模块划分与边界

### 4.1 模块优先级与本期范围

| 模块 | 优先级 | 本期 | 职责摘要 |
|------|--------|------|----------|
| 租户（APP）管理 | P0 | ✅ | App 主数据：创建/查询/更新/停用；作为 Auth、Billing、Ops 的共享租户内核 |
| 统一注册/登录（Auth） | P0 | ✅ | 邮箱登录、Google/Apple OAuth、JWT 双令牌、应用级 OAuth 凭证（BYO） |
| 支付（Billing） | P0 | ✅ | 统一收款 API、Provider 适配、订单/Webhook；依赖租户与（可选）Auth 用户身份 |
| Admin 管理后台 | P0 | ✅ | 管理员登录 + App 管理 UI 对应 API + App 详情内各模块配置 |
| 运营配置（Online Operations） | P1 | ❌ 本期不做 | 远程配置/开关等；设计上挂在 App 详情与独立子域，接口与集合预留 |

### 4.2 模块关系

```
                    ┌──────────────┐
                    │   Tenant     │  ← 唯一「应用」权威源
                    │   (APP)      │
                    └──────┬───────┘
           ┌───────────────┼───────────────┐
           ▼               ▼               ▼
       ┌───────┐      ┌─────────┐    ┌──────────┐
       │ Auth  │      │ Billing │    │ Ops(P1)  │
       └───┬───┘      └────┬────┘    └────┬─────┘
           │               │              │
           └───────────────┼──────────────┘
                           ▼
                     ┌──────────┐
                     │  Admin   │  编排各模块配置读写
                     └──────────┘
```

| 关系 | 约定 |
|------|------|
| Tenant → Auth / Billing / Ops | 所有业务写路径先 `require_active_app(app_id)`；停用 App 则拒绝新登录 / 新下单 |
| Auth → Tenant | OAuth Client、redirect_uris、App 状态等挂在 App 实体（或 `apps/{id}/auth_config`） |
| Billing → Tenant | Provider 路由、API Key、商品映射等挂在 App（或 `apps/{id}/billing_config`） |
| Billing → Auth | 服务端 API 可用 Auth JWT 识别终端用户；服务间可用 App Secret / API Key |
| Admin → 全部 | 仅管理面；不承载 C 端登录业务逻辑，登录复用 Auth（特定租户 + 白名单） |
| Ops(P1) → Tenant | 配置按 `app_id` 隔离；Admin App 详情预留 Tab / API 命名空间 |

### 4.3 从参考工程的收敛策略

| 来源 | 迁入 harbor 的方式 |
|------|-------------------|
| `wachi-auth` 中的 App 创建/管理 | **抽离为 Tenant 模块**，成为中台共享内核，不再只服务 Auth |
| `wachi-auth` 认证与安全模型 | **Auth 模块**语义对齐：JWT、Refresh Rotation、BYO OAuth、限流、管理员白名单 |
| `wachi-auth` Admin 登录 | **Admin**：特定租户（如 `harborAdmin`）+ `ADMIN_EMAILS` 白名单，fail-closed |
| `wachi-billing` | **Billing 模块**承接统一 MoR / 后续 Stripe 抽象；租户改为依赖 harbor Tenant，而非自建平行「租户」模型 |

---

## 5. 多租户模型

### 5.1 核心概念

| 概念 | 说明 |
|------|------|
| **App（租户）** | 一个产品/业务线对应一个 `app_id`；平台内隔离单元 |
| **Admin 租户** | 预置特殊 App（如 `harborAdmin`），仅用于管理后台登录，不对外作普通业务租户 |
| **终端用户** | 属于某个 `app_id`；用户唯一性一般为 `(app_id, email)` 或 `(app_id, provider, subject)` |
| **管理员** | 必须登录到 Admin 租户，且邮箱 ∈ 服务端白名单 |

### 5.2 隔离策略

- **数据隔离**：Firestore 文档键或查询条件强制带 `app_id`；仓储层禁止跨租户扫描（Admin list apps 除外）。
- **凭证隔离**：每个 App 自有 Google/Apple Client（BYO）；Billing Provider 密钥按 App 加密存储。
- **鉴权隔离**：Access/Refresh Token payload 含 `app_id`；校验时核对 App 仍为 `active`。
- **缓存隔离**：App 配置缓存 key = `app:{app_id}`，更新/停用时主动失效。

### 5.3 App 生命周期（简）

```
created → active ⇄ suspended → deleted(软删/归档)
```

- `active`：可注册登录、可收款。
- `suspended`：拒绝新业务写；已发 Token 在下次校验时失效（或短宽限期，实现期定）。
- Admin 可列表、详情、更新配置；删除默认软删，保留审计与对账线索。

---

## 6. 核心模块设计要点

### 6.1 Tenant（APP）— 共享内核

**职责**：App CRUD、状态机、基础元数据；对外提供「活跃租户校验」与只读查询。

**建议核心字段**（逻辑模型，非最终表结构）：

| 字段 | 说明 |
|------|------|
| `app_id` | 对外主键 |
| `app_name` | 展示名 |
| `app_secret_hash` | 服务端调用凭证哈希（若需要） |
| `status` | active / suspended / ... |
| `redirect_uris` | Auth OAuth 回调白名单 |
| `created_at` / `updated_at` | 审计 |

Auth / Billing 专属配置可作为：

- **同文档嵌入**（简单、强一致读取），或
- **子集合** `apps/{app_id}/configs/auth|billing|ops`（扩展更清晰）

**推荐**：主文档放稳定元数据；易变且敏感的模块配置分子文档，便于权限与加密边界。

### 6.2 Auth — 对齐 wachi-auth

**能力（P0）**：

- 邮箱注册 / 登录（密码哈希、防暴力）
- Google OAuth 2.0、Apple Sign In（按 App BYO 凭证）
- JWT 双令牌（短 Access + 长 Refresh；Rotation + 盗用检测）
- `app_id` / `redirect_uri` 合法性校验
- 邮箱验证、密码重置（按参考实现裁剪 MVP）
- IP / 账号级限流

**关键路径**：

```
请求 → 解析 app_id → require_active_app
     → 解析 Provider（按 App 凭证）→ redirect_uri ∈ 白名单
     → 签发 JWT(app_id, sub, ...)
```

**Admin 登录复用**：仅允许指定 `app_id`（Admin 租户）+ 邮箱白名单；与业务用户共用 Auth 协议，额外 `RequireAdmin` 中间件。

### 6.3 Billing — 对齐 wachi-billing 中台思路

**定位**：统一收款门面；对内 Adapter 对接多家 MoR（Creem / Pancake / Paddle 等），后期 Stripe。

**P0 建议闭环**：

1. App 绑定 Provider 与凭证  
2. 商品 / 价格映射  
3. 创建 Checkout / Payment Session → 跳转 URL  
4. 入站 Webhook 验签 → 幂等落单 → 标准化事件（可选出站）  
5. 订单查询  

**与 Tenant / Auth**：

- 所有资源归属 `app_id`
- 终端用户身份优先使用 Auth 的 `user_id`；允许纯服务端 API Key 场景（机器调用）

**扩展点**：`Provider` 接口 + 注册表；新增通道只加 Adapter，不改业务 API。

### 6.4 Admin — 管理面

| 能力 | 说明 |
|------|------|
| 登录 | 仅登录；特定租户 + 白名单，逻辑对齐 `wachi-auth` Admin |
| App 列表 / 创建 / 管理 | 调用 Tenant 应用服务 |
| App 详情 | 聚合页：Auth 三方登录 Client 等、Billing 配置、Online Operations（P1 占位） |

Admin API 建议独立前缀：`/api/v1/admin/...`，全部经 `Auth JWT + Admin 白名单`。

App 详情配置分区示例：

```
GET/PUT /api/v1/admin/apps/{app_id}
GET/PUT /api/v1/admin/apps/{app_id}/auth-config
GET/PUT /api/v1/admin/apps/{app_id}/billing-config
GET/PUT /api/v1/admin/apps/{app_id}/ops-config   # P1：可先返回 501 或空实现
```

### 6.5 Online Operations（P1 预留）

本期不实现业务，但架构必须：

- 保留模块包路径与路由命名空间
- App 详情预留配置入口
- Firestore 集合命名预留（如 `ops_configs`）
- 不阻塞 Tenant / Auth / Billing 的领域模型（避免把运营字段硬塞死进不可演进结构）

---

## 7. API 与集成面

### 7.1 API 分区

| 前缀 | 受众 | 鉴权 |
|------|------|------|
| `/health` | 探活 | 无 |
| `/api/v1/auth/*` | C 端 / 各产品客户端 | 按接口：无 / Access Token |
| `/api/v1/billing/*` | 业务服务端 / 客户端 | API Key 或 Auth JWT（按场景） |
| `/api/v1/admin/*` | 管理后台 | Admin 租户 JWT + 白名单 |
| `/api/v1/apps/*` | （可选）内部或受限的租户只读 | Admin 或 App Secret |
| `/webhooks/billing/*` | MoR 回调 | Provider 签名校验 |

统一响应约定（建议）：HTTP 状态 + 业务 `code` / `message` / `data`，错误码分段（Auth / Tenant / Billing / Admin），便于客户端与参考实现对照。

### 7.2 对外集成模式

```
产品客户端 ──Auth REST──► harbor Auth ──► 持有 JWT
产品服务端 ──Billing REST──► harbor Billing ──► MoR
Admin FE   ──Admin REST──► harbor Admin ──► Tenant/Auth/Billing 配置
MoR        ──Webhook──► harbor Billing
```

新产品接入路径（目标体验）：

1. Admin 创建 App  
2. 配置 Auth OAuth 与 Billing Provider  
3. 客户端接 Auth SDK/API，服务端接 Billing API  
4. （P1）挂运营配置  

---

## 8. 数据架构（Firestore）

### 8.1 集合规划（示意）

| 集合 | 归属 | 说明 |
|------|------|------|
| `apps` | Tenant | 租户主数据 |
| `app_auth_configs` 或子文档 | Auth | OAuth 凭证等（敏感字段加密存储） |
| `app_billing_configs` | Billing | Provider、密钥、路由 |
| `users` | Auth | 按 `app_id` 分区字段或文档 ID 设计 |
| `refresh_tokens` / `oauth_accounts` | Auth | 会话与第三方绑定 |
| `billing_products` / `billing_orders` / `billing_webhook_events` | Billing | 商品、订单、幂等事件 |
| `ops_configs` | Ops P1 | 预留 |
| `audit_logs`（可选） | 平台 | Admin 关键操作审计 |

### 8.2 设计约束

- **按 `app_id` 查询**：复合索引提前规划（如 `app_id + created_at`）。
- **敏感字段**：Client Secret、私钥、MoR API Key 使用应用层对称加密后再写入。
- **幂等**：Billing Webhook 以 `provider + event_id` 唯一约束/文档 ID 防重。
- **缓存**：App 与高频只读配置进程内短 TTL（如 5min），写路径 invalidate。

---

## 9. 安全架构

| 层面 | 措施 |
|------|------|
| 传输 | 全站 HTTPS（Cloud Run 默认） |
| 认证 | JWT 双令牌；Refresh Rotation；盗用检测（对齐 wachi-auth） |
| 授权 | Admin 白名单 fail-closed；业务接口校验 `app_id` 与资源归属 |
| 开放重定向防护 | `redirect_uri` 精确匹配 App 白名单 |
| 密钥 | 环境注入；库内加密字段；禁止日志打印明文 secret |
| 滥用防护 | IP / 账号限流；登录失败锁定策略（可分期） |
| Webhook | 验签 + 时间窗 + 幂等 |
| 多租户 | 仓储强制租户条件；禁止信任客户端随意改 `app_id` 越权 |

---

## 10. 可观测与运维

| 能力 | 方案 |
|------|------|
| 日志 | JSON 结构化：`request_id`、`app_id`、`module`、`latency_ms`、错误码 |
| 健康检查 | `/health` 供 Cloud Run / LB |
| 指标（建议） | 请求量、错误率、Auth 登录成功率、Billing Webhook 成功率 |
| 追踪 | 中间件注入 `X-Request-Id`；需要时再接 Cloud Trace |
| 发布 | Docker 镜像 → Artifact Registry → Cloud Run 修订流量切换 |
| 本地 | Docker Compose：API + Firestore Emulator |

---

## 11. 工程结构建议

```
harbor-services/
├── cmd/
│   └── server/                 # main
├── internal/
│   ├── tenant/                 # 或按层再拆 domain/app/infra
│   ├── auth/
│   ├── billing/
│   ├── admin/
│   ├── ops/                    # P1 占位
│   ├── shared/
│   └── platform/               # config, logging, middleware
├── deployments/                # Cloud Run / compose
├── Dockerfile
├── go.mod
├── intro_01.md
└── arch_01.md                  # 本文档
```

**模块内**仍建议遵循 presentation / application / domain / infrastructure，避免「按层全局大泥球」或「按模块无分层」两种极端。

---

## 12. 演进路线

| 阶段 | 内容 |
|------|------|
| **MVP（本期 P0）** | Tenant + Auth（核心登录链路）+ Billing（结账/订单/Webhook 闭环）+ Admin（登录 + App CRUD + Auth/Billing 配置） |
| **P1** | Online Operations；Admin App 详情启用 Ops 配置；按需补订阅/退款等 Billing 能力 |
| **后续** | Billing 引入 Stripe；模块若流量或团队拆分需要，可再拆独立 Cloud Run Service，保持 REST 契约稳定 |

### 12.1 架构验收标准（总体）

1. 新 App 仅通过 Tenant/Admin 创建一次，Auth 与 Billing 共用同一 `app_id`。  
2. 普通产品用户无法访问 `/admin`；非白名单无法登录 Admin 租户管理面。  
3. App 停用后，Auth 与 Billing 写路径均失败。  
4. 新增一种 Billing Provider 不修改对外 REST 契约。  
5. Ops 未实现时，不影响 P0 上线；启用时无需推翻租户模型。

---

## 13. 风险与取舍

| 决策 | 取舍说明 |
|------|----------|
| 单服务多模块 | 运维简单、本地一体；未来某模块需独立扩缩时再拆服务 |
| Firestore | 免运维、契合 Serverless；复杂事务与关联查询需克制，订单状态机用文档设计 + 幂等弥补 |
| Go 重写 Auth | 与平台统一技术栈；需严格对照 wachi-auth 安全语义，避免「简化丢安全」 |
| Admin 与 API 同仓 | 管理 API 与业务 API 同进程，靠路由与鉴权隔离；前端可独立仓库 |

---

## 14. 小结

harbor-services 以 **Tenant（APP）为共享内核**，在 **Go + Gin + Firestore + Cloud Run** 上收敛 **Auth、Billing、Admin** 为统一轻量 BaaS 中台；本期交付 P0，并在路由、数据与 Admin 详情为 **Online Operations（P1）** 预留扩展点。能力语义对齐 `wachi-auth` / `wachi-billing`，工程上模块化、租户强隔离、管理面统一，支撑独立开发者多产品快速上线。
