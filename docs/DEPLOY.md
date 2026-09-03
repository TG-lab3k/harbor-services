# harbor-services 部署到 Google Cloud Run

> 预计耗时：30–45 分钟（首次）  
> 相关文档：[API](./api.md) · [重新部署](./RE_DEPLOY.md) · 索引定义 `deployments/gcp/firestore.indexes.json`

---

## 架构概览

```
                    HTTPS
    Client ──────────────────▶ Cloud Run (harbor-services)
                                   │
                                   │  Application Default Credentials
                                   ▼
                              Firestore (Native)
```

Cloud Run 使用绑定的服务账号自动获取 GCP 凭据，**无需**配置 `GOOGLE_APPLICATION_CREDENTIALS`。

生产必须使用 `DB_BACKEND=firestore`。`memory` 仅适合本地开发，进程重启数据即丢。

---

## 部署清单（Checklist）


| 步骤  | 内容                               |
| --- | -------------------------------- |
| 1   | 创建 / 选定 GCP 项目，启用 API            |
| 2   | 创建 Firestore Native 数据库          |
| 3   | 部署复合索引                           |
| 4   | 创建 Cloud Run 服务账号并授权             |
| 5   | Secret Manager：加密密钥 + RSA JWT 密钥 |
| 6   | Artifact Registry + 构建推送镜像       |
| 7   | 部署 Cloud Run（环境变量 + Secrets）     |
| 8   | 初始白名单与 seed（`harborAdmin`）       |
| 9   | 验证健康检查 / JWKS / Admin 登录         |


---



## 0. 变量约定

下文命令统一使用这些 shell 变量，请按实际替换：

```bash
export PROJECT_ID=your-gcp-project-id   # GCP 项目 ID
export REGION=asia-east1                # 建议与 Firestore 同区域
export SERVICE_NAME=harbor-services
export SA_NAME=harbor-services-sa
export AR_REPO=harbor-services          # Artifact Registry 仓库名
export IMAGE="${REGION}-docker.pkg.dev/${PROJECT_ID}/${AR_REPO}/${SERVICE_NAME}:latest"

# Admin 白名单（生产务必改成真实邮箱）
export ADMIN_EMAIL='you@example.com'
export ADMIN_PASSWORD='ChangeMePass1'   # ≥8 位，含大小写+数字
export ADMIN_APP_ID=harborAdmin

# 对外访问基址（自定义域名后改成正式 URL）
# 首次部署可先用 Cloud Run 默认 URL，再回写 BASE_URL / JWT_ISSUER
export JWT_ISSUER=https://harbor.example.com
export BASE_URL=https://harbor.example.com
```

```bash
gcloud config set project "$PROJECT_ID"
gcloud config set run/region "$REGION"
```

---



## 1. GCP 项目准备



### 1.1 登录与选区

```bash
gcloud auth login
gcloud config set project "$PROJECT_ID"
gcloud config set run/region "$REGION"
```



### 1.2 启用必需 API

```bash
gcloud services enable \
  run.googleapis.com \
  firestore.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com \
  cloudbuild.googleapis.com \
  iam.googleapis.com
```

---



## 2. 数据库：Firestore



### 2.1 创建 Native 模式数据库

```bash
gcloud firestore databases create \
  --database="(default)" \
  --type=firestore-native \
  --location="$REGION" \
  --project="$PROJECT_ID"
```

> 若项目已有 `(default)` 库可跳过。区域选定后不可随意更改，建议与 Cloud Run 同区以降低延迟。



### 2.2 集合说明（由应用写入，无需手工建表）


| 集合                    | 用途                       |
| --------------------- | ------------------------ |
| `apps`                | Tenant 主数据               |
| `app_auth_configs`    | 每 App 的 OAuth 配置（密钥加密存储） |
| `users`               | 用户                       |
| `oauth_accounts`      | 第三方账号绑定                  |
| `refresh_tokens`      | Refresh 家族 / 旋转          |
| `verification_tokens` | 邮箱验证 / 重置密码              |


Billing / Ops 配置集合为后续预留，P0 可不建。

### 2.3 部署复合索引

`gcloud firestore indexes composite create` **不接受**直接喂 JSON 文件，需按字段逐条创建。完整定义见 `deployments/gcp/firestore.indexes.json`。

```bash
# 1) users: (app_id, email)
gcloud firestore indexes composite create \
  --project="$PROJECT_ID" \
  --collection-group=users \
  --field-config=field-path=app_id,order=ascending \
  --field-config=field-path=email,order=ascending

# 2) users: (app_id, status, created_at DESC)
gcloud firestore indexes composite create \
  --project="$PROJECT_ID" \
  --collection-group=users \
  --field-config=field-path=app_id,order=ascending \
  --field-config=field-path=status,order=ascending \
  --field-config=field-path=created_at,order=descending

# 3) oauth_accounts: (app_id, provider, provider_user_id)
gcloud firestore indexes composite create \
  --project="$PROJECT_ID" \
  --collection-group=oauth_accounts \
  --field-config=field-path=app_id,order=ascending \
  --field-config=field-path=provider,order=ascending \
  --field-config=field-path=provider_user_id,order=ascending

# 4) refresh_tokens: (user_id, revoked)
gcloud firestore indexes composite create \
  --project="$PROJECT_ID" \
  --collection-group=refresh_tokens \
  --field-config=field-path=user_id,order=ascending \
  --field-config=field-path=revoked,order=ascending

# 5) refresh_tokens: (family_id, revoked)
gcloud firestore indexes composite create \
  --project="$PROJECT_ID" \
  --collection-group=refresh_tokens \
  --field-config=field-path=family_id,order=ascending \
  --field-config=field-path=revoked,order=ascending

# 6) verification_tokens: (user_id, token_type, used)
gcloud firestore indexes composite create \
  --project="$PROJECT_ID" \
  --collection-group=verification_tokens \
  --field-config=field-path=user_id,order=ascending \
  --field-config=field-path=token_type,order=ascending \
  --field-config=field-path=used,order=ascending
```

每个索引构建约需数分钟。查看状态：

```bash
gcloud firestore indexes composite list --project="$PROJECT_ID"
```

也可在装有 Firebase CLI 的环境用：

```bash
firebase deploy --only firestore:indexes --project="$PROJECT_ID"
```

（需项目内配置指向 `deployments/gcp/firestore.indexes.json`。）

---



## 3. 服务账号与 IAM

```bash
gcloud iam service-accounts create "$SA_NAME" \
  --display-name="Harbor Services Cloud Run SA" \
  --project="$PROJECT_ID"

export SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

# Firestore 读写（Datastore 用户角色）
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/datastore.user"

# 读取 Secret Manager
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/secretmanager.secretAccessor"
```

---



## 4. 密钥（Secret Manager）

生产至少需要：


| Secret 名          | 映射环境变量                | 说明                                  |
| ----------------- | --------------------- | ----------------------------------- |
| `encryption-key`  | `ENCRYPTION_KEY`      | OAuth client secret / Apple 私钥等落库加密 |
| `rsa-private-key` | `RSA_PRIVATE_KEY_PEM` | RS256 JWT 私钥（**生产必配**）              |
| `rsa-public-key`  | `RSA_PUBLIC_KEY_PEM`  | 公钥（可省略；未设时从私钥推导）                    |


> **重要：**  
> - 未设置 `RSA_PRIVATE_KEY_PEM` 且 `DB_BACKEND=firestore` 时进程**拒绝启动**（禁止临时密钥）。  
> - `kid` 由公钥 SPKI 的 SHA-256 派生，**同一 PEM 跨重启 / 多实例 kid 恒定**。此前每次启动 `RandomURLSafe` 换 kid 会导致业务侧 `Unknown kid`，即使私钥 Secret 已挂载。  
> - 本地 `DB_BACKEND=memory` 仍可省略 PEM（进程内临时密钥，仅开发）。



### 4.1 加密密钥

```bash
ENC_KEY=$(python3 -c "import secrets; print(secrets.token_urlsafe(32))")

echo -n "$ENC_KEY" | gcloud secrets create encryption-key \
  --project="$PROJECT_ID" \
  --replication-policy=automatic \
  --data-file=-

gcloud secrets add-iam-policy-binding encryption-key \
  --project="$PROJECT_ID" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/secretmanager.secretAccessor"

unset ENC_KEY
```



### 4.2 RSA JWT 密钥对

```bash
openssl genrsa -out jwt-private.pem 2048
openssl rsa -in jwt-private.pem -pubout -out jwt-public.pem

gcloud secrets create rsa-private-key \
  --project="$PROJECT_ID" --replication-policy=automatic
gcloud secrets create rsa-public-key \
  --project="$PROJECT_ID" --replication-policy=automatic

gcloud secrets versions add rsa-private-key \
  --project="$PROJECT_ID" --data-file=jwt-private.pem
gcloud secrets versions add rsa-public-key \
  --project="$PROJECT_ID" --data-file=jwt-public.pem

gcloud secrets add-iam-policy-binding rsa-private-key \
  --project="$PROJECT_ID" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/secretmanager.secretAccessor"

gcloud secrets add-iam-policy-binding rsa-public-key \
  --project="$PROJECT_ID" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/secretmanager.secretAccessor"

rm -f jwt-private.pem jwt-public.pem
```

PEM 为多行文本，Secret Manager 存整段（含 `BEGIN/END`）即可；Cloud Run 挂载为环境变量时会保留换行。

### 4.3 Admin 初始密码（可选 Secret）

若不想把 `ADMIN_PASSWORD` 明文写在 env 里，可单独建 Secret，仅在 **首次 seed** 时注入：

```bash
echo -n "$ADMIN_PASSWORD" | gcloud secrets create admin-password \
  --project="$PROJECT_ID" \
  --replication-policy=automatic \
  --data-file=-

gcloud secrets add-iam-policy-binding admin-password \
  --project="$PROJECT_ID" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/secretmanager.secretAccessor"
```

Seed 完成后可从 Cloud Run 配置中移除该 Secret 绑定，并轮换管理员密码。

---



## 5. 构建并推送镜像



### 5.1 Artifact Registry

```bash
gcloud artifacts repositories create "$AR_REPO" \
  --repository-format=docker \
  --location="$REGION" \
  --project="$PROJECT_ID"

gcloud auth configure-docker "${REGION}-docker.pkg.dev"
```



### 5.2 构建推送

仓库根目录执行（`Dockerfile` 构建 `./cmd/server`）：

```bash
# Apple Silicon 需指定 amd64，以匹配 Cloud Run
docker build --platform linux/amd64 -t "$IMAGE" .
docker push "$IMAGE"
```

或使用 Cloud Build（无需本地 Docker）：

```bash
gcloud builds submit --tag "$IMAGE" --project="$PROJECT_ID"
```

---



## 6. 部署 Cloud Run



### 6.1 环境变量一览


| 变量                        | 生产建议                 | 必填     | 说明                               |
| ------------------------- | -------------------- | ------ | -------------------------------- |
| `PORT`                    | `8080`               | 否      | Cloud Run 默认注入；镜像已 `EXPOSE 8080` |
| `DB_BACKEND`              | `firestore`          | **是**  | 生产禁止 `memory`                    |
| `GCP_PROJECT_ID`          | `$PROJECT_ID`        | **是**  | Firestore 项目                     |
| `FIRESTORE_EMULATOR_HOST` | **不设置**              | —      | 设置则会连模拟器，生产切勿配置                  |
| `BASE_URL`                | 公网 HTTPS 基址          | 推荐     | OAuth / 链接拼接等                    |
| `JWT_ISSUER`              | 与对外域名一致              | 推荐     | 默认 `harbor-services`             |
| `ACCESS_TOKEN_TTL`        | `7200`               | 否      | Access 秒数                        |
| `REFRESH_TOKEN_TTL`       | `2592000`            | 否      | Refresh 秒数（30d）                  |
| `RSA_PRIVATE_KEY_PEM`     | Secret               | **是**  | RS256 私钥                         |
| `RSA_PUBLIC_KEY_PEM`      | Secret               | 推荐     | 公钥                               |
| `ENCRYPTION_KEY`          | Secret               | **是**  | 配置类密钥加密                          |
| `BCRYPT_COST`             | `12`                 | 否      |                                  |
| `APP_CACHE_TTL_SEC`       | `300`                | 否      | App 内存缓存 TTL                     |
| `ADMIN_APP_ID`            | `harborAdmin`        | 推荐     | 管理租户固定 ID                        |
| `ADMIN_EMAILS`            | JSON 数组              | **是**  | 管理员白名单；**空则全部 Admin API 拒绝**     |
| `ADMIN_PASSWORD`          | Secret / 临时 env      | seed 时 | 仅引导创建管理员用户；日常可不保留                |
| `SEED_ON_START`           | 首次 `true`，之后 `false` | 引导     | 启动时幂等创建 Admin App + 白名单用户        |


`ADMIN_EMAILS` 解析：优先 JSON 数组，例如 `["a@x.com","b@y.com"]`；也支持逗号分隔 `a@x.com,b@y.com`。邮箱会归一化为小写。

### 6.2 首次部署命令

```bash
# 注意：ADMIN_EMAILS 含 JSON 方括号，建议用文件或转义；此处用逗号分隔更简单
gcloud run deploy "$SERVICE_NAME" \
  --project="$PROJECT_ID" \
  --image="$IMAGE" \
  --platform=managed \
  --region="$REGION" \
  --service-account="$SA_EMAIL" \
  --cpu=1 \
  --memory=512Mi \
  --min-instances=0 \
  --max-instances=10 \
  --concurrency=80 \
  --timeout=30s \
  --port=8080 \
  --allow-unauthenticated \
  --set-env-vars="DB_BACKEND=firestore,\
GCP_PROJECT_ID=${PROJECT_ID},\
ADMIN_APP_ID=${ADMIN_APP_ID},\
ADMIN_EMAILS=${ADMIN_EMAIL},\
JWT_ISSUER=${JWT_ISSUER},\
BASE_URL=${BASE_URL},\
ACCESS_TOKEN_TTL=7200,\
REFRESH_TOKEN_TTL=2592000,\
BCRYPT_COST=12,\
APP_CACHE_TTL_SEC=300,\
SEED_ON_START=true" \
  --set-secrets="ENCRYPTION_KEY=encryption-key:latest,\
RSA_PRIVATE_KEY_PEM=rsa-private-key:latest,\
RSA_PUBLIC_KEY_PEM=rsa-public-key:latest,\
ADMIN_PASSWORD=admin-password:latest"
```

若未创建 `admin-password` Secret，可临时：

```bash
  --update-env-vars="ADMIN_PASSWORD=${ADMIN_PASSWORD}"
```

**参数说明：**


| 参数                        | 建议   | 说明                       |
| ------------------------- | ---- | ------------------------ |
| `--min-instances`         | `0`  | 冷启动可接受时省成本；要更低延迟可设 `1`   |
| `--max-instances`         | `10` | 按流量调整                    |
| `--allow-unauthenticated` | 开启   | Auth 公共 API 需公网访问；鉴权在应用层 |


部署完成后立刻取 URL，并建议把 `BASE_URL` / `JWT_ISSUER` 改成真实地址：

```bash
export SERVICE_URL=$(gcloud run services describe "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --format="value(status.url)")

echo "$SERVICE_URL"

# 若尚未绑定自定义域名，先用 Cloud Run URL
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --update-env-vars="BASE_URL=${SERVICE_URL},JWT_ISSUER=${SERVICE_URL}"
```



### 6.3 Seed 完成后关闭启动种子

`SEED_ON_START` 幂等，但生产日常不建议保留 `ADMIN_PASSWORD`：

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --update-env-vars="SEED_ON_START=false" \
  --remove-secrets=ADMIN_PASSWORD
```



### 6.4 备选：本地 / CI 对 Firestore 执行 seed

不依赖 `SEED_ON_START` 时，在具备 Firestore 权限的环境执行：

```bash
export DB_BACKEND=firestore
export GCP_PROJECT_ID="$PROJECT_ID"
export ADMIN_APP_ID=harborAdmin
export ADMIN_EMAILS="[\"${ADMIN_EMAIL}\"]"
export ADMIN_PASSWORD='ChangeMePass1'
export ENCRYPTION_KEY='...'   # 与生产同一密钥，或仅 seed 用户时任意值也可
# 不要设置 FIRESTORE_EMULATOR_HOST
# 使用 Application Default Credentials：
gcloud auth application-default login

go run ./cmd/seed
```

`DB_BACKEND=memory` 时 `cmd/seed` **不会**持久化到 Firestore，生产勿用。

---



## 7. 初始白名单与 Admin 引导



### 7.1 概念


| 项    | 值                                | 说明                                    |
| ---- | -------------------------------- | ------------------------------------- |
| 管理租户 | `ADMIN_APP_ID`（默认 `harborAdmin`） | Seed 创建的固定 App；`allow_register=false` |
| 白名单  | `ADMIN_EMAILS`                   | 仅这些邮箱可调 `/api/v1/admin/*`             |
| 初始密码 | `ADMIN_PASSWORD`                 | Seed 写入；须满足密码规则（≥8，大小写+数字）            |


Admin **无独立登录接口**：对 `harborAdmin` 走普通 Auth 登录，再用 Bearer 调 Admin API。

Fail-closed：`ADMIN_EMAILS` 为空时，**所有** Admin 请求返回 `2004 Forbidden`。

### 7.2 Seed 产物

幂等执行后 Firestore 中应有：

1. `apps/harborAdmin`（`status=active`，`settings.allow_register=false`）
2. `app_auth_configs/harborAdmin`（空配置占位）
3. `users/*`：每个白名单邮箱一条，`email_verified=true`，`status=active`

已存在的 App / 用户不会被覆盖密码（用户已存在则跳过）。

### 7.3 首次登录与建租户

```bash
export SERVICE_URL=$(gcloud run services describe "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --format="value(status.url)")

# 1) Admin 登录
curl -sS -X POST "${SERVICE_URL}/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"app_id\":\"${ADMIN_APP_ID}\",\"email\":\"${ADMIN_EMAIL}\",\"password\":\"${ADMIN_PASSWORD}\"}"

# 记下 data.access_token
export TOKEN='<access_token>'

# 2) 创建业务 App
curl -sS -X POST "${SERVICE_URL}/api/v1/admin/apps" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "app_name": "My Product",
    "redirect_uris": ["https://app.example.com/oauth/callback"],
    "settings": { "allow_register": true }
  }'

# 保存响应中的 app_id 与一次性 app_secret
```



### 7.4 增删管理员

1. 更新 Cloud Run 的 `ADMIN_EMAILS`（加入或去掉邮箱）。
2. **新增**邮箱还需在 `harborAdmin` 下有对应用户：
  - 临时打开 `SEED_ON_START=true` + `ADMIN_PASSWORD` 滚动重启一次（已存在用户不会改密），或
  - 用 `go run ./cmd/seed` 对 Firestore 补建，或
  - 由已有管理员在控制台/脚本创建用户（当前无「邀请 Admin」专用 API，以 seed 为准）。
3. 从白名单移除邮箱后，该用户立刻无法访问 Admin（下次请求校验 email 集合），无需删用户文档。



### 7.5 修改 Admin 密码

Seed 不重置已存在用户。请用登录后的：

`POST /api/v1/user/me/password`（Bearer），或在 Firestore 外通过应用流程重置。

---



## 8. 验证部署

```bash
export SERVICE_URL=$(gcloud run services describe "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --format="value(status.url)")

# 健康检查（非信封）
curl -sS "${SERVICE_URL}/health"

# JWKS（非信封）—— 多实例下 kid/公钥应稳定（说明 RSA Secret 已生效）
curl -sS "${SERVICE_URL}/.well-known/jwks.json"

# Admin 登录
curl -sS -X POST "${SERVICE_URL}/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"app_id\":\"harborAdmin\",\"email\":\"${ADMIN_EMAIL}\",\"password\":\"${ADMIN_PASSWORD}\"}"
```

查看日志：

```bash
gcloud run services logs read "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" --limit=50
```

启动日志中应出现类似：

```text
[seed] created app harborAdmin
[seed] created admin user you@example.com
```

或 `already exists`（幂等重跑）。

---



## 9. 自定义域名（可选）

```bash
gcloud domains verify example.com

gcloud run domain-mappings create \
  --service="$SERVICE_NAME" \
  --domain=harbor.example.com \
  --region="$REGION" \
  --project="$PROJECT_ID"
```

DNS 生效后更新：

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --update-env-vars="BASE_URL=https://harbor.example.com,JWT_ISSUER=https://harbor.example.com"
```

---



## 10. 后续维护

代码变更、环境变量 / Secret 更新、回滚等日常操作见 **[重新部署手册](./RE_DEPLOY.md)**。

### 更新镜像（摘要）

```bash
docker build --platform linux/amd64 -t "$IMAGE" .
docker push "$IMAGE"

gcloud run deploy "$SERVICE_NAME" \
  --project="$PROJECT_ID" \
  --image="$IMAGE" \
  --region="$REGION"
```

### 轮换 ENCRYPTION_KEY / RSA（摘要）

- `ENCRYPTION_KEY`：更换后**无法**解密库内旧密文；需重新 `PUT .../auth-config`。
- RSA：写入 Secret 新版本并滚动修订；已签发 token 全部失效（无多公钥并存）。

详情与触发 `:latest` 生效的步骤见 [RE_DEPLOY §3](./RE_DEPLOY.md#3-场景-csecret-变更--轮换)。

### 查看当前环境变量 / Secret 绑定

```bash
gcloud run services describe "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --format="yaml(spec.template.spec.containers[0].env)"
```


---



## 11. 生产 vs 本地对照


| 变量                        | Cloud Run 生产   | 本地 Compose / `.env`                 |
| ------------------------- | -------------- | ----------------------------------- |
| `DB_BACKEND`              | `firestore`    | `memory` 或 `firestore`              |
| `GCP_PROJECT_ID`          | 真实项目 ID        | `local-project`（模拟器）                |
| `FIRESTORE_EMULATOR_HOST` | **不设置**        | `localhost:8081` / `firestore:8081` |
| `RSA_PRIVATE_KEY_PEM`     | Secret Manager | 可空（每次临时密钥，仅本地）                      |
| `ENCRYPTION_KEY`          | Secret Manager | `.env` 开发值                          |
| `ADMIN_EMAILS`            | 真实管理员邮箱        | `["admin@example.com"]`             |
| `SEED_ON_START`           | 仅首次 `true`     | 本地可长期 `true`                        |
| `BASE_URL` / `JWT_ISSUER` | 公网 HTTPS       | `http://localhost:8080`             |


---



## 12. 常见问题

**Admin 全部 2004**  
→ 检查 `ADMIN_EMAILS` 是否为空或邮箱与登录账号不一致（大小写已归一化，但域名/拼写必须一致）。

**登录 2001 App not found**  
→ 未 seed，或 `ADMIN_APP_ID` 与 seed 不一致；确认 Firestore 存在 `apps/harborAdmin`。

**多实例 JWKS / token 校验混乱**  
→ 未挂载 `RSA_PRIVATE_KEY_PEM`，或旧版本每次启动随机 `kid`（已改为由公钥派生）。确认 Secret 已挂载且部署含稳定 kid 修复。

**连不上 Firestore / permission denied**  
→ 服务账号是否具备 `roles/datastore.user`；是否误设了 `FIRESTORE_EMULATOR_HOST`。

**复合索引缺失导致查询失败**  
→ 按 §2.3 创建索引，等待状态变为 `READY`。

**OAuth 2007 provider not configured**  
→ Admin 登录后 `PUT /api/v1/admin/apps/{app_id}/auth-config` 写入凭证；确认 `ENCRYPTION_KEY` 与写入时一致。

---



## 13. 成本粗估（早期）


| 资源                | 免费额度量级           | 小流量         |
| ----------------- | ---------------- | ----------- |
| Cloud Run         | 月请求 / CPU 时间有免费层 | 常可 $0 起     |
| Firestore         | 日读写免费额度          | 小 DAU 常可 $0 |
| Secret Manager    | 少量密钥版本           | 极低          |
| Artifact Registry | 小镜像              | 极低          |


建议配置预算告警，避免误配导致费用异常。