import pytest
from unittest.mock import Mock, patch, MagicMock

from kuma_sync.npm.sync import (
    get_proxy_hosts,
    get_cname_to_container_mapping,
)


class TestGetProxyHosts:
    @patch('kuma_sync.npm.sync.requests.get')
    def test_get_proxy_hosts_basic(self, mock_get):
        mock_response = Mock()
        mock_response.json.return_value = [{"id": 1, "domain_names": ["example.com"]}]
        mock_get.return_value = mock_response
        
        result = get_proxy_hosts("http://localhost:81", "admin", "admin")
        
        assert len(result) == 1
        mock_get.assert_called_once()

    @patch('kuma_sync.npm.sync.requests.get')
    def test_get_proxy_hosts_raises_on_error(self, mock_get):
        mock_response = Mock()
        mock_response.raise_for_status.side_effect = Exception("API Error")
        mock_get.return_value = mock_response
        
        with pytest.raises(Exception):
            get_proxy_hosts("http://localhost:81", "admin", "admin")


class TestGetCnameToContainerMapping:
    @patch('kuma_sync.npm.sync.get_proxy_hosts')
    def test_get_cname_to_container_mapping_basic(self, mock_get_hosts):
        mock_get_hosts.return_value = [
            {
                "domain_names": ["app.example.com", "www.example.com"],
                "forwarding": {
                    "container": "myapp",
                    "protocol": "http",
                }
            }
        ]
        
        result = get_cname_to_container_mapping("http://localhost:81", "admin", "admin")
        
        assert len(result) == 2
        assert result[0] == {"cname": "app.example.com", "container": "myapp"}
        assert result[1] == {"cname": "www.example.com", "container": "myapp"}

    @patch('kuma_sync.npm.sync.get_proxy_hosts')
    def test_get_cname_to_container_mapping_empty_domains(self, mock_get_hosts):
        mock_get_hosts.return_value = [
            {
                "domain_names": [],
                "forwarding": {"container": "myapp"}
            }
        ]
        
        result = get_cname_to_container_mapping("http://localhost:81", "admin", "admin")
        
        assert result == []

    @patch('kuma_sync.npm.sync.get_proxy_hosts')
    def test_get_cname_to_container_mapping_no_forwarding(self, mock_get_hosts):
        mock_get_hosts.return_value = [
            {
                "domain_names": ["example.com"]
            }
        ]
        
        result = get_cname_to_container_mapping("http://localhost:81", "admin", "admin")
        
        assert result == []

    @patch('kuma_sync.npm.sync.get_proxy_hosts')
    def test_get_cname_to_container_mapping_no_container(self, mock_get_hosts):
        mock_get_hosts.return_value = [
            {
                "domain_names": ["example.com"],
                "forwarding": {}
            }
        ]
        
        result = get_cname_to_container_mapping("http://localhost:81", "admin", "admin")
        
        assert result == []