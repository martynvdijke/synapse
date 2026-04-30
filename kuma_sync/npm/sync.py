import requests
from requests.auth import HTTPBasicAuth


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

    def add_monitor(self, monitor_type, name, url=None, interval=60, retry_interval=60, max_retries=3):
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

        resp = self.session.post(
            f"{self.url}/api/monitors",
            headers=self._headers(),
            json=payload,
            timeout=30,
        )
        resp.raise_for_status()
        return resp.json()


def get_proxy_hosts(npm_host, npm_user, npm_pass):
    url = f"{npm_host}/api/nginx/proxy-hosts"
    resp = requests.get(url, auth=HTTPBasicAuth(npm_user, npm_pass))
    resp.raise_for_status()
    return resp.json()


def get_cname_to_container_mapping(npm_host, npm_user, npm_pass):
    hosts = get_proxy_hosts(npm_host, npm_user, npm_pass)
    cnames = []
    for host in hosts:
        domain = host.get("domain_names", [])
        forwarding = host.get("forwarding", {})
        if not domain or not forwarding:
            continue
        container = forwarding.get("container")
        if container:
            for d in domain:
                cnames.append({"cname": d, "container": container})
    return cnames


def syncnpm(npm_host, npm_user, npm_pass, kuma_url, kuma_user, kuma_pass,
            parent_domain=None, dry_run=False, gotify_url=None, gotify_token=None):
    proxies = get_cname_to_container_mapping(npm_host, npm_user, npm_pass)
    if not proxies:
        print("No proxy hosts found in NPM")
        return {"added": 0, "skipped": 0}

    print(f"Found {len(proxies)} proxy hosts in NPM")

    if dry_run:
        added = 0
        skipped = 0
        for proxy in proxies:
            print(f"[+] {proxy['cname']}: would add HTTP monitor -> http://{proxy['cname']}")
            added += 1
        return {"added": added, "skipped": skipped}

    api = KumaAPI(kuma_url)
    try:
        api.login(kuma_user, kuma_pass)
    except Exception as e:
        print(f"Login failed: {e}")
        return {"added": 0, "skipped": 0}

    existing = {m["name"]: m for m in api.get_monitors()}
    print(f"Existing monitors: {len(existing)}")

    added = 0
    skipped = 0
    new_monitors = []

    for proxy in proxies:
        cname = proxy["cname"]
        monitor_name = cname

        if parent_domain and cname.endswith("." + parent_domain):
            monitor_name = cname[: -len(parent_domain) - 1]

        if monitor_name in existing:
            print(f"[-] {monitor_name}: already exists, skipping")
            skipped += 1
            continue

        try:
            print(f"[+] {monitor_name}: adding HTTP monitor -> http://{cname}")
            api.add_monitor(
                monitor_type="http",
                name=monitor_name,
                url=f"http://{cname}",
            )
            new_monitors.append(monitor_name)
            added += 1
        except Exception as e:
            print(f"[!] {monitor_name}: failed - {e}")

    if gotify_url and gotify_token and added > 0:
        msg = f"Added {added} new NPM proxy monitors:\n"
        msg += "\n".join(f"• {s}" for s in new_monitors[:10])
        if len(new_monitors) > 10:
            msg += f"\n... and {len(new_monitors) - 10} more"
        send_gotify_alert(gotify_url, gotify_token, "New NPM Monitors Added", msg)

    return {"added": added, "skipped": skipped}