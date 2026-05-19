# Synapse

Sync Docker Compose containers and Nginx Proxy Manager hosts to Uptime Kuma for monitoring.

A single-binary web application that provides a dashboard for managing your Uptime Kuma monitors. Supports both Docker container monitors and HTTP monitors from NPM proxy hosts.

## Quick Start

```sh
go build -o synapse-server .
./synapse-server
```

Then open `http://localhost:6270`.

## Environment Variables

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

## Tasks

```sh
task build        # Build binary
task run          # Build and run
task dev          # Hot reload (requires air)
task test         # Run tests
task lint         # Run go vet
task docker-build # Build Docker image
task docker-run   # Run Docker container
```

## Docker

```sh
docker build -t synapse .
docker run -p 6270:6270 -e KUMA_PASS=yourpass synapse
```
