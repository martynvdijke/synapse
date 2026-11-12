# Synapse — Uptime Kuma Monitor Sync

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Gin-1.12-00ADD8?style=flat&logo=go" alt="Gin">
  <img src="https://img.shields.io/badge/SQLite3-003B57?style=flat&logo=sqlite" alt="SQLite">
  <img src="https://img.shields.io/badge/license-MIT-blue" alt="License">
  <img src="https://img.shields.io/badge/docker-ready-2496ED?style=flat&logo=docker" alt="Docker">
</p>

A single-binary web application that synchronizes Docker Compose containers and Nginx Proxy Manager (NPM) proxy hosts to Uptime Kuma for centralized monitoring. Also provides Authelia configuration synchronization for domain-level access control.

## Features

### Docker Compose Sync
- **Service Discovery** — Parse docker-compose files and discover all defined services
- **Automatic Monitor Creation** — Create HTTP monitors in Uptime Kuma for each service
- **Status Tracking** — View which services are monitored and their current status
- **Duplicate Detection** — Avoid creating duplicate monitors

### Nginx Proxy Manager Sync
- **Proxy Host Discovery** — Fetch all proxy hosts from NPM API
- **Automatic Monitor Creation** — Create HTTP monitors for each proxy host
- **Monitor Status** — View proxy hosts with their current Uptime Kuma monitoring status
- **Connection Testing** — Test NPM connectivity and credentials

### Authelia Integration
- **Config Parsing** — Parse Authelia configuration to extract protected domains
- **CNAME Comparison** — Compare NPM proxy CNAMEs against Authelia access control rules
- **Auto-Sync** — Automatically add missing domains to Authelia configuration
- **Config Dry-Run** — Preview changes before applying
- **Sync Overrides** — Custom domain-to-policy mappings in JSON
- **Alert System** — Open alerts for uncovered domains when auto-sync is disabled
- **Alert Resolution** — Resolve alerts from the dashboard

### Temporary IP Access
- **Create Temp Rules** — Grant temporary IP access to Authelia with expiration
- **Duration Support** — Set access duration (e.g., "24h", "7d") or specific expiry time
- **Auto-Cleanup** — Expired rules automatically cleaned up
- **Revoke Rules** — Manually revoke access before expiration

### Alerts
- **Alert Rules** — Create rules for monitor downtime (`monitor_down_for`), container stops (`container_down`), stale syncs (`sync_stale`), and reconcile drift (`reconcile_drift`) with configurable thresholds and glob subjects
- **Incident Lifecycle** — Open incidents auto-resolve when the condition clears; acknowledge to suppress from the open count or resolve manually from the dashboard (Alerts tab)
- **Evaluation Engine** — Periodic evaluator with sustained-downtime tracking, per-subject state, and optional reminder notifications for still-open incidents
- **Notifications** — Rule opens/resolves fan out through the multi-channel notifier when `NOTIFY_ALERTS` is enabled

### Dashboard & Monitoring
- **Service Dashboard** — See all discovered services with monitor status
- **Proxy Dashboard** — View all NPM proxy hosts with monitor status
- **Monitor Detail View** — Full Uptime Kuma monitor details including uptime percentages (24h, 7d, 1y), average ping, and last message
- **Status Overview** — Dashboard showing counts for Docker services, NPM proxies, monitors, and open alert incidents (click to jump to Alerts)

### Synchronization
- **On-Demand Sync** — Trigger Docker or NPM sync manually
- **Periodic Sync** — Configurable sync interval via `SYNC_INTERVAL` environment variable
- **Reconciliation** — Enforce desired state for linked services (NPM proxy hosts + Kuma monitors) via the `synapse.*` compose labels, with dry-run previews and per-service filtering
- **Progress Streaming** — Real-time sync progress via Server-Sent Events (SSE)
- **Sync History** — View past sync runs with timestamps and results
- **Concurrency Protection** — Prevents overlapping sync operations

### Monitoring & Events
- **Docker Event Tracking** — Streams container events (restarts, stops, image updates) over the Docker socket and persists them for inspection
- **Unified Events Feed** — Combined view of Docker events and reconcile runs
- **Multi-Channel Notifications** — Event-driven alerts for unexpected container stops, unhealthy containers, image updates, and reconcile drift (per-category toggles + cooldown dedup), fanned out to every enabled channel: ntfy, Telegram, Discord, generic webhooks, and Gotify. Configure channels in Settings → Notifications; when none are configured the legacy Gotify URL/token fields act as a single fallback channel. `POST /api/notify/test` verifies one (`{"channel":"ntfy"}`) or all channels and reports per-channel results.

### Administration
- **First-Time Setup** — Create admin account on first run
- **Session Authentication** — Cookie-based sessions with bcrypt password hashing
- **Settings Management** — Configure all integrations through the web UI
- **Connection Testing** — Test NPM and Kuma connections individually

### Observability
- **OpenTelemetry** — Request tracing via OTel middleware
- **Structured Logging** — Gin-based request logging

## Quick Start

### Docker

```bash
docker build -t synapse .
docker run -p 6270:6270 \
  -e KUMA_PASS=your-kuma-password \
  synapse
```

### Manual Setup

```bash
# Install dependencies
go mod download

# Build
CGO_ENABLED=1 go build -o synapse-server .

# Run
./synapse-server
```

Open **[http://localhost:6270](http://localhost:6270)** and complete the initial setup.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `KUMA_URL` | `http://uptime-kuma:3001` | Uptime Kuma base URL |
| `KUMA_USER` | `admin` | Uptime Kuma username |
| `KUMA_PASS` | — | Uptime Kuma password |
| `COMPOSE_PATH` | `docker-compose.yml` | Path to docker-compose file |
| `NPM_HOST` | `http://nginx:81` | Nginx Proxy Manager URL |
| `NPM_USER` | `admin` | NPM username |
| `NPM_PASS` | — | NPM password |
| `DB_PATH` | `synapse.db` | SQLite database path |
| `LISTEN_ADDR` | `:6270` | HTTP listen address |
| `SYNC_INTERVAL` | `0` | Periodic sync interval in minutes (0 = disabled) |
| `AUTHELIA_CONFIG_PATH` | — | Path to Authelia configuration.yml |
| `AUTHELIA_DB_PATH` | — | Path to Authelia SQLite database |
| `AUTHELIA_SYNC_ENABLED` | `false` | Enable automatic Authelia config sync |
| `AUTHELIA_DEFAULT_POLICY` | `deny` | Default access policy for new domains |
| `OTEL_ENDPOINT` | — | OpenTelemetry OTLP collector endpoint (gRPC by default, e.g. `http://otel-collector:4317`) |
| `OTEL_ENABLED` | `false` | Enable OpenTelemetry tracing |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` | OTLP transport for traces/metrics/logs: `grpc` or `http/protobuf` |
| `DOCKER_SOCKET` | — | Initial Docker Engine socket (`unix:///var/run/docker.sock`, `tcp://`, or path); the socket saved in Settings takes precedence. Empty = disabled until set in Settings |
| `DOCKER_EVENTS_ENABLED` | `false` | Track Docker container events |
| `DOCKER_EVENTS_RETENTION_DAYS` | `30` | Days to retain Docker events before purging |
| `RECONCILE_ENABLED` | `false` | Periodically reconcile linked services to desired state |
| `RECONCILE_INTERVAL_MINUTES` | `60` | Reconcile interval in minutes |
| `RECONCILE_DRY_RUN_DEFAULT` | `true` | Default dry-run mode for scheduled reconcile runs |
| `NOTIFY_DOCKER_DIE` | `false` | Notify on unexpected container stops |
| `NOTIFY_DOCKER_HEALTH` | `false` | Notify on unhealthy container health checks |
| `NOTIFY_DOCKER_IMAGE` | `false` | Notify when a container's image changes |
| `NOTIFY_RECONCILE` | `false` | Notify when a reconcile run reports changes or errors |
| `NOTIFY_COOLDOWN_MINUTES` | `5` | Cooldown window for repeat event notifications |
| `NOTIFY_ALERTS` | `false` | Notify on alert rule transitions via configured notification channels |
| `ALERTS_ENABLED` | `false` | Enable the alert rule evaluation engine |
| `ALERT_EVAL_INTERVAL_SECONDS` | `60` | Interval between alert rule evaluation ticks (10–3600) |
| `ALERT_REMINDER_MINUTES` | `0` | Repeat-notification interval for sustained open alerts in minutes (0 = no reminders) |
| `KUMA_DEFAULT_TAGS` | — | Default tag IDs applied to newly created Kuma monitors (comma-separated or JSON array, e.g. `1,2` or `[1,2]`) |
| `OIDC_ISSUER` | `https://authelia.vandijke.xyz` | Authelia OIDC issuer (empty `OIDC_CLIENT_ID` disables OIDC) |
| `OIDC_CLIENT_ID` | — | OIDC client ID; unset = local auth only |
| `OIDC_CLIENT_SECRET` | — | OIDC client secret (or `OIDC_CLIENT_SECRET_FILE` for a secret mount) |
| `OIDC_REDIRECT_URL` | — | Exact callback URL registered in Authelia, e.g. `https://synapse.example.com/api/auth/oidc/callback` |
| `OIDC_SCOPES` | `openid email profile groups` | Requested OIDC scopes |

### OIDC login (Authelia)

> This synapse is the Authelia sync tool, not Matrix Synapse — native Matrix OIDC/SSO is out of scope.

- Browser flow: `GET /api/auth/oidc/login` → Authelia (Authorization Code + PKCE S256) → `GET /api/auth/oidc/callback` verifies `state`/`nonce` and the ID token (issuer, audience, expiry, RS256), requires `email_verified=true`, then links/provisions a local user by verified email and creates a session.
- Mutation auth is OR: a valid OIDC session **or** a valid service-account bearer token (`Authorization: Bearer …`) authorizes `POST/PUT/PATCH/DELETE /api/*`. Service-token calls bypass NPM forward-auth; browser UI goes through OIDC after cutover.
- Public reads (`GET /overview`, `GET /api/public/overview`, health/metrics, `GET /api/v1/trmnl/stats`) stay anonymous (200 without auth).
- OIDC users get an unusable password marker and can only log in via OIDC.

## API Endpoints

### Authentication

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/check-setup` | Check if admin account exists |
| `POST` | `/api/login` | Login or create initial admin account |
| `POST` | `/api/logout` | Logout |
| `GET` | `/api/auth/oidc/status` | OIDC enabled flag + issuer (public, no secrets) |
| `GET` | `/api/auth/oidc/login` | Start Authelia OIDC login (redirect) |
| `GET` | `/api/auth/oidc/callback` | OIDC callback (code exchange, session creation) |

### Settings

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/settings` | Get current settings |
| `POST` | `/api/settings` | Save settings |
| `POST` | `/api/test/npm` | Test NPM connection |
| `POST` | `/api/test/kuma` | Test Kuma connection |

### Sync

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/sync/docker` | Trigger Docker service sync |
| `POST` | `/api/sync/npm` | Trigger NPM proxy sync |
| `GET` | `/api/sync/progress` | SSE stream for sync progress |
| `GET` | `/api/sync/history?source=docker\|npm` | Sync run history |
| `POST` | `/api/reconcile` | Run reconciliation (`{dry_run, service}` — dry run default) |
| `GET` | `/api/reconcile/runs` | Reconcile run history |

### Data

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/services` | List Docker services with monitor status |
| `GET` | `/api/proxies` | List NPM proxy hosts with monitor status |
| `GET` | `/api/monitors` | List Uptime Kuma monitors with uptime stats, active state, interval/retry/maxretries, and tags |
| `GET` | `/api/monitors/:id/stats?instance=` | Uptime stats detail for a single monitor |
| `POST` | `/api/monitors` | Create a Kuma monitor (instance-targeted) |
| `PUT` | `/api/monitors/:kumaId?instance=` | Update a Kuma monitor |
| `DELETE` | `/api/monitors/:kumaId?instance=` | Delete a Kuma monitor |
| `POST` | `/api/monitors/:kumaId/pause?instance=` | Pause a monitor (`pauseMonitor` Socket.IO command) |
| `POST` | `/api/monitors/:kumaId/resume?instance=` | Resume a paused monitor (`resumeMonitor` Socket.IO command) |
| `PUT` | `/api/monitors/:kumaId/tags?instance=` | Replace monitor tags (diff current vs desired, per-op results) |
| `GET` | `/api/status` | Dashboard status overview |
| `GET` | `/api/docker/events?type=&action=&container=&since=&limit=` | List tracked Docker events |
| `GET` | `/api/events` | Unified feed (Docker events + reconcile runs) |

### Authelia

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/authelia/status` | Authelia integration status |
| `GET` | `/api/authelia/alerts` | List open Authelia alerts |
| `POST` | `/api/authelia/alerts/:id/resolve` | Resolve an alert |
| `GET` | `/api/authelia/temp-access` | List temporary access rules |
| `POST` | `/api/authelia/temp-access` | Create temporary access rule |
| `POST` | `/api/authelia/temp-access/:id/revoke` | Revoke temporary access |
| `POST` | `/api/authelia/sync` | Run Authelia config sync |

### Alerts

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/alert-rules` | List alert rules |
| `POST` | `/api/alert-rules` | Create alert rule (`name`, `type`, `subject`, `threshold` as duration string or seconds, `enabled`) |
| `PUT` | `/api/alert-rules/:id` | Update alert rule (partial) |
| `DELETE` | `/api/alert-rules/:id` | Delete alert rule |
| `GET` | `/api/incidents?status=&limit=` | List incidents (filter by `open`/`acknowledged`/`resolved`) |
| `POST` | `/api/incidents/:id/ack` | Acknowledge an open incident |
| `POST` | `/api/incidents/:id/resolve` | Resolve an open or acknowledged incident |

## Push notifications

Synapse can notify users when a new item (service link or alert rule) is
created: in-app realtime over SSE, in the browser via Web Push, and on Android
via Firebase Cloud Messaging (FCM). Configuration is admin-wide; each user
controls their own opt-ins.

### Admin settings

All of these are stored through the normal settings API (`POST /api/settings`)
and editable in **Settings → Push notifications**. Secret values
(`web_push_vapid_private_key`, `fcm_service_account_json`) are write-only: the
read API returns `****` plus a `*_set` flag, and posting `****` or an empty
string keeps the stored value.

| Setting | Default | Description |
|---------|---------|-------------|
| `push_enabled` | `false` | Global master switch for new-item push |
| `notify_new_item` | `true` | General "new item created" toggle |
| `web_push_enabled` | `false` | Enable browser Web Push delivery |
| `web_push_vapid_public_key` | — | VAPID public key (base64url) |
| `web_push_vapid_private_key` | — | VAPID private key (secret) |
| `web_push_subject` | `mailto:admin@localhost` | VAPID `sub` claim (mailto or https URL) |
| `fcm_enabled` | `false` | Enable FCM (Android) delivery |
| `fcm_project_id` | — | Firebase project id |
| `fcm_service_account_json` | — | Google service-account JSON (secret) |
| `fcm_android_channel_id` | `synapse_general` | Android notification channel id |

### Web Push setup

1. Generate a VAPID key pair with `POST /api/notifications/vapid` (returns
   `public_key` / `private_key`; nothing is saved automatically).
2. Save both keys and `web_push_enabled=true` in settings.
3. The frontend registers the browser subscription with
   `POST /api/notifications/subscriptions` using the public key, and serves the
   service worker from `/sw.js` (root-scoped, unauthenticated).

Browsers that report an endpoint as gone (404/410) are pruned automatically.

### FCM (Android) setup

1. Create a Firebase project and a service account with the
   *Firebase Cloud Messaging API* enabled. Download its JSON key.
2. Save the JSON in `fcm_service_account_json`, set `fcm_project_id`, and set
   `fcm_enabled=true`.
3. The server signs a JWT-bearer OAuth2 assertion (scope
   `https://www.googleapis.com/auth/firebase.messaging`), caches the access
   token, and sends to `POST /v1/projects/{project}/messages:send`.
   Tokens FCM reports as `UNREGISTERED`/`NOT_FOUND` are pruned.

### Per-user API

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/notifications` | Current user's preferences + server capabilities |
| `GET` | `/api/notifications/stream` | SSE stream of realtime events (`Event` JSON) |
| `PUT` | `/api/notifications/preferences` | Save `{enabled, web_push, fcm, new_item}` |
| `POST` | `/api/notifications/subscriptions` | Register Web Push `{endpoint, keys:{p256dh, auth}}` |
| `DELETE` | `/api/notifications/subscriptions` | Remove `{endpoint}` |
| `POST` | `/api/notifications/devices` | Register FCM `{token, platform, device_name}` |
| `DELETE` | `/api/notifications/devices/:id` | Remove a device |
| `POST` | `/api/notifications/test` | Send a test push to the current user |
| `POST` | `/api/notifications/vapid` | Generate a new VAPID key pair (not saved) |

Mutation routes live in the bearer-token-protected group, so automation can use
the same contract with `Authorization: Bearer <token>`.

### Android integration contract

1. Obtain an FCM registration token and register it:
   `POST /api/notifications/devices` with a bearer token and body
   `{"token": "...", "platform": "android", "device_name": "Pixel"}`.
2. The notification payload contains `notification.title`,
   `notification.body`, and a `data` map with `type`, `kind`, `name`,
   `event_id` and `channel_id`. The Android app must create a notification
   channel with id `synapse_general` (or whatever `fcm_android_channel_id` is
   set to) before displaying messages.
3. Re-registering the same token refreshes `last_seen`; the token stays unique
   per device.

## Project Structure

```
synapse/
├── main.go                    # Application entry point & route setup
├── internal/
│   ├── db/
│   │   └── db.go              # Database models & operations
│   ├── docker/
│   │   └── client.go          # Minimal Docker Engine client + event watcher
│   ├── kuma/
│   │   └── kuma.go            # Uptime Kuma client (REST + Socket.IO)
│   ├── sync/
│   │   ├── sync.go            # Docker & NPM sync logic
│   │   └── reconcile.go       # Desired-state reconciliation engine
│   ├── notify/
│   │   └── *.go               # Gotify notifications + event notifier
│   ├── authelia/
│   │   └── authelia.go        # Authelia config parser & sync
│   └── telemetry/
│       └── telemetry.go       # OpenTelemetry initialization
├── static/                    # Frontend HTML templates
├── testdata/                  # Test fixtures
├── Dockerfile                 # Multi-stage Docker build
├── e2e/                       # Playwright E2E tests
└── go.mod / go.sum            # Go module dependencies
```

## Development

```bash
task build        # Build binary
task run          # Build and run
task dev          # Hot reload (requires air)
task test         # Run tests
task lint         # Run go vet
task docker-build # Build Docker image
task docker-run   # Run Docker container
```

## License

MIT
