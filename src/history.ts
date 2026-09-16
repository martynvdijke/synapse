// Sync history, event feed, and reconcile runner.
import type { SyncRun, FeedItem, ReconcileResult } from './types';
import { esc, apiFetch, emptyRow, loadingRow } from './api';
import { toast } from './toast';
import { loadDockerServices } from './docker';

export function renderHistory(runs: SyncRun[]): void {
    var tbody = document.getElementById('history-tbody')!;
    if (!runs.length) { tbody.innerHTML = emptyRow(9, 'No sync history yet'); return; }
    tbody.innerHTML = runs.map(function(r) {
            var badge = 'bg-primary';
            if (r.status === 'completed') badge = 'bg-success';
            else if (r.status === 'completed_with_errors') badge = 'bg-warning text-dark';
            else if (r.status === 'error') badge = 'bg-danger';
            var statusIcon = r.status === 'completed' ? '\u2713' : r.status === 'completed_with_errors' ? '\u26A0' : r.status === 'error' ? '\u2717' : '\u25CB';
            var skippedCell = r.skipped > 0 ? '<span class="badge bg-secondary">' + r.skipped + '</span>' : String(r.skipped);
            return '<tr>'
                + '<td data-label="ID">#' + r.id + '</td>'
                + '<td data-label="Source"><span class="badge ' + (r.source === 'docker' ? 'bg-primary' : r.source === 'reconcile' ? 'bg-info' : 'bg-success') + '">' + r.source + '</span></td>'
                + '<td data-label="Status"><span class="badge ' + badge + '">' + statusIcon + ' ' + r.status + '</span></td>'
                + '<td data-label="Started" class="small">' + (r.started_at ? new Date(r.started_at).toLocaleString() : '') + '</td>'
                + '<td data-label="Added">' + r.added + '</td>'
                + '<td data-label="Updated">' + (r.updated ?? 0) + '</td>'
                + '<td data-label="Skipped">' + skippedCell + '</td>'
                + '<td data-label="Failed">' + r.failed + '</td>'
                + '<td data-label="Error" class="small text-danger">' + esc(r.error_message || '') + '</td>'
                + '</tr>';
        }).join('');
}

export function loadHistory(): void {
    document.getElementById('history-tbody')!.innerHTML = loadingRow(9);
    apiFetch('/api/sync/history').then(function(r){return r.json() as Promise<SyncRun[]>;}).then(function(runs) { renderHistory(runs); });
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

function renderReconcileChanges(changes: ReconcileResult['changes']): string {
    if (!changes || !changes.length) return '<div class="small text-muted mt-2">No changes</div>';
    var rows = changes.map(function(c){
        var badge = 'bg-primary';
        if (c.action === 'created') badge = 'bg-success';
        else if (c.action === 'updated') badge = 'bg-info';
        else if (c.action === 'skipped') badge = 'bg-secondary';
        else if (c.action === 'error') badge = 'bg-danger';
        var isSkippedPaused = c.action === 'skipped' && c.detail && c.detail.toLowerCase().indexOf('paused') >= 0;
        var trAttr = isSkippedPaused ? ' class="text-muted" style="opacity:0.7"' : '';
        return '<tr' + trAttr + '>'
            + '<td>' + esc(c.service) + '</td>'
            + '<td><span class="badge ' + badge + '">' + esc(c.target) + '</span></td>'
            + '<td><span class="badge ' + badge + '">' + esc(c.action) + '</span></td>'
            + '<td class="small">' + esc(c.detail || '') + '</td>'
            + '</tr>';
    }).join('');
    return '<div class="table-responsive bg-white rounded shadow-sm mt-2"><table class="table table-sm table-hover align-middle mb-0" aria-label="Reconcile changes">'
        + '<thead class="table-light"><tr><th>Service</th><th>Target</th><th>Action</th><th>Detail</th></tr></thead>'
        + '<tbody>' + rows + '</tbody></table></div>';
}

export function runReconcile(): void {
    var btn = document.getElementById('btn-reconcile') as HTMLButtonElement;
    var resultEl = document.getElementById('reconcile-result')!;
    var changesEl = document.getElementById('reconcile-changes')!;
    var dryRun = (document.getElementById('reconcile-dry-run') as HTMLInputElement).checked;
    var service = (document.getElementById('reconcile-service') as HTMLInputElement).value.trim();
    var payload: Record<string, unknown> = { dry_run: dryRun };
    if (service) payload.service = service;

    btn.disabled = true;
    resultEl.textContent = 'Running...';
    if (changesEl) changesEl.innerHTML = '';
    apiFetch('/api/reconcile', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
        .then(function(r){return r.json() as Promise<ReconcileResult>;})
        .then(function(res) {
            var summary = res.run.status + ': ' + res.changes.length + ' change(s)';
            if (res.dry_run) summary += ' (dry run)';
            resultEl.textContent = summary;
            toast('Reconcile ' + (res.dry_run ? 'preview' : 'finished') + ': ' + res.changes.length + ' change(s)', res.run.status === 'completed_with_errors' ? 'error' : 'success');
            if (changesEl) changesEl.innerHTML = renderReconcileChanges(res.changes);
            if (res.changes.length) { loadEvents(); loadHistory(); loadDockerServices(); }
        })
        .catch(function(err: unknown) {
            if (err instanceof Error && err.message === 'not authenticated') return;
            resultEl.textContent = 'Failed';
            toast('Reconcile failed', 'error');
        })
        .finally(function() { btn.disabled = false; });
}
