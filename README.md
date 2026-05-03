# Pulsenode

Sync Docker containers and Nginx Proxy Manager hosts to Uptime Kuma for monitoring. Works with local or remote Uptime Kuma instances.

## Commands

**Sync Docker containers from docker-compose.yml:**

```sh
go run ./cmd/kuma-sync docker --compose=docker-compose.yml --kuma-url=http://uptime-kuma:3001 --docker-host=Deathstar
```

**Sync NPM proxy hosts:**

```sh
go run ./cmd/kuma-sync npm --npm-host=http://nginx:81 --kuma-url=http://uptime-kuma:3001
```

**Dry run** (preview without creating monitors):

```sh
go run ./cmd/kuma-sync docker --dry-run
```

## Environment variables

Configure via environment variables or `.env-kuma-sync`:

| Variable | Default | Description |
|----------|---------|-------------|
| `KUMA_URL` | `http://uptime-kuma:3001` | Uptime Kuma base URL |
| `KUMA_USER` | `martynvandijke` | Uptime Kuma username |
| `KUMA_PASS` | - | Uptime Kuma password |
| `NPM_HOST` | `http://nginx:81` | Nginx Proxy Manager URL |
| `NPM_USER` | `admin` | NPM username |
| `NPM_PASS` | `admin` | NPM password |
| `COMPOSE_PATH` | `docker-compose.yml` | Path to docker-compose file |
| `DOCKER_HOST_NAME` | - | Docker host name in Uptime Kuma |
| `GOTIFY_URL` | - | Gotify server URL |
| `GOTIFY_TOKEN` | - | Gotify app token |

## Build

```sh
go build -o pulsenode ./cmd/kuma-sync
```
