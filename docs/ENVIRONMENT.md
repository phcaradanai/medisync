# Environment Variables

MediSync reads all configuration from environment variables via `internal/platform/config/config.go`. Nothing else in the codebase reads from the environment.

## Quick Start

```bash
# Development
cp .env.example .env
node scripts/env/validate.mjs .env

# Production
cp .env.production.example .env.production
# Edit .env.production with real values
node scripts/env/validate.mjs --mode prod .env.production
```

## Variable Reference

All six core-service variables read by the Go core, plus three new ones for Identity integration, plus three Compose-only variables.

### Core Service Variables

| Variable | Required | Default (dev) | Purpose |
|---|---|---|---|
| `DATABASE_URL` | Yes (prod) | `postgres://medisync:***@localhost:5432/medisync?sslmode=disable` | pgx-compatible PostgreSQL connection string |
| `NATS_URL` | Yes (prod) | `nats://localhost:4222` | NATS server URL with JetStream support |
| `HTTP_ADDR` | No | `:8080` | Connect-RPC API listen address |
| `LOG_LEVEL` | No | `info` | slog log level: `debug`, `info`, `warn`, `error` |
| `STARTUP_TIMEOUT_SECONDS` | No | `60` | Seconds to retry DB and NATS connections at startup |
| `JWT_SECRET` | Yes (prod) | `medisync-dev-secret-change-in-production` | HMAC key for signing JWT access tokens. Minimum 32 bytes. |
| `JWT_EXPIRY_SECONDS` | No | `3600` | Access-token lifetime in seconds (positive integer). |
| `ADMIN_BOOTSTRAP_PASSWORD` | Yes | (none - must be set) | Bootstrap admin password. Hashed with bcrypt before storage; only used on first startup. Minimum 12 bytes; empty, short, and placeholder values are rejected. |
| `CARD_TOKEN_HMAC_KEY` | Yes (prod) | `medisync-dev-card-hmac-change-in-prod` | HMAC key for deterministic card-token hashing. Cards are stored as HMAC-SHA256(key, token) in a BYTEA column. Minimum 32 bytes; the dev default is rejected in production. |
| `LOGIN_RATE_LIMIT_MAX` | No | `10` | Maximum login attempts (Login + CardLogin) per window per identifier (username or card token) and per remote IP. Set to 0 to disable rate limiting. |
| `LOGIN_RATE_LIMIT_WINDOW_SECONDS` | No | `60` | Sliding-window size in seconds for login rate limiting. Must be positive. |

### Compose-Only Variables (production)

These are used in `infra/docker-compose.prod.yml` and not read by the Go core.

| Variable | Required | Purpose |
|---|---|---|
| `POSTGRES_USER` | Yes (prod) | PostgreSQL superuser name |
| `POSTGRES_PASSWORD` | Yes (prod) | PostgreSQL superuser password |
| `POSTGRES_DB` | Yes (prod) | PostgreSQL database name |

### Optional Compose Variables

| Variable | Default | Purpose |
|---|---|---|
| `CORE_TAG` | `latest` | Docker image tag for core service |
| `ADMIN_TAG` | `latest` | Docker image tag for admin app |
| `KIOSK_TAG` | `latest` | Docker image tag for kiosk app |
| `ADMIN_PORT` | `8081` | Host port for admin web app |
| `KIOSK_PORT` | `8082` | Host port for kiosk web app |

## Development vs Production

| Aspect | Development | Production |
|---|---|---|---|
| DATABASE_URL host | `localhost` | Service name (`postgres`) or RDS host |
| DATABASE_URL sslmode | `disable` | `require` or `verify-full` |
| NATS_URL host | `localhost` | Service name (`nats`) |
| STARTUP_TIMEOUT_SECONDS | `60` | `120` (cold-start margin) |
| `JWT_SECRET` | `medisync-dev-secret-change-in-production` | Strong random string (min 32 bytes) |
| `JWT_EXPIRY_SECONDS` | `3600` | `3600` or shorter for high-security environments |
| `ADMIN_BOOTSTRAP_PASSWORD` | Known local-only value in `.env.example` | Strong unique password (minimum 12 bytes) |
| `CARD_TOKEN_HMAC_KEY` | `medisync-dev-card-hmac-change-in-prod` | Strong random string (minimum 32 bytes) |
| `LOGIN_RATE_LIMIT_MAX` | `10` | `5` - `20` (tighter for kiosk-facing deployments) |
| `LOGIN_RATE_LIMIT_WINDOW_SECONDS` | `60` | `60` - `300` (wider window = fewer lockouts) |
| `POSTGRES_*` vars | Not used (hardcoded in dev compose) | Required; set in `.env.production` |
| Placeholder detection | Not enforced | Rejected with error |

## Secret Handling

### Classification

- **Secret:** `DATABASE_URL` (contains password), `POSTGRES_PASSWORD`, `JWT_SECRET`, `ADMIN_BOOTSTRAP_PASSWORD`, `CARD_TOKEN_HMAC_KEY`. Never print, log, or commit these values.
- **Public:** `NATS_URL`, `HTTP_ADDR`, `LOG_LEVEL`, `STARTUP_TIMEOUT_SECONDS`, `POSTGRES_USER`, `POSTGRES_DB`, `JWT_EXPIRY_SECONDS`, `LOGIN_RATE_LIMIT_MAX`, `LOGIN_RATE_LIMIT_WINDOW_SECONDS`.

### Rules

1. Never commit `.env`, `.env.production`, or any `.env.*` file to version control (`.gitignore` enforces this).
2. Only `.env.example` and `.env.production.example` are tracked — they contain safe defaults or placeholders, never real credentials.
3. Pass secrets through your infrastructure secret manager (Vault, AWS Secrets Manager, Doppler, etc.) into the environment at deploy time.
4. The env validator never prints values of variables classified as secret — it masks them.
5. Do not put secrets in `VITE_*` variables — Vite bakes these into the browser bundle at build time.
6. The `DATABASE_URL` default in the Go code (`services/core/internal/platform/config/config.go`) uses `***` as the password so it cannot function accidentally. This is a cross-team concern (Team 1 ownership) — the Go default should be updated to match the development Compose credentials tracked in `.env.example`.

### Rotation

To rotate database credentials:

1. Create new credentials on the PostgreSQL server.
2. Update the secret in your infrastructure secret manager.
3. Redeploy the core service (`docker compose up -d core`).
4. Verify the new credentials work (`docker compose logs core`).
5. Revoke the old credentials on PostgreSQL.
6. No downtime required — the core retries connections on startup.

For NATS, credentials are currently unauthenticated. If NATS auth is added later, the rotation process is similar: update `NATS_URL` to include the new token/user/password.

## Environment Validation

Use the bundled validator before deploying:

```bash
# Development
node scripts/env/validate.mjs .env

# Production — enforces:
#   - all required variables are present
#   - no placeholder values (e.g. <postgres-url>)
#   - valid URLs, ports, log levels, positive integers
node scripts/env/validate.mjs --mode prod .env.production
```

The validator exits with code 0 on success and non-zero on failure. Integrate it into CI pipelines or pre-deploy hooks.

### Common Failures

| Symptom | Likely Cause |
|---|---|
| `MISSING: required variable` | A required variable is not set in your env file |
| `contains a placeholder` | You copied the example but didn't fill in a real value |
| `not a valid URL` | DATABASE_URL or NATS_URL is malformed |
| `duplicate key` | A variable is defined twice in the file |
| `malformed line` | A line has no `=` separator |
