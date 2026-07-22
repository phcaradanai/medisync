# MediSync — Operations Runbook

## Architecture Overview

```
┌──────────┐   NATS    ┌──────────────┐   HTTP    ┌───────────────┐
│   HIS    │──JetStream→│  Core (Go)   │──Bearer──→│ Vending Agent │
│ (extern) │           │  :8080        │           │ (serial/MQTT) │
└──────────┘           └───┬────┬──────┘           └───────────────┘
                           │    │
                      ┌────┘    └─────┐
                      ▼               ▼
               ┌──────────┐   ┌─────────────┐
               │PostgreSQL│   │  PrintOps    │
               │  :5432   │   │  (sticker)   │
               └──────────┘   └─────────────┘
```

- **Core** — modular monolith (identity, catalog, inventory, dispensing, vending, printing)
- **PostgreSQL 16** — single database, auto-migrated at startup
- **NATS 2.10** — JetStream for event-driven communication
- **Vending Agent** — external Node.js service controlling cabinet hardware
- **PrintOps** — external sticker printing gateway

### Physical sticker camera

The cabinet's camera/QR reader is consumed by the Vending Agent's QR/NFC
serial channel. On the deployed cabinet in this project it is `/dev/ttyS1` at
`9600` baud. The browser never connects to that device directly. The agent
publishes one canonical QR/barcode/NFC event to the Core JetStream subject
`medisync.scanner.read`; the event contains the immutable `kioskCode`,
`scanType` (`QR`, `BARCODE`, `NFC`), `scanPurpose` (`STICKER`,
`DRUG_BARCODE`, `USER_NFC`), parsed value, and complete raw text/bytes/hex.
Core's
`GET /api/v1/kiosks/{kiosk_code}/scanner/events` endpoint then streams only the
matching kiosk's events to its authenticated browser.

```bash
# Run on the cabinet PC (verify the device first)
ls -l /dev/ttyS1
docker compose -f docker-compose.cabinet.yml up -d --build vending
docker compose -f docker-compose.cabinet.yml logs -f vending
```

Required routing/configuration:

- `SERIAL_QR_NFC=/dev/ttyS1`
- `SERIAL_QR_NFC_BAUD_RATE=9600`
- `KIOSK_CODE=00010001` must be stable and unique for that cabinet.
- `NATS_ENABLED=true` and `NATS_URL=nats://<core-host>:4222` must point to the
  Core NATS listener. Use `NATS_SCANNER_SUBJECT=medisync.scanner.read` and
  `NATS_SCANNER_STREAM=MEDISYNC`.
- `VENDING_ENDPOINTS_JSON` still maps the immutable kiosk code (for example
  `00010001`) to that agent endpoint for dispense commands; it is not the
  scanner event transport.
- Do not configure `/dev/ttyS1` as `SERIAL_COMPRESSOR` at the same time; one
  serial device cannot be opened by both channels.

When the agent is deployed on a separate hardware PC, Core only needs network
access to that PC. Set, for example:

```env
VENDING_ENDPOINTS_JSON={"00010001":"http://10.0.0.25:3303"}
VENDING_API_BEARER_TOKEN=<same-token-as-agent>
FULFILLMENT_FAKE=false
```

On the cabinet PC, configure the agent with the real Core NATS address:

```env
KIOSK_CODE=00010001
VENDING_CODE=00010001
NATS_ENABLED=true
NATS_URL=nats://<core-host>:4222
NATS_SCANNER_SUBJECT=medisync.scanner.read
NATS_SCANNER_STREAM=MEDISYNC
SERIAL_QR_NFC=/dev/ttyS1
SERIAL_QR_NFC_BAUD_RATE=9600
```

Verify the route from the Core host/container before opening the Kiosk:

```bash
curl -H "Authorization: Bearer <same-token-as-agent>" \
  http://10.0.0.25:3303/api/v1/health
curl -N -H "Authorization: Bearer <same-token-as-agent>" \
  http://10.0.0.25:3303/api/v1/qr-nfc/events
```

The agent still publishes its existing MQTT reader event. The SSE stream is an
additional Core-to-Kiosk path and does not broadcast a scan to other cabinets.

---

## Deployment

### Prerequisites

- Docker Engine 24+ and Docker Compose v2
- `.env.production` file with real secrets (see `.env.production.example`)
- 2 GB RAM, 2 CPU cores minimum
- Persistent disk for PostgreSQL and NATS data

### First Deployment

```bash
# 1. Create .env.production from template
cp .env.production.example .env.production

# 2. Fill in ALL values — placeholders are rejected at startup
#    - JWT_SECRET:      openssl rand -base64 64
#    - CARD_TOKEN_HMAC_KEY: openssl rand -base64 64
#    - ADMIN_BOOTSTRAP_PASSWORD: strong unique password (min 12 chars)
#    - POSTGRES_USER/PASSWORD/DB: match DATABASE_URL credentials
#    - PRINT_OPS_API_KEY: from print_ops deployment
#    - VENDING_API_KEY: from vending-3d-ctl-agent deployment

# 3. Start services
docker compose -f infra/docker-compose.prod.yml up -d

# 4. Verify health
docker compose -f infra/docker-compose.prod.yml ps
# All services should be "healthy"

# 5. Check logs
docker compose -f infra/docker-compose.prod.yml logs core --tail=50
# Look for: "core started"
#           "database ready"
#           "nats streams ready"
#           "admin user created"

# 6. First admin login
#    URL:  http://<host>:8080 (proxied through kiosk/admin apps)
#    User: admin
#    Pass: <ADMIN_BOOTSTRAP_PASSWORD>
#    Change password immediately via Users page.
```

### Updates

```bash
# 1. Pull latest code
git pull origin main

# 2. Rebuild and restart
docker compose -f infra/docker-compose.prod.yml up -d --build core

# 3. Verify migration ran
docker compose -f infra/docker-compose.prod.yml logs core | grep "migration"
```

---

## Health Checks

| Service | Check | What to look for |
|---|---|---|
| PostgreSQL | `pg_isready -U medisync -d medisync` | accepting connections |
| NATS | `wget -qO- localhost:8222/healthz` | `ok` |
| Core | `pgrep -x core` | process alive |
| Vending | `curl http://vending:3000/api/v1/health` | `{"status":"ok"}` |
| PrintOps | `curl -H "X-Api-Key: <key>" http://print-ops:3000/api/v1/health` | 200 OK |

### Core Logs

```bash
# Structured JSON logs — filter with jq
docker compose logs core | tail -100

# Key log events
docker compose logs core | grep -E "database ready|nats streams ready|core started|admin user created"

# Error monitoring
docker compose logs core | grep -E 'level.*(ERROR|WARN)'
```

---

## Admin Operations

### Create a Kiosk

1. Login as admin → Kiosks page
2. Click "+ Add Kiosk"
3. Enter Code (e.g. `KIOSK-1`), Display Name (e.g. `ตู้ชั้น 1`)
4. Enter PIN (min 4 chars) — PIN is shown once, must be recorded
5. Enter the code + PIN on the kiosk terminal to log in

### Add Drug to Catalog

1. Drugs page → "+ Add Drug"
2. Fill in: Code, Name, Generic Name, Form, Strength, Unit
3. The drug is now available for slot assignment

### Assign Drug to Slot

1. Inventory page → find the slot → "Assign"
2. Search and select the drug
3. Set Capacity and Low Threshold
4. Drug is now dispensable from that slot

### Refill Stock

1. Inventory page → find the slot → "Refill"
2. Enter quantity added
3. Stock is incremented and audited

### Create User Account

1. Users page → "+ Add User"
2. Enter username, password, display name, role
3. Set ward IDs (comma-separated, e.g. `WARD-3A,WARD-9Z`)
   - Empty for ADMIN = all wards
   - Nurse can only see prescriptions in their wards

---

## Recovery Procedures

### PostgreSQL Volume Full

```bash
# Check disk usage
docker compose exec postgres df -h /var/lib/postgresql/data

# Manual vacuum
docker compose exec postgres psql -U medisync -c "VACUUM FULL;"

# Archive old audit logs (if > 500M)
docker compose exec postgres psql -U medisync -c \
  "SELECT count(*), pg_size_pretty(pg_total_relation_size('audit.audit_log')) FROM audit.audit_log;"
```

### NATS JetStream Full

```bash
# Check stream sizes
docker compose exec nats nats stream ls

# Purge old data (if needed)
docker compose exec nats nats stream purge RX --keep 1000
docker compose exec nats nats stream purge MEDISYNC --keep 5000
```

### Core Crash Loop

```bash
# Check logs for crash cause
docker compose logs core --tail=100

# Common causes:
# 1. Database unreachable → check postgres health
# 2. NATS unreachable → check nats health and network
# 3. Disk full → check volume space
# 4. Invalid config → check .env.production values

# Restart after fixing
docker compose -f infra/docker-compose.prod.yml up -d core
```

### Vending Agent Unreachable

```bash
# Core will mark dispenses as FAILED when agent is down
# Check vending agent: curl http://vending:3000/api/v1/health
# Check serial ports: dmesg | grep ttyUSB
# Restart vending: docker compose restart vending
```

### Database Backup

```bash
# Backup
docker compose exec postgres pg_dump -U medisync medisync > backups/medisync-$(date +%Y%m%d-%H%M).sql

# Restore
cat backups/medisync-YYYYMMDD-HHMM.sql | docker compose exec -T postgres psql -U medisync medisync
```

---

## Security

### Rotating Secrets

```bash
# 1. Generate new secrets
NEW_JWT=$(openssl rand -base64 64)

# 2. Update .env.production
sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$NEW_JWT/" .env.production

# 3. Restart core (all existing sessions invalidated)
docker compose up -d core

# 4. Verify
docker compose logs core | grep "core started"
```

### Admin Password Reset (via DB)

```bash
# Only if locked out of admin UI. Core must be stopped first.
docker compose stop core

# Generate bcrypt hash
docker compose run --rm core htpasswd -bnBC 10 "" "new-password" | tr -d ':\n'

# Update in DB
docker compose exec postgres psql -U medisync -c \
  "UPDATE identity.users SET password_hash='<hash>' WHERE username='admin';"

docker compose start core
```

---

## Migration Reference

| Migration | Table | Purpose |
|---|---|---|
| 0001_init | `audit.audit_log` | Append-only audit trail |
| 0002_identity | `identity.users` | User accounts, roles, ward scopes |
| 0003_card_token_hash | `identity.users` | Card token HMAC column |
| 0004_drop_plaintext_card_token | `identity.users` | Remove raw card token column |
| 0005_catalog_drug | `catalog.drug` | Drug master catalog (pg_trgm search) |
| 0006_inventory_slot | `inventory.slot` | Cabinet slots with stock tracking |
| 0007_outbox | `dispensing.outbox` | Transactional outbox pattern |
| 0008_kiosk | `kiosk.kiosk` | Kiosk terminal registry |
| 0009_cabinet | `cabinet.cabinet` | Physical cabinet registry |

---

## Monitoring Checklist

Daily:
- [ ] All containers healthy (`docker compose ps`)
- [ ] No ERROR logs in last hour
- [ ] Disk space > 20% free on all volumes
- [ ] Database connection pool within limits

Weekly:
- [ ] Backup completed successfully
- [ ] Audit log size acceptable
- [ ] No stale NATS messages in DLQ

Monthly:
- [ ] Rotate secrets if policy requires
- [ ] Review user accounts for inactive users
- [ ] Check for available dependency updates

---

## Troubleshooting Quick Reference

| Symptom | Check | Fix |
|---|---|---|
| Kiosk can't log in | `docker compose logs core` | Kiosk code/PIN correct? Kiosk active? |
| Admin login fails | `docker compose logs core` | Using admin username + password? |
| Prescription not visible | `docker compose logs core` | NATS connected? feeder sending? |
| Dispense stuck at DISPENSING | Core logs + vending logs | Vending agent reachable? VENDING_FAKE=true? |
| Stock not decreasing | Completion consumer logs | `medisync.dispense.completed` published? |
| Print job failing | PrintOps logs | PrintOps reachable? PRINT_OPS_FAKE=true? |
| Database errors | `docker compose logs postgres` | Disk full? Connection pool exhausted? |
