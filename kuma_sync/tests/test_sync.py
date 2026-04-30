import pytest
import yaml
import os
import tempfile
from pathlib import Path

from kuma_sync.docker.sync import (
    parse_healthcheck,
    get_services,
)


class TestParseHealthcheck:
    def test_parse_healthcheck_basic(self):
        service_data = {
            "healthcheck": {
                "test": ["CMD", "curl", "-f", "http://localhost:3000/"]
            }
        }
        url = parse_healthcheck("test-service", service_data)
        assert url == "http://test-service:3000/"

    def test_parse_healthcheck_with_port(self):
        service_data = {
            "healthcheck": {
                "test": ["CMD", "curl", "-f", "http://localhost:8123/"]
            }
        }
        url = parse_healthcheck("homeassistant", service_data)
        assert url == "http://homeassistant:8123/"

    def test_parse_healthcheck_string_format(self):
        service_data = {
            "healthcheck": {
                "test": "curl -f http://localhost:80/"
            }
        }
        url = parse_healthcheck("test-service", service_data)
        assert url == "http://test-service:80/"

    def test_parse_healthcheck_default_port(self):
        service_data = {
            "healthcheck": {
                "test": ["CMD", "curl", "-f", "http://localhost/"]
            }
        }
        url = parse_healthcheck("test-service", service_data)
        assert url == "http://test-service:80/"

    def test_parse_healthcheck_with_service_prefix(self):
        service_data = {
            "healthcheck": {
                "test": ["CMD", "curl", "-f", "http://localhost:8112/"]
            },
            "network_mode": "service:gluetun"
        }
        url = parse_healthcheck("deluge", service_data)
        assert url == "http://gluetun:8112/"

    def test_parse_healthcheck_no_healthcheck(self):
        service_data = {}
        url = parse_healthcheck("test-service", service_data)
        assert url is None

    def test_parse_healthcheck_no_http_in_test(self):
        service_data = {
            "healthcheck": {
                "test": ["CMD", "pg_isready"]
            }
        }
        url = parse_healthcheck("test-service", service_data)
        assert url is None

    def test_parse_healthcheck_uses_container_name(self):
        service_data = {
            "container_name": "my-custom-container",
            "healthcheck": {
                "test": ["CMD", "curl", "-f", "http://localhost:80/"]
            }
        }
        url = parse_healthcheck("my-service", service_data)
        assert url == "http://my-custom-container:80/"


class TestGetServices:
    def test_get_services_basic(self):
        compose_data = {
            "services": {
                "web": {"image": "nginx"},
                "db": {"image": "postgres"}
            }
        }
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yml', delete=False) as f:
            yaml.dump(compose_data, f)
            f.flush()
            
            services = get_services(f.name)
            assert len(services) == 2
            assert "web" in services
            assert "db" in services
            
            os.unlink(f.name)

    def test_get_services_no_services(self):
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yml', delete=False) as f:
            yaml.dump({}, f)
            f.flush()
            
            services = get_services(f.name)
            assert services == {}
            
            os.unlink(f.name)

    def test_get_services_file_not_found(self):
        with pytest.raises(FileNotFoundError):
            get_services("/nonexistent/docker-compose.yml")


class TestRealDockerCompose:
    """Tests using the real docker-compose file from homelab"""
    
    COMPOSE_PATH = "/root/homelab/deathstar/docker-compose.yml"
    
    @pytest.fixture
    def real_compose_path(self):
        return self.COMPOSE_PATH
    
    def test_get_services_real_file(self, real_compose_path):
        if not os.path.exists(real_compose_path):
            pytest.skip("docker-compose.yml not found")
        
        services = get_services(real_compose_path)
        assert services is not None
        assert len(services) > 0
    
    def test_parse_healthcheck_for_known_services(self, real_compose_path):
        if not os.path.exists(real_compose_path):
            pytest.skip("docker-compose.yml not found")
        
        services = get_services(real_compose_path)
        
        parsed = []
        for service_name, service_data in services.items():
            url = parse_healthcheck(service_name, service_data)
            if url:
                parsed.append((service_name, url))
        
        assert len(parsed) > 0
        
        expected_services = [
            ("homeassistant", "http://homeassistant:8123/"),
            ("vandijke.xyz", "http://vandijke/"),
            ("nginx", "http://nginx:81/"),
            ("uptime-kuma", "http://uptime-kuma:3001/"),
        ]
        
        for name, expected_url in expected_services:
            matching = [p for p in parsed if p[0] == name]
            assert len(matching) > 0, f"Service {name} should have a healthcheck URL"