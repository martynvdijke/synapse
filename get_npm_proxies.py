#!/usr/bin/env python3
import os
import requests
from requests.auth import HTTPBasicAuth

NPM_HOST = os.environ.get("NPM_HOST", "http://localhost:81")
NPM_USER = os.environ.get("NPM_USER", "admin")
NPM_PASS = os.environ.get("NPM_PASS", "admin")


def get_proxy_hosts():
    url = f"{NPM_HOST}/api/nginx/proxy-hosts"
    resp = requests.get(url, auth=HTTPBasicAuth(NPM_USER, NPM_PASS))
    resp.raise_for_status()
    return resp.json()


def get_cname_to_container():
    hosts = get_proxy_hosts()
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


def main():
    proxies = get_cname_to_container()
    print("CNAME -> Container mappings:")
    for p in proxies:
        print(f"  {p['cname']} -> {p['container']}")


if __name__ == "__main__":
    main()