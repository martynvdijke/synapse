import sys
import os

# Add app to path
sys.path.insert(0, '/app')
os.chdir('/app')

# Import directly
import importlib.util
spec = importlib.util.spec_from_file_location("sync", "/app/kuma_sync/docker/sync.py")
sync_module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sync_module)

result = sync_module.synccontainers(
    compose_path='/docker-compose.yml',
    kuma_url='http://uptime-kuma:3001',
    kuma_user='martynvandijke',
    kuma_pass='NckQTkKUIjT3VI3Nj7xi'
)
print(result)