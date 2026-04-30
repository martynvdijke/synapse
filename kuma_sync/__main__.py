import argparse
import os
import sys

from kuma_sync.docker.sync import synccontainers
from kuma_sync.npm.sync import syncnpm


DEFAULT_KUMA_URL = os.getenv("KUMA_URL", "http://uptime-kuma:3001")
DEFAULT_KUMA_USER = os.getenv("KUMA_USER", "martynvandijke")
DEFAULT_KUMA_PASS = os.getenv("KUMA_PASS", "")

DEFAULT_NPM_HOST = os.getenv("NPM_HOST", "http://nginx:81")
DEFAULT_NPM_USER = os.getenv("NPM_USER", "admin")
DEFAULT_NPM_PASS = os.getenv("NPM_PASS", "admin")

DEFAULT_DOCKER_COMPOSE = os.getenv("COMPOSE_PATH", "docker-compose.yml")

DEFAULT_GOTIFY_URL = os.getenv("GOTIFY_URL", "http://gotify:80")
DEFAULT_GOTIFY_TOKEN = os.getenv("GOTIFY_TOKEN", "")


def add_docker_parser(subparsers):
    parser = subparsers.add_parser("docker", help="Sync docker-compose containers to Uptime Kuma")
    parser.add_argument("--compose", "-c", default=DEFAULT_DOCKER_COMPOSE, help="Path to docker-compose.yml")
    parser.add_argument("--kuma-url", default=DEFAULT_KUMA_URL, help="Uptime Kuma URL")
    parser.add_argument("--kuma-user", default=DEFAULT_KUMA_USER, help="Uptime Kuma username")
    parser.add_argument("--kuma-pass", default=DEFAULT_KUMA_PASS, help="Uptime Kuma password")
    parser.add_argument("--docker-host", help="Docker host name in Uptime Kuma")
    parser.add_argument("--dry-run", action="store_true", help="Print actions without executing")
    parser.add_argument(
        "--docker-socket",
        default="/var/run/docker.sock",
        help="Path to Docker socket (default: /var/run/docker.sock)"
    )
    parser.add_argument("--gotify-url", help="Gotify URL for alerts")
    parser.add_argument("--gotify-token", help="Gotify app token")
    parser.set_defaults(func=_run_docker)


def add_npm_parser(subparsers):
    parser = subparsers.add_parser("npm", help="Sync NPM proxy hosts to Uptime Kuma")
    parser.add_argument("--npm-host", default=DEFAULT_NPM_HOST, help="Nginx Proxy Manager host")
    parser.add_argument("--npm-user", default=DEFAULT_NPM_USER, help="NPM username")
    parser.add_argument("--npm-pass", default=DEFAULT_NPM_PASS, help="NPM password")
    parser.add_argument("--kuma-url", default=DEFAULT_KUMA_URL, help="Uptime Kuma URL")
    parser.add_argument("--kuma-user", default=DEFAULT_KUMA_USER, help="Uptime Kuma username")
    parser.add_argument("--kuma-pass", default=DEFAULT_KUMA_PASS, help="Uptime Kuma password")
    parser.add_argument("--parent-domain", help="Parent domain to strip from CNAMEs")
    parser.add_argument("--dry-run", action="store_true", help="Print actions without executing")
    parser.add_argument("--gotify-url", help="Gotify URL for alerts")
    parser.add_argument("--gotify-token", help="Gotify app token")
    parser.set_defaults(func=_run_npm)


def _run_docker(args):
    result = synccontainers(
        compose_path=args.compose,
        kuma_url=args.kuma_url,
        kuma_user=args.kuma_user,
        kuma_pass=args.kuma_pass,
        docker_host_name=args.docker_host,
        docker_socket=args.docker_socket,
        dry_run=args.dry_run,
        gotify_url=args.gotify_url,
        gotify_token=args.gotify_token,
    )
    print(f"\nDocker sync done: {result['added']} added, {result['skipped']} skipped")
    return 0


def _run_npm(args):
    result = syncnpm(
        npm_host=args.npm_host,
        npm_user=args.npm_user,
        npm_pass=args.npm_pass,
        kuma_url=args.kuma_url,
        kuma_user=args.kuma_user,
        kuma_pass=args.kuma_pass,
        parent_domain=args.parent_domain,
        dry_run=args.dry_run,
        gotify_url=args.gotify_url,
        gotify_token=args.gotify_token,
    )
    print(f"\nNPM sync done: {result['added']} added, {result['skipped']} skipped")
    return 0


def main():
    parser = argparse.ArgumentParser(
        prog="kuma-sync",
        description="Sync docker-compose containers and NPM proxy hosts to Uptime Kuma",
    )
    subparsers = parser.add_subparsers(dest="command", help="Available commands")

    add_docker_parser(subparsers)
    add_npm_parser(subparsers)

    args = parser.parse_args()

    if not args.command:
        parser.print_help()
        return 1

    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())