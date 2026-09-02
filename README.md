# harbor-services

Multi-tenant lightweight BaaS mid-platform (Go + Gin). P0 modules: **Tenant**, **Auth**, **Admin**, plus Billing/Ops config stubs.

## Quick start (memory backend)

```bash
cp .env.example .env
export $(grep -v '^#' .env | xargs)
go run ./cmd/server
```

Or Docker (memory is the default):

```bash
docker compose up --build
```

- Health: `GET /health`
- JWKS: `GET /.well-known/jwks.json`
- Auth: `/api/v1/auth/*`, `/api/v1/user/*`, `/api/v1/oauth/introspect`
- Admin: `/api/v1/admin/*` (Bearer + `ADMIN_APP_ID` + `ADMIN_EMAILS`)

With `SEED_ON_START=true`, the process creates `harborAdmin` and admin users from `ADMIN_EMAILS` / `ADMIN_PASSWORD`.

## Storage backends

| `DB_BACKEND` | Persistence | Notes |
|---|---|---|
| `memory` (default) | Process memory | Fast local MVP; data lost on restart |
| `firestore` | Google Cloud Firestore | Requires `GCP_PROJECT_ID`; set `FIRESTORE_EMULATOR_HOST` for local emulator |

### Firestore (emulator)

```bash
# API + emulator via Compose
DB_BACKEND=firestore \
GCP_PROJECT_ID=local-project \
FIRESTORE_EMULATOR_HOST=firestore:8081 \
  docker compose --profile firestore up --build

# Or run the API locally against an emulator on :8081
export DB_BACKEND=firestore
export GCP_PROJECT_ID=local-project
export FIRESTORE_EMULATOR_HOST=localhost:8081
go run ./cmd/server
```

Deploy composite indexes from `deployments/gcp/firestore.indexes.json`:

```bash
gcloud firestore indexes composite create --project=$GCP_PROJECT_ID \
  # or: firebase deploy --only firestore:indexes
```

Standalone seed against Firestore:

```bash
export DB_BACKEND=firestore GCP_PROJECT_ID=local-project FIRESTORE_EMULATOR_HOST=localhost:8081
go run ./cmd/seed
```

`go run ./cmd/seed` with `DB_BACKEND=memory` does not persist across processes — use `SEED_ON_START=true` on the API instead.

## Docs

- `intro_01.md` — product intro
- `arch_01.md` — architecture
- `tech_01.md` — Tenant / Auth / Admin detailed design
