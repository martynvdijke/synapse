// Docker services tab: table, expandable detail rows, link badges.
import type { ServiceInfo, ServiceLink, AutheliaCoverageResponse, ApiErrorBody } from './types';
import { esc, apiFetch, emptyRow, loadingRow, detailField, detailInline, detailContainer } from './api';

// Populated by loadDockerServices; read by the link editor to resolve row indices.
export let linkServices: ServiceInfo[] = [];

function renderDockerDetailRow(svc: ServiceInfo): string {
    var fields: string[] = [];
    function addField(label: string, value: unknown): void {
        if (value === null || value === undefined || value === '' || (Array.isArray(value) && value.length === 0) || (typeof value === 'object' && !Array.isArray(value) && Object.keys(value).length === 0)) return;
        if (Array.isArray(value)) fields.push(detailField(label, value.map(function(v) { return esc(String(v)); }).join('<br>')));
        else if (typeof value === 'object') {
            var inner = '';
            for (var k in value as Record<string, unknown>) { if ((value as Record<string, unknown>).hasOwnProperty(k)) inner += detailInline(esc(k), esc(String((value as Record<string, unknown>)[k]))); }
            if (inner) fields.push(detailField(label, inner));
        } else fields.push(detailField(label, esc(String(value))));
    }
    addField('Image', svc.image); addField('Ports', svc.ports); addField('Environment', svc.environment); addField('Volumes', svc.volumes); addField('Depends On', svc.depends_on); addField('Labels', svc.labels); addField('Restart', svc.restart); addField('Command', svc.command); addField('Entrypoint', svc.entrypoint); addField('User', svc.user); addField('Working Dir', svc.working_dir);
    if (svc.healthcheck) {
        var hc = svc.healthcheck; var hcHtml = '';
        if (hc.test) { if (Array.isArray(hc.test)) hcHtml += detailInline('test', esc(hc.test.join(' '))); else hcHtml += detailInline('test', esc(String(hc.test))); }
        if (hc.interval) hcHtml += detailInline('interval', esc(hc.interval));
        if (hc.timeout) hcHtml += detailInline('timeout', esc(hc.timeout));
        if (hc.retries) hcHtml += detailInline('retries', String(hc.retries));
        if (hc.start_period) hcHtml += detailInline('start_period', esc(hc.start_period));
        fields.push(detailField('Healthcheck', hcHtml));
    } else fields.push(detailField('Healthcheck', '<span class="text-muted">Not configured</span>'));
    return detailContainer(fields);
}

export function renderDockerServices(services: (ServiceInfo & ApiErrorBody)[], links: ServiceLink[], covResp: AutheliaCoverageResponse | null): void {
    var coverageByService: Record<string, { covered: boolean; policy: string }> = {};
    if (covResp && covResp.instances) {
        for (var instIdx = 0; instIdx < covResp.instances.length; instIdx++) {
            var inst = covResp.instances[instIdx];
            for (var dIdx = 0; dIdx < inst.domains.length; dIdx++) {
                var d = inst.domains[dIdx];
                if (d.service && !coverageByService[d.service]) coverageByService[d.service] = { covered: d.covered, policy: d.policy };
            }
        }
    }
    var linkMap: Record<string, ServiceLink> = {};
    links.forEach(function(l) { linkMap[l.service_name] = l; });
    linkServices = services as ServiceInfo[];
    var tbody = document.getElementById('docker-tbody')!;
    var svcErr = (services as unknown as ApiErrorBody).error;
    if (svcErr) { tbody.innerHTML = '<tr><td colspan="7" class="text-center text-danger py-3">' + esc(svcErr) + '</td></tr>'; return; }
    if (!services.length) { tbody.innerHTML = emptyRow(7, 'No services found'); return; }
    var rows: string[] = [];
    services.forEach(function(s, idx) {
        var link = linkMap[s.name];
        var linksHtml = '';
        if (link) {
            if (link.npm_host_name) linksHtml += '<span class="badge bg-secondary me-1" title="NPM proxy host">\u2699 ' + esc(link.npm_host_name) + '</span>';
            if (link.kuma_monitor_name) linksHtml += '<span class="badge bg-info me-1" title="Kuma monitor">\u25CB ' + esc(link.kuma_monitor_name) + '</span>';
        }
        var cov = coverageByService[s.name];
        if (cov) {
            if (cov.covered) linksHtml += '<span class="badge bg-success me-1" title="Authelia access rule: ' + esc(cov.policy) + '" style="cursor:pointer" data-action="switch-tab" data-tab="tab-btn-authelia">\uD83D\uDEE1 ' + esc(cov.policy) + '</span>';
            else linksHtml += '<span class="badge bg-warning me-1" title="Authelia access rule missing" style="cursor:pointer" data-action="switch-tab" data-tab="tab-btn-authelia">\uD83D\uDEE1 missing</span>';
        }
        if (!linksHtml) linksHtml = '<span class="text-muted me-1">\u2014</span>';
        linksHtml += '<button class="btn btn-sm btn-outline-primary" title="Link to NPM / Kuma" data-action="open-link-editor" data-idx="' + idx + '">Link</button>';
        rows.push('<tr class="docker-service-row" data-idx="' + idx + '" data-action="toggle-docker-detail">'
            + '<td data-label="Service"><code>' + esc(s.name) + '</code></td>'
            + '<td data-label="Container">' + esc(s.container_name) + '</td>'
            + '<td data-label="Image">' + (s.image ? '<code>' + esc(s.image) + '</code>' : '—') + '</td>'
            + '<td data-label="Type"><span class="badge ' + (s.type === 'http' ? 'bg-info' : 'bg-secondary') + '">' + s.type.toUpperCase() + '</span></td>'
            + '<td data-label="URL" class="text-truncate" style="max-width:250px">' + (s.url ? '<a href="' + esc(s.url) + '">' + esc(s.url) + '</a>' : '—') + '</td>'
            + '<td data-label="In Kuma">' + (s.in_kuma ? '<span class="badge bg-success">\u2713 In Kuma</span>' : '<span class="badge bg-secondary">\u2717 Missing</span>') + '</td>'
            + '<td data-label="Links" class="text-nowrap">' + linksHtml + '</td></tr>');
        var detailHtml = renderDockerDetailRow(s);
        if (detailHtml) rows.push('<tr class="docker-detail-row" data-idx="' + idx + '" style="display:none"><td colspan="7">' + detailHtml + '</td></tr>');
    });
    tbody.innerHTML = rows.join('');
}

export function loadDockerServices(): void {
    document.getElementById('docker-tbody')!.innerHTML = loadingRow(7);
    var svcReq = apiFetch('/api/services').then(function(r){return r.json() as Promise<(ServiceInfo & ApiErrorBody)[]>;});
    var linkReq = apiFetch('/api/service-links').then(function(r){return r.ok ? r.json() as Promise<ServiceLink[]> : Promise.resolve([] as ServiceLink[]);});
    var covReq = apiFetch('/api/authelia/coverage').then(function(r){return r.ok ? r.json() as Promise<AutheliaCoverageResponse> : Promise.resolve(null as AutheliaCoverageResponse | null);});
    Promise.all([svcReq, linkReq, covReq]).then(function(res: [(ServiceInfo & ApiErrorBody)[], ServiceLink[], AutheliaCoverageResponse | null]) {
        renderDockerServices(res[0], res[1], res[2]);
    });
}

export function toggleDockerDetail(row: HTMLElement): void {
    var idx = row.getAttribute('data-idx');
    var detailRow = row.parentNode!.querySelector('.docker-detail-row[data-idx="' + idx + '"]') as HTMLElement | null;
    if (detailRow) { var isVisible = detailRow.style.display !== 'none'; detailRow.style.display = isVisible ? 'none' : 'table-row'; row.classList.toggle('detail-expanded', !isVisible); }
}
