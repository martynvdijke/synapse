// Settings tab logic

export function loadSettings() {
    apiFetch('/api/settings').then(function(r){return r.json();}).then(function(s) {
        document.getElementById('s-npm-host').value = s.npm_host || '';
        document.getElementById('s-npm-user').value = s.npm_user || '';
        document.getElementById('s-npm-pass').value = '';
        document.getElementById('s-compose-path').value = s.compose_path || '';
        document.getElementById('s-auth-config-path').value = s.authelia_config_path || '';
        document.getElementById('s-auth-db-path').value = s.authelia_db_path || '';
        document.getElementById('s-auth-sync-enabled').checked = !!s.authelia_sync_enabled;
        document.getElementById('s-auth-default-policy').value = s.authelia_default_policy || 'one_factor';
        document.getElementById('s-auth-overrides').value = s.authelia_sync_overrides || '';
    });
}

export function saveSettings(e) {
    e.preventDefault();
    var btn = document.querySelector('#settings-form button[type="submit"]');
    btn.disabled = true;
    var orig = btn.innerHTML;
    btn.innerHTML = '<span class="spinner-sm"></span> Saving...';

    var payload = {
        npm_host:  document.getElementById('s-npm-host').value,
        npm_user: document.getElementById('s-npm-user').value,
        npm_pass: document.getElementById('s-npm-pass').value,
        compose_path: document.getElementById('s-compose-path')?.value || '',
        authelia_config_path: document.getElementById('s-auth-config-path')?.value || '',
        authelia_db_path: document.getElementById('s-auth-db-path')?.value || '',
        authelia_sync_enabled: document.getElementById('s-auth-sync-enabled')?.checked || false,
        authelia_default_policy: document.getElementById('s-auth-default-policy')?.value || 'one_factor',
        authelia_sync_overrides: document.getElementById('s-auth-overrides')?.value || ''
    };
    apiFetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
        .then(function(r) { if (!r.ok) throw new Error('Save failed'); return r.json(); })
        .then(function() { toast('Settings saved', 'success'); })
        .catch(function(err) { if (err.message === 'not authenticated') return; toast('Failed to save settings', 'error'); })
        .finally(function() { btn.disabled = false; btn.innerHTML = orig; });
}

export function testConnection(service) {
    if (service === 'kuma') return; // legacy, no longer used
    var btn = document.getElementById('btn-test-npm');
    var resultEl = document.getElementById('test-npm-result');
    resultEl.innerHTML = '<span class="spinner-sm"></span> Testing...';
    btn.disabled = true;

    var payload = {
        npm_host:  document.getElementById('s-npm-host').value,
        npm_user: document.getElementById('s-npm-user').value,
        npm_pass: document.getElementById('s-npm-pass').value || ''
    };

    apiFetch('/api/test/' + service, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
        .then(function(r) { return r.json(); })
        .then(function(d) {
            resultEl.innerHTML = d.ok
                ? '<span class="text-success fw-semibold">\u2713 OK</span>'
                : '<span class="text-danger">\u2717 ' + esc(d.message) + '</span>';
            if (d.ok) toast('NPM connection OK', 'success');
            else toast(d.message, 'error');
        })
        .catch(function(err) { if (err.message === 'not authenticated') return; resultEl.innerHTML = '<span class="text-danger">' + esc('' + err) + '</span>'; toast('Connection test failed', 'error'); })
        .finally(function() { btn.disabled = false; });
}

// ─── Kuma Instance CRUD ───────────────────────────────────────

var kumaInstancesCache = [];

export function loadKumaInstances() {
    var listEl = document.getElementById('kuma-instances-list');
    if (!listEl) return;
    listEl.innerHTML = '<div class="text-center text-muted py-3"><span class="spinner-sm"></span> Loading...</div>';
    apiFetch('/api/kuma-instances').then(function(r){return r.json();}).then(function(instances) {
        kumaInstancesCache = instances || [];
        renderKumaInstances(kumaInstancesCache);
    }).catch(function(err) {
        if (err.message === 'not authenticated') return;
        listEl.innerHTML = '<div class="text-center text-danger py-3">Failed to load instances</div>';
    });
}

function renderKumaInstances(instances) {
    var listEl = document.getElementById('kuma-instances-list');
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

export function showKumaInstanceForm(editId) {
    var form = document.getElementById('kuma-instance-form');
    var title = document.getElementById('kuma-form-title');
    var editIdEl = document.getElementById('ki-edit-id');

    if (editId !== null && editId !== undefined) {
        var inst = kumaInstancesCache.find(function(i) { return i.id === editId; });
        if (!inst) { toast('Instance not found', 'error'); return; }
        title.textContent = 'Edit Instance';
        editIdEl.value = editId;
        document.getElementById('ki-name').value = inst.name || '';
        document.getElementById('ki-url').value = inst.url || '';
        document.getElementById('ki-user').value = inst.username || '';
        document.getElementById('ki-pass').value = '';
        document.getElementById('ki-pass').placeholder = 'Leave blank to keep current';
        document.getElementById('ki-enabled').checked = !!inst.enabled;
    } else {
        title.textContent = 'Add Instance';
        editIdEl.value = '';
        document.getElementById('ki-name').value = '';
        document.getElementById('ki-url').value = '';
        document.getElementById('ki-user').value = '';
        document.getElementById('ki-pass').value = '';
        document.getElementById('ki-pass').placeholder = 'Password';
        document.getElementById('ki-enabled').checked = true;
    }
    form.classList.remove('d-none');
}

export function hideKumaInstanceForm() {
    var form = document.getElementById('kuma-instance-form');
    form.classList.add('d-none');
    document.getElementById('ki-edit-id').value = '';
}

export function saveKumaInstance() {
    var editId = document.getElementById('ki-edit-id').value;
    var name = document.getElementById('ki-name').value.trim();
    var url = document.getElementById('ki-url').value.trim();
    var user = document.getElementById('ki-user').value.trim();
    var pass = document.getElementById('ki-pass').value;
    var enabled = document.getElementById('ki-enabled').checked;

    if (!name) { toast('Name is required', 'error'); return; }
    if (!url) { toast('URL is required', 'error'); return; }

    var payload = { name: name, url: url, username: user, password: pass, enabled: enabled };
    var btn = document.getElementById('btn-kuma-save');
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
        .catch(function(err) {
            if (err.message === 'not authenticated') return;
            toast('Failed to save instance', 'error');
        })
        .finally(function() { btn.disabled = false; btn.innerHTML = orig; });
}

export function deleteKumaInstance(id, name) {
    if (!confirm('Delete instance "' + name + '"? This will also remove all monitors synced to this instance from the database.')) return;
    apiFetch('/api/kuma-instances/' + id, { method: 'DELETE' })
        .then(function(r) { if (!r.ok) throw new Error('Delete failed'); return r.json(); })
        .then(function() { toast('Instance deleted', 'success'); loadKumaInstances(); })
        .catch(function(err) { if (err.message === 'not authenticated') return; toast('Failed to delete instance', 'error'); });
}

export function testKumaInstance(id) {
    toast('Testing connection...', 'info');
    apiFetch('/api/kuma-instances/' + id + '/test', { method: 'POST' })
        .then(function(r) { return r.json(); })
        .then(function(d) {
            if (d.ok) toast('Connection OK: ' + (d.message || 'success'), 'success');
            else toast('Connection failed: ' + (d.message || 'unknown error'), 'error');
        })
        .catch(function(err) { if (err.message === 'not authenticated') return; toast('Connection test failed', 'error'); });
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
window.editKumaInstance = function(id) { showKumaInstanceForm(id); };
