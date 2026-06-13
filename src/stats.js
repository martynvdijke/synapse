// Dashboard stat card loading

export function loadStatus() {
    apiFetch('/api/status').then(function(r){return r.json();}).then(function(d) {
        document.getElementById('stat-docker').textContent = d.docker_count;
        document.getElementById('stat-npm').textContent = d.npm_error ? '\u26A0' : d.npm_count;
        document.getElementById('stat-monitors').textContent = d.monitor_count;
        if (!d.running) {
            document.getElementById('stat-status').innerHTML = '<span class="badge bg-secondary">Idle</span>';
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
