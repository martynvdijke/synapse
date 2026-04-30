from kuma_sync.docker.sync import synccontainers

result = synccontainers(
    compose_path='/app/docker-compose.yml',
    kuma_url='http://uptime-kuma:3001',
    kuma_user='martynvandijke',
    kuma_pass='NckQTkKUIjT3VI3Nj7xi'
)
print(result)