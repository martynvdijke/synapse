import subprocess
import yaml
import re
import requests


def send_gotify_alert(gotify_url, gotify_token, title, message, priority=5):
    """Send alert to Gotify"""
    try:
        resp = requests.post(
            f"{gotify_url}/message",
            data={"title": title, "message": message, "priority": priority},
            headers={"X-Token": gotify_token},
            timeout=10,
        )
        return resp.status_code == 200
    except Exception as e:
        print(f"[!] Failed to send Gotify alert: {e}")
        return False


class KumaAPI:
    """Direct REST API client for Uptime Kuma v2"""

    def __init__(self, url):
        self.url = url.rstrip("/")
        self.token = None
        self.session = requests.Session()

    def login(self, username, password):
        resp = self.session.post(
            f"{self.url}/api/login",
            json={"username": username, "password": password},
            timeout=30,
        )
        resp.raise_for_status()
        data = resp.json()
        self.token = data.get("token")
        if not self.token:
            raise Exception("No token received from login")
        return True

    def _headers(self):
        return {"Authorization": f"Bearer {self.token}"}

    def get_monitors(self):
        resp = self.session.get(
            f"{self.url}/api/monitors",
            headers=self._headers(),
            timeout=30,
        )
        resp.raise_for_status()
        return resp.json().get("monitors", [])

    def get_docker_hosts(self):
        resp = self.session.get(
            f"{self.url}/api/docker-hosts",
            headers=self._headers(),
            timeout=30,
        )
        resp.raise_for_status()
        return resp.json()

    def add_monitor(self, monitor_type, name, url=None, docker_container=None,
                   docker_host=None, interval=60, retry_interval=60, max_retries=3):
        payload = {
            "name": name,
            "type": monitor_type,
            "interval": interval,
            "retryInterval": retry_interval,
            "maxretries": max_retries,
            "conditions": [],
        }

        if monitor_type == "http":
            payload["url"] = url
            payload["method"] = "GET"
            payload["accepted_statuscodes"] = [200, 201, 204, 301, 302]
        elif monitor_type == "docker":
            payload["docker_container"] = docker_container
            payload["docker_host"] = docker_host

        resp = self.session.post(
            f"{self.url}/api/monitors",
            headers=self._headers(),
            json=payload,
            timeout=30,
        )
        resp.raise_for_status()
        return resp.json()


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


def get_services(compose_path):
    if not os.path.exists(compose_path):
        raise FileNotFoundError(f"docker-compose.yml not found: {compose_path}")

    with open(compose_path, "r") as f:
        compose_data = yaml.safe_load(f)

    return compose_data.get("services", {})


def _synccontainers_try_run(services, api, docker_host_id, dry_run=False, existing=None):
    added = 0
    skipped = 0
    new_monitors = []

    for service_name, service_data in services.items():
        display_name = service_data.get("container_name", service_name)

        if display_name in existing:
            print(f"[-] {display_name}: already exists, skipping")
            skipped += 1
            continue

        url = parse_healthcheck(service_name, service_data)

        if dry_run:
            if url:
                print(f"[+] {display_name}: would add HTTP monitor -> {url}")
            else:
                print(f"[+] {display_name}: would add Docker monitor")
            added += 1
            continue

        try:
            if url:
                print(f"[+] {display_name}: adding HTTP monitor -> {url}")
                api.add_monitor(
                    monitor_type="http",
                    name=display_name,
                    url=url,
                )
                new_monitors.append(display_name)
                added += 1
            else:
                container_id = service_data.get("container_name", service_name)
                print(f"[+] {display_name}: adding Docker monitor (container: {container_id})")
                api.add_monitor(
                    monitor_type="docker",
                    name=display_name,
                    docker_container=container_id,
                    docker_host=docker_host_id,
                )
                new_monitors.append(display_name)
                added += 1
        except Exception as e:
            print(f"[!] {display_name}: failed - {e}")

    return {"added": added, "skipped": skipped, "new_monitors": new_monitors}


def synccontainers(compose_path, kuma_url, kuma_user, kuma_pass, docker_host_name=None,
                 docker_socket="/var/run/docker.sock", dry_run=False,
                 gotify_url=None, gotify_token=None):
    import os

    services = get_services(compose_path)
    if not services:
        print("No services found in docker-compose.yml")
        return {"added": 0, "skipped": 0}

    print(f"Found {len(services)} services in {compose_path}")

    if dry_run:
        return _synccontainers_try_run(services, None, None, dry_run=True)

    api = KumaAPI(kuma_url)
    try:
        api.login(kuma_user, kuma_pass)
    except Exception as e:
        print(f"Login failed: {e}")
        return {"added": 0, "skipped": 0}

    docker_hosts = api.get_docker_hosts()
    docker_host_id = None
    for host in docker_hosts:
        if docker_host_name and host.get("name") == docker_host_name:
            docker_host_id = host.get("id")
            break
    if not docker_host_id and docker_hosts:
        docker_host_id = docker_hosts[0].get("id")
        print(f"Using Docker host: {docker_hosts[0].get('name')} (ID: {docker_host_id})")
    else:
        print("No Docker host found in Uptime Kuma")
        print("Please add a Docker host first via the Uptime Kuma UI")
        return {"added": 0, "skipped": 0}

    existing = {m["name"]: m for m in api.get_monitors()}
    print(f"Existing monitors: {len(existing)}")

    result = _synccontainers_try_run(services, api, docker_host_id, dry_run=False, existing=existing)

    if gotify_url and gotify_token and result["added"] > 0:
        msg = f"Added {result['added']} new monitors to Uptime Kuma:\n"
        msg += "\n".join(f"• {s}" for s in result["new_monitors"][:10])
        if len(result["new_monitors"]) > 10:
            msg += f"\n... and {len(result['new_monitors']) - 10} more"
        send_gotify_alert(gotify_url, gotify_token, "New Monitors Added", msg)

    return result