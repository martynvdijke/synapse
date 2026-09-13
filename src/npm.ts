// Nginx Proxy Manager tab: proxy list + expandable detail rows.
import type { ProxyResponse, NPMProxyHost } from './types';
import { esc, apiFetch, emptyRow, loadingRow, detailInline, detailContainer } from './api';

export function loadNPMProxies(): void {
    document.getElementById('npm-tbody')!.innerHTML = loadingRow(4);
    var summaryReq = apiFetch('/api/proxies').then(function(r){ return r.json() as Promise<(ProxyResponse & {error?: string})[]>; });
    var detailReq = apiFetch('/api/npm/proxy-hosts').then(function(r){ return r.ok ? r.json() as Promise<NPMProxyHost[]> : Promise.resolve([]); });
    Promise.all([summaryReq, detailReq]).then(function(res: any[]) {
        var proxies = res[0] as (ProxyResponse & {error?: string})[];
        var hosts = res[1] as NPMProxyHost[];
        var tbody = document.getElementById('npm-tbody')!;
        if ((proxies as any).error) {
            tbody.innerHTML = '<tr><td colspan="4" class="text-center text-danger py-3">' + esc((proxies as any).error) + '</td></tr>';
            return;
        }
        if (!proxies.length && !hosts.length) {
            tbody.innerHTML = emptyRow(4, 'No proxy hosts found');
            return;
        }
        var hostByDomain: Record<string, NPMProxyHost> = {};
        hosts.forEach(function(h) {
            if (h.domain_names && h.domain_names.length) hostByDomain[h.domain_names[0]] = h;
        });
        var rows: string[] = [];
        proxies.forEach(function(p, idx) {
            var full = hostByDomain[p.cname];
            rows.push('<tr class="npm-proxy-row" data-idx="' + idx + '" data-action="toggle-npm-proxy-detail">'
                + '<td data-label="Domain"><code>' + esc(p.cname) + '</code></td>'
                + '<td data-label="Instance">' + (p.source_instance_name
                    ? '<span class="badge bg-secondary">' + esc(p.source_instance_name) + '</span>'
                    : '<span class="text-muted">\u2014</span>') + '</td>'
                + '<td data-label="Container">' + (p.container ? esc(p.container) : '<span class="text-muted">\u2014</span>') + '</td>'
                + '<td data-label="In Kuma">' + (p.in_kuma
                    ? '<span class="badge bg-success">\u2713 In Kuma</span>'
                    : '<span class="badge bg-secondary">\u2717 Missing</span>') + '</td>'
                + '</tr>');
            if (full) {
                rows.push('<tr class="npm-detail-row" data-idx="' + idx + '" style="display:none"><td colspan="4">' + renderNPMProxyDetail(full) + '</td></tr>');
            }
        });
        tbody.innerHTML = rows.join('');
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        document.getElementById('npm-tbody')!.innerHTML = '<tr><td colspan="4" class="text-center text-danger py-3">Failed to load proxies</td></tr>';
    });
}

function renderNPMProxyDetail(h: NPMProxyHost): string {
    var parts: string[] = [];
    parts.push(detailInline('ID', String(h.id)));
    parts.push(detailInline('Forward', esc(h.forward_scheme + '://' + h.forward_host + ':' + h.forward_port)));
    parts.push(detailInline('Enabled', h.enabled ? 'yes' : 'no'));
    if (h.ssl_forced) parts.push(detailInline('SSL Forced', 'yes'));
    if (h.hsts_enabled) parts.push(detailInline('HSTS', 'yes'));
    if (h.allow_websocket_upgrade) parts.push(detailInline('WebSocket Upgrade', 'yes'));
    if (h.advanced_config) parts.push(detailInline('Advanced Config', '<pre class="detail-pre">' + esc(h.advanced_config) + '</pre>'));
    if (h.locations && h.locations.length) {
        var locs = '<span class="detail-inline-label">Locations:</span><br>';
        h.locations.forEach(function(l) {
            locs += '<div class="ms-3">' + esc(l.path || '/') + ' → ' + esc(l.forward_scheme + '://' + l.forward_host + ':' + l.forward_port) + '</div>';
        });
        parts.push(locs);
    }
    return detailContainer(parts);
}

export function toggleNPMProxyDetail(row: HTMLElement): void {
    var idx = row.getAttribute('data-idx');
    var detailRow = row.parentNode!.querySelector('.npm-detail-row[data-idx="' + idx + '"]') as HTMLElement | null;
    if (detailRow) {
        var isVisible = detailRow.style.display !== 'none';
        detailRow.style.display = isVisible ? 'none' : 'table-row';
        row.classList.toggle('detail-expanded', !isVisible);
    }
}
