// Uptime Kuma tab: monitor list, stats detail, pause/resume, edit/delete.
import type { MonitorResponse, MonitorStats, MonitorTag, ApiErrorBody, MonitorTagAck, TagInput } from './types';
import { esc, apiFetch, emptyRow, loadingRow, pauseKumaMonitor, resumeKumaMonitor, updateKumaMonitor, deleteKumaMonitor, setMonitorTags } from './api';
import { toast } from './toast';
import { renderTagChips, parseTagsInput, tagsEqual } from './monitorTags';
import { loadDockerServices } from './docker';

function extractError(body: unknown, fallback: string): string {
    var b = body as ApiErrorBody;
    return (b && (b.error || b.msg || b.message)) || fallback;
}

export function pauseKumaMonitorAction(kumaId: number, instanceId: number): void {
    pauseKumaMonitor(kumaId, instanceId).then(function(r: Response){
        return r.json().then(function(body: unknown){ if(!r.ok) throw new Error(extractError(body, 'HTTP '+r.status)); return body as ApiErrorBody; });
    }).then(function(body: ApiErrorBody){
        toast(body && body.msg ? body.msg : 'Monitor paused', 'success');
        loadKumaMonitors();
    }).catch(function(err: unknown){
        var msg = err instanceof Error ? err.message : String(err);
        if(msg==='not authenticated') return;
        toast('Pause failed: '+msg, 'error');
    });
}

export function resumeKumaMonitorAction(kumaId: number, instanceId: number): void {
    resumeKumaMonitor(kumaId, instanceId).then(function(r: Response){
        return r.json().then(function(body: unknown){ if(!r.ok) throw new Error(extractError(body, 'HTTP '+r.status)); return body as ApiErrorBody; });
    }).then(function(body: ApiErrorBody){
        toast(body && body.msg ? body.msg : 'Monitor resumed', 'success');
        loadKumaMonitors();
    }).catch(function(err: unknown){
        var msg = err instanceof Error ? err.message : String(err);
        if(msg==='not authenticated') return;
        toast('Resume failed: '+msg, 'error');
    });
}

// ─── Monitor detail stats cache ────────────────────────────────
var monitorStatsCache = new Map<string, CacheEntry>();
var STATS_CACHE_TTL = 60000; // 60 seconds

interface CacheEntry {
    stats: MonitorStats;
    timestamp: number;
}

function getCachedStats(instanceId: string, monitorId: string): MonitorStats | null {
    var key = instanceId + ':' + monitorId;
    var entry = monitorStatsCache.get(key);
    if (entry && Date.now() - entry.timestamp < STATS_CACHE_TTL) return entry.stats;
    return null;
}

function renderMonitorStats(stats: MonitorStats, mon?: MonitorResponse): string {
    var statusBadge = stats.status === 1
        ? '<span class="badge bg-success">UP</span>'
        : stats.status === 0
        ? '<span class="badge bg-danger">DOWN</span>'
        : '<span class="badge bg-secondary">UNKNOWN</span>';
    var activeBadge = '';
    var tagsRow = '';
    if (mon) {
        if (mon.active === false) {
            activeBadge = ' <span class="badge bg-warning text-dark">⏸ Paused</span>';
        } else if (mon.active === true) {
            activeBadge = ' <span class="badge bg-success">Active</span>';
        }
        if (mon.tags && mon.tags.length) {
            tagsRow = '<div class="col-12"><div class="small text-muted">Tags</div><div>' + renderTagChips(mon.tags) + '</div></div>';
        }
    }

    return '<div class="row g-3">'
        + '<div class="col-md-4"><div class="small text-muted">Status</div><div>' + statusBadge + activeBadge + '</div></div>'
        + '<div class="col-md-4"><div class="small text-muted">Uptime 24h</div><div class="fs-5 fw-bold">' + (stats.uptime_24h != null ? stats.uptime_24h.toFixed(1) + '%' : '—') + '</div></div>'
        + '<div class="col-md-4"><div class="small text-muted">Uptime 7d</div><div class="fs-5 fw-bold">' + (stats.uptime_7d != null ? stats.uptime_7d.toFixed(1) + '%' : '—') + '</div></div>'
        + '<div class="col-md-4"><div class="small text-muted">Uptime 1y</div><div class="fs-5 fw-bold">' + (stats.uptime_1y != null ? stats.uptime_1y.toFixed(1) + '%' : '—') + '</div></div>'
        + '<div class="col-md-4"><div class="small text-muted">Avg Ping</div><div class="fs-5 fw-bold">' + (stats.avg_ping != null ? stats.avg_ping.toFixed(1) + 'ms' : '—') + '</div></div>'
        + '<div class="col-md-4"><div class="small text-muted">Last Message</div><div class="text-truncate">' + (stats.last_msg ? esc(stats.last_msg) : '—') + '</div></div>'
        + (stats.cert_info ? '<div class="col-12"><div class="small text-muted">Certificate</div><div><code>' + esc(stats.cert_info) + '</code></div></div>' : '')
        + tagsRow
        + '</div>';
}

export function loadMonitorStats(monitorId: string, instanceId: string): void {
    var cacheKey = instanceId + ':' + monitorId;
    var mon: MonitorResponse | undefined;
    for (var i=0;i<kumaMonitorList.length;i++){ if(String(kumaMonitorList[i].id)===monitorId && String(kumaMonitorList[i].instance_id)===instanceId){ mon=kumaMonitorList[i]; break; } }
    var cached = getCachedStats(instanceId, monitorId);
    if (cached) {
        document.getElementById('monitor-detail-title')!.textContent = 'Monitor #' + monitorId + (mon ? ' — ' + mon.name : '');
        document.getElementById('monitor-detail-body')!.innerHTML = renderMonitorStats(cached, mon);
        document.getElementById('monitor-detail-panel')!.classList.remove('d-none');
        return;
    }

    document.getElementById('monitor-detail-title')!.textContent = 'Monitor #' + monitorId + (mon ? ' — ' + mon.name : '');
    document.getElementById('monitor-detail-body')!.innerHTML = '<div class="text-center text-muted py-3"><span class="spinner-border spinner-border-sm" role="status"></span> Loading stats...</div>';
    document.getElementById('monitor-detail-panel')!.classList.remove('d-none');

    apiFetch('/api/monitors/' + monitorId + '/stats?instance=' + instanceId)
        .then(function(r) {
            if (!r.ok) { throw new Error('' + r.status); }
            return r.json() as Promise<MonitorStats>;
        })
        .then(function(stats) {
            monitorStatsCache.set(cacheKey, { stats: stats, timestamp: Date.now() });
            document.getElementById('monitor-detail-body')!.innerHTML = renderMonitorStats(stats, mon);
        })
        .catch(function(err: unknown) {
            var msg2 = err instanceof Error ? err.message : String(err);
            if (msg2 === 'not authenticated') return;
            var msg = 'Stats unavailable';
            if (msg2 === '404') msg = 'Instance not found';
            else if (msg2 === '502') msg = 'Stats unavailable (Socket.IO connection failed)';
            document.getElementById('monitor-detail-body')!.innerHTML = '<div class="text-center text-danger py-3">' + msg + '</div>';
        });
}

export function loadKumaMonitors(): void {
    document.getElementById('kuma-tbody')!.innerHTML = loadingRow(10);
    apiFetch('/api/monitors').then(function(r){return r.json() as Promise<(MonitorResponse & ApiErrorBody)[]>;}).then(function(monitors) {
        var tbody = document.getElementById('kuma-tbody')!;
        var monErr = (monitors as unknown as ApiErrorBody).error;
        if (monErr) {
            tbody.innerHTML = '<tr><td colspan="10" class="text-center text-danger py-3">' + esc(monErr) + '</td></tr>';
            return;
        }
        if (!monitors.length) {
            tbody.innerHTML = emptyRow(10, 'No monitors in Uptime Kuma');
            return;
        }
        kumaMonitorList = monitors as MonitorResponse[];
        tbody.innerHTML = monitors.map(function(m) {
            var isPaused = m.active === false;
            var pausedBadge = isPaused ? ' <span class="badge bg-warning text-dark">⏸ Paused</span>' : '';
            var tagsHtml = renderTagChips(m.tags);
            var rowOpacity = isPaused ? ' style="cursor:pointer;opacity:0.65"' : ' style="cursor:pointer"';
            var toggleBtn = isPaused
                ? '<button class="btn btn-sm btn-outline-success me-1" data-action="resume-kuma-monitor" data-kuma-id="' + m.id + '" data-instance-id="' + m.instance_id + '">Resume</button>'
                : '<button class="btn btn-sm btn-outline-warning me-1" data-action="pause-kuma-monitor" data-kuma-id="' + m.id + '" data-instance-id="' + m.instance_id + '">Pause</button>';
            var editBtn = isPaused
                ? '<button class="btn btn-sm btn-outline-secondary" disabled title="Resume to edit" data-action="open-monitor-edit" data-id="' + m.id + '" data-instance-id="' + m.instance_id + '">Edit</button>'
                : '<button class="btn btn-sm btn-outline-secondary" data-action="open-monitor-edit" data-id="' + m.id + '" data-instance-id="' + m.instance_id + '">Edit</button>';
            return '<tr' + rowOpacity + ' data-action="show-monitor-stats" data-monitor-id="' + m.id + '" data-instance-id="' + m.instance_id + '">'
                + '<td data-label="ID">#' + m.id + '</td>'
                + '<td data-label="Name">' + esc(m.name) + pausedBadge + '</td>'
                + '<td data-label="Instance"><span class="badge bg-primary">' + esc(m.instance_name || '—') + '</span></td>'
                + '<td data-label="Type"><span class="badge ' + (m.type === 'http' ? 'bg-info' : m.type === 'docker' ? 'bg-warning text-dark' : 'bg-secondary') + '">' + (m.type === 'http' ? '\u25CB ' : m.type === 'docker' ? '\u25A3 ' : '') + m.type.toUpperCase() + '</span></td>'
                + '<td data-label="URL / Container" class="text-truncate" style="max-width:220px">' + (m.url ? esc(m.url) : m.docker_container ? esc(m.docker_container) : '—') + '</td>'
                + '<td data-label="Interval">' + (m.interval ? m.interval + 's' : '—') + '</td>'
                + '<td data-label="Retry">' + (m.retry_interval ? m.retry_interval + 's' : '—') + '</td>'
                + '<td data-label="Max Retries">' + (m.maxretries || '—') + '</td>'
                + '<td data-label="Tags">' + tagsHtml + '</td>'
                + '<td data-label="Actions" class="text-nowrap">' + toggleBtn + editBtn + '</td>'
                + '</tr>';
        }).join('');
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
    var tagsEl = document.getElementById('monitor-edit-tags') as HTMLInputElement | null;
    if (tagsEl) {
        tagsEl.value = mon.tags && mon.tags.length ? mon.tags.map(function(t){ return String(t.id); }).join(',') : '';
    }
    // Show paused badge in modal title area
    var badgeEl = document.getElementById('monitor-edit-paused-badge');
    if (badgeEl) {
        badgeEl.innerHTML = mon.active === false ? '<span class="badge bg-warning text-dark ms-2">⏸ Paused</span>' : '';
    }
    var chipsEl = document.getElementById('monitor-edit-tags-preview');
    if (chipsEl) {
        chipsEl.innerHTML = mon.tags && mon.tags.length ? renderTagChips(mon.tags) : '<span class="text-muted small">No tags</span>';
    }
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
    var tagsEl = document.getElementById('monitor-edit-tags') as HTMLInputElement | null;
    var tagsRaw = tagsEl ? tagsEl.value.trim() : '';
    var tagsParsed = parseTagsInput(tagsRaw);
    // Find original for diff
    var origMon: MonitorResponse | null = null;
    for (var i=0;i<kumaMonitorList.length;i++){ if(kumaMonitorList[i].id===monitorEditState.id && kumaMonitorList[i].instance_id===monitorEditState.instanceId){ origMon=kumaMonitorList[i]; break; } }
    var tagsChanged = false;
    if (origMon) {
        tagsChanged = !tagsEqual(origMon.tags, tagsParsed);
    } else {
        tagsChanged = tagsRaw.length>0;
    }
    updateKumaMonitor(monitorEditState.id, monitorEditState.instanceId, input).then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body: unknown) { throw new Error(extractError(body, 'HTTP ' + r.status)); });
        }
        return r.json() as Promise<unknown>;
    }).then(function() {
        if (!tagsChanged) {
            toast('Monitor updated');
            var modal = bootstrap.Modal.getInstance(document.getElementById('monitor-edit-modal')!);
            if (modal) modal.hide();
            loadKumaMonitors();
            loadDockerServices();
            return null;
        }
        // Call setMonitorTags after update
        return setMonitorTags(monitorEditState!.id, monitorEditState!.instanceId, tagsParsed as TagInput[]).then(function(r: Response){
            return r.json().then(function(body: unknown){ if(!r.ok) throw new Error(extractError(body, 'HTTP '+r.status)); return body as MonitorTagAck; });
        }).then(function(body: MonitorTagAck){
            var msg = body && body.msg ? body.msg : (body && body.added ? 'Tags updated' : 'Monitor updated');
            // surface Kuma ack verbatim if present
            if (body && body.errors && body.errors.length) {
                toast(msg + ' (' + body.errors.join('; ') + ')', body.errors.length ? 'error' : 'success');
            } else {
                toast(msg, 'success');
            }
            var modal2 = bootstrap.Modal.getInstance(document.getElementById('monitor-edit-modal')!);
            if (modal2) modal2.hide();
            loadKumaMonitors();
            loadDockerServices();
        }).catch(function(err: unknown){
            var m = err instanceof Error ? err.message : String(err);
            if(m==='not authenticated') return;
            toast('Tags update failed: '+m, 'error');
            // still reload to reflect monitor edit
            loadKumaMonitors();
        });
    }).catch(function(err: unknown) {
        var m2 = err instanceof Error ? err.message : String(err);
        if (m2 === 'not authenticated') return;
        toast('Update failed: ' + m2, 'error');
    });
}

export function deleteMonitor(): void {
    if (!monitorEditState) return;
    deleteKumaMonitor(monitorEditState.id, monitorEditState.instanceId).then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body: unknown) { throw new Error(extractError(body, 'HTTP ' + r.status)); });
        }
        return r.json() as Promise<unknown>;
    }).then(function() {
        toast('Monitor deleted');
        var modal = bootstrap.Modal.getInstance(document.getElementById('monitor-edit-modal')!);
        if (modal) modal.hide();
        loadKumaMonitors();
        loadDockerServices();
    }).catch(function(err: unknown) {
        var m = err instanceof Error ? err.message : String(err);
        if (m === 'not authenticated') return;
        toast('Delete failed: ' + m, 'error');
    });
}

export function setupMonitorEditListeners(): void {
    document.getElementById('monitor-edit-save')!.addEventListener('click', saveMonitorEdit);
    document.getElementById('monitor-edit-delete')!.addEventListener('click', deleteMonitor);
    var tagsInputEl = document.getElementById('monitor-edit-tags') as HTMLInputElement | null;
    if (tagsInputEl) {
        tagsInputEl.addEventListener('input', function(){
            var preview = document.getElementById('monitor-edit-tags-preview');
            if (!preview) return;
            var parsed = parseTagsInput(tagsInputEl!.value);
            if (!parsed.length) { preview.innerHTML = '<span class="text-muted small">No tags</span>'; return; }
            var tmp: MonitorTag[] = parsed.map(function(p: TagInput){
                if ('id' in p) return { id: p.id, name: String(p.id) } as MonitorTag;
                return { id: 0, name: p.name } as MonitorTag;
            });
            preview.innerHTML = tmp.map(function(t){ return '<span class="badge bg-dark me-1">' + esc(t.name) + '</span>'; }).join('');
        });
    }
}
