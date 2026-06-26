// Settings tab logic
import type { KumaInstanceJSON, NPMInstanceJSON, SettingsResponse } from './types';

export function loadSettings(): void {
    apiFetch('/api/settings').then(function(r){return r.json() as Promise<SettingsResponse>;}).then(function(s) {
        (document.getElementById('s-compose-path') as HTMLInputElement).value = (s.compose_path as string) || '';
        (document.getElementById('s-auth-config-path') as HTMLInputElement).value = (s.authelia_config_path as string) || '';
        (document.getElementById('s-auth-db-path') as HTMLInputElement).value = (s.authelia_db_path as string) || '';
        (document.getElementById('s-auth-sync-enabled') as HTMLInputElement).checked = !!(s.authelia_sync_enabled);
        (document.getElementById('s-auth-default-policy') as HTMLSelectElement).value = (s.authelia_default_policy as string) || 'one_factor';
        (document.getElementById('s-auth-overrides') as HTMLTextAreaElement).value = (s.authelia_sync_overrides as string) || '';
    });
}

export function saveSettings(e: Event): void {
    e.preventDefault();
    var btn = document.querySelector('#settings-form button[type="submit"]') as HTMLButtonElement;
    btn.disabled = true;
    var orig = btn.innerHTML;
    btn.innerHTML = '<span class="spinner-sm"></span> Saving...';

    var payload: Record<string, unknown> = {
        compose_path: (document.getElementById('s-compose-path') as HTMLInputElement)?.value || '',
        authelia_config_path: (document.getElementById('s-auth-config-path') as HTMLInputElement)?.value || '',
        authelia_db_path: (document.getElementById('s-auth-db-path') as HTMLInputElement)?.value || '',
        authelia_sync_enabled: (document.getElementById('s-auth-sync-enabled') as HTMLInputElement)?.checked || false,
        authelia_default_policy: (document.getElementById('s-auth-default-policy') as HTMLSelectElement)?.value || 'one_factor',
        authelia_sync_overrides: (document.getElementById('s-auth-overrides') as HTMLTextAreaElement)?.value || ''
    };
    apiFetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
        .then(function(r) { if (!r.ok) throw new Error('Save failed'); return r.json(); })
        .then(function() { toast('Settings saved', 'success'); })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to save settings', 'error'); })
        .finally(function() { btn.disabled = false; btn.innerHTML = orig; });
}

export function testConnection(_service: string): void {
    // Legacy — NPM testing is now per-instance via testNPMInstance
}

// ─── Kuma Instance CRUD ───────────────────────────────────────

var kumaInstancesCache: KumaInstanceJSON[] = [];

export function loadKumaInstances(): void {
    var listEl = document.getElementById('kuma-instances-list');
    if (!listEl) return;
    listEl.innerHTML = '<div class="text-center text-muted py-3"><span class="spinner-sm"></span> Loading...</div>';
    apiFetch('/api/kuma-instances').then(function(r){return r.json() as Promise<KumaInstanceJSON[]>;}).then(function(instances) {
        kumaInstancesCache = instances || [];
        renderKumaInstances(kumaInstancesCache);
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        listEl!.innerHTML = '<div class="text-center text-danger py-3">Failed to load instances</div>';
    });
}

function renderKumaInstances(instances: KumaInstanceJSON[]): void {
    var listEl = document.getElementById('kuma-instances-list')!;
    if (!instances.length) {
        listEl.innerHTML = '<div class="text-center text-muted py-3">No Kuma instances configured. Click "Add Instance" to create one.</div>';
        return;
    }
    var html = '';
    instances.forEach(function(inst) {
        var enabledBadge = inst.enabled
            ? '<span class="badge bg-success">Enabled</span>'
            : '<span class="badge bg-secondary">Disabled</span>';
        html += '<div class="card card-body bg-light p-2 mb-2 d-flex flex-row align-items-center justify-content-between">'
            + '<div class="flex-grow-1">'
            + '<div class="fw-semibold">' + esc(inst.name) + ' ' + enabledBadge + '</div>'
            + '<div class="small text-muted">' + esc(inst.url) + ' &middot; ' + esc(inst.username) + '</div>'
            + '</div>'
            + '<div class="d-flex gap-1">'
            + '<button type="button" class="btn btn-outline-secondary btn-sm" onclick="editKumaInstance(' + inst.id + ')">Edit</button>'
            + '<button type="button" class="btn btn-outline-info btn-sm" onclick="testKumaInstance(' + inst.id + ')">Test</button>'
            + '<button type="button" class="btn btn-outline-danger btn-sm" onclick="deleteKumaInstance(' + inst.id + ',\'' + esc(inst.name).replace(/'/g, "\\'") + '\')">Delete</button>'
            + '</div>'
            + '</div>';
    });
    listEl.innerHTML = html;
}

export function showKumaInstanceForm(editId: number | null): void {
    var form = document.getElementById('kuma-instance-form')!;
    var title = document.getElementById('kuma-form-title')!;

    if (editId !== null && editId !== undefined) {
        var inst = kumaInstancesCache.find(function(i) { return i.id === editId; });
        if (!inst) { toast('Instance not found', 'error'); return; }
        title.textContent = 'Edit Instance';
        (document.getElementById('ki-edit-id') as HTMLInputElement).value = '' + editId;
        (document.getElementById('ki-name') as HTMLInputElement).value = inst.name || '';
        (document.getElementById('ki-url') as HTMLInputElement).value = inst.url || '';
        (document.getElementById('ki-user') as HTMLInputElement).value = inst.username || '';
        (document.getElementById('ki-pass') as HTMLInputElement).value = '';
        (document.getElementById('ki-pass') as HTMLInputElement).placeholder = 'Leave blank to keep current';
        (document.getElementById('ki-enabled') as HTMLInputElement).checked = !!inst.enabled;
    } else {
        title.textContent = 'Add Instance';
        (document.getElementById('ki-edit-id') as HTMLInputElement).value = '';
        (document.getElementById('ki-name') as HTMLInputElement).value = '';
        (document.getElementById('ki-url') as HTMLInputElement).value = '';
        (document.getElementById('ki-user') as HTMLInputElement).value = '';
        (document.getElementById('ki-pass') as HTMLInputElement).value = '';
        (document.getElementById('ki-pass') as HTMLInputElement).placeholder = 'Password';
        (document.getElementById('ki-enabled') as HTMLInputElement).checked = true;
    }
    form.classList.remove('d-none');
}

export function hideKumaInstanceForm(): void {
    var form = document.getElementById('kuma-instance-form')!;
    form.classList.add('d-none');
    (document.getElementById('ki-edit-id') as HTMLInputElement).value = '';
}

export function saveKumaInstance(): void {
    var editId = (document.getElementById('ki-edit-id') as HTMLInputElement).value;
    var name = (document.getElementById('ki-name') as HTMLInputElement).value.trim();
    var url = (document.getElementById('ki-url') as HTMLInputElement).value.trim();
    var user = (document.getElementById('ki-user') as HTMLInputElement).value.trim();
    var pass = (document.getElementById('ki-pass') as HTMLInputElement).value;
    var enabled = (document.getElementById('ki-enabled') as HTMLInputElement).checked;

    if (!name) { toast('Name is required', 'error'); return; }
    if (!url) { toast('URL is required', 'error'); return; }

    var payload = { name: name, url: url, username: user, password: pass, enabled: enabled };
    var btn = document.getElementById('btn-kuma-save') as HTMLButtonElement;
    btn.disabled = true;
    var orig = btn.innerHTML;
    btn.innerHTML = '<span class="spinner-sm"></span> Saving...';

    var method = editId ? 'PUT' : 'POST';
    var endpoint = editId ? '/api/kuma-instances/' + editId : '/api/kuma-instances';

    apiFetch(endpoint, { method: method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
        .then(function(r) { if (!r.ok) throw new Error('Save failed'); return r.json(); })
        .then(function() {
            toast(editId ? 'Instance updated' : 'Instance added', 'success');
            hideKumaInstanceForm();
            loadKumaInstances();
        })
        .catch(function(err: Error) {
            if (err.message === 'not authenticated') return;
            toast('Failed to save instance', 'error');
        })
        .finally(function() { btn.disabled = false; btn.innerHTML = orig; });
}

export function deleteKumaInstance(id: number, name: string): void {
    if (!confirm('Delete instance "' + name + '"? This will also remove all monitors synced to this instance from the database.')) return;
    apiFetch('/api/kuma-instances/' + id, { method: 'DELETE' })
        .then(function(r) { if (!r.ok) throw new Error('Delete failed'); return r.json(); })
        .then(function() { toast('Instance deleted', 'success'); loadKumaInstances(); })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to delete instance', 'error'); });
}

export function testKumaInstance(id: number): void {
    toast('Testing connection...', 'info');
    apiFetch('/api/kuma-instances/' + id + '/test', { method: 'POST' })
        .then(function(r) { return r.json() as Promise<{ok: boolean; message?: string}>; })
        .then(function(d) {
            if (d.ok) toast('Connection OK: ' + (d.message || 'success'), 'success');
            else toast('Connection failed: ' + (d.message || 'unknown error'), 'error');
        })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Connection test failed', 'error'); });
}

// ─── NPM Instance CRUD ────────────────────────────────────────

var npmInstancesCache: NPMInstanceJSON[] = [];

export function loadNPMInstances(): void {
    var listEl = document.getElementById('npm-instances-list');
    if (!listEl) return;
    listEl.innerHTML = '<div class="text-center text-muted py-3"><span class="spinner-sm"></span> Loading...</div>';
    apiFetch('/api/npm-instances').then(function(r){return r.json() as Promise<NPMInstanceJSON[]>;}).then(function(instances) {
        npmInstancesCache = instances || [];
        renderNPMInstances(npmInstancesCache);
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        listEl!.innerHTML = '<div class="text-center text-danger py-3">Failed to load instances</div>';
    });
}

function renderNPMInstances(instances: NPMInstanceJSON[]): void {
    var listEl = document.getElementById('npm-instances-list')!;
    if (!instances.length) {
        listEl.innerHTML = '<div class="text-center text-muted py-3">No NPM instances configured. Click "Add Instance" to create one.</div>';
        return;
    }
    var html = '';
    instances.forEach(function(inst) {
        var enabledBadge = inst.enabled
            ? '<span class="badge bg-success">Enabled</span>'
            : '<span class="badge bg-secondary">Disabled</span>';
        html += '<div class="card card-body bg-light p-2 mb-2 d-flex flex-row align-items-center justify-content-between">'
            + '<div class="flex-grow-1">'
            + '<div class="fw-semibold">' + esc(inst.name) + ' ' + enabledBadge + '</div>'
            + '<div class="small text-muted">' + esc(inst.url) + ' &middot; ' + esc(inst.username) + '</div>'
            + '</div>'
            + '<div class="d-flex gap-1">'
            + '<button type="button" class="btn btn-outline-secondary btn-sm" onclick="editNPMInstance(' + inst.id + ')">Edit</button>'
            + '<button type="button" class="btn btn-outline-info btn-sm" onclick="testNPMInstance(' + inst.id + ')">Test</button>'
            + '<button type="button" class="btn btn-outline-danger btn-sm" onclick="deleteNPMInstance(' + inst.id + ',\'' + esc(inst.name).replace(/'/g, "\\'") + '\')">Delete</button>'
            + '</div>'
            + '</div>';
    });
    listEl.innerHTML = html;
}

export function showNPMInstanceForm(editId: number | null): void {
    var form = document.getElementById('npm-instance-form')!;
    var title = document.getElementById('npm-form-title')!;

    if (editId !== null && editId !== undefined) {
        var inst = npmInstancesCache.find(function(i) { return i.id === editId; });
        if (!inst) { toast('Instance not found', 'error'); return; }
        title.textContent = 'Edit Instance';
        (document.getElementById('ni-edit-id') as HTMLInputElement).value = '' + editId;
        (document.getElementById('ni-name') as HTMLInputElement).value = inst.name || '';
        (document.getElementById('ni-url') as HTMLInputElement).value = inst.url || '';
        (document.getElementById('ni-user') as HTMLInputElement).value = inst.username || '';
        (document.getElementById('ni-pass') as HTMLInputElement).value = '';
        (document.getElementById('ni-pass') as HTMLInputElement).placeholder = 'Leave blank to keep current';
        (document.getElementById('ni-enabled') as HTMLInputElement).checked = !!inst.enabled;
    } else {
        title.textContent = 'Add Instance';
        (document.getElementById('ni-edit-id') as HTMLInputElement).value = '';
        (document.getElementById('ni-name') as HTMLInputElement).value = '';
        (document.getElementById('ni-url') as HTMLInputElement).value = '';
        (document.getElementById('ni-user') as HTMLInputElement).value = '';
        (document.getElementById('ni-pass') as HTMLInputElement).value = '';
        (document.getElementById('ni-pass') as HTMLInputElement).placeholder = 'Password';
        (document.getElementById('ni-enabled') as HTMLInputElement).checked = true;
    }
    form.classList.remove('d-none');
}

export function hideNPMInstanceForm(): void {
    var form = document.getElementById('npm-instance-form')!;
    form.classList.add('d-none');
    (document.getElementById('ni-edit-id') as HTMLInputElement).value = '';
}

export function saveNPMInstance(): void {
    var editId = (document.getElementById('ni-edit-id') as HTMLInputElement).value;
    var name = (document.getElementById('ni-name') as HTMLInputElement).value.trim();
    var url = (document.getElementById('ni-url') as HTMLInputElement).value.trim();
    var user = (document.getElementById('ni-user') as HTMLInputElement).value.trim();
    var pass = (document.getElementById('ni-pass') as HTMLInputElement).value;
    var enabled = (document.getElementById('ni-enabled') as HTMLInputElement).checked;

    if (!name) { toast('Name is required', 'error'); return; }
    if (!url) { toast('URL is required', 'error'); return; }

    var payload = { name: name, url: url, username: user, password: pass, enabled: enabled };
    var btn = document.getElementById('btn-npm-save') as HTMLButtonElement;
    btn.disabled = true;
    var orig = btn.innerHTML;
    btn.innerHTML = '<span class="spinner-sm"></span> Saving...';

    var method = editId ? 'PUT' : 'POST';
    var endpoint = editId ? '/api/npm-instances/' + editId : '/api/npm-instances';

    apiFetch(endpoint, { method: method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
        .then(function(r) { if (!r.ok) throw new Error('Save failed'); return r.json(); })
        .then(function() {
            toast(editId ? 'Instance updated' : 'Instance added', 'success');
            hideNPMInstanceForm();
            loadNPMInstances();
        })
        .catch(function(err: Error) {
            if (err.message === 'not authenticated') return;
            toast('Failed to save instance', 'error');
        })
        .finally(function() { btn.disabled = false; btn.innerHTML = orig; });
}

export function deleteNPMInstance(id: number, name: string): void {
    if (!confirm('Delete NPM instance "' + name + '"? This will remove all proxy hosts synced from this instance.')) return;
    apiFetch('/api/npm-instances/' + id, { method: 'DELETE' })
        .then(function(r) { if (!r.ok) throw new Error('Delete failed'); return r.json(); })
        .then(function() { toast('Instance deleted', 'success'); loadNPMInstances(); })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to delete instance', 'error'); });
}

export function testNPMInstance(id: number): void {
    toast('Testing connection...', 'info');
    apiFetch('/api/npm-instances/' + id + '/test', { method: 'POST' })
        .then(function(r) { return r.json() as Promise<{ok: boolean; message?: string}>; })
        .then(function(d) {
            if (d.ok) toast('Connection OK: ' + (d.message || 'success'), 'success');
            else toast('Connection failed: ' + (d.message || 'unknown error'), 'error');
        })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Connection test failed', 'error'); });
}

window.loadSettings = loadSettings;
window.saveSettings = saveSettings;
window.testConnection = testConnection;
window.loadKumaInstances = loadKumaInstances;
window.showKumaInstanceForm = showKumaInstanceForm;
window.hideKumaInstanceForm = hideKumaInstanceForm;
window.saveKumaInstance = saveKumaInstance;
window.deleteKumaInstance = deleteKumaInstance;
window.testKumaInstance = testKumaInstance;
window.editKumaInstance = function(id: number) { showKumaInstanceForm(id); };
window.loadNPMInstances = loadNPMInstances;
window.showNPMInstanceForm = showNPMInstanceForm;
window.hideNPMInstanceForm = hideNPMInstanceForm;
window.saveNPMInstance = saveNPMInstance;
window.deleteNPMInstance = deleteNPMInstance;
window.testNPMInstance = testNPMInstance;
window.editNPMInstance = function(id: number) { showNPMInstanceForm(id); };
