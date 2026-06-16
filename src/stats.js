// Dashboard stat card loading and connection health indicators

function setHealthDot(id, ok) {
    var dot = document.getElementById(id);
    if (!dot) return;
    dot.className = 'health-dot';
    if (ok === true) {
        dot.classList.add('healthy');
        dot.title = 'Connected';
    } else if (ok === false) {
        dot.classList.add('error');
        dot.title = 'Connection error';
    } else {
        dot.classList.add('unknown');
        dot.title = 'Not configured';
    }
}

export function loadStatus() {
    apiFetch('/api/status').then(function(r){return r.json();}).then(function(d) {
        document.getElementById('stat-docker').textContent = d.docker_count;
        document.getElementById('stat-npm').textContent = d.npm_error ? '\u26A0' : d.npm_count;
        document.getElementById('stat-monitors').textContent = d.monitor_count;
        if (!d.running) {
            document.getElementById('stat-status').innerHTML = '<span class="badge bg-secondary">Idle</span>';
        }

        // Connection health indicators
        if (d.connection_health) {
            setHealthDot('health-docker', d.connection_health.docker ? d.connection_health.docker.ok : null);
            setHealthDot('health-npm', d.connection_health.npm ? d.connection_health.npm.ok : null);
            setHealthDot('health-kuma', d.connection_health.kuma ? d.connection_health.kuma.ok : null);
        } else {
            // Fallback for older API responses
            setHealthDot('health-docker', d.docker_error ? false : true);
            setHealthDot('health-npm', d.npm_error ? false : true);
            setHealthDot('health-kuma', d.kuma_error ? false : (d.kuma_error === '' ? true : null));
        }

        document.getElementById('btn-docker').disabled = d.running;
        document.getElementById('btn-npm').disabled = d.running;
    });
    // Load authelia status separately
    apiFetch('/api/authelia/status').then(function(r){return r.json();}).then(function(d) {
        var el = document.getElementById('stat-authelia');
        if (!d.configured) {
            el.innerHTML = '<span class="badge bg-secondary">Not configured</span>';
            return;
        }
        if (d.error) {
            el.innerHTML = '<span class="badge bg-danger">Config error</span>';
            return;
        }
        var total = d.npm_cnames ? d.npm_cnames.length : 0;
        var matched = d.matched ? d.matched.length : 0;
        var coverage = total > 0 ? Math.round(matched / total * 100) : 0;
        el.innerHTML = '<span class="badge ' + (coverage >= 100 ? 'bg-success' : coverage > 0 ? 'bg-warning text-dark' : 'bg-danger') + '">' + matched + '/' + total + ' (' + coverage + '%)</span>';
    });
}

export function refreshAll() {
    loadStatus();
    loadDockerServices();
    loadKumaMonitors();
    loadNPMProxies();
    loadHistory();
}

window.loadStatus = loadStatus;
window.refreshAll = refreshAll;
