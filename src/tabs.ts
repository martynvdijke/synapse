// Tab data loaders (Docker, Kuma, NPM, History)
import type { ServiceInfo, MonitorResponse, MonitorStats, ProxyResponse, SyncRun, FeedItem, ReconcileResult, ServiceLink, NPMProxyHost, AutheliaCoverageResponse } from './types';

function renderDockerDetailRow(svc: ServiceInfo): string {
    var fields: string[] = [];

    function addField(label: string, value: unknown): void {
        if (value === null || value === undefined || value === '' || (Array.isArray(value) && value.length === 0) || (typeof value === 'object' && !Array.isArray(value) && Object.keys(value).length === 0)) {
            return;
        }
        if (Array.isArray(value)) {
            fields.push('<div class="detail-field"><span class="detail-label">' + label + '</span><span class="detail-value">' + value.map(function(v) { return esc(String(v)); }).join('<br>') + '</span></div>');
        } else if (typeof value === 'object') {
            var inner = '';
            for (var k in value as Record<string, unknown>) {
                if ((value as Record<string, unknown>).hasOwnProperty(k)) {
                    inner += '<span class="detail-inline-label">' + esc(k) + ':</span> ' + esc(String((value as Record<string, unknown>)[k])) + '<br>';
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

export function loadDockerServices(): void {
    document.getElementById('docker-tbody')!.innerHTML = loadingRow(7);
    var svcReq = apiFetch('/api/services').then(function(r){return r.json() as Promise<(ServiceInfo & {error?: string})[]>;});
    var linkReq = apiFetch('/api/service-links').then(function(r){return r.ok ? r.json() as Promise<ServiceLink[]> : Promise.resolve([]);});
    var covReq = apiFetch('/api/authelia/coverage').then(function(r){return r.ok ? r.json() as Promise<AutheliaCoverageResponse> : Promise.resolve(null);});
    Promise.all([svcReq, linkReq, covReq]).then(function(res: any[]) {
        var services = res[0] as (ServiceInfo & {error?: string})[];
        var links = res[1] as ServiceLink[];
        var covResp = res[2] as AutheliaCoverageResponse | null;
        var coverageByService: Record<string, { covered: boolean; policy: string }> = {};
        if (covResp && covResp.instances) {
            for (var instIdx = 0; instIdx < covResp.instances.length; instIdx++) {
                var inst = covResp.instances[instIdx];
                for (var dIdx = 0; dIdx < inst.domains.length; dIdx++) {
                    var d = inst.domains[dIdx];
                    if (d.service && !coverageByService[d.service]) {
                        coverageByService[d.service] = { covered: d.covered, policy: d.policy };
                    }
                }
            }
        }
        var linkMap: Record<string, ServiceLink> = {};
        links.forEach(function(l) { linkMap[l.service_name] = l; });
        linkServices = services as ServiceInfo[];
        var tbody = document.getElementById('docker-tbody')!;
        if ((services as any).error) {
            tbody.innerHTML = '<tr><td colspan="7" class="text-center text-danger py-3">' + esc((services as any).error) + '</td></tr>';
            return;
        }
        if (!services.length) {
            tbody.innerHTML = emptyRow(7, 'No services found');
            return;
        }
        var rows: string[] = [];
        services.forEach(function(s, idx) {
            var link = linkMap[s.name];
            var linksHtml = '';
            if (link) {
                if (link.npm_host_name) {
                    linksHtml += '<span class="badge bg-secondary me-1" title="NPM proxy host">\u2699 ' + esc(link.npm_host_name) + '</span>';
                }
                if (link.kuma_monitor_name) {
                    linksHtml += '<span class="badge bg-info me-1" title="Kuma monitor">\u25CB ' + esc(link.kuma_monitor_name) + '</span>';
                }
            }
            var cov = coverageByService[s.name];
            if (cov) {
                if (cov.covered) {
                    linksHtml += '<span class="badge bg-success me-1" title="Authelia access rule: ' + esc(cov.policy) + '" style="cursor:pointer" onclick="event.stopPropagation();document.getElementById(\'tab-btn-authelia\').click()">\uD83D\uDEE1 ' + esc(cov.policy) + '</span>';
                } else {
                    linksHtml += '<span class="badge bg-warning me-1" title="Authelia access rule missing" style="cursor:pointer" onclick="event.stopPropagation();document.getElementById(\'tab-btn-authelia\').click()">\uD83D\uDEE1 missing</span>';
                }
            }
            if (!linksHtml) {
                linksHtml = '<span class="text-muted me-1">\u2014</span>';
            }
            linksHtml += '<button class="btn btn-sm btn-outline-primary" title="Link to NPM / Kuma" onclick="event.stopPropagation();openLinkEditorByIndex(' + idx + ')">Link</button>';
            rows.push('<tr class="docker-service-row" data-idx="' + idx + '" onclick="toggleDockerDetail(this)">'
                + '<td data-label="Service"><code>' + esc(s.name) + '</code></td>'
                + '<td data-label="Container">' + esc(s.container_name) + '</td>'
                + '<td data-label="Image">' + (s.image ? '<code>' + esc(s.image) + '</code>' : '—') + '</td>'
                + '<td data-label="Type"><span class="badge ' + (s.type === 'http' ? 'bg-info' : 'bg-secondary') + '">' + s.type.toUpperCase() + '</span></td>'
                + '<td data-label="URL" class="text-truncate" style="max-width:250px">' + (s.url ? '<a href="' + esc(s.url) + '">' + esc(s.url) + '</a>' : '—') + '</td>'
                + '<td data-label="In Kuma">' + (s.in_kuma
                    ? '<span class="badge bg-success">\u2713 In Kuma</span>'
                    : '<span class="badge bg-secondary">\u2717 Missing</span>') + '</td>'
                + '<td data-label="Links" class="text-nowrap">' + linksHtml + '</td>'
                + '</tr>');
            var detailHtml = renderDockerDetailRow(s);
            if (detailHtml) {
                rows.push('<tr class="docker-detail-row" data-idx="' + idx + '" style="display:none"><td colspan="7">' + detailHtml + '</td></tr>');
            }
        });
        tbody.innerHTML = rows.join('');
    });
}

window.toggleDockerDetail = function(row: HTMLElement) {
    var idx = row.getAttribute('data-idx');
    var detailRow = row.parentNode!.querySelector('.docker-detail-row[data-idx="' + idx + '"]') as HTMLElement | null;
    if (detailRow) {
        var isVisible = detailRow.style.display !== 'none';
        detailRow.style.display = isVisible ? 'none' : 'table-row';
        row.classList.toggle('detail-expanded', !isVisible);
    }
};

// ─── Service link editor state ──────────────────────────────────
var linkServices: ServiceInfo[] = [];
var linkEditorService = '';
var linkEditorLink: ServiceLink | null = null;
var linkNPMHosts: NPMProxyHost[] = [];
var linkKumaMonitors: MonitorResponse[] = [];
var linkNPMInstances: Array<{ id: number; name: string }> = [];
var linkKumaInstances: Array<{ id: number; name: string }> = [];
var linkAutheliaInstances: Array<{ id: number; name: string }> = [];

function populateSelect(el: HTMLSelectElement, items: Array<{ label: string; value: string }>, selectedValue: string): void {
    var html = '';
    for (var i = 0; i < items.length; i++) {
        html += '<option value="' + items[i].value + '"' + (items[i].value === selectedValue ? ' selected' : '') + '>' + esc(items[i].label) + '</option>';
    }
    el.innerHTML = html;
}

function loadLinkTargets(): Promise<void> {
    var npmReq = apiFetch('/api/npm/proxy-hosts').then(function(r){ return r.ok ? r.json() as Promise<NPMProxyHost[]> : Promise.resolve([]); });
    var kumaReq = apiFetch('/api/monitors').then(function(r){ return r.ok ? r.json() as Promise<MonitorResponse[]> : Promise.resolve([]); });
    return Promise.all([npmReq, kumaReq]).then(function(res: any[]) {
        linkNPMHosts = res[0] || [];
        linkKumaMonitors = res[1] || [];
        var npmSel = document.getElementById('link-npm-select') as HTMLSelectElement;
        var opts = '<option value="">— Not linked —</option>';
        for (var i = 0; i < linkNPMHosts.length; i++) {
            var h = linkNPMHosts[i];
            opts += '<option value="' + i + '">' + esc(h.domain_names.join(', ')) + ' (' + esc(h.instance_name || '?') + ')</option>';
        }
        npmSel.innerHTML = opts;
        var kumaSel = document.getElementById('link-kuma-select') as HTMLSelectElement;
        var kopts = '<option value="">— Not linked —</option>';
        for (var j = 0; j < linkKumaMonitors.length; j++) {
            var m = linkKumaMonitors[j];
            kopts += '<option value="' + j + '">' + esc(m.name) + ' (' + esc(m.instance_name || '?') + ')</option>';
        }
        kumaSel.innerHTML = kopts;
    });
}

function selectedNPMHost(): NPMProxyHost | null {
    var sel = document.getElementById('link-npm-select') as HTMLSelectElement;
    var idx = parseInt(sel.value, 10);
    if (isNaN(idx) || !linkNPMHosts[idx]) return null;
    return linkNPMHosts[idx];
}

function selectedKumaMonitor(): MonitorResponse | null {
    var sel = document.getElementById('link-kuma-select') as HTMLSelectElement;
    var idx = parseInt(sel.value, 10);
    if (isNaN(idx) || !linkKumaMonitors[idx]) return null;
    return linkKumaMonitors[idx];
}

export function openLinkEditorByIndex(idx: number): void {
    var svc = linkServices[idx];
    if (!svc) return;
    openLinkEditor(svc.name);
}

export function openLinkEditor(serviceName: string): void {
    linkEditorService = serviceName;
    linkEditorLink = null;
    document.getElementById('link-editor-service')!.textContent = serviceName;

    apiFetch('/api/npm-instances').then(function(r){ return r.json(); }).then(function(insts: any[]) {
        linkNPMInstances = (insts || []).filter(function(i: any){ return i.enabled; });
        populateSelect(document.getElementById('link-npm-instance') as HTMLSelectElement,
            linkNPMInstances.map(function(i){ return { label: i.name, value: String(i.id) }; }), '');
    }).catch(function(err: Error) { if (err.message !== 'not authenticated') toast('Failed to load NPM instances', 'error'); });

    apiFetch('/api/kuma-instances').then(function(r){ return r.json(); }).then(function(insts: any[]) {
        linkKumaInstances = (insts || []).filter(function(i: any){ return i.enabled; });
        populateSelect(document.getElementById('link-kuma-instance') as HTMLSelectElement,
            linkKumaInstances.map(function(i){ return { label: i.name, value: String(i.id) }; }), '');
    }).catch(function(err: Error) { if (err.message !== 'not authenticated') toast('Failed to load Kuma instances', 'error'); });

    apiFetch('/api/authelia-instances').then(function(r){ return r.ok ? r.json() : Promise.resolve([]); }).then(function(insts: any[]) {
        linkAutheliaInstances = (insts || []).filter(function(i: any){ return i.enabled; });
        var sel = document.getElementById('link-authelia-instance') as HTMLSelectElement;
        var opts = '<option value="">— Not linked —</option>';
        for (var i = 0; i < linkAutheliaInstances.length; i++) {
            opts += '<option value="' + linkAutheliaInstances[i].id + '">' + esc(linkAutheliaInstances[i].name) + '</option>';
        }
        sel.innerHTML = opts;
    }).catch(function(err: Error) { if (err.message !== 'not authenticated') toast('Failed to load Authelia instances', 'error'); });

    apiFetch('/api/service-links').then(function(r){ return r.json() as Promise<ServiceLink[]>; }).then(function(links: ServiceLink[]) {
        for (var i = 0; i < links.length; i++) {
            if (links[i].service_name === serviceName) { linkEditorLink = links[i]; break; }
        }
        (document.getElementById('link-unlink-btn') as HTMLButtonElement).disabled = !linkEditorLink;
        (document.getElementById('link-refresh-btn') as HTMLButtonElement).disabled = !linkEditorLink;
        return loadLinkTargets();
    }).then(function() {
        if (linkEditorLink) {
            var npmSel = document.getElementById('link-npm-select') as HTMLSelectElement;
            for (var i = 0; i < linkNPMHosts.length; i++) {
                if (linkNPMHosts[i].domain_names.indexOf(linkEditorLink.npm_host_name || '') >= 0) {
                    npmSel.value = String(i);
                    break;
                }
            }
            var kumaSel = document.getElementById('link-kuma-select') as HTMLSelectElement;
            for (var j = 0; j < linkKumaMonitors.length; j++) {
                if (linkKumaMonitors[j].id === linkEditorLink.kuma_monitor_id) {
                    kumaSel.value = String(j);
                    break;
                }
            }
        }
        new bootstrap.Modal(document.getElementById('link-editor-modal')!).show();
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        toast('Failed to open link editor', 'error');
    });
}

export function saveServiceLink(): void {
    var npmHost = selectedNPMHost();
    var kumaMon = selectedKumaMonitor();
    var input: Record<string, unknown> = { service_name: linkEditorService };
    if (npmHost) {
        input.npm_instance_id = npmHost.instance_id;
        input.npm_host_name = npmHost.domain_names[0];
    } else {
        input.npm_instance_id = 0;
        input.npm_host_name = '';
    }
    if (kumaMon) {
        input.kuma_instance_id = kumaMon.instance_id;
        input.kuma_monitor_id = kumaMon.id;
        input.kuma_monitor_name = kumaMon.name;
    } else {
        input.kuma_instance_id = 0;
        input.kuma_monitor_id = 0;
        input.kuma_monitor_name = '';
    }
    var ensureMissing = (document.getElementById('link-ensure-missing') as HTMLInputElement).checked;
    input.ensure_missing = ensureMissing;
    var autheliaSel = document.getElementById('link-authelia-instance') as HTMLSelectElement;
    var autheliaId = parseInt(autheliaSel.value, 10);
    input.authelia_instance_id = autheliaId > 0 ? autheliaId : null;
    input.authelia_policy = (document.getElementById('link-authelia-policy') as HTMLSelectElement).value;
    var ensureRule = (document.getElementById('link-authelia-ensure') as HTMLInputElement).checked;
    input.dry_run = !ensureRule;
    var req = linkEditorLink
        ? updateServiceLink(linkEditorLink.id, input)
        : createServiceLink(input);
    req.then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body) { throw new Error((body && body.error) || ('HTTP ' + r.status)); });
        }
        return r.json();
    }).then(function(body: any) {
        toast('Service link saved');
        var actions: Array<{ action: string; cname: string; policy?: string; message: string }> = (body && body.authelia_actions) || [];
        var actionsBox = document.getElementById('link-authelia-actions')!;
        var actionsList = document.getElementById('link-authelia-actions-list')!;
        if (actions.length) {
            var html = '';
            for (var i = 0; i < actions.length; i++) {
                var a = actions[i];
                var cls = a.action === 'add' ? 'text-success' : 'text-muted';
                html += '<div class="' + cls + '">\u2022 ' + esc(a.cname) + ' \u2014 ' + esc(a.message) + '</div>';
            }
            actionsList.innerHTML = html;
            actionsBox.classList.remove('d-none');
        } else {
            actionsBox.classList.add('d-none');
            actionsList.innerHTML = '';
        }
        if (actions.length) return;
        var modal = bootstrap.Modal.getInstance(document.getElementById('link-editor-modal')!);
        if (modal) modal.hide();
        loadDockerServices();
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        toast('Save failed: ' + err.message, 'error');
    });
}

export function unlinkServiceLink(): void {
    if (!linkEditorLink) return;
    deleteServiceLink(linkEditorLink.id).then(function(r) {
        if (!r.ok) { throw new Error('HTTP ' + r.status); }
        toast('Link removed');
        var modal = bootstrap.Modal.getInstance(document.getElementById('link-editor-modal')!);
        if (modal) modal.hide();
        loadDockerServices();
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        toast('Unlink failed', 'error');
    });
}

export function refreshServiceLinkDetails(): void {
    if (!linkEditorLink) return;
    refreshServiceLink(linkEditorLink.id).then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body) { throw new Error((body && body.error) || ('HTTP ' + r.status)); });
        }
        return r.json();
    }).then(function() {
        toast('Link details refreshed');
        loadDockerServices();
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        toast('Refresh failed: ' + err.message, 'error');
    });
}

export function createNPMHostFromLink(): void {
    var instanceId = parseInt((document.getElementById('link-npm-instance') as HTMLSelectElement).value, 10);
    if (!instanceId) { toast('Select an NPM instance', 'error'); return; }
    var domains = (document.getElementById('link-npm-domains') as HTMLInputElement).value.trim();
    if (!domains) { toast('Enter at least one domain', 'error'); return; }
    var createdDomain = domains.split(',')[0].trim();
    var input: Record<string, unknown> = {
        instance_id: instanceId,
        domain_names: domains.split(',').map(function(d){ return d.trim(); }).filter(function(d){ return d.length > 0; }),
        forward_host: (document.getElementById('link-npm-host') as HTMLInputElement).value.trim(),
        forward_port: parseInt((document.getElementById('link-npm-port') as HTMLInputElement).value, 10) || 80,
        forward_scheme: (document.getElementById('link-npm-scheme') as HTMLSelectElement).value,
        service_name: linkEditorService
    };
    createNPMProxyHost(input).then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body) { throw new Error((body && body.error) || ('HTTP ' + r.status)); });
        }
        return r.json();
    }).then(function() {
        toast('NPM proxy host created');
        return loadLinkTargets();
    }).then(function() {
        var npmSel = document.getElementById('link-npm-select') as HTMLSelectElement;
        for (var i = 0; i < linkNPMHosts.length; i++) {
            if (linkNPMHosts[i].domain_names[0] === createdDomain) {
                npmSel.value = String(i);
                break;
            }
        }
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        toast('Create failed: ' + err.message, 'error');
    });
}

export function createKumaMonitorFromLink(): void {
    var instanceId = parseInt((document.getElementById('link-kuma-instance') as HTMLSelectElement).value, 10);
    if (!instanceId) { toast('Select a Kuma instance', 'error'); return; }
    var name = (document.getElementById('link-kuma-name') as HTMLInputElement).value.trim();
    if (!name) { toast('Enter a monitor name', 'error'); return; }
    var input: Record<string, unknown> = {
        instance_id: instanceId,
        name: name,
        type: (document.getElementById('link-kuma-type') as HTMLSelectElement).value,
        url: (document.getElementById('link-kuma-url') as HTMLInputElement).value.trim(),
        docker_container: (document.getElementById('link-kuma-container') as HTMLInputElement).value.trim(),
        service_name: linkEditorService
    };
    createKumaMonitor(input).then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body) { throw new Error((body && body.error) || ('HTTP ' + r.status)); });
        }
        return r.json();
    }).then(function(res: MonitorResponse) {
        toast('Kuma monitor created');
        return loadLinkTargets().then(function() {
            var kumaSel = document.getElementById('link-kuma-select') as HTMLSelectElement;
            for (var i = 0; i < linkKumaMonitors.length; i++) {
                if (linkKumaMonitors[i].id === res.id && linkKumaMonitors[i].instance_id === res.instance_id) {
                    kumaSel.value = String(i);
                    break;
                }
            }
        });
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        toast('Create failed: ' + err.message, 'error');
    });
}

// ─── Monitor detail stats cache ────────────────────────────────
var monitorStatsCache = new Map();
var STATS_CACHE_TTL = 60000; // 60 seconds

interface CacheEntry {
    stats: MonitorStats;
    timestamp: number;
}

function getCachedStats(instanceId: string, monitorId: string): MonitorStats | null {
    var key = instanceId + ':' + monitorId;
    var entry = monitorStatsCache.get(key) as CacheEntry | undefined;
    if (entry && Date.now() - entry.timestamp < STATS_CACHE_TTL) return entry.stats;
    return null;
}

function renderMonitorStats(stats: MonitorStats): string {
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

function loadMonitorStats(monitorId: string, instanceId: string): void {
    var cacheKey = instanceId + ':' + monitorId;
    var cached = getCachedStats(instanceId, monitorId);
    if (cached) {
        document.getElementById('monitor-detail-title')!.textContent = 'Monitor #' + monitorId;
        document.getElementById('monitor-detail-body')!.innerHTML = renderMonitorStats(cached);
        document.getElementById('monitor-detail-panel')!.classList.remove('d-none');
        return;
    }

    document.getElementById('monitor-detail-title')!.textContent = 'Monitor #' + monitorId;
    document.getElementById('monitor-detail-body')!.innerHTML = '<div class="text-center text-muted py-3"><span class="spinner-border spinner-border-sm" role="status"></span> Loading stats...</div>';
    document.getElementById('monitor-detail-panel')!.classList.remove('d-none');

    apiFetch('/api/monitors/' + monitorId + '/stats?instance=' + instanceId)
        .then(function(r) {
            if (!r.ok) { throw new Error('' + r.status); }
            return r.json() as Promise<MonitorStats>;
        })
        .then(function(stats) {
            monitorStatsCache.set(cacheKey, { stats: stats, timestamp: Date.now() });
            document.getElementById('monitor-detail-body')!.innerHTML = renderMonitorStats(stats);
        })
        .catch(function(err: Error) {
            if (err.message === 'not authenticated') return;
            var msg = 'Stats unavailable';
            if (err.message === '404') msg = 'Instance not found';
            else if (err.message === '502') msg = 'Stats unavailable (Socket.IO connection failed)';
            document.getElementById('monitor-detail-body')!.innerHTML = '<div class="text-center text-danger py-3">' + msg + '</div>';
        });
}

export function loadKumaMonitors(): void {
    document.getElementById('kuma-tbody')!.innerHTML = loadingRow(9);
    apiFetch('/api/monitors').then(function(r){return r.json() as Promise<(MonitorResponse & {error?: string})[]>;}).then(function(monitors) {
        var tbody = document.getElementById('kuma-tbody')!;
        if ((monitors as any).error) {
            tbody.innerHTML = '<tr><td colspan="9" class="text-center text-danger py-3">' + esc((monitors as any).error) + '</td></tr>';
            return;
        }
        if (!monitors.length) {
            tbody.innerHTML = emptyRow(9, 'No monitors in Uptime Kuma');
            return;
        }
        kumaMonitorList = monitors as MonitorResponse[];
        tbody.innerHTML = monitors.map(function(m) {
            return '<tr style="cursor:pointer" data-monitor-id="' + m.id + '" data-instance-id="' + m.instance_id + '">'
                + '<td data-label="ID">#' + m.id + '</td>'
                + '<td data-label="Name">' + esc(m.name) + '</td>'
                + '<td data-label="Instance"><span class="badge bg-primary">' + esc(m.instance_name || '—') + '</span></td>'
                + '<td data-label="Type"><span class="badge ' + (m.type === 'http' ? 'bg-info' : m.type === 'docker' ? 'bg-warning text-dark' : 'bg-secondary') + '">' + (m.type === 'http' ? '\u25CB ' : m.type === 'docker' ? '\u25A3 ' : '') + m.type.toUpperCase() + '</span></td>'
                + '<td data-label="URL / Container" class="text-truncate" style="max-width:220px">' + (m.url ? esc(m.url) : m.docker_container ? esc(m.docker_container) : '—') + '</td>'
                + '<td data-label="Interval">' + (m.interval ? m.interval + 's' : '—') + '</td>'
                + '<td data-label="Retry">' + (m.retry_interval ? m.retry_interval + 's' : '—') + '</td>'
                + '<td data-label="Max Retries">' + (m.maxretries || '—') + '</td>'
                + '<td data-label="Actions" class="text-nowrap"><button class="btn btn-sm btn-outline-secondary" onclick="event.stopPropagation();openMonitorEdit(' + m.id + ',' + m.instance_id + ')">Edit</button></td>'
                + '</tr>';
        }).join('');

        // Wire click handlers for detail stats
        tbody.querySelectorAll('tr[data-monitor-id]').forEach(function(row) {
            row.addEventListener('click', function() {
                loadMonitorStats(row.getAttribute('data-monitor-id')!, row.getAttribute('data-instance-id')!);
            });
        });
    });
}

// ─── Monitor edit state ─────────────────────────────────────────
var kumaMonitorList: MonitorResponse[] = [];
var monitorEditState: { id: number; instanceId: number } | null = null;

export function openMonitorEdit(monitorId: number, instanceId: number): void {
    var mon: MonitorResponse | null = null;
    for (var i = 0; i < kumaMonitorList.length; i++) {
        if (kumaMonitorList[i].id === monitorId && kumaMonitorList[i].instance_id === instanceId) { mon = kumaMonitorList[i]; break; }
    }
    if (!mon) return;
    monitorEditState = { id: monitorId, instanceId: instanceId };
    document.getElementById('monitor-edit-id')!.textContent = '#' + monitorId + ' (' + mon.instance_name + ')';
    (document.getElementById('monitor-edit-name') as HTMLInputElement).value = mon.name || '';
    (document.getElementById('monitor-edit-type') as HTMLSelectElement).value = mon.type || 'http';
    (document.getElementById('monitor-edit-url') as HTMLInputElement).value = mon.url || '';
    (document.getElementById('monitor-edit-container') as HTMLInputElement).value = mon.docker_container || '';
    (document.getElementById('monitor-edit-interval') as HTMLInputElement).value = mon.interval != null ? String(mon.interval) : '';
    (document.getElementById('monitor-edit-retry') as HTMLInputElement).value = mon.retry_interval != null ? String(mon.retry_interval) : '';
    (document.getElementById('monitor-edit-maxretries') as HTMLInputElement).value = mon.maxretries != null ? String(mon.maxretries) : '';
    new bootstrap.Modal(document.getElementById('monitor-edit-modal')!).show();
}

export function saveMonitorEdit(): void {
    if (!monitorEditState) return;
    var input: Record<string, unknown> = {
        name: (document.getElementById('monitor-edit-name') as HTMLInputElement).value.trim(),
        type: (document.getElementById('monitor-edit-type') as HTMLSelectElement).value,
        url: (document.getElementById('monitor-edit-url') as HTMLInputElement).value.trim(),
        docker_container: (document.getElementById('monitor-edit-container') as HTMLInputElement).value.trim(),
        interval: parseInt((document.getElementById('monitor-edit-interval') as HTMLInputElement).value, 10) || undefined,
        retry_interval: parseInt((document.getElementById('monitor-edit-retry') as HTMLInputElement).value, 10) || undefined,
        maxretries: parseInt((document.getElementById('monitor-edit-maxretries') as HTMLInputElement).value, 10) || undefined
    };
    updateKumaMonitor(monitorEditState.id, monitorEditState.instanceId, input).then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body) { throw new Error((body && body.error) || ('HTTP ' + r.status)); });
        }
        return r.json();
    }).then(function() {
        toast('Monitor updated');
        var modal = bootstrap.Modal.getInstance(document.getElementById('monitor-edit-modal')!);
        if (modal) modal.hide();
        loadKumaMonitors();
        loadDockerServices();
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        toast('Update failed: ' + err.message, 'error');
    });
}

export function deleteMonitor(): void {
    if (!monitorEditState) return;
    deleteKumaMonitor(monitorEditState.id, monitorEditState.instanceId).then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body) { throw new Error((body && body.error) || ('HTTP ' + r.status)); });
        }
        return r.json();
    }).then(function() {
        toast('Monitor deleted');
        var modal = bootstrap.Modal.getInstance(document.getElementById('monitor-edit-modal')!);
        if (modal) modal.hide();
        loadKumaMonitors();
        loadDockerServices();
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        toast('Delete failed: ' + err.message, 'error');
    });
}

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
            rows.push('<tr class="npm-proxy-row" data-idx="' + idx + '" onclick="toggleNPMProxyDetail(this)">'
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
    var parts = '';
    parts += '<span class="detail-inline-label">ID:</span> ' + h.id + '<br>';
    parts += '<span class="detail-inline-label">Forward:</span> ' + esc(h.forward_scheme + '://' + h.forward_host + ':' + h.forward_port) + '<br>';
    parts += '<span class="detail-inline-label">Enabled:</span> ' + (h.enabled ? 'yes' : 'no') + '<br>';
    if (h.ssl_forced) parts += '<span class="detail-inline-label">SSL Forced:</span> yes<br>';
    if (h.hsts_enabled) parts += '<span class="detail-inline-label">HSTS:</span> yes<br>';
    if (h.allow_websocket_upgrade) parts += '<span class="detail-inline-label">WebSocket Upgrade:</span> yes<br>';
    if (h.advanced_config) parts += '<span class="detail-inline-label">Advanced Config:</span><pre class="detail-pre">' + esc(h.advanced_config) + '</pre>';
    if (h.locations && h.locations.length) {
        parts += '<span class="detail-inline-label">Locations:</span><br>';
        h.locations.forEach(function(l) {
            parts += '<div class="ms-3">' + esc(l.path || '/') + ' → ' + esc(l.forward_scheme + '://' + l.forward_host + ':' + l.forward_port) + '</div>';
        });
    }
    return '<div class="detail-container">' + parts + '</div>';
}

window.toggleNPMProxyDetail = function(row: HTMLElement) {
    var idx = row.getAttribute('data-idx');
    var detailRow = row.parentNode!.querySelector('.npm-detail-row[data-idx="' + idx + '"]') as HTMLElement | null;
    if (detailRow) {
        var isVisible = detailRow.style.display !== 'none';
        detailRow.style.display = isVisible ? 'none' : 'table-row';
        row.classList.toggle('detail-expanded', !isVisible);
    }
};

export function loadHistory(): void {
    document.getElementById('history-tbody')!.innerHTML = loadingRow(9);
    apiFetch('/api/sync/history').then(function(r){return r.json() as Promise<SyncRun[]>;}).then(function(runs) {
        var tbody = document.getElementById('history-tbody')!;
        if (!runs.length) {
            tbody.innerHTML = emptyRow(9, 'No sync history yet');
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
                + '<td data-label="Source"><span class="badge ' + (r.source === 'docker' ? 'bg-primary' : r.source === 'reconcile' ? 'bg-info' : 'bg-success') + '">' + r.source + '</span></td>'
                + '<td data-label="Status"><span class="badge ' + badge + '">' + statusIcon + ' ' + r.status + '</span></td>'
                + '<td data-label="Started" class="small">' + (r.started_at ? new Date(r.started_at).toLocaleString() : '') + '</td>'
                + '<td data-label="Added">' + r.added + '</td>'
                + '<td data-label="Updated">' + (r.updated ?? 0) + '</td>'
                + '<td data-label="Skipped">' + r.skipped + '</td>'
                + '<td data-label="Failed">' + r.failed + '</td>'
                + '<td data-label="Error" class="small text-danger">' + esc(r.error_message || '') + '</td>'
                + '</tr>';
        }).join('');
    });
}

export function loadEvents(): void {
    document.getElementById('events-tbody')!.innerHTML = loadingRow(5);
    apiFetch('/api/events').then(function(r){return r.json() as Promise<FeedItem[]>;}).then(function(items) {
        var tbody = document.getElementById('events-tbody')!;
        if (!items.length) {
            tbody.innerHTML = emptyRow(5, 'No events recorded yet');
            return;
        }
        tbody.innerHTML = items.map(function(it) {
            var kindBadge = it.kind === 'docker' ? 'bg-primary' : it.kind === 'reconcile' ? 'bg-info' : 'bg-secondary';
            var statusBadge = 'bg-secondary';
            if (it.status === 'completed') statusBadge = 'bg-success';
            else if (it.status === 'completed_with_errors') statusBadge = 'bg-warning text-dark';
            else if (it.status === 'error' || it.status === 'died' || it.status === 'unhealthy' || it.status === 'kill') statusBadge = 'bg-danger';
            return '<tr>'
                + '<td data-label="Time" class="small">' + (it.time ? new Date(it.time).toLocaleString() : '') + '</td>'
                + '<td data-label="Kind"><span class="badge ' + kindBadge + '">' + esc(it.kind) + '</span></td>'
                + '<td data-label="Title">' + esc(it.title) + '</td>'
                + '<td data-label="Detail" class="text-truncate" style="max-width:350px">' + esc(it.detail || '') + '</td>'
                + '<td data-label="Status"><span class="badge ' + statusBadge + '">' + esc(it.status || '') + '</span></td>'
                + '</tr>';
        }).join('');
    });
}

export function runReconcile(): void {
    var btn = document.getElementById('btn-reconcile') as HTMLButtonElement;
    var resultEl = document.getElementById('reconcile-result')!;
    var dryRun = (document.getElementById('reconcile-dry-run') as HTMLInputElement).checked;
    var service = (document.getElementById('reconcile-service') as HTMLInputElement).value.trim();
    var payload: Record<string, unknown> = { dry_run: dryRun };
    if (service) payload.service = service;

    btn.disabled = true;
    resultEl.textContent = 'Running...';
    apiFetch('/api/reconcile', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
        .then(function(r){return r.json() as Promise<ReconcileResult>;})
        .then(function(res) {
            var summary = res.run.status + ': ' + res.changes.length + ' change(s)';
            if (res.dry_run) summary += ' (dry run)';
            resultEl.textContent = summary;
            toast('Reconcile ' + (res.dry_run ? 'preview' : 'finished') + ': ' + res.changes.length + ' change(s)', res.run.status === 'completed_with_errors' ? 'error' : 'success');
            if (res.changes.length) { loadEvents(); loadHistory(); loadDockerServices(); }
        })
        .catch(function(err: Error) {
            if (err.message === 'not authenticated') return;
            resultEl.textContent = 'Failed';
            toast('Reconcile failed', 'error');
        })
        .finally(function() { btn.disabled = false; });
}

window.loadDockerServices = loadDockerServices;
window.loadKumaMonitors = loadKumaMonitors;
window.loadNPMProxies = loadNPMProxies;
window.loadHistory = loadHistory;
window.loadEvents = loadEvents;
window.runReconcile = runReconcile;
window.openLinkEditorByIndex = openLinkEditorByIndex;
window.openLinkEditor = openLinkEditor;
window.saveServiceLink = saveServiceLink;
window.unlinkServiceLink = unlinkServiceLink;
window.refreshServiceLinkDetails = refreshServiceLinkDetails;
window.createNPMHostFromLink = createNPMHostFromLink;
window.createKumaMonitorFromLink = createKumaMonitorFromLink;
window.openMonitorEdit = openMonitorEdit;
window.saveMonitorEdit = saveMonitorEdit;
window.deleteMonitor = deleteMonitor;
window.toggleNPMProxyDetail = toggleNPMProxyDetail;

// Wire modal actions (modals exist in static/index.html before this module runs)
document.getElementById('link-save-btn')!.addEventListener('click', saveServiceLink);
document.getElementById('link-unlink-btn')!.addEventListener('click', unlinkServiceLink);
document.getElementById('link-refresh-btn')!.addEventListener('click', refreshServiceLinkDetails);
document.getElementById('link-npm-create-btn')!.addEventListener('click', createNPMHostFromLink);
document.getElementById('link-kuma-create-btn')!.addEventListener('click', createKumaMonitorFromLink);
document.getElementById('monitor-edit-save')!.addEventListener('click', saveMonitorEdit);
document.getElementById('monitor-edit-delete')!.addEventListener('click', deleteMonitor);
