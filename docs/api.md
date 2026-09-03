# harbor-services API

P0 对外模块 HTTP API：**Auth**、**Tenant**（经 Admin 暴露）、**Admin**。  
基于当前实现梳理（`internal/*/presentation`）。

Base URL 示例：`http://localhost:8080`

---

## 目录

1. [通用约定](#1-通用约定)
2. [鉴权方式](#2-鉴权方式)
3. [平台接口](#3-平台接口)
4. [Auth 模块](#4-auth-模块)
5. [Admin / Tenant 模块](#5-admin--tenant-模块)
6. [错误码](#6-错误码)
7. [路由速查](#7-路由速查)

---

## 1. 通用约定

### 1.1 响应信封

除 `GET /health`、`GET /`、`GET /.well-known/jwks.json` 外，业务接口统一使用：

```json
{
  "code": 0,
  "message": "ok",
  "data": {},
  "request_id": "req_xxx"
}
```

| 字段 | 说明 |
|------|------|
| `code` | `0` 成功；非 0 为业务错误码（见 [§6](#6-错误码)） |
| `message` | 人类可读说明 |
| `data` | 成功时为业务载荷；失败时常为 `{}` |
| `request_id` | 请求追踪 ID（中间件注入，响应头亦有 `X-Request-Id`） |

成功时 HTTP 状态一般为 `200`；失败时 HTTP 状态与错误码表一致。

### 1.2 时间与命名

- JSON / 路径参数：`snake_case`
- 时间：RFC3339 UTC（Go `time.Time` JSON）
- 邮箱：服务端归一化为小写

### 1.3 密码规则

至少 8 位，且同时包含大写、小写、数字。

---

## 2. 鉴权方式

| 类型 | 用法 |
|------|------|
| 无 | 注册 / 登录 / refresh / OAuth authorize·callback / 验证邮箱 / 重置密码等 |
| Bearer Access Token | `Authorization: Bearer <access_token>` — `/api/v1/user/*`、部分 Auth、全部 `/api/v1/admin/*` |
| App Secret | Token Introspection：`Authorization: Basic base64(app_id:app_secret)`，或 `X-App-Id` + `X-App-Secret` |
| Admin | Bearer + JWT 用户 `app_id == ADMIN_APP_ID`（默认 `harborAdmin`）+ `email ∈ ADMIN_EMAILS`（空白名单则全部 Admin 拒绝，fail-closed） |

Admin **没有独立登录接口**：使用 Auth 的 `POST /api/v1/auth/login`，`app_id` 填管理租户（默认 `harborAdmin`），再用返回的 access token 调用 Admin API。

---

## 3. 平台接口

### `GET /`

服务元信息（非信封）。

### `GET /health`

健康检查（非信封）：

```json
{
  "status": "healthy",
  "service": "harbor-services",
  "version": "0.1.0"
}
```

### `GET /.well-known/jwks.json`

JWKS 公钥集（非信封），供外部校验 Access Token。  
响应头：`Cache-Control: public, max-age=3600`。

---

## 4. Auth 模块

前缀：`/api/v1`

### 4.1 公共对象

#### TokenPair

```json
{
  "access_token": "<jwt>",
  "refresh_token": "<jwt>",
  "token_type": "Bearer",
  "expires_in": 7200
}
```

#### UserPublic

```json
{
  "user_id": "usr_...",
  "app_id": "app_...",
  "email": "user@example.com",
  "email_verified": true,
  "nickname": "Alice",
  "avatar_url": "https://...",
  "phone": "",
  "status": "active",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "last_login_at": "2026-01-01T00:00:00Z"
}
```

`status`：`unverified` | `active` | `disabled` | `deleted`

#### LoginResult

TokenPair 字段 + `user`（UserPublic）。

---

### 4.2 邮箱注册 / 登录 / 令牌

#### `POST /api/v1/auth/register`

注册（不签发 token；需先验证邮箱再登录）。

**请求**

```json
{
  "app_id": "app_xxx",
  "email": "user@example.com",
  "password": "Password1",
  "nickname": "Alice"
}
```

| 字段 | 必填 | 说明 |
|------|------|------|
| `app_id` | 是 | 租户须为 `active` |
| `email` | 是 | |
| `password` | 是 | 见密码规则 |
| `nickname` | 否 | |

App `settings.allow_register` 为 `false` 时拒绝（默认允许）。

**响应 `data`**

```json
{
  "user_id": "usr_...",
  "email": "user@example.com",
  "status": "unverified"
}
```

常见错误：`1001` / `1002` / `1010` / `2001` / `2004`

---

#### `POST /api/v1/auth/login`

**请求**

```json
{
  "app_id": "app_xxx",
  "email": "user@example.com",
  "password": "Password1"
}
```

**响应 `data`**：LoginResult（TokenPair + `user`）

| 用户状态 | 错误 |
|----------|------|
| `unverified` | `1004` |
| `disabled` | `1005` |
| 锁定（连续失败 ≥5，锁 15 分钟） | `1006` |
| 密码错误 | `1003` |

限流：同 IP 约 20 次/分钟 → `1010`

---

#### `POST /api/v1/auth/refresh`

Refresh Rotation：旧 refresh 吊销后签发新 pair；复用已吊销 token 会吊销整个 family。

**请求**

```json
{
  "refresh_token": "<jwt>"
}
```

**响应 `data`**：TokenPair

错误：`1007` / `2001`

---

#### `POST /api/v1/auth/logout`

鉴权：Bearer

吊销该用户全部 refresh，并 `token_version++`（旧 Access 立即失效）。

**响应 `data`**：`{}`

---

#### `POST /api/v1/auth/verify-email`

**请求**

```json
{
  "token": "<verification_token>",
  "app_id": "app_xxx"
}
```

**响应 `data`**

```json
{ "verified": true }
```

成功后用户变为 `active` 且 `email_verified=true`。

---

#### `POST /api/v1/auth/forgot-password`

无论邮箱是否存在，成功时均返回空 `data`（防枚举）。

**请求**

```json
{
  "app_id": "app_xxx",
  "email": "user@example.com"
}
```

---

#### `POST /api/v1/auth/reset-password`

**请求**

```json
{
  "token": "<reset_token>",
  "new_password": "Password2"
}
```

成功后吊销该用户全部 refresh。

---

### 4.3 OAuth（Google / Apple）

`provider` 路径参数：`google` | `apple`  
须先经 Admin 配置对应 App 的 Auth Config。

#### `GET /api/v1/auth/oauth/:provider/authorize`

Query：

| 参数 | 必填 | 说明 |
|------|------|------|
| `app_id` | 是 | |
| `redirect_uri` | 是 | 须在 App `redirect_uris` 白名单中（精确匹配） |

**响应 `data`**

```json
{
  "authorize_url": "https://accounts.google.com/o/oauth2/v2/auth?...",
  "state": "..."
}
```

错误：`2006`（redirect 不允许）/ `2007`（未配置）/ `2001`

---

#### `POST /api/v1/auth/oauth/:provider/callback`

`code` 与 `id_token` 二选一。

**请求**

```json
{
  "app_id": "app_xxx",
  "code": "optional_auth_code",
  "id_token": "optional_id_token",
  "redirect_uri": "https://app.example/callback",
  "state": "from_authorize"
}
```

**响应 `data`**：LoginResult

行为概要：已绑定则登录；否则按邮箱关联或新建 `active` 用户。

---

#### `POST /api/v1/auth/oauth/:provider/link`

鉴权：Bearer

**请求**

```json
{
  "code": "...",
  "id_token": "...",
  "redirect_uri": "https://app.example/callback"
}
```

**响应 `data`**

```json
{ "linked": true }
```

已绑定其他用户 → `1009`

---

#### `DELETE /api/v1/auth/oauth/:provider/unlink`

鉴权：Bearer

**响应 `data`**

```json
{ "unlinked": true }
```

唯一登录方式不可解绑 → `2005`

---

### 4.4 Token Introspection

#### `POST /api/v1/oauth/introspect`

鉴权：App Secret（Basic 或 Header）

**请求**

```json
{
  "token": "<access_token>"
}
```

**响应 `data`（active）**

```json
{
  "active": true,
  "user_id": "usr_...",
  "app_id": "app_...",
  "email": "user@example.com",
  "token_type": "access",
  "exp": 1735689600,
  "tv": 1
}
```

无效 / 过期 / App 不匹配 / 用户非 active / `tv` 不匹配时：`{ "active": false }`（仍 HTTP 200）。

App Secret 错误 → `2002`

---

### 4.5 用户资料 `/api/v1/user/*`

以下均需 Bearer Access Token。

#### `GET /api/v1/user/me`

**响应 `data`**：UserPublic

---

#### `POST /api/v1/user/me`

更新资料（仅提交字段生效）。

**请求**

```json
{
  "nickname": "Bob",
  "avatar_url": "https://...",
  "phone": "+8613800000000"
}
```

**响应 `data`**：UserPublic

---

#### `POST /api/v1/user/me/password`

**请求**

```json
{
  "old_password": "Password1",
  "new_password": "Password2"
}
```

成功后 `token_version++` 并吊销全部 refresh。

---

#### `POST /api/v1/user/me/email`

**请求**

```json
{
  "email": "new@example.com",
  "password": "Password1"
}
```

成功后状态回到 `unverified`，需重新验证邮箱；旧 token 失效。

---

#### `GET /api/v1/user/me/account-links`

**响应 `data`**

```json
{
  "links": [
    {
      "account_id": "...",
      "app_id": "app_xxx",
      "user_id": "usr_...",
      "provider": "google",
      "provider_user_id": "...",
      "email": "user@gmail.com",
      "created_at": "2026-01-01T00:00:00Z"
    }
  ]
}
```

---

#### `DELETE /api/v1/user/me`

软删除账号。有密码时 body 需带 `password`。

**请求（可选）**

```json
{
  "password": "Password1"
}
```

---

## 5. Admin / Tenant 模块

前缀：`/api/v1/admin`  
**全部接口**需要 Admin 鉴权（见 [§2](#2-鉴权方式)）。

Tenant 无独立公开路由；App CRUD 均经 Admin。

### 5.1 App 对象

```json
{
  "app_id": "app_...",
  "app_name": "My App",
  "redirect_uris": ["https://app.example/callback"],
  "status": "active",
  "settings": {},
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z"
}
```

- `status`：`active` | `disabled`
- 响应**永不**返回 `app_secret_hash`
- 创建 / 轮换 secret 时额外返回一次性明文 `app_secret`

`settings` 为平台扩展袋；约定键示例：`allow_register`（bool）。OAuth / Billing / Ops 密钥不放此处。

---

### 5.2 App CRUD（Tenant）

#### `POST /api/v1/admin/apps`

创建 App；同时触发 Auth 空 `app_auth_configs` 初始化。

**请求**

```json
{
  "app_name": "My App",
  "redirect_uris": ["https://app.example/callback"],
  "settings": { "allow_register": true }
}
```

| 字段 | 必填 |
|------|------|
| `app_name` | 是 |
| `redirect_uris` | 否（默认 `[]`） |
| `settings` | 否（默认 `{}`） |

**响应 `data`**

```json
{
  "app_id": "app_...",
  "app_secret": "<one-time plaintext>",
  "app_name": "My App",
  "redirect_uris": ["https://app.example/callback"],
  "status": "active",
  "settings": {},
  "created_at": "...",
  "updated_at": "..."
}
```

`app_secret` 仅此响应出现一次，请妥善保存。

---

#### `GET /api/v1/admin/apps`

Query：`include_disabled=true|1` 时包含已停用；默认仅 `active`。

**响应 `data`**

```json
{
  "apps": [ /* App[]，无 app_secret */ ]
}
```

---

#### `GET /api/v1/admin/apps/:app_id`

主数据详情（无 OAuth / Billing 密钥）。

**响应 `data`**：App

---

#### `POST /api/v1/admin/apps/:app_id`

部分更新。`settings` / `redirect_uris` 若出现在 JSON 中则为**整对象替换**（可传空数组/空对象清空）。

**请求示例**

```json
{
  "app_name": "Renamed",
  "redirect_uris": ["https://app.example/cb"],
  "settings": { "allow_register": false },
  "status": "active"
}
```

**响应 `data`**：App

---

#### `POST /api/v1/admin/apps/:app_id/secret`

轮换 `app_secret`；旧 secret 立即失效。

**响应 `data`**：同创建（含一次性 `app_secret`）

---

#### `DELETE /api/v1/admin/apps/:app_id`

软停用（`status=disabled`），不物理删除。

**响应 `data`**

```json
{ "disabled": true }
```

---

### 5.3 Auth Config

#### `GET /api/v1/admin/apps/:app_id/auth-config`

**响应 `data`**（Admin 可回显已配置的 secret / 私钥明文，供编辑页密文展示与眼睛切换）

```json
{
  "app_id": "app_xxx",
  "google_client_id": "xxx.apps.googleusercontent.com",
  "google_client_secret": "plain-secret-if-configured",
  "google_configured": true,
  "apple_client_id": null,
  "apple_team_id": null,
  "apple_key_id": null,
  "apple_private_key": null,
  "apple_configured": false,
  "updated_at": "2026-01-01T00:00:00Z"
}
```

---

#### `PUT /api/v1/admin/apps/:app_id/auth-config`

**请求**

```json
{
  "google_client_id": "xxx.apps.googleusercontent.com",
  "google_client_secret": "plain-secret",
  "apple_client_id": "...",
  "apple_team_id": "...",
  "apple_key_id": "...",
  "apple_private_key": "-----BEGIN PRIVATE KEY-----...",
  "clear_google_secret": false,
  "clear_apple_key": false
}
```

| 字段 | 说明 |
|------|------|
| `google_client_secret` / `apple_private_key` | 明文写入；落库加密；本次响应可回显明文一次 |
| `clear_google_secret` / `clear_apple_key` | `true` 时清除对应密文 |

**响应 `data`**：AuthConfigPublic + 本次提交的明文 secret/key（若有）

---

### 5.4 Billing / Ops Config（P0 Stub）

路由已挂载；当前返回骨架，业务未实现。

#### `GET|PUT /api/v1/admin/apps/:app_id/billing-config`

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

PUT 当前仍返回上述 stub（忽略写入语义）。

#### `GET|PUT /api/v1/admin/apps/:app_id/ops-config`

```json
{
  "app_id": "app_xxx",
  "enabled": false,
  "entries": {},
  "updated_at": null
}
```

---

### 5.5 Admin 登录流程（参考）

```text
1. SEED_ON_START=true 或 go run ./cmd/seed
   → 创建 harborAdmin + ADMIN_EMAILS 用户
2. POST /api/v1/auth/login
   { "app_id": "harborAdmin", "email": "...", "password": "..." }
3. 使用 access_token 调用 /api/v1/admin/*
```

---

## 6. 错误码

| Code | HTTP | 含义 |
|------|------|------|
| 0 | 200 | 成功 |
| 1001 | 422 | 参数校验失败 |
| 1002 | 409 | 邮箱已注册 |
| 1003 | 401 | 凭证错误 |
| 1004 | 403 | 邮箱未验证 |
| 1005 | 403 | 用户已禁用 |
| 1006 | 429 | 账号锁定 |
| 1007 | 401 | Token 无效 / 过期 / 吊销 / 盗用 |
| 1008 | 401 | OAuth 失败 |
| 1009 | 409 | OAuth 已绑定其他用户 |
| 1010 | 429 | 限流 |
| 2001 | 404 | App 不存在或已停用 |
| 2002 | 401 | App Secret 无效 |
| 2003 | 401 | 未授权 |
| 2004 | 403 | 禁止（非 Admin / 注册关闭等） |
| 2005 | 400 | 不能解绑唯一登录方式 |
| 2006 | 400 | redirect_uri 不允许 |
| 2007 | 400 | Provider 未配置 |
| 3xxx | — | 预留 Billing |
| 4xxx | — | 预留 Ops |
| 9999 | 500 | 内部错误 |

---

## 7. 路由速查

### 平台

| Method | Path | Auth |
|--------|------|------|
| GET | `/` | 无 |
| GET | `/health` | 无 |
| GET | `/.well-known/jwks.json` | 无 |

### Auth

| Method | Path | Auth |
|--------|------|------|
| POST | `/api/v1/auth/register` | 无 |
| POST | `/api/v1/auth/login` | 无 |
| POST | `/api/v1/auth/refresh` | 无 |
| POST | `/api/v1/auth/logout` | Bearer |
| POST | `/api/v1/auth/verify-email` | 无 |
| POST | `/api/v1/auth/forgot-password` | 无 |
| POST | `/api/v1/auth/reset-password` | 无 |
| GET | `/api/v1/auth/oauth/:provider/authorize` | 无 |
| POST | `/api/v1/auth/oauth/:provider/callback` | 无 |
| POST | `/api/v1/auth/oauth/:provider/link` | Bearer |
| DELETE | `/api/v1/auth/oauth/:provider/unlink` | Bearer |
| POST | `/api/v1/oauth/introspect` | App Secret |
| GET | `/api/v1/user/me` | Bearer |
| POST | `/api/v1/user/me` | Bearer |
| POST | `/api/v1/user/me/password` | Bearer |
| POST | `/api/v1/user/me/email` | Bearer |
| GET | `/api/v1/user/me/account-links` | Bearer |
| DELETE | `/api/v1/user/me` | Bearer |

### Admin / Tenant

| Method | Path | Auth | 模块 |
|--------|------|------|------|
| POST | `/api/v1/admin/apps` | Admin | Tenant |
| GET | `/api/v1/admin/apps` | Admin | Tenant |
| GET | `/api/v1/admin/apps/:app_id` | Admin | Tenant |
| POST | `/api/v1/admin/apps/:app_id` | Admin | Tenant |
| POST | `/api/v1/admin/apps/:app_id/secret` | Admin | Tenant |
| DELETE | `/api/v1/admin/apps/:app_id` | Admin | Tenant |
| GET | `/api/v1/admin/apps/:app_id/auth-config` | Admin | Auth Config |
| PUT | `/api/v1/admin/apps/:app_id/auth-config` | Admin | Auth Config |
| GET | `/api/v1/admin/apps/:app_id/billing-config` | Admin | Billing Stub |
| PUT | `/api/v1/admin/apps/:app_id/billing-config` | Admin | Billing Stub |
| GET | `/api/v1/admin/apps/:app_id/ops-config` | Admin | Ops Stub |
| PUT | `/api/v1/admin/apps/:app_id/ops-config` | Admin | Ops Stub |
