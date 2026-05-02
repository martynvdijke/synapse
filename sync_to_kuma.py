import yaml
import re
import os
import subprocess
from uptime_kuma_api import UptimeKumaApi, MonitorType

KUMA_URL = os.getenv("KUMA_URL", "http://uptime-kuma:3001")
KUMA_USER = os.getenv("KUMA_USER", "martynvandijke")
KUMA_PASS = os.getenv("KUMA_PASS", "uk2_u0JZ3sHqhj7tRRu-kjjA3mi8flv-HDljLY6sniK9")
DOCKER_HOST_NAME = os.getenv("DOCKER_HOST_NAME", "local")


def get_docker_host_id(api):
    docker_hosts = api.get_docker_hosts()
    for host in docker_hosts:
        if host["name"] == DOCKER_HOST_NAME:
            return host["id"]
    if docker_hosts:
        return docker_hosts[0]["id"]
    return None


def get_container_ip(container_name):
    try:
        result = subprocess.run(
            ["docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", container_name],
            capture_output=True, text=True, check=True
        )
        ip = result.stdout.strip()
        if ip:
            return ip
    except Exception:
        pass
    return None


def parse_healthcheck(service_name, service_data):
    healthcheck = service_data.get("healthcheck", {})
    test = healthcheck.get("test", [])

    if isinstance(test, list):
        test_str = " ".join(test)
    else:
        test_str = str(test)

    match = re.search(r"https?://(?:localhost|127\.0\.0\.1)(?::(\d+))?(/[\w/.-]*)?", test_str)
    if match:
        port = match.group(1) or "80"
        path = match.group(2) or "/"

        network_mode = service_data.get("network_mode", "")
        if network_mode.startswith("service:"):
            hostname = network_mode.split(":")[1]
        else:
            hostname = service_name

        return f"http://{hostname}:{port}{path}"
    return None


def main():
    if not os.path.exists("docker-compose.yml"):
        print("Error: docker-compose.yml not found")
        return

    try:
        with open("docker-compose.yml", "r") as f:
            compose_data = yaml.safe_load(f)
    except Exception as e:
        print(f"Error reading docker-compose.yml: {e}")
        return

    services = compose_data.get("services", {})
    if not services:
        print("No services found")
        return

    print(f"Found {len(services)} services")

    with UptimeKumaApi(KUMA_URL) as api:
        try:
            api.login(KUMA_USER, KUMA_PASS)
        except Exception as e:
            print(f"Login failed: {e}")
            return

        docker_host_id = get_docker_host_id(api)
        if not docker_host_id:
            print("No Docker host found in Uptime Kuma")
            print("Please add a Docker host first via the Uptime Kuma UI")
            return

        print(f"Using Docker host ID: {docker_host_id}")

        existing = {m["name"]: m for m in api.get_monitors()}

        added = 0
        skipped = 0

        for service_name, service_data in services.items():
            display_name = service_data.get("container_name", service_name)

            if display_name in existing:
                print(f"[-] {display_name}: already exists, skipping")
                skipped += 1
                continue

            url = parse_healthcheck(service_name, service_data)

            try:
                if url:
                    print(f"[+] {display_name}: adding HTTP monitor -> {url}")
                    api.add_monitor(
                        type=MonitorType.HTTP,
                        name=display_name,
                        url=url,
                        interval=60,
                        retryInterval=60,
                        maxretries=3,
                    )
                    added += 1
                else:
                    container_id = service_data.get("container_name", service_name)
                    print(f"[+] {display_name}: adding Docker monitor (container: {container_id})")
                    api.add_monitor(
                        type=MonitorType.DOCKER,
                        name=display_name,
                        docker_container=container_id,
                        docker_host=docker_host_id,
                        interval=60,
                        retryInterval=60,
                        maxretries=3,
                    )
                    added += 1
            except Exception as e:
                print(f"[!] {display_name}: failed - {e}")

    print(f"\nDone: {added} added, {skipped} skipped")


if __name__ == "__main__":
    main()