// API fetch wrapper and HTML helpers

export function esc(s: string): string {
    return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

export function apiFetch(url: string, opts?: RequestInit): Promise<Response> {
    return fetch(url, opts).then(function(r) {
        if (r.status === 401) {
            window.location.href = '/login';
            throw new Error('not authenticated');
        }
        return r;
    });
}

export function logout(): void {
    apiFetch('/api/logout', { method: 'POST' }).then(function() {
        window.location.href = '/login';
    });
}

export function emptyIcon(): string {
    return '<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path><polyline points="13 2 13 9 20 9"></polyline></svg>';
}

export function emptyRow(colspan: number, msg: string): string {
    return '<tr><td colspan="' + colspan + '" class="empty-state">' + emptyIcon() + '<div class="mt-2">' + esc(msg) + '</div></td></tr>';
}

export function skeletonCell(width?: string): string {
    return '<span class="skeleton skeleton-sm" style="width:' + (width || '60%') + '">&nbsp;</span>';
}

export function skeletonRows(cols: number, count?: number): string {
    count = count || 5;
    var rows = '';
    for (var i = 0; i < count; i++) {
        rows += '<tr class="skeleton-row">';
        for (var c = 0; c < cols; c++) {
            var widths = ['40%', '55%', '30%', '65%', '45%'];
            rows += '<td>' + skeletonCell(widths[c % widths.length]) + '</td>';
        }
        rows += '</tr>';
    }
    return rows;
}

export function loadingRow(colspan: number): string {
    return skeletonRows(colspan, 5);
}

// --- Service links ---
export function getServiceLinks(): Promise<Response> {
    return apiFetch('/api/service-links');
}

export function createServiceLink(input: Record<string, unknown>): Promise<Response> {
    return apiFetch('/api/service-links', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input)
    });
}

export function updateServiceLink(id: number, input: Record<string, unknown>): Promise<Response> {
    return apiFetch('/api/service-links/' + id, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input)
    });
}

export function deleteServiceLink(id: number): Promise<Response> {
    return apiFetch('/api/service-links/' + id, { method: 'DELETE' });
}

export function refreshServiceLink(id: number): Promise<Response> {
    return apiFetch('/api/service-links/' + id + '/refresh', { method: 'POST' });
}

// --- NPM proxy hosts ---
export function getNPMProxyHosts(instanceId?: number): Promise<Response> {
    var q = instanceId ? '?instance=' + instanceId : '';
    return apiFetch('/api/npm/proxy-hosts' + q);
}

export function createNPMProxyHost(input: Record<string, unknown>): Promise<Response> {
    return apiFetch('/api/npm/proxy-hosts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input)
    });
}

export function updateNPMProxyHost(id: number, input: Record<string, unknown>): Promise<Response> {
    return apiFetch('/api/npm/proxy-hosts/' + id, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input)
    });
}

// --- Kuma monitors ---
export function createKumaMonitor(input: Record<string, unknown>): Promise<Response> {
    return apiFetch('/api/monitors', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input)
    });
}

export function updateKumaMonitor(kumaId: number, instanceId: number, input: Record<string, unknown>): Promise<Response> {
    return apiFetch('/api/monitors/' + kumaId + '?instance=' + instanceId, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input)
    });
}

export function deleteKumaMonitor(kumaId: number, instanceId: number): Promise<Response> {
    return apiFetch('/api/monitors/' + kumaId + '?instance=' + instanceId, { method: 'DELETE' });
}

// --- Instances ---
export function getKumaInstances(): Promise<Response> {
    return apiFetch('/api/kuma-instances');
}

export function getNPMInstances(): Promise<Response> {
    return apiFetch('/api/npm-instances');
}

// --- Authelia ---
export function getAutheliaCoverage(): Promise<Response> {
    return apiFetch('/api/authelia/coverage');
}

export function getAutheliaInstances(): Promise<Response> {
    return apiFetch('/api/authelia-instances');
}

// Attach to window for inline event handlers
window.esc = esc;
window.apiFetch = apiFetch;
window.logout = logout;
window.emptyRow = emptyRow;
window.loadingRow = loadingRow;
window.getServiceLinks = getServiceLinks;
window.createServiceLink = createServiceLink;
window.updateServiceLink = updateServiceLink;
window.deleteServiceLink = deleteServiceLink;
window.refreshServiceLink = refreshServiceLink;
window.getNPMProxyHosts = getNPMProxyHosts;
window.createNPMProxyHost = createNPMProxyHost;
window.updateNPMProxyHost = updateNPMProxyHost;
window.createKumaMonitor = createKumaMonitor;
window.updateKumaMonitor = updateKumaMonitor;
window.deleteKumaMonitor = deleteKumaMonitor;
window.getKumaInstances = getKumaInstances;
window.getNPMInstances = getNPMInstances;
window.getAutheliaCoverage = getAutheliaCoverage;
window.getAutheliaInstances = getAutheliaInstances;
