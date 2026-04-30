#!/usr/bin/env python3
import yaml
import re
import requests
import sys

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
    print(f"Docker hosts: {[h['name'] for h in docker_hosts]}")
    docker_host_id = docker_hosts[0]["id"] if docker_hosts else None
    
    existing = {m["name"]: m for m in api.get_monitors()}
    print(f"Existing monitors: {len(existing)}")

    for service_name, service_data in list(services.items())[:5]:
        display_name = service_data.get("container_name", service_name)
        print(f"Checking: {display_name} (exists: {display_name in existing})")