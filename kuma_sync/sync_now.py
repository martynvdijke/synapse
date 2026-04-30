#!/usr/bin/env python3
import yaml
import re
import requests

KUMA_URL = "http://uptime-kuma:3001"
KUMA_USER = "martynvandijke"
KUMA_PASS = "NckQTkKUIjT3VI3Nj7xi"
COMPOSE_PATH = "/docker-compose.yml"

from uptime_kuma_api import UptimeKumaApi, MonitorType


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
            hostname = service_data.get("container_name", service_name)
        return f"http://{hostname}:{port}{path}"
    return None


with open(COMPOSE_PATH, "r") as f:
    services = yaml.safe_load(f).get("services", {})

print(f"Found {len(services)} services")

with UptimeKumaApi(KUMA_URL) as api:
    api.login(KUMA_USER, KUMA_PASS)
    
    docker_hosts = api.get_docker_hosts()
    docker_host_id = docker_hosts[0]["id"] if docker_hosts else None
    print(f"Using Docker host: {docker_hosts[0]['name']} (ID: {docker_host_id})")
    
    existing = {m["name"]: m for m in api.get_monitors()}
    print(f"Existing monitors: {len(existing)}")

    added = 0
    skipped = 0

    for service_name, service_data in services.items():
        display_name = service_data.get("container_name", service_name)

        if display_name in existing:
            print(f"[-] {display_name}: already exists")
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