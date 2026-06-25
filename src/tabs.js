// Tab data loaders (Docker, Kuma, NPM, History)

function renderDockerDetailRow(svc) {
    var fields = [];

    function addField(label, value) {
        if (value === null || value === undefined || value === '' || (Array.isArray(value) && value.length === 0) || (typeof value === 'object' && !Array.isArray(value) && Object.keys(value).length === 0)) {
            return;
        }
        if (Array.isArray(value)) {
            fields.push('<div class="detail-field"><span class="detail-label">' + label + '</span><span class="detail-value">' + value.map(function(v) { return esc(String(v)); }).join('<br>') + '</span></div>');
        } else if (typeof value === 'object') {
            var inner = '';
            for (var k in value) {
                if (value.hasOwnProperty(k)) {
                    inner += '<span class="detail-inline-label">' + esc(k) + ':</span> ' + esc(String(value[k])) + '<br>';
                }
            }
            if (inner) {
                fields.push('<div class="detail-field"><span class="detail-label">' + label + '</span><span class="detail-value">' + inner + '</span></div>');
            }
        } else {
            fields.push('<div class="detail-field"><span class="detail-label">' + label + '</span><span class="detail-value">' + esc(String(value)) + '</span></div>');
        }
    }

    addField('Image', svc.image);
    addField('Ports', svc.ports);
    addField('Environment', svc.environment);
    addField('Volumes', svc.volumes);
    addField('Depends On', svc.depends_on);
    addField('Labels', svc.labels);
    addField('Restart', svc.restart);
    addField('Command', svc.command);
    addField('Entrypoint', svc.entrypoint);
    addField('User', svc.user);
    addField('Working Dir', svc.working_dir);

    // Healthcheck
    if (svc.healthcheck) {
        var hc = svc.healthcheck;
        var hcHtml = '';
        if (hc.test) {
            if (Array.isArray(hc.test)) {
                hcHtml += '<span class="detail-inline-label">test:</span> ' + esc(hc.test.join(' ')) + '<br>';
            } else {
                hcHtml += '<span class="detail-inline-label">test:</span> ' + esc(String(hc.test)) + '<br>';
            }
        }
        if (hc.interval) hcHtml += '<span class="detail-inline-label">interval:</span> ' + esc(hc.interval) + '<br>';
        if (hc.timeout) hcHtml += '<span class="detail-inline-label">timeout:</span> ' + esc(hc.timeout) + '<br>';
        if (hc.retries) hcHtml += '<span class="detail-inline-label">retries:</span> ' + hc.retries + '<br>';
        if (hc.start_period) hcHtml += '<span class="detail-inline-label">start_period:</span> ' + esc(hc.start_period) + '<br>';
        fields.push('<div class="detail-field"><span class="detail-label">Healthcheck</span><span class="detail-value">' + hcHtml + '</span></div>');
    } else {
        fields.push('<div class="detail-field"><span class="detail-label">Healthcheck</span><span class="detail-value text-muted">Not configured</span></div>');
    }

    return fields.length ? '<div class="detail-container">' + fields.join('') + '</div>' : '';
}

export function loadDockerServices() {
    document.getElementById('docker-tbody').innerHTML = loadingRow(6);
    apiFetch('/api/services').then(function(r){return r.json();}).then(function(services) {
        var tbody = document.getElementById('docker-tbody');
        if (services.error) {
            tbody.innerHTML = '<tr><td colspan="6" class="text-center text-danger py-3">' + esc(services.error) + '</td></tr>';
            return;
        }
        if (!services.length) {
            tbody.innerHTML = emptyRow(6, 'No services found');
            return;
        }
        var rows = [];
        services.forEach(function(s, idx) {
            rows.push('<tr class="docker-service-row" data-idx="' + idx + '" onclick="toggleDockerDetail(this)">'
                + '<td data-label="Service"><code>' + esc(s.name) + '</code></td>'
                + '<td data-label="Container">' + esc(s.container_name) + '</td>'
                + '<td data-label="Image">' + (s.image ? '<code>' + esc(s.image) + '</code>' : '—') + '</td>'
                + '<td data-label="Type"><span class="badge ' + (s.type === 'http' ? 'bg-info' : 'bg-secondary') + '">' + s.type.toUpperCase() + '</span></td>'
                + '<td data-label="URL" class="text-truncate" style="max-width:250px">' + (s.url ? '<a href="' + esc(s.url) + '">' + esc(s.url) + '</a>' : '—') + '</td>'
                + '<td data-label="In Kuma">' + (s.in_kuma
                    ? '<span class="badge bg-success">\u2713 In Kuma</span>'
                    : '<span class="badge bg-secondary">\u2717 Missing</span>') + '</td>'
                + '</tr>');
            var detailHtml = renderDockerDetailRow(s);
            if (detailHtml) {
                rows.push('<tr class="docker-detail-row" data-idx="' + idx + '" style="display:none"><td colspan="6">' + detailHtml + '</td></tr>');
            }
        });
        tbody.innerHTML = rows.join('');
    });
}

window.toggleDockerDetail = function(row) {
    var idx = row.getAttribute('data-idx');
    var detailRow = row.parentNode.querySelector('.docker-detail-row[data-idx="' + idx + '"]');
    if (detailRow) {
        var isVisible = detailRow.style.display !== 'none';
        detailRow.style.display = isVisible ? 'none' : 'table-row';
        row.classList.toggle('detail-expanded', !isVisible);
    }
};

// ─── Monitor detail stats cache ────────────────────────────────
var monitorStatsCache = new Map();
var STATS_CACHE_TTL = 60000; // 60 seconds

function getCachedStats(instanceId, monitorId) {
    var key = instanceId + ':' + monitorId;
    var entry = monitorStatsCache.get(key);
    if (entry && Date.now() - entry.timestamp < STATS_CACHE_TTL) return entry.stats;
    return null;
}

function renderMonitorStats(stats) {
    var statusBadge = stats.status === 1
        ? '<span class="badge bg-success">UP</span>'
        : stats.status === 0
        ? '<span class="badge bg-danger">DOWN</span>'
        : '<span class="badge bg-secondary">UNKNOWN</span>';

    return '<div class="row g-3">'
        + '<div class="col-md-4"><div class="small text-muted">Status</div><div>' + statusBadge + '</div></div>'
        + '<div class="col-md-4"><div class="small text-muted">Uptime 24h</div><div class="fs-5 fw-bold">' + (stats.uptime_24h != null ? stats.uptime_24h.toFixed(1) + '%' : '—') + '</div></div>'
        + '<div class="col-md-4"><div class="small text-muted">Uptime 7d</div><div class="fs-5 fw-bold">' + (stats.uptime_7d != null ? stats.uptime_7d.toFixed(1) + '%' : '—') + '</div></div>'
        + '<div class="col-md-4"><div class="small text-muted">Uptime 1y</div><div class="fs-5 fw-bold">' + (stats.uptime_1y != null ? stats.uptime_1y.toFixed(1) + '%' : '—') + '</div></div>'
        + '<div class="col-md-4"><div class="small text-muted">Avg Ping</div><div class="fs-5 fw-bold">' + (stats.avg_ping != null ? stats.avg_ping.toFixed(1) + 'ms' : '—') + '</div></div>'
        + '<div class="col-md-4"><div class="small text-muted">Last Message</div><div class="text-truncate">' + (stats.last_msg ? esc(stats.last_msg) : '—') + '</div></div>'
        + (stats.cert_info ? '<div class="col-12"><div class="small text-muted">Certificate</div><div><code>' + esc(stats.cert_info) + '</code></div></div>' : '')
        + '</div>';
}

function loadMonitorStats(monitorId, instanceId) {
    var cacheKey = instanceId + ':' + monitorId;
    var cached = getCachedStats(instanceId, monitorId);
    if (cached) {
        document.getElementById('monitor-detail-title').textContent = 'Monitor #' + monitorId;
        document.getElementById('monitor-detail-body').innerHTML = renderMonitorStats(cached);
        document.getElementById('monitor-detail-panel').classList.remove('d-none');
        return;
    }

    document.getElementById('monitor-detail-title').textContent = 'Monitor #' + monitorId;
    document.getElementById('monitor-detail-body').innerHTML = '<div class="text-center text-muted py-3"><span class="spinner-border spinner-border-sm" role="status"></span> Loading stats...</div>';
    document.getElementById('monitor-detail-panel').classList.remove('d-none');

    apiFetch('/api/monitors/' + monitorId + '/stats?instance=' + instanceId)
        .then(function(r) {
            if (!r.ok) { throw new Error(r.status); }
            return r.json();
        })
        .then(function(stats) {
            monitorStatsCache.set(cacheKey, { stats: stats, timestamp: Date.now() });
            document.getElementById('monitor-detail-body').innerHTML = renderMonitorStats(stats);
        })
        .catch(function(err) {
            if (err.message === 'not authenticated') return;
            var msg = 'Stats unavailable';
            if (err.message === '404') msg = 'Instance not found';
            else if (err.message === '502') msg = 'Stats unavailable (Socket.IO connection failed)';
            document.getElementById('monitor-detail-body').innerHTML = '<div class="text-center text-danger py-3">' + msg + '</div>';
        });
}

export function loadKumaMonitors() {
    document.getElementById('kuma-tbody').innerHTML = loadingRow(5);
    apiFetch('/api/monitors').then(function(r){return r.json();}).then(function(monitors) {
        var tbody = document.getElementById('kuma-tbody');
        if (monitors.error) {
            tbody.innerHTML = '<tr><td colspan="5" class="text-center text-danger py-3">' + esc(monitors.error) + '</td></tr>';
            return;
        }
        if (!monitors.length) {
            tbody.innerHTML = emptyRow(5, 'No monitors in Uptime Kuma');
            return;
        }
        tbody.innerHTML = monitors.map(function(m) {
            return '<tr style="cursor:pointer" data-monitor-id="' + m.id + '" data-instance-id="' + m.instance_id + '">'
                + '<td data-label="ID">#' + m.id + '</td>'
                + '<td data-label="Name">' + esc(m.name) + '</td>'
                + '<td data-label="Instance"><span class="badge bg-primary">' + esc(m.instance_name || '—') + '</span></td>'
                + '<td data-label="Type"><span class="badge ' + (m.type === 'http' ? 'bg-info' : m.type === 'docker' ? 'bg-warning text-dark' : 'bg-secondary') + '">' + (m.type === 'http' ? '\u25CB ' : m.type === 'docker' ? '\u25A3 ' : '') + m.type.toUpperCase() + '</span></td>'
                + '<td data-label="URL / Container" class="text-truncate" style="max-width:300px">' + (m.url ? esc(m.url) : m.docker_container ? esc(m.docker_container) : '—') + '</td>'
                + '</tr>';
        }).join('');

        // Wire click handlers for detail stats
        tbody.querySelectorAll('tr[data-monitor-id]').forEach(function(row) {
            row.addEventListener('click', function() {
                loadMonitorStats(row.getAttribute('data-monitor-id'), row.getAttribute('data-instance-id'));
            });
        });
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
