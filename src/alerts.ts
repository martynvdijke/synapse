// Alerts tab logic — rule CRUD + incident lifecycle
import type { AlertRuleJSON, AlertIncidentJSON } from './types';

var alertRules: AlertRuleJSON[] = [];

function formatThreshold(seconds: number): string {
    if (!seconds) return '—';
    if (seconds % 86400 === 0) return seconds / 86400 + 'd';
    if (seconds % 3600 === 0) return seconds / 3600 + 'h';
    if (seconds % 60 === 0) return seconds / 60 + 'm';
    return seconds + 's';
}

function typeLabel(t: string): string {
    switch (t) {
        case 'monitor_down_for': return 'Monitor down for';
        case 'container_down': return 'Container down';
        case 'sync_stale': return 'Sync stale';
        case 'reconcile_drift': return 'Reconcile drift';
        default: return t;
    }
}

function statusBadge(status: string): string {
    switch (status) {
        case 'open': return '<span class="badge bg-danger">Open</span>';
        case 'acknowledged': return '<span class="badge bg-warning text-dark">Acknowledged</span>';
        case 'resolved': return '<span class="badge bg-secondary">Resolved</span>';
        default: return esc(status);
    }
}

export function loadAlertRules(): void {
    apiFetch('/api/alert-rules').then(function(r){return r.json() as Promise<AlertRuleJSON[]>;}).then(function(rules) {
        alertRules = rules || [];
        var tbody = document.getElementById('alert-rules-tbody')!;
        if (!alertRules.length) { tbody.innerHTML = emptyRow(6, 'No rules defined — add one to start monitoring'); return; }
        tbody.innerHTML = alertRules.map(function(rule) {
            return '<tr>'
                + '<td data-label="Name">' + esc(rule.name) + '</td>'
                + '<td data-label="Type">' + esc(typeLabel(rule.type)) + '</td>'
                + '<td data-label="Subject">' + (rule.subject ? '<code>' + esc(rule.subject) + '</code>' : '<span class="text-muted">all</span>') + '</td>'
                + '<td data-label="Threshold">' + formatThreshold(rule.threshold_seconds) + '</td>'
                + '<td data-label="Enabled">' + (rule.enabled ? '<span class="badge bg-success">On</span>' : '<span class="badge bg-secondary">Off</span>') + '</td>'
                + '<td data-label="Action">'
                + '<button class="btn btn-outline-primary btn-sm me-1" onclick="editAlertRule(' + rule.id + ')">Edit</button>'
                + '<button class="btn btn-outline-danger btn-sm" onclick="deleteAlertRule(' + rule.id + ')">Delete</button>'
                + '</td>'
                + '</tr>';
        }).join('');
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        toast('Failed to load alert rules', 'error');
    });
}

export function resetAlertRuleForm(): void {
    (document.getElementById('alert-rule-id') as HTMLInputElement).value = '';
    (document.getElementById('alert-rule-name') as HTMLInputElement).value = '';
    (document.getElementById('alert-rule-type') as HTMLSelectElement).value = '';
    (document.getElementById('alert-rule-subject') as HTMLInputElement).value = '';
    (document.getElementById('alert-rule-threshold') as HTMLInputElement).value = '';
    (document.getElementById('alert-rule-enabled') as HTMLInputElement).checked = true;
    document.getElementById('alert-rule-hint')!.textContent = '';
}

export function editAlertRule(id: number): void {
    var rule = null;
    for (var i = 0; i < alertRules.length; i++) {
        if (alertRules[i].id === id) { rule = alertRules[i]; break; }
    }
    if (!rule) return;
    (document.getElementById('alert-rule-id') as HTMLInputElement).value = '' + rule.id;
    (document.getElementById('alert-rule-name') as HTMLInputElement).value = rule.name;
    (document.getElementById('alert-rule-type') as HTMLSelectElement).value = rule.type;
    (document.getElementById('alert-rule-subject') as HTMLInputElement).value = rule.subject;
    (document.getElementById('alert-rule-threshold') as HTMLInputElement).value = rule.threshold_seconds ? formatThreshold(rule.threshold_seconds) : '';
    (document.getElementById('alert-rule-enabled') as HTMLInputElement).checked = rule.enabled;
    var collapse = bootstrap.Collapse.getOrCreateInstance(document.getElementById('alert-rule-form')!);
    collapse.show();
}

export function deleteAlertRule(id: number): void {
    apiFetch('/api/alert-rules/' + id, { method: 'DELETE' })
        .then(function(r){return r.json();})
        .then(function(d: any) {
            if (d.error) { toast(d.error, 'error'); return; }
            toast('Alert rule deleted', 'success');
            resetAlertRuleForm();
            loadAlertRules();
        })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to delete rule', 'error'); });
}

export function saveAlertRule(): void {
    var id = (document.getElementById('alert-rule-id') as HTMLInputElement).value.trim();
    var name = (document.getElementById('alert-rule-name') as HTMLInputElement).value.trim();
    var type = (document.getElementById('alert-rule-type') as HTMLSelectElement).value;
    var subject = (document.getElementById('alert-rule-subject') as HTMLInputElement).value.trim();
    var threshold = (document.getElementById('alert-rule-threshold') as HTMLInputElement).value.trim();
    var enabled = (document.getElementById('alert-rule-enabled') as HTMLInputElement).checked;

    if (!name) { toast('Rule name is required', 'error'); return; }
    if (!type) { toast('Rule type is required', 'error'); return; }

    var body: Record<string, unknown> = { name: name, type: type, enabled: enabled };
    if (subject) body.subject = subject;
    if (threshold) body.threshold = threshold;

    var req: Promise<Response>;
    if (id) {
        req = apiFetch('/api/alert-rules/' + id, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    } else {
        req = apiFetch('/api/alert-rules', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    }
    req.then(function(r){return r.json();})
        .then(function(d: any) {
            if (d.error) { toast(d.error, 'error'); return; }
            toast(id ? 'Alert rule updated' : 'Alert rule created', 'success');
            resetAlertRuleForm();
            var collapse = bootstrap.Collapse.getInstance(document.getElementById('alert-rule-form')!);
            if (collapse) collapse.hide();
            loadAlertRules();
        })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to save rule', 'error'); });
}

export function loadIncidents(): void {
    var filterEl = document.getElementById('incident-filter-status') as HTMLSelectElement | null;
    var status = filterEl ? filterEl.value : '';
    apiFetch('/api/incidents?limit=100' + (status ? '&status=' + encodeURIComponent(status) : ''))
        .then(function(r){return r.json() as Promise<AlertIncidentJSON[]>;})
        .then(function(incidents) {
            var tbody = document.getElementById('incidents-tbody')!;
            if (!incidents || !incidents.length) { tbody.innerHTML = emptyRow(6, 'No incidents'); return; }
            tbody.innerHTML = incidents.map(function(inc) {
                var actions = '';
                if (inc.status === 'open') {
                    actions = '<button class="btn btn-outline-secondary btn-sm me-1" onclick="ackIncident(' + inc.id + ')">Ack</button>'
                        + '<button class="btn btn-outline-success btn-sm" onclick="resolveIncident(' + inc.id + ')">Resolve</button>';
                } else if (inc.status === 'acknowledged') {
                    actions = '<button class="btn btn-outline-success btn-sm" onclick="resolveIncident(' + inc.id + ')">Resolve</button>';
                } else {
                    actions = '<span class="text-muted">—</span>';
                }
                return '<tr>'
                    + '<td data-label="Rule">' + esc(inc.rule_name || ('#' + inc.rule_id)) + '</td>'
                    + '<td data-label="Subject">' + (inc.subject ? '<code>' + esc(inc.subject) + '</code>' : '<span class="text-muted">all</span>') + '</td>'
                    + '<td data-label="Status">' + statusBadge(inc.status) + '</td>'
                    + '<td data-label="Opened" class="small">' + (inc.opened_at ? new Date(inc.opened_at).toLocaleString() : '—') + '</td>'
                    + '<td data-label="Message" class="small">' + esc(inc.message) + '</td>'
                    + '<td data-label="Action">' + actions + '</td>'
                    + '</tr>';
            }).join('');
        })
        .catch(function(err: Error) {
            if (err.message === 'not authenticated') return;
            toast('Failed to load incidents', 'error');
        });
}

export function ackIncident(id: number): void {
    apiFetch('/api/incidents/' + id + '/ack', { method: 'POST' })
        .then(function(r){return r.json();})
        .then(function(d: any) {
            if (d.error) { toast(d.error, 'error'); return; }
            toast('Incident acknowledged', 'success');
            loadIncidents();
        })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to acknowledge incident', 'error'); });
}

export function resolveIncident(id: number): void {
    apiFetch('/api/incidents/' + id + '/resolve', { method: 'POST' })
        .then(function(r){return r.json();})
        .then(function(d: any) {
            if (d.error) { toast(d.error, 'error'); return; }
            toast('Incident resolved', 'success');
            loadIncidents();
        })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to resolve incident', 'error'); });
}

window.loadAlertRules = loadAlertRules;
window.loadIncidents = loadIncidents;
window.editAlertRule = editAlertRule;
window.deleteAlertRule = deleteAlertRule;
window.saveAlertRule = saveAlertRule;
window.resetAlertRuleForm = resetAlertRuleForm;
window.ackIncident = ackIncident;
window.resolveIncident = resolveIncident;
