# harbor-services 重新部署（代码变更 / 环境变量）

> 适用：已完成 [首次部署](./DEPLOY.md) 后，因**代码变更**或**环境变量 / Secret 变更**需要滚动更新 Cloud Run。  
> 预计耗时：5–15 分钟  
> 相关文档：[DEPLOY](./DEPLOY.md) · [API](./api.md)

---

## 何时需要重新部署


| 场景 | 是否需要重建镜像 | 是否需要改 Cloud Run 配置 | 说明 |
| --- | --- | --- | --- |
| 仅业务代码 / Dockerfile 变更 | **是** | 否（可只换镜像） | 见 §2 |
| 仅普通环境变量变更 | 否 | **是** | 见 §3 |
| 仅 Secret 内容轮换 | 否 | 通常否（`:latest` 需触发新修订） | 见 §4 |
| 代码 + 环境变量同时变 | **是** | **是** | 先改配置再部署镜像，或一次 `deploy` 带齐参数 |
| 增删管理员白名单 | 否 | **是**（`ADMIN_EMAILS`） | 见 [DEPLOY §7.4](./DEPLOY.md#74-增删管理员) |
| Firestore 索引新增 | 否 | 否 | 按 [DEPLOY §2.3](./DEPLOY.md#23-部署复合索引) 建索引；与镜像无关 |

**不在本手册范围：** 新建项目、建库、建服务账号、首次 Secret / 首次 seed —— 请走 [DEPLOY.md](./DEPLOY.md)。

---

## 重新部署清单（Checklist）


| 步骤 | 内容 |
| --- | --- |
| 1 | 确认 shell 变量与当前 GCP 项目 / 区域一致 |
| 2 | （代码变更）构建并推送新镜像 |
| 3 | （配置变更）更新 env / Secret 绑定 |
| 4 | 触发 Cloud Run 新修订并等待 Ready |
| 5 | 验证健康检查 / JWKS / 关键登录 |

---



## 0. 变量约定

与 [DEPLOY §0](./DEPLOY.md#0-变量约定) 相同，请按实际替换：

```bash
export PROJECT_ID=your-gcp-project-id
export REGION=asia-east1
export SERVICE_NAME=harbor-services
export SA_NAME=harbor-services-sa
export SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"
export AR_REPO=harbor-services
export IMAGE="${REGION}-docker.pkg.dev/${PROJECT_ID}/${AR_REPO}/${SERVICE_NAME}:latest"

gcloud config set project "$PROJECT_ID"
gcloud config set run/region "$REGION"
```

查看当前服务状态与 URL：

```bash
export SERVICE_URL=$(gcloud run services describe "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --format="value(status.url)")

echo "$SERVICE_URL"

gcloud run services describe "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --format="yaml(spec.template.spec.containers[0].env)"
```

---



## 1. 场景 A：仅代码变更（更新镜像）

仓库根目录构建并推送（与 [DEPLOY §5.2](./DEPLOY.md#52-构建推送) 一致）：

```bash
# Apple Silicon 需指定 amd64，以匹配 Cloud Run
docker build --platform linux/amd64 -t "$IMAGE" .
docker push "$IMAGE"
```

或使用 Cloud Build（无需本地 Docker）：

```bash
gcloud builds submit --tag "$IMAGE" --project="$PROJECT_ID"
```

部署新镜像（保留已有 env / Secret，只滚动修订）：

```bash
gcloud run deploy "$SERVICE_NAME" \
  --project="$PROJECT_ID" \
  --image="$IMAGE" \
  --region="$REGION"
```

> 使用固定 tag（如 `:latest`）时，Cloud Run 仍会拉取并创建新修订。若推送失败或未真正更新 digest，线上不会变——可用 `gcloud artifacts docker images describe "$IMAGE"` 核对 digest。

可选：用带时间戳或 git SHA 的 tag，便于回滚：

```bash
export TAG="$(git rev-parse --short HEAD)"
export IMAGE_TAGGED="${REGION}-docker.pkg.dev/${PROJECT_ID}/${AR_REPO}/${SERVICE_NAME}:${TAG}"

docker build --platform linux/amd64 -t "$IMAGE_TAGGED" -t "$IMAGE" .
docker push "$IMAGE_TAGGED"
docker push "$IMAGE"

gcloud run deploy "$SERVICE_NAME" \
  --project="$PROJECT_ID" \
  --image="$IMAGE_TAGGED" \
  --region="$REGION"
```

---



## 2. 场景 B：仅环境变量变更

### 2.1 更新 / 新增普通环境变量

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --update-env-vars="BASE_URL=https://harbor.example.com,JWT_ISSUER=https://harbor.example.com"
```

一次改多项示例（按需裁剪）：

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --update-env-vars="\
ACCESS_TOKEN_TTL=7200,\
REFRESH_TOKEN_TTL=2592000,\
BCRYPT_COST=12,\
APP_CACHE_TTL_SEC=300,\
ADMIN_EMAILS=${ADMIN_EMAIL},\
SEED_ON_START=false"
```

`ADMIN_EMAILS` 含 JSON 方括号时，建议用逗号分隔（与首次部署一致），或通过 `--env-vars-file` 写入 YAML/ENV 文件，避免 shell 转义问题。

### 2.2 删除环境变量

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --remove-env-vars="ADMIN_PASSWORD,SEED_ON_START"
```

### 2.3 生产环境变量注意点


| 变量 | 重新部署时注意 |
| --- | --- |
| `DB_BACKEND` | 必须保持 `firestore`；切勿改成 `memory` |
| `GCP_PROJECT_ID` | 错误会导致连错库或权限失败 |
| `FIRESTORE_EMULATOR_HOST` | **生产切勿设置**；若误加，用 `--remove-env-vars` 删掉 |
| `SEED_ON_START` | 日常保持 `false`；仅补建 Admin 用户时短暂打开 |
| `ADMIN_PASSWORD` | Seed 完成后应从配置中移除 |
| `BASE_URL` / `JWT_ISSUER` | 自定义域名或默认 URL 变更后需同步更新 |
| `ADMIN_EMAILS` | 空则全部 Admin API 拒绝（fail-closed） |

完整说明见 [DEPLOY §6.1](./DEPLOY.md#61-环境变量一览)。

---



## 3. 场景 C：Secret 变更 / 轮换

Cloud Run 若绑定 `secret:latest`，**仅**在 Secret Manager 增加新版本**不会**自动重启实例。需要再触发一次服务更新（空更新或换镜像均可），使新修订读到新版本。

### 3.1 更新已有 Secret 内容

```bash
# 示例：写入新版本（按实际 secret 名与数据来源替换）
echo -n 'NEW_VALUE' | gcloud secrets versions add encryption-key \
  --project="$PROJECT_ID" --data-file=-

# 或 PEM 文件：
# gcloud secrets versions add rsa-private-key \
#   --project="$PROJECT_ID" --data-file=jwt-private.pem
```

触发滚动，使修订使用 `latest`：

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --update-secrets="ENCRYPTION_KEY=encryption-key:latest"
```

若多个 Secret 都已换版本，可一次列出，或任意改一个无关 env 触发新修订，例如：

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --update-env-vars="APP_CACHE_TTL_SEC=300"
```

### 3.2 新增 Secret 绑定

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --update-secrets="RSA_PUBLIC_KEY_PEM=rsa-public-key:latest"
```

确认服务账号仍有 `roles/secretmanager.secretAccessor`（见 [DEPLOY §3](./DEPLOY.md#3-服务账号与-iam)）。

### 3.3 移除 Secret 绑定

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --remove-secrets=ADMIN_PASSWORD
```

### 3.4 轮换影响（务必先读）


| Secret | 轮换后果 |
| --- | --- |
| `encryption-key` / `ENCRYPTION_KEY` | **无法**解密库内旧的 Google/Apple 等密文；需用 Admin `PUT .../auth-config` 重新提交明文凭证 |
| `rsa-private-key` / `RSA_PRIVATE_KEY_PEM` | 已签发 Access/Refresh 全部失效，用户需重新登录；当前不支持多公钥 JWKS 并存 |
| `rsa-public-key` | 建议与私钥成对更新；可省略时由私钥推导 |
| `admin-password` | 仅影响 seed；**不**重置已存在用户密码 |

轮换 RSA 推荐步骤：

1. 生成新密钥对，写入 Secret **新版本**。
2. 滚动 Cloud Run（§3.1）。
3. 验证 `/.well-known/jwks.json` 的 `kid` / 公钥已变。
4. 通知客户端需重新登录。

---



## 4. 场景 D：代码 + 环境变量同时变更

推荐一次 `gcloud run deploy`，同时指定镜像与 env / secrets，避免中间态只更新了一半：

```bash
docker build --platform linux/amd64 -t "$IMAGE" .
docker push "$IMAGE"

gcloud run deploy "$SERVICE_NAME" \
  --project="$PROJECT_ID" \
  --image="$IMAGE" \
  --platform=managed \
  --region="$REGION" \
  --update-env-vars="BASE_URL=${BASE_URL},JWT_ISSUER=${JWT_ISSUER},SEED_ON_START=false" \
  --update-secrets="ENCRYPTION_KEY=encryption-key:latest,\
RSA_PRIVATE_KEY_PEM=rsa-private-key:latest,\
RSA_PUBLIC_KEY_PEM=rsa-public-key:latest"
```

> `deploy` / `update` 的 `--update-env-vars` **不会**清空未列出的变量；只覆盖列出的键。`--set-env-vars` 会替换整组普通 env（行为更激进，日常维护慎用）。

---



## 5. 常见运维操作速查

### 5.1 临时打开 seed（补建 Admin 用户）

与 [DEPLOY §7.4](./DEPLOY.md#74-增删管理员) 一致：先更新 `ADMIN_EMAILS`，再短暂注入密码并打开 seed：

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --update-env-vars="ADMIN_EMAILS=${ADMIN_EMAIL},SEED_ON_START=true" \
  --update-secrets="ADMIN_PASSWORD=admin-password:latest"
```

确认日志出现 `created admin user` 或 `already exists` 后关闭：

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --update-env-vars="SEED_ON_START=false" \
  --remove-secrets=ADMIN_PASSWORD
```

已存在用户**不会**被 seed 改密。

### 5.2 扩缩容 / 资源

```bash
gcloud run services update "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --min-instances=0 \
  --max-instances=10 \
  --cpu=1 \
  --memory=512Mi \
  --concurrency=80 \
  --timeout=30s
```

### 5.3 回滚到上一修订

```bash
# 列出修订
gcloud run revisions list \
  --service="$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION"

# 将全部流量切到指定旧修订
gcloud run services update-traffic "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --to-revisions=REVISION_NAME=100
```

回滚只恢复该修订的镜像与当时模板配置；**不会**回滚 Firestore 数据或 Secret Manager 版本。

### 5.4 查看日志

```bash
gcloud run services logs read "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" --limit=50
```

---



## 6. 验证

与 [DEPLOY §8](./DEPLOY.md#8-验证部署) 相同，部署后至少执行：

```bash
export SERVICE_URL=$(gcloud run services describe "$SERVICE_NAME" \
  --project="$PROJECT_ID" --region="$REGION" \
  --format="value(status.url)")

curl -sS "${SERVICE_URL}/health"
curl -sS "${SERVICE_URL}/.well-known/jwks.json"

# 若持有管理员凭据：
curl -sS -X POST "${SERVICE_URL}/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"app_id\":\"harborAdmin\",\"email\":\"${ADMIN_EMAIL}\",\"password\":\"${ADMIN_PASSWORD}\"}"
```

轮换 RSA 后重点确认 JWKS 稳定且登录签发的 token 可被本服务校验。  
更换 `ENCRYPTION_KEY` 后重点确认 OAuth 相关配置是否需重新写入。

---



## 7. 常见问题

**推了镜像但线上行为没变**  
→ 确认 `docker push` / Cloud Build 成功；`gcloud run deploy --image=...` 是否指向同一 tag；查看最新 revision 是否 Ready。

**改了 Secret 版本但进程仍用旧值**  
→ 需 `services update` / `run deploy` 触发新修订（§3.1）。

**Admin 全部 2004**  
→ `ADMIN_EMAILS` 被改空或与登录邮箱不一致。

**多实例 JWKS / token 混乱**  
→ `RSA_PRIVATE_KEY_PEM` 绑定丢失，或旧镜像仍用随机 `kid`；按 [DEPLOY §4](./DEPLOY.md#4-密钥secret-manager) 检查 Secret，并部署含「公钥派生 kid」的版本。

**OAuth 2007 / 解密失败**  
→ 刚轮换了 `ENCRYPTION_KEY`，需重新 `PUT` auth-config。

更多见 [DEPLOY §12](./DEPLOY.md#12-常见问题)。
