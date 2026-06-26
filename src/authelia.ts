// Authelia tab logic
import type { AutheliaStatusResponse, AutheliaAlert, TempAccessRule, AutheliaSyncResult } from './types';

export function loadAutheliaDashboard(): void {
    loadAutheliaStatus();
    loadAutheliaAlerts();
    loadAutheliaTempAccess();
}

export function loadAutheliaStatus(): void {
    apiFetch('/api/authelia/status').then(function(r){return r.json() as Promise<AutheliaStatusResponse>;}).then(function(d) {
        if (!d.configured) {
            document.getElementById('auth-domain-count')!.textContent = '—';
            document.getElementById('auth-coverage')!.textContent = '—';
            document.getElementById('auth-open-alerts')!.textContent = '—';
            document.getElementById('auth-coverage-tbody')!.innerHTML = emptyRow(4, 'Authelia not configured. Set config path in Settings.');
            return;
        }
        if (d.error) {
            document.getElementById('auth-domain-count')!.textContent = '\u26A0';
            document.getElementById('auth-coverage')!.textContent = '\u26A0';
            document.getElementById('auth-open-alerts')!.textContent = '\u26A0';
            document.getElementById('auth-coverage-tbody')!.innerHTML = emptyRow(4, 'Error loading config: ' + esc(d.error));
            return;
        }

        document.getElementById('auth-domain-count')!.textContent = '' + (d.domains ? d.domains.length : 0);

        var total = d.npm_cnames ? d.npm_cnames.length : 0;
        var matched = d.matched ? d.matched.length : 0;
        document.getElementById('auth-coverage')!.textContent = matched + '/' + total;
        document.getElementById('auth-open-alerts')!.textContent = '' + (d.open_alerts || 0);

        var tbody = document.getElementById('auth-coverage-tbody')!;
        if (total === 0) { tbody.innerHTML = emptyRow(4, 'No NPM proxy hosts found'); return; }

        var matchedMap: Record<string, boolean> = {};
        (d.matched || []).forEach(function(c) { matchedMap[c] = true; });

        tbody.innerHTML = (d.npm_cnames || []).map(function(cname) {
            var isCovered = matchedMap[cname];
            return '<tr>'
                + '<td data-label="CNAME"><code>' + esc(cname) + '</code></td>'
                + '<td data-label="Container">—</td>'
                + '<td data-label="Coverage">' + (isCovered ? '<span class="badge bg-success">\u2713 Covered</span>' : '<span class="badge bg-danger">\u2717 Missing</span>') + '</td>'
                + '<td data-label="Policy">' + (isCovered ? 'Existing rule' : '<span class="text-warning">\u26A0 No rule found</span>') + '</td>'
                + '</tr>';
        }).join('');
    });
}

export function loadAutheliaAlerts(): void {
    apiFetch('/api/authelia/alerts').then(function(r){return r.json() as Promise<AutheliaAlert[]>;}).then(function(alerts) {
        var tbody = document.getElementById('auth-alerts-tbody')!;
        if (!alerts || !alerts.length) { tbody.innerHTML = emptyRow(4, 'No alerts'); return; }
        tbody.innerHTML = alerts.map(function(a) {
            var sevBadge = a.severity === 'error' ? 'bg-danger' : a.severity === 'warning' ? 'bg-warning text-dark' : 'bg-info';
            var sevIcon = a.severity === 'error' ? '\u2717' : a.severity === 'warning' ? '\u26A0' : '\u2139';
            return '<tr>'
                + '<td data-label="CNAME"><code>' + esc(a.cname) + '</code></td>'
                + '<td data-label="Message" class="small">' + esc(a.message) + '</td>'
                + '<td data-label="Severity"><span class="badge ' + sevBadge + '">' + sevIcon + ' ' + esc(a.severity) + '</span></td>'
                + '<td data-label="Action">' + (a.status === 'open'
                    ? '<button class="btn btn-outline-success btn-sm" onclick="resolveAlert(' + a.id + ')">Resolve</button>'
                    : '<span class="text-muted">' + esc(a.status) + '</span>') + '</td>'
                + '</tr>';
        }).join('');
    });
}

export function resolveAlert(id: number): void {
    apiFetch('/api/authelia/alerts/' + id + '/resolve', { method: 'POST' })
        .then(function(r){return r.json();})
        .then(function() { toast('Alert resolved', 'success'); loadAutheliaAlerts(); loadAutheliaStatus(); })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to resolve alert', 'error'); });
}

export function loadAutheliaTempAccess(): void {
    apiFetch('/api/authelia/temp-access').then(function(r){return r.json() as Promise<TempAccessRule[]>;}).then(function(rules) {
        var tbody = document.getElementById('auth-temp-tbody')!;
        if (!rules || !rules.length) { tbody.innerHTML = emptyRow(5, 'No temporary access rules'); return; }
        tbody.innerHTML = rules.map(function(r) {
            var statusBadge = r.status === 'active' ? 'bg-success' : r.status === 'expired' ? 'bg-secondary' : 'bg-danger';
            var statusIcon = r.status === 'active' ? '\u25CF' : r.status === 'expired' ? '\u2717' : '\u26A0';
            return '<tr>'
                + '<td data-label="IP"><code>' + esc(r.ip) + '</code></td>'
                + '<td data-label="Reason" class="small">' + esc(r.reason) + '</td>'
                + '<td data-label="Expires" class="small">' + (r.expires_at ? new Date(r.expires_at).toLocaleString() : '—') + '</td>'
                + '<td data-label="Status"><span class="badge ' + statusBadge + '">' + statusIcon + ' ' + esc(r.status) + '</span></td>'
                + '<td data-label="Action">' + (r.status === 'active'
                    ? '<button class="btn btn-outline-danger btn-sm" onclick="revokeTempAccess(' + r.id + ')">Revoke</button>'
                    : '—') + '</td>'
                + '</tr>';
        }).join('');
    });
}

export function revokeTempAccess(id: number): void {
    apiFetch('/api/authelia/temp-access/' + id + '/revoke', { method: 'POST' })
        .then(function(r){return r.json();})
        .then(function() { toast('Access rule revoked', 'success'); loadAutheliaTempAccess(); })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to revoke rule', 'error'); });
}

export function runAutheliaSync(dryRun: boolean): void {
    var btn = document.getElementById(dryRun ? 'btn-auth-dryrun' : 'btn-auth-sync') as HTMLButtonElement;
    btn.disabled = true;
    var orig = btn.innerHTML;
    btn.innerHTML = '<span class="spinner-sm"></span> ' + (dryRun ? 'Dry running...' : 'Syncing...');

    apiFetch('/api/authelia/sync', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ dry_run: dryRun }) })
        .then(function(r){return r.json() as Promise<AutheliaSyncResult>;})
        .then(function(d) {
            if (d.error) { toast('Sync error: ' + d.error, 'error'); return; }
            var resultHtml = '<div class="alert ' + (dryRun ? 'alert-info' : 'alert-success') + ' p-2 mb-0"><strong>' + (dryRun ? 'Dry Run Results' : 'Sync Results') + '</strong>: Added: ' + (d.added || 0) + ', Skipped: ' + (d.skipped || 0) + ', Alerted: ' + (d.alerted || 0);
            var actions = d.actions || [];
            if (actions.length) {
                resultHtml += '<ul class="mb-0 mt-1 small">';
                actions.forEach(function(a) { resultHtml += '<li>[' + esc(a.action) + '] ' + esc(a.cname) + (a.policy ? ' → ' + esc(a.policy) : '') + ': ' + esc(a.message) + '</li>'; });
                resultHtml += '</ul>';
            }
            resultHtml += '</div>';
            document.getElementById('auth-sync-result')!.innerHTML = resultHtml;
            document.getElementById('auth-sync-result')!.classList.remove('d-none');
            loadAutheliaStatus(); loadAutheliaAlerts();
            if (!dryRun) toast('Sync completed', 'success');
        })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Sync failed', 'error'); })
        .finally(function() { btn.disabled = false; btn.innerHTML = orig; });
}

window.loadAutheliaDashboard = loadAutheliaDashboard;
window.loadAutheliaStatus = loadAutheliaStatus;
window.loadAutheliaAlerts = loadAutheliaAlerts;
window.resolveAlert = resolveAlert;
window.loadAutheliaTempAccess = loadAutheliaTempAccess;
window.revokeTempAccess = revokeTempAccess;
window.runAutheliaSync = runAutheliaSync;
