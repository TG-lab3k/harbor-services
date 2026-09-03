# harbor-billing 需求文档（PRD）

| 属性 | 内容 |
|------|------|
| 文档版本 | v1.0 |
| 文档状态 | Draft |
| 关联文档 | `billing_intro_01.md`、`README.md`、`docs/ARCH_README.md`、`docs/tech_details.md` |
| 所属工程 | `harbor-services`（Billing 模块） |
| 参考能力 | `wachi-billing`（统一收款中台思路） |
| 读者 | 产品 / 后端 / Admin 前端 / 评审 |

---

## 1. 背景与目标

### 1.1 背景

独立开发者会持续推出多个产品（Micro SaaS、H5、App）。每个新产品若各自对接收款平台，会重复建设、切换成本高，且后期从 MoR（Merchant of Record）迁到自有支付网关（如 Stripe）时，产品侧往往需要改代码。

**harbor-billing** 作为 `harbor-services` 多租户轻量 BaaS 中台的支付模块，提供统一收款门面：产品只对接中台 API，由中台适配多家 MoR；未来切换底层通道时，产品侧尽量无感。

### 1.2 一句话定义

**harbor-billing** 是接入多家 MoR（后期可替换为 Stripe）的 **统一收款 / 支付网关中台 SaaS 能力**，挂在 `harbor-services` 的 Tenant（APP）内核上，为各产品提供标准化结账、订单与回调能力。

### 1.3 目标

| 目标 | 说明 |
|------|------|
| **统一接口** | 产品侧一套 REST 契约完成结账 / 查单 / 订阅态查询，不感知具体 MoR API |
| **降低对接成本** | 新产品只需 Admin 配好 Provider + 调 Billing API，无需为每个 MoR 写一遍接入 |
| **降低切换成本** | App 可切换默认 MoR；后续引入 Stripe 时对外 API 保持稳定，产品少改或不改代码 |
| **多租户隔离** | 以 `app_id` 为边界：配置、密钥、商品、订单、Webhook 全链路隔离 |
| **与中台一体** | 复用 Tenant / Auth / Admin；不自建第二套「应用」模型 |

### 1.4 非目标（本期不做）

- 不自建完整财务账本 / 税务申报系统（MoR 侧承担税务合规时，中台只做交易编排与状态同步）
- 不做复杂分账、多商户 marketplace
- 不做 Online Operations（P1，见架构文档）
- 不在本期落地 Stripe 正式通道（仅预留 Provider 抽象与演进路径）
- 不提供独立于 `harbor-services` 的第二套部署进程（本期仍为单服务多模块）

---

## 2. 用户与场景

### 2.1 角色

| 角色 | 说明 |
|------|------|
| **终端用户（C 端）** | 使用 Micro SaaS / H5 / App 的付费用户；通过产品跳转到 MoR 收银台完成支付 |
| **产品服务端（B 端调用方）** | 各产品后端；调用 harbor Billing API 创建结账会话、查询订单 |
| **平台管理员（Admin）** | 登录 `harborAdmin` 管理面；为每个 App 配置 MoR 账户、密钥，并切换默认 Provider |
| **独立开发者（平台所有者）** | 拥有多个 App；希望一次中台对接、多产品复用 |

### 2.2 核心场景

1. **新产品上线收款**：Admin 创建 App → 配置 Billing Provider 凭证 → 配置商品映射 → 产品服务端创建 Checkout → 用户支付 → Webhook 落单 → 产品按订单状态开通权益。
2. **App 切换 MoR**：Admin 在 App 的 Billing 配置中改 `default_provider`（或按商品指定 Provider），新产品请求无需改对接代码。
3. **同一开发者多产品**：App A 用 Creem，App B 用 Paddle；配置与订单互不串扰。
4. **后期迁 Stripe**：新增 Stripe Adapter；Admin 切换默认 Provider；产品仍调用同一 `/api/v1/billing/*`。

---

## 3. 范围与优先级

### 3.1 本期范围（P0）

| 能力 | 优先级 | 说明 |
|------|--------|------|
| Billing 配置（Admin） | P0 | MoR 账户 / App ID / Key 等配置；按 App 启停；test/live；切换默认 Provider |
| Provider 适配（MoR） | P0 | 至少落地 **1 家** 完整闭环，其余可分期；目标名单见 §4 |
| 商品 / 价格映射 | P0 | 中台商品与各 Provider 商品 ID 的映射 |
| 创建 Checkout / Payment Session | P0 | 返回跳转 URL（或等价托管结账入口） |
| 入站 Webhook | P0 | 验签 → 幂等落单 → 标准化订单状态 |
| 订单查询 | P0 | 按 `order_id` / `app_id` 查询；支持按用户维度列表（可选 MVP） |
| 与 Tenant 联动 | P0 | `require_active_app`；App 停用后拒绝新下单 |
| 与 Auth 可选联动 | P0 | 订单可关联 Auth `user_id`；亦支持纯服务端 API Key / App Secret 场景 |

### 3.2 后续范围（P1+）

| 能力 | 优先级 | 说明 |
|------|--------|------|
| 其余 MoR 全量适配 | P1 | Creem / Waffo Pancake / Paddle 全部可用 |
| 订阅生命周期 | P1 | 续费、取消、升级/降级事件标准化 |
| 退款 / 部分退款 | P1 | 统一退款 API + 状态回写 |
| 出站 Webhook / 事件推送 | P1 | 向产品服务端推送标准化事件 |
| Stripe Provider | P2 | 替换或并存 MoR，产品侧无感切换 |
| 对账导出 / 简易报表 | P2 | Admin 或 API 导出订单 |

---

## 4. MoR / Provider 规划

### 4.1 本期目标平台

| Provider 标识（建议） | 平台 | 本期 |
|----------------------|------|------|
| `creem` | Creem | P0 优先候选（实现期按对接成本选定首发） |
| `waffo_pancake` | Waffo Pancake | P0/P1 |
| `paddle` | Paddle | P0/P1 |
| `stripe` | Stripe（支付网关，非 MoR） | **后期**；架构预留 |

> **实现约定**：对外业务 API **禁止**暴露某一 MoR 的专有字段作为必填；Provider 差异收敛在 Adapter 与 Admin 配置内。

### 4.2 Provider 抽象要求

- 统一接口（逻辑）：`CreateCheckout`、`ParseWebhook`、`GetRemoteOrder`（可选）、`Refund`（P1）等。
- 注册表模式：新增通道只加 Adapter + 配置 schema，**不修改**对外 REST 契约。
- 每个 App 可配置多家 Provider，但有且仅有一个 `default_provider`（可为空表示 Billing 未就绪）。
- 支持 `test_mode`：与 MoR 的 sandbox/test 凭证隔离；禁止 test 密钥写入 live 订单路径。

---

## 5. 功能需求

### 5.1 管理端（Admin）

对应 `billing_intro_01.md`：**提供 MoR 所需配置；App 可切换 MoR 平台。**

#### 5.1.1 Billing 配置读写

沿用已挂载路由（当前为 stub，本期填实）：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/admin/apps/{app_id}/billing-config` | 读取配置（密钥脱敏） |
| PUT | `/api/v1/admin/apps/{app_id}/billing-config` | 创建/更新配置 |

**配置模型（在现有 stub 上扩展）**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `app_id` | string | 租户 ID |
| `enabled` | bool | 是否对该 App 开放收款 |
| `default_provider` | string \| null | 默认 MoR，如 `creem` / `paddle` / `waffo_pancake` |
| `test_mode` | bool | 是否走测试环境凭证与沙箱 |
| `providers` | object | 按 provider 名存放账户信息、app id、api key、webhook secret 等 |
| `updated_at` | datetime \| null | 更新时间 |

**`providers` 内单通道示意（按平台差异扩展，敏感字段加密存储）**：

```json
{
  "creem": {
    "enabled": true,
    "account_id": "...",
    "api_key": "<write-only / encrypted>",
    "webhook_secret": "<write-only / encrypted>",
    "extra": {}
  }
}
```

**安全与交互约定**：

- 明文密钥仅在 PUT 写路径接受；GET **不回显**完整 secret（可返回 `api_key_set: true` 类标记）。
- 敏感字段使用平台已有 `ENCRYPTION_KEY` 应用层加密后写入 Firestore。
- Admin 鉴权：`Admin 租户 JWT + ADMIN_EMAILS` 白名单（与现有 Admin 一致）。

#### 5.1.2 切换 MoR 平台

- Admin 通过修改 `default_provider` 完成 App 级切换。
- 切换前校验：目标 Provider 已配置且 `enabled=true`，否则拒绝并返回明确错误码。
- 切换 **不影响** 历史订单归属；新 Checkout 默认走新 Provider（除非请求显式指定，见 §5.2）。
- 可选（P1）：按商品覆盖 Provider，实现「同一 App 多通道并存」。

#### 5.1.3 商品映射管理（P0 建议）

Admin 或 Billing API 维护中台商品与 Provider 商品的映射，至少支持：

| 字段 | 说明 |
|------|------|
| `product_id` | 中台商品 ID |
| `app_id` | 归属租户 |
| `name` / `description` | 展示信息 |
| `type` | `one_time` \| `subscription`（MVP 可先 one_time） |
| `provider_price_ids` | `{ "creem": "price_xxx", "paddle": "pri_xxx" }` |
| `status` | `active` \| `archived` |

> 若首期为加速闭环，允许「创建 Checkout 时直接传 `provider_product_id`」，但正式产品路径仍应以中台 `product_id` 为准，避免产品绑死某一 MoR。

---

### 5.2 用户端 / 产品侧（收款服务）

对应 `billing_intro_01.md`：**为 Micro SaaS、H5、App 提供收款服务。**

#### 5.2.1 API 分区与鉴权

采用 **「服务端主路径 + 可选用户只读」**：按接口固定鉴权策略，禁止「App Secret 与 JWT 全局随便选」。

**推荐集成拓扑（P0）**：

```
产品客户端 ──Auth JWT──► 产品服务端 / BFF（业务侧）
产品服务端 ──App Secret──► harbor Billing（结账 / 服务端查单）
产品客户端 ──Auth JWT──► harbor Billing（可选：仅「我的订单」只读）
MoR        ──签名校验──► /webhooks/billing/*
```

| 前缀 | 受众 | 鉴权 |
|------|------|------|
| `/api/v1/billing/*` | 见下方按接口矩阵 | **按接口写死**，见矩阵 |
| `/webhooks/billing/{provider}` | MoR 回调 | Provider 签名校验（不走用户 JWT / App Secret） |

##### 按接口鉴权矩阵

| 接口 | 鉴权 | 授权范围 | P0 |
|------|------|----------|----|
| `POST /api/v1/billing/checkouts` | **仅 App Secret** | 本 `app_id` 下创建结账；**禁止**浏览器 / 原生 App 直连 | 必做 |
| `GET /api/v1/billing/orders/{order_id}` | App Secret **或** 用户 JWT | Secret：本 App 任意单；JWT：仅当订单 `user_id` == token `sub` 且同 `app_id` | 必做 |
| `GET /api/v1/billing/orders`（列表） | App Secret：**可查本 App**（支持 `user_id` 过滤）；用户 JWT：**仅「我的订单」** | JWT 强制按 `sub` 过滤，**忽略**客户端传入的 `user_id` | JWT 列表可分期 |
| `POST /webhooks/billing/{provider}` | Provider 验签 | 映射到唯一 App 后更新订单 | 必做 |

原则：**能写钱 / 开结账的只给产品服务端；用户 JWT 最多读自己的单。**

##### 凭证与硬规则

1. **App Secret 不出客户端**：H5 / App / 前端不得嵌入 `app_secret`；结账必须经产品服务端转发。传法对齐平台既有约定（如 `Basic app_id:app_secret` 或等价 Header，与 Auth introspect 体系一致）。
2. **`app_id` 只从凭证解析**：Secret → 对应 App；JWT → claim 中的 `app_id`。**禁止**仅信任 Body / Query 中的 `app_id` 做授权。
3. **JWT 身份绑定**：凡用户 JWT 路径，主体用户 **只取** Access Token 的 `sub`；Body/Query 的 `user_id` 若存在必须与 `sub` 一致，否则 403；列表接口直接忽略外部 `user_id`。
4. **`require_active_app`**：所有业务写路径（及需租户有效的读路径）校验 App 为 `active`；停用后拒绝新结账。
5. **商品与金额**：以中台 `product_id` 映射为准，客户端 / 调用方不得自报任意成交价绕过目录。
6. **中间件按路由挂载**：每个 Billing 路由声明允许的鉴权类型（allowlist），校验失败返回既有 `2002`/`2003`/`1007` 等，业务拒绝用 3xxx。
7. **响应信封**：与平台一致 `{ code, message, data, request_id }`；Billing 业务错误码使用 **3xxx** 段。

#### 5.2.2 创建结账会话（Checkout）

**意图**：产品**服务端**发起一笔收款，中台向当前（或指定）Provider 创建托管结账，返回用户可跳转的 URL。

**建议接口**：`POST /api/v1/billing/checkouts`  
**鉴权**：**仅 App Secret**（见 §5.2.1 矩阵）。

**请求要点**：

| 字段 | 必填 | 说明 |
|------|------|------|
| `product_id` | 是* | 中台商品；或与 `provider_product_id` 二选一（正式路径推荐 product_id） |
| `success_url` / `cancel_url` | 是 | 支付完成/取消回跳 |
| `user_id` | 否 | 写入订单的 Auth 用户 ID（由**产品服务端**传入；中台信任持有 Secret 的服务端） |
| `customer_email` | 否 | 便于 MoR 预填 |
| `provider` | 否 | 覆盖 `default_provider` |
| `metadata` | 否 | 透传业务字段（大小限制，如 ≤ 4KB） |
| `idempotency_key` | 强烈建议 | 防重复下单 |

**响应要点**：`checkout_id`、`order_id`、`checkout_url`、`provider`、`status=pending`。

> **不在 P0 开放**「终端用户 JWT 直连创建 Checkout」。若未来需要，须单独立项：强制 `user_id=sub`、严格目录价、限流与风控，且默认仍不推荐替代服务端主路径。

#### 5.2.3 订单查询

| 方法 | 路径 | 鉴权 | 说明 |
|------|------|------|------|
| GET | `/api/v1/billing/orders/{order_id}` | App Secret 或用户 JWT | Secret 可查本 App 任意单；JWT 仅本人订单 |
| GET | `/api/v1/billing/orders?user_id=&status=&cursor=` | App Secret；或用户 JWT（忽略 `user_id`，只返回本人） | 列表可分期；JWT 场景即「我的订单」 |

订单需包含标准化状态，供产品开通权益：

| 状态 | 含义 |
|------|------|
| `pending` | 已创建，待支付 |
| `paid` | 已支付成功 |
| `failed` | 支付失败 |
| `canceled` | 已取消 |
| `refunded` | 已退款（P1 可细分为 partial） |
| `expired` | 会话过期未付 |

#### 5.2.4 Webhook 入站

- 路径示例：`POST /webhooks/billing/creem`、`/webhooks/billing/paddle`、`/webhooks/billing/waffo_pancake`
- **鉴权**：仅 Provider 签名校验（见 §5.2.1）；不接受用户 JWT / App Secret 代替验签
- 流程：验签 → 解析 → 按 `(provider, event_id)` **幂等**写入 `billing_webhook_events` → 更新 `billing_orders` →（P1）出站通知产品
- 对 MoR 快速返回 2xx；业务处理失败可记日志/重试队列，避免对方无限重推导致重复开通（以幂等为准）

#### 5.2.5 面向终端形态的说明

| 形态 | 集成方式 |
|------|----------|
| Micro SaaS | 客户端 → **产品服务端**（App Secret）创建 Checkout → 浏览器跳转 MoR → 回跳 success_url → 服务端查单或等 Webhook；可选 JWT 查「我的订单」 |
| H5 | 同 SaaS；**禁止**在前端放置 App Secret；注意移动浏览器回跳与 deep link |
| App（原生） | 同 SaaS：结账经自家后端；优先系统浏览器打开 `checkout_url`；以 Webhook 为准开通权益，客户端回跳仅作 UX |

---

## 6. 领域模型与数据（逻辑）

### 6.1 核心实体

| 实体 | 说明 |
|------|------|
| `BillingConfig` | 每 App 一份；Provider 凭证与默认通道 |
| `Product` | 中台商品及 Provider 价格映射 |
| `Order` | 中台订单；关联 app、user、provider、金额/货币、状态 |
| `CheckoutSession` | 可与 Order 合一或子实体；持有 `checkout_url` 与过期时间 |
| `WebhookEvent` | 幂等事件日志 |

### 6.2 Firestore 集合（与架构对齐）

| 集合 | 说明 |
|------|------|
| `app_billing_configs` | Billing 配置（敏感字段加密） |
| `billing_products` | 商品映射 |
| `billing_orders` | 订单 |
| `billing_webhook_events` | Webhook 幂等 |

索引建议：`app_id + created_at`、`app_id + user_id + created_at`、`provider + provider_order_id`。

### 6.3 与现有模块边界

```
Tenant ──提供──► require_active_app / GetApp
Auth   ──可选──► user_id（JWT sub）；Billing 不反向依赖 Auth 包写用户
Admin  ──配置──► BillingConfigService（替换 stub）
Billing ──适配──► Creem / Pancake / Paddle /（未来）Stripe
```

- 包路径：`internal/billing/{domain,application,infrastructure,presentation}`
- Admin 仅依赖 `billing.ConfigService`（及后续 Product/Order 应用服务接口），不直连 MoR SDK。

---

## 7. 非功能需求

| 类别 | 要求 |
|------|------|
| **多租户安全** | 仓储强制 `app_id`；Webhook 必须能解析到唯一 App（如 path/app 映射或 payload 内安全绑定） |
| **密钥安全** | 加密存储；日志禁止打印明文 key；GET 脱敏 |
| **幂等** | Checkout `idempotency_key`；Webhook `(provider, event_id)` |
| **可用性** | Provider 超时/错误映射为稳定 3xxx；不因单通道故障拖垮进程 |
| **可观测** | 结构化日志含 `request_id`、`app_id`、`module=billing`、`provider`、订单 ID |
| **环境隔离** | `test_mode` 与 live 数据/密钥分离；建议部署层亦区分 staging/prod |
| **性能** | Checkout 创建以 Provider RTT 为主；配置可读短 TTL 缓存（写路径失效） |
| **技术栈** | 与中台一致：Go / Gin / Firestore / Cloud Run |

---

## 8. 错误码（Billing 段）

沿用平台信封；Billing 使用 **3xxx**（在 `tech_details.md` 预留段落地）：

| Code | HTTP | 含义（建议） |
|------|------|----------------|
| 3001 | 400 | Billing 未启用或未配置 default_provider |
| 3002 | 400 | Provider 不支持或未配置 |
| 3003 | 404 | 商品不存在或未映射到当前 Provider |
| 3004 | 404 | 订单不存在 |
| 3005 | 409 | 幂等键冲突且参数不一致 |
| 3006 | 400 | Checkout 参数非法（URL、金额、货币等） |
| 3007 | 502 | 上游 MoR 调用失败 |
| 3008 | 401 | Webhook 验签失败 |
| 3009 | 409 | 订单状态不允许该操作（如已支付再付） |
| 3010 | 403 | App 非 active，拒绝收款 |

具体码表实现期可微调，但需写入 API 文档并保持客户端可依赖。

---

## 9. 用户故事与验收标准

### 9.1 用户故事

1. **作为**管理员，**我希望**在 App 详情配置 Creem/Paddle/Pancake 的账户与密钥，**以便**该产品能收款。
2. **作为**管理员，**我希望**一键切换 App 的默认 MoR，**以便**更换通道时产品少改代码。
3. **作为**产品服务端，**我希望**调用统一 Checkout API 拿到支付链接，**以便** H5/SaaS/App 拉起支付。
4. **作为**产品服务端，**我希望**通过 Webhook/查单得到标准化 `paid` 状态，**以便**开通会员或下载权益。
5. **作为**平台，**我希望**新增 Provider 不改对外 API，**以便**后期用 Stripe 替换 MoR 时产品无感。

### 9.2 P0 验收标准

1. Admin 可对任意业务 App GET/PUT `billing-config`；密钥加密存储且 GET 不回显明文。
2. 设置合法 `default_provider` 并 `enabled=true` 后，产品服务端可用 **App Secret** 创建 Checkout 并获得可访问的 `checkout_url`；持用户 JWT **无法**调用创建结账接口。
3. 至少 **一家** MoR 完成：Checkout → 用户支付（或沙箱模拟）→ Webhook → 订单变为 `paid`，且重复 Webhook 不重复开通（幂等）。
4. 修改 `default_provider` 后，新 Checkout 走新通道；历史订单仍可按原 `provider` 查询。
5. App 停用后，创建 Checkout 返回失败（如 3010 / 2001 语义对齐）。
6. 对外 `/api/v1/billing/*` 不强制产品传某一 MoR 专有必填字段。
7. 替换 stub 后，Admin 现有路由契约保持兼容（字段可扩展，不破坏已有键）。
8. 鉴权矩阵生效：JWT 用户不能读取他人订单；列表在 JWT 下仅返回 `sub` 本人订单；Webhook 缺签/错签拒绝。

---

## 10. 实现分期建议

| 迭代 | 交付 |
|------|------|
| B0 | 填实 `BillingConfigService` + Firestore `app_billing_configs` + 密钥加密/脱敏；Admin 可切换 `default_provider` |
| B1 | Provider 接口 + **首家 MoR** Adapter；Checkout + Order 落库 |
| B2 | Webhook 验签与幂等；订单查询 API；沙箱 E2E |
| B3 | 商品映射正式化；第二家 MoR；错误码与 API 文档 |
| B4 | 订阅/退款（P1）；出站事件；Stripe 适配预研 |

---

## 11. 风险与开放决策

| 项 | 风险 / 选项 | 建议 |
|----|-------------|------|
| 首发 MoR 选择 | Creem / Pancake / Paddle 文档与沙箱成熟度不一 | 实现前做一次对接 Spike，选闭环成本最低者首发 |
| 金额与货币 | MoR 对 currency/tax 处理不同 | P0 以 Provider 侧定价为准，中台存快照字段 |
| 订阅模型差异 | 各 MoR 订阅事件语义不同 | P0 可先 one_time；订阅进 P1 并做事件映射表 |
| Checkout 鉴权 | 仅 App Secret vs 允许用户 JWT | **已定**：结账仅 App Secret；JWT 仅可选只读「我的订单」（见 §5.2.1） |
| Webhook 租户绑定 | payload 无 app_id | 使用 per-app webhook URL 或签名密钥全局唯一映射到 app |
| 与 wachi-billing 差异 | 参考实现可能字段不同 | 对齐「统一门面 + Adapter」思路，不强制 API 字节级兼容 |

---

## 12. 文档与交付物

| 交付物 | 说明 |
|--------|------|
| 本文 `billing_prd_01.md` | 需求基线 |
| API 文档（后续） | `/api/v1/billing/*`、`/webhooks/billing/*` 细则 |
| Admin FE | App 详情 Billing Tab：配置表单、Provider 切换、密钥只写 |
| 实现代码 | `internal/billing` 替换 stub，并在 `cmd/server` 注入真实服务 |

---

## 13. 小结

**harbor-billing** 在 `harbor-services` 内以 Tenant 为枢纽，通过 Admin 管理 MoR 配置与通道切换，通过统一 Billing API 为 Micro SaaS / H5 / App 提供收款闭环；以 Provider Adapter 隔离 Creem、Waffo Pancake、Paddle，并为后期 Stripe 替换预留同一对外契约，从而实现「统一接口、低对接成本、低切换成本」。
