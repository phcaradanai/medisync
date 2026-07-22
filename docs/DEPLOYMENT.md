# Deployment

MediSync is a Docker Compose-based deployment. This document covers deploying from a clean Linux server, verifying the deployment, backing up, and rolling back.

## Prerequisites

- Docker Engine 24+ with Compose plugin (`docker compose`, not `docker-compose`)
- Git (to clone the repository)
- A PostgreSQL connection string, NATS URL, and service credentials
- TLS certificates if exposing the application beyond the internal network (recommended)

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  Host (Linux)                                           │
│                                                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │  kiosk   │  │  admin   │  │   core   │              │
│  │  :8082   │  │  :8081   │  │  (internal)│             │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘              │
│       │             │             │                     │
│       └─────────────┼─────────────┘                     │
│                     │ (Connect-RPC over HTTP)            │
│       ┌─────────────┴──────────────────┐               │
│       │  medisync-frontend network     │               │
│       └────────────────────────────────┘               │
│                     │                                    │
│       ┌─────────────┴──────────────────┐               │
│       │  medisync-backend network      │               │
│       │  (internal: true)              │               │
│       │  ┌──────────┐  ┌──────────┐    │               │
│       │  │ postgres │  │   nats   │    │               │
│       │  │  :5432   │  │  :4222   │    │               │
│       │  └──────────┘  └──────────┘    │               │
│       └────────────────────────────────┘               │
│                                                         │
│   Volumes: postgres-data, nats-data (persistent)        │
└─────────────────────────────────────────────────────────┘
```

PostgreSQL and NATS are isolated on the `medisync-backend` network (`internal: true`) — no outbound internet access. Core sits on both networks: it communicates with databases over the backend network and serves admin/kiosk over the frontend network. Admin and kiosk are on `medisync-frontend` only, with published host ports. PostgreSQL and NATS are not exposed to the host network.

## Fresh-Server Deployment

### 1. Clone and prepare

```bash
git clone <repository-url> /opt/medisync
cd /opt/medisync
```

### 2. Create production environment

```bash
cp .env.production.example .env.production
```

Edit `.env.production` with real values:

```bash
DATABASE_URL=postgres://<user>:***@postgres:5432/<db>?sslmode=disable
NATS_URL=nats://nats:4222
HTTP_ADDR=:8080
LOG_LEVEL=info
STARTUP_TIMEOUT_SECONDS=120
JWT_SECRET=<strong-random-string-min-32-bytes>
JWT_EXPIRY_SECONDS=3600
ADMIN_BOOTSTRAP_PASSWORD=<strong-admin-password>
CARD_TOKEN_HMAC_KEY=<card-token-hmac-key>
POSTGRES_USER=<db-user>
POSTGRES_PASSWORD=<strong-password>
POSTGRES_DB=<db-name>
```

Note: Inside Docker Compose, `DATABASE_URL` should point to `postgres` (the service name), not `localhost`.

### 3. Validate environment

```bash
node scripts/env/validate.mjs --mode prod .env.production
```

Fix any errors before continuing.

### 4. Build and start

```bash
docker compose --env-file .env.production -f infra/docker-compose.prod.yml build
docker compose --env-file .env.production -f infra/docker-compose.prod.yml up -d --wait
```

The `--wait` flag blocks until all health checks pass (or fail). Services start in order: postgres → nats → core → admin, kiosk.

### 5. Verify

```bash
# Check service health
docker compose -f infra/docker-compose.prod.yml ps

# View core logs
docker compose -f infra/docker-compose.prod.yml logs core

# Verify database connectivity
docker compose -f infra/docker-compose.prod.yml exec core pgrep -x core
```

### 6. Access

- **Admin:** `http://<server>:8081`
- **Kiosk:** `http://<server>:8082`

Both proxy Connect-RPC calls to the core service internally.

## Validation Commands

After deployment, run these checks:

```bash
# All services healthy?
docker compose -f infra/docker-compose.prod.yml ps | grep -v "healthy" | grep -v "NAME"

# Core started successfully?
docker compose -f infra/docker-compose.prod.yml logs core | grep "database ready"
docker compose -f infra/docker-compose.prod.yml logs core | grep "nats streams ready"
docker compose -f infra/docker-compose.prod.yml logs core | grep "core started"

# Any ERROR-level logs?
docker compose -f infra/docker-compose.prod.yml logs 2>&1 | grep -i error
```

## Log Inspection

```bash
# Tail all services
docker compose -f infra/docker-compose.prod.yml logs -f

# Core only (last 200 lines)
docker compose -f infra/docker-compose.prod.yml logs --tail 200 core

# Specific time window
docker compose -f infra/docker-compose.prod.yml logs --since 30m core
```

Logs are structured JSON (slog). Use `jq` for filtering:

```bash
docker compose -f infra/docker-compose.prod.yml logs core | jq 'select(.level == "ERROR")'
```

## Backup

### PostgreSQL

```bash
# Create backup
docker compose -f infra/docker-compose.prod.yml exec postgres \
  pg_dump -U medisync medisync > medisync-backup-$(date +%Y%m%d-%H%M%S).sql

# Compress
gzip medisync-backup-*.sql
```

Schedule this daily with a cron job. Store backups off-server.

### NATS JetStream

NATS JetStream data is ephemeral by design — events are durable but have a 7-day retention for the `MEDISYNC` stream. For disaster recovery:

1. The `RX` stream uses WorkQueue retention — once consumed, messages are deleted. The source of truth is the hospital's central DB.
2. The `MEDISYNC` stream uses Limits retention with a 7-day max age. Domain events are replayable for 7 days.

Backup is handled by periodic pg_dump of the PostgreSQL audit log.

### Volumes

Named Docker volumes (`medisync-postgres-data`, `medisync-nats-data`) persist across container rebuilds. Do not delete them unless performing a clean reset.

## Rollback

### To a previous Docker image tag

```bash
export CORE_TAG=<previous-tag>
docker compose --env-file .env.production -f infra/docker-compose.prod.yml up -d core
```

### Full rollback from database backup

```bash
# 1. Stop services
docker compose -f infra/docker-compose.prod.yml down

# 2. Restore PostgreSQL
docker compose -f infra/docker-compose.prod.yml up -d postgres
docker compose -f infra/docker-compose.prod.yml exec -T postgres \
  psql -U medisync medisync < medisync-backup-YYYYMMDD-HHMMSS.sql

# 3. Restart all services
docker compose --env-file .env.production -f infra/docker-compose.prod.yml up -d --wait
```

### Git-based rollback

```bash
git checkout <previous-commit>
docker compose --env-file .env.production -f infra/docker-compose.prod.yml build core
docker compose --env-file .env.production -f infra/docker-compose.prod.yml up -d core
```

## Known Blockers

### 1. No reverse proxy / TLS termination

The current Compose exposes the admin and kiosk apps directly on their host ports without TLS. For production, place nginx, Caddy, or a cloud load balancer in front with TLS certificates (Let's Encrypt or managed certificates).

### 2. No Postgres replication

The Compose runs a single PostgreSQL instance. For production, consider managed PostgreSQL (RDS, Cloud SQL) or at minimum configure streaming replication with a standby.

### 3. No monitoring / alerting

There is no Prometheus exporter, log aggregation, or alerting configured. Add at minimum:
- Health-check pings from an external monitor (e.g. UptimeRobot) to the admin/kiosk ports
- Docker log driver forwarding to a central logging system

### 4. External service dependencies

The dispensing flow depends on `vending-3d-ctl-agent` and `print_ops` reaching the internal network. These services are not part of this Compose file; ensure they are accessible or provide fake adapters for development.

### Remote cabinet scanner over VPN

The cabinet agent and the kiosk UI do not connect directly. The agent publishes
the scanner envelope to the central NATS JetStream, Core routes it by the
immutable kiosk code, and the authenticated kiosk browser receives its own SSE
stream.

Configure the cabinet stack with the same code used by the kiosk database row:

```bash
MEDISYNC_KIOSK_CODE=00010001 \
MEDISYNC_NATS_URL=nats://<server-vpn-ip>:4222 \
VENDING_API_BEARER_TOKEN=<shared-agent-token> \
docker compose -f docker-compose.vending.yml up -d --force-recreate
```

`MEDISYNC_NATS_URL` must point to the server/VPN address, never `localhost` on
the cabinet PC. The central stack must route the same cabinet back to the
agent's published port and disable fake fulfillment when testing hardware:

```bash
VENDING_00010001_URL=http://<cabinet-vpn-ip>:6811 \
VENDING_API_BEARER_TOKEN=<shared-agent-token> \
FULFILLMENT_FAKE=false \
docker compose up -d --force-recreate medisync-core medisync-kiosk
```

Verify the agent health response reports `nats.enabled=true`,
`nats.isConnected=true`, and `nats.kioskCode=00010001`. The browser must be
logged into that same code and have the Withdraw page open before scanning;
scanner events are live-only and are not replayed to a later session.

## Cleanup

```bash
# Stop all services (persistent volumes remain)
docker compose -f infra/docker-compose.prod.yml down

# Stop and delete volumes (DESTRUCTIVE)
docker compose -f infra/docker-compose.prod.yml down -v
```
