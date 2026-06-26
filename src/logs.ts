// Log viewer tab logic
import type { LogEntry, LogFilters } from './types';

var logsOffset = 0;
var logsLimit = 200;
var logsSSE: EventSource | null = null;
var logsLoaded = false;

export function isLogsLoaded(): boolean { return logsLoaded; }

export function getLogFilters(): LogFilters {
    return {
        level: (document.getElementById('log-filter-level') as HTMLSelectElement).value,
        source: (document.getElementById('log-filter-source') as HTMLSelectElement).value,
        search: (document.getElementById('log-filter-search') as HTMLInputElement).value.trim(),
        error_kind: (document.getElementById('log-filter-error-kind') as HTMLSelectElement).value
    };
}

export function levelBadge(level: string): string {
    var map: Record<string, string> = { 'DEBUG': 'bg-secondary', 'INFO': 'bg-primary', 'WARN': 'bg-warning text-dark', 'ERROR': 'bg-danger' };
    return '<span class="badge ' + (map[level] || 'bg-secondary') + '">' + level + '</span>';
}

export function durationStr(ns: number): string {
    if (!ns || ns <= 0) return '';
    if (ns < 1000) return ns + 'ns';
    if (ns < 1000000) return (ns / 1000).toFixed(1) + '\u00b5s';
    if (ns < 1000000000) return (ns / 1000000).toFixed(1) + 'ms';
    return (ns / 1000000000).toFixed(2) + 's';
}

export function timeStr(ts: string): string {
    var d = new Date(ts);
    return d.toLocaleDateString() + ' ' + d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' }) + '.' + String(d.getMilliseconds()).padStart(3, '0');
}

export function renderLogRow(e: LogEntry, isNew: boolean): string {
    var cls = isNew ? 'log-row-new' : '';
    var meta = e.metadata && Object.keys(e.metadata).length
        ? '<tr class="log-meta-row" data-log-idx="' + esc(e.timestamp) + '"><td colspan="6"><div class="log-meta-inner">' + esc(JSON.stringify(e.metadata, null, 2)) + '</div></td></tr>'
        : '';
    return '<tr class="' + cls + '" onclick="toggleLogMeta(this)" data-log-ts="' + esc(e.timestamp) + '">'
        + '<td class="log-time">' + timeStr(e.timestamp) + '</td>'
        + '<td>' + levelBadge(e.level) + '</td>'
        + '<td class="log-source">' + esc(e.source) + '</td>'
        + '<td class="log-msg" title="' + esc(e.message) + '">' + esc(e.message) + '</td>'
        + '<td class="log-duration">' + durationStr(e.duration) + '</td>'
        + '<td class="log-error" title="' + esc(e.error || '') + '">' + (e.error ? esc(e.error) : '') + '</td>'
        + '</tr>'
        + meta;
}

export function toggleLogMeta(row: HTMLElement): void {
    var ts = row.getAttribute('data-log-ts');
    var metaRow = row.parentNode!.querySelector('.log-meta-row[data-log-idx="' + ts + '"]') as HTMLElement | null;
    if (metaRow) metaRow.classList.toggle('show');
}

var logSearchDebounce: number | null = null;

export function setupLogFilters(): void {
    document.getElementById('log-filter-level')!.addEventListener('change', function() { if (logsLoaded) loadLogs(false); });
    document.getElementById('log-filter-source')!.addEventListener('change', function() { if (logsLoaded) loadLogs(false); });
    document.getElementById('log-filter-error-kind')!.addEventListener('change', function() { if (logsLoaded) loadLogs(false); });
    document.getElementById('log-filter-search')!.addEventListener('input', function() {
        if (logSearchDebounce != null) clearTimeout(logSearchDebounce);
        logSearchDebounce = setTimeout(function() { if (logsLoaded) loadLogs(false); }, 300);
    });
    document.getElementById('btn-log-refresh')!.addEventListener('click', function() { loadLogs(false); });
    document.getElementById('btn-log-clear')!.addEventListener('click', function() {
        document.getElementById('logs-tbody')!.innerHTML = emptyRow(6, 'No log entries yet');
        logsOffset = 0;
        logsLoaded = false;
    });
    document.getElementById('btn-log-load-more')!.addEventListener('click', function() { loadLogs(true); });
}

export function loadLogs(append: boolean): void {
    if (!append) logsOffset = 0;
    var filters = getLogFilters();
    var params = new URLSearchParams();
    if (filters.level) params.set('level', filters.level);
    if (filters.source) params.set('source', filters.source);
    if (filters.search) params.set('search', filters.search);
    if (filters.error_kind) params.set('error_kind', filters.error_kind);
    params.set('limit', '' + logsLimit);
    params.set('offset', '' + logsOffset);

    var tbody = document.getElementById('logs-tbody')!;
    if (!append) tbody.innerHTML = loadingRow(6);

    apiFetch('/api/logs?' + params.toString()).then(function(r){return r.json() as Promise<LogEntry[]>;}).then(function(entries) {
        if (append) {
            var html = entries.map(function(e) { return renderLogRow(e, false); }).join('');
            var ib = tbody.querySelector('#log-load-more-row');
            if (ib) ib.insertAdjacentHTML('beforebegin', html);
            else tbody.insertAdjacentHTML('beforeend', html);
        } else {
            tbody.innerHTML = entries.length
                ? entries.map(function(e) { return renderLogRow(e, true); }).join('')
                : emptyRow(6, 'No log entries yet');
        }
        var loadMore = document.getElementById('log-load-more-area');
        if (loadMore) loadMore.style.display = entries.length < logsLimit ? 'none' : '';
        logsOffset += entries.length;
        logsLoaded = true;
    }).catch(function(err: Error) { if (err.message === 'not authenticated') return; if (!append) tbody.innerHTML = emptyRow(6, 'Failed to load logs'); });
}

export function connectLogSSE(): void {
    if (logsSSE) { logsSSE.close(); logsSSE = null; }
    var retryDelay = 1000;
    function connect() {
        logsSSE = new EventSource('/api/logs/stream');
        logsSSE.onmessage = function(e: MessageEvent) {
            retryDelay = 1000;
            var entry: LogEntry = JSON.parse(e.data);
            var tbody = document.getElementById('logs-tbody')!;
            var newRow = renderLogRow(entry, true);
            tbody.insertAdjacentHTML('afterbegin', newRow);
            var rows = tbody.querySelectorAll('tr:not(.log-meta-row)');
            var maxRows = logsLimit + 50;
            if (rows.length > maxRows) {
                for (var i = maxRows; i < rows.length; i++) {
                    var meta = rows[i].parentNode!.querySelector('.log-meta-row[data-log-idx="' + rows[i].getAttribute('data-log-ts') + '"]');
                    if (meta) meta.remove();
                    rows[i].remove();
                }
            }
            var es = tbody.querySelector('.empty-state');
            if (es) (es.closest('tr') as HTMLElement | null)?.remove();
        };
        logsSSE.onerror = function() { if (logsSSE) logsSSE.close(); setTimeout(connect, retryDelay); retryDelay = Math.min(retryDelay * 2, 30000); };
    }
    connect();
}

window.toggleLogMeta = toggleLogMeta;
window.setupLogFilters = setupLogFilters;
window.loadLogs = loadLogs;
window.connectLogSSE = connectLogSSE;
