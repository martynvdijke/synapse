// Tab data loaders (Docker, Kuma, NPM, History)

export function loadDockerServices() {
    document.getElementById('docker-tbody').innerHTML = loadingRow(5);
    apiFetch('/api/services').then(function(r){return r.json();}).then(function(services) {
        var tbody = document.getElementById('docker-tbody');
        if (services.error) {
            tbody.innerHTML = '<tr><td colspan="5" class="text-center text-danger py-3">' + esc(services.error) + '</td></tr>';
            return;
        }
        if (!services.length) {
            tbody.innerHTML = emptyRow(5, 'No services found');
            return;
        }
        tbody.innerHTML = services.map(function(s) {
            return '<tr>'
                + '<td data-label="Service"><code>' + esc(s.name) + '</code></td>'
                + '<td data-label="Container">' + esc(s.container_name) + '</td>'
                + '<td data-label="Type"><span class="badge ' + (s.type === 'http' ? 'bg-info' : 'bg-secondary') + '">' + s.type.toUpperCase() + '</span></td>'
                + '<td data-label="URL" class="text-truncate" style="max-width:300px">' + (s.url ? '<a href="' + esc(s.url) + '">' + esc(s.url) + '</a>' : '—') + '</td>'
                + '<td data-label="In Kuma">' + (s.in_kuma
                    ? '<span class="badge bg-success">\u2713 In Kuma</span>'
                    : '<span class="badge bg-secondary">\u2717 Missing</span>') + '</td>'
                + '</tr>';
        }).join('');
    });
}

export function loadKumaMonitors() {
    document.getElementById('kuma-tbody').innerHTML = loadingRow(4);
    apiFetch('/api/monitors').then(function(r){return r.json();}).then(function(monitors) {
        var tbody = document.getElementById('kuma-tbody');
        if (monitors.error) {
            tbody.innerHTML = '<tr><td colspan="4" class="text-center text-danger py-3">' + esc(monitors.error) + '</td></tr>';
            return;
        }
        if (!monitors.length) {
            tbody.innerHTML = emptyRow(4, 'No monitors in Uptime Kuma');
            return;
        }
        tbody.innerHTML = monitors.map(function(m) {
            return '<tr>'
                + '<td data-label="ID">#' + m.id + '</td>'
                + '<td data-label="Name">' + esc(m.name) + '</td>'
                + '<td data-label="Type"><span class="badge ' + (m.type === 'http' ? 'bg-info' : m.type === 'docker' ? 'bg-warning text-dark' : 'bg-secondary') + '">' + (m.type === 'http' ? '\u25CB ' : m.type === 'docker' ? '\u25A3 ' : '') + m.type.toUpperCase() + '</span></td>'
                + '<td data-label="URL / Container" class="text-truncate" style="max-width:300px">' + (m.url ? esc(m.url) : m.docker_container ? esc(m.docker_container) : '—') + '</td>'
                + '</tr>';
        }).join('');
    });
}

export function loadNPMProxies() {
    document.getElementById('npm-tbody').innerHTML = loadingRow(3);
    apiFetch('/api/proxies').then(function(r){return r.json();}).then(function(proxies) {
        var tbody = document.getElementById('npm-tbody');
        if (proxies.error) {
            tbody.innerHTML = '<tr><td colspan="3" class="text-center text-danger py-3">' + esc(proxies.error) + '</td></tr>';
            return;
        }
        if (!proxies.length) {
            tbody.innerHTML = emptyRow(3, 'No proxy hosts found');
            return;
        }
        tbody.innerHTML = proxies.map(function(p) {
            return '<tr>'
                + '<td data-label="Domain"><code>' + esc(p.cname) + '</code></td>'
                + '<td data-label="Container">' + esc(p.container) + '</td>'
                + '<td data-label="In Kuma">' + (p.in_kuma
                    ? '<span class="badge bg-success">\u2713 In Kuma</span>'
                    : '<span class="badge bg-secondary">\u2717 Missing</span>') + '</td>'
                + '</tr>';
        }).join('');
    });
}

export function loadHistory() {
    document.getElementById('history-tbody').innerHTML = loadingRow(8);
    apiFetch('/api/sync/history').then(function(r){return r.json();}).then(function(runs) {
        var tbody = document.getElementById('history-tbody');
        if (!runs.length) {
            tbody.innerHTML = emptyRow(8, 'No sync history yet');
            return;
        }
        tbody.innerHTML = runs.map(function(r) {
            var badge = 'bg-primary';
            if (r.status === 'completed') badge = 'bg-success';
            else if (r.status === 'completed_with_errors') badge = 'bg-warning text-dark';
            else if (r.status === 'error') badge = 'bg-danger';
            var statusIcon = r.status === 'completed' ? '\u2713' : r.status === 'completed_with_errors' ? '\u26A0' : r.status === 'error' ? '\u2717' : '\u25CB';
            return '<tr>'
                + '<td data-label="ID">#' + r.id + '</td>'
                + '<td data-label="Source"><span class="badge ' + (r.source === 'docker' ? 'bg-primary' : 'bg-success') + '">' + r.source + '</span></td>'
                + '<td data-label="Status"><span class="badge ' + badge + '">' + statusIcon + ' ' + r.status + '</span></td>'
                + '<td data-label="Started" class="small">' + (r.started_at ? new Date(r.started_at).toLocaleString() : '') + '</td>'
                + '<td data-label="Added">' + r.added + '</td>'
                + '<td data-label="Skipped">' + r.skipped + '</td>'
                + '<td data-label="Failed">' + r.failed + '</td>'
                + '<td data-label="Error" class="small text-danger">' + esc(r.error_message || '') + '</td>'
                + '</tr>';
        }).join('');
    });
}

window.loadDockerServices = loadDockerServices;
window.loadKumaMonitors = loadKumaMonitors;
window.loadNPMProxies = loadNPMProxies;
window.loadHistory = loadHistory;
