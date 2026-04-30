# kuma-sync

Sync docker-compose containers and Nginx Proxy Manager proxy hosts to Uptime Kuma for monitoring.

## Features

- Sync all containers from a docker-compose.yml to Uptime Kuma
- Automatic healthcheck URL detection from compose file
- Support for Docker container monitoring via Uptime Kuma Docker Host
- Sync Nginx Proxy Manager proxy hosts to Uptime Kuma
- Multiple output support (sync to multiple Uptime Kuma instances)
- Dry-run mode for preview

## Installation

```bash
pip install -e .
```

Or using uv:

```bash
uv pip install -e .
```

## Usage

### Sync Docker Compose Containers

```bash
# Basic usage
kuma-sync docker --compose docker-compose.yml \
  --kuma-url http://uptime-kuma:3001 \
  --kuma-user admin \
  --kuma-pass your-password

# With Docker host name
kuma-sync docker --compose docker-compose.yml \
  --kuma-url http://uptime-kuma:3001 \
  --kuma-user admin \
  --kuma-pass password \
  --docker-host "local"

# Dry run (preview)
kuma-sync docker --compose docker-compose.yml --dry-run
```

### Sync NPM Proxy Hosts

```bash
# Basic usage
kuma-sync npm \
  --npm-host http://localhost:81 \
  --npm-user admin \
  --npm-pass admin \
  --kuma-url http://uptime-kuma:3001 \
  --kuma-user admin \
  --kuma-pass password

# With parent domain stripping
kuma-sync npm \
  --npm-host http://localhost:81 \
  --npm-user admin \
  --npm-pass admin \
  --kuma-url http://uptime-kuma:3001 \
  --kuma-user admin \
  --kuma-pass password \
  --parent-domain example.com
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `KUMA_URL` | Uptime Kuma URL | `http://uptime-kuma:3001` |
| `KUMA_USER` | Uptime Kuma username | `admin` |
| `KUMA_PASS` | Uptime Kuma password | `` |
| `NPM_HOST` | Nginx Proxy Manager URL | `http://localhost:81` |
| `NPM_USER` | NPM username | `admin` |
| `NPM_PASS` | NPM password | `admin` |
| `COMPOSE_PATH` | Path to docker-compose.yml | `docker-compose.yml` |

## Docker Compose Healthcheck Detection

The tool automatically detects healthcheck URLs from your docker-compose.yml:

```yaml
services:
  homeassistant:
    container_name: homeassistant
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8123/"]
```

This will create an HTTP monitor pointing to `http://homeassistant:8123/`.

### Service Prefix

If a container uses `network_mode: service:container`, the healthcheck URL will use the correct hostname:

```yaml
  deluge:
    network_mode: service:gluetun
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8112/"]
```

This will create a monitor pointing to `http://gluetun:8112/`.

## Development

### Install Dependencies

```bash
pip install -e ".[dev]"
```

### Run Tests

```bash
pytest kuma_sync/tests/ -v
```

## Project Structure

```
kuma_sync/
├── __main__.py          # CLI entry point
├── docker/
│   └── sync.py        # Docker compose sync logic
├── npm/
│   └── sync.py       # NPM proxy sync logic
├── tests/
│   ├── test_sync.py  # Docker sync tests
│   └── test_npm.py # NPM sync tests
└── pyproject.toml   # Project config
```

## License

MIT