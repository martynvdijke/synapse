// Settings tab logic

export function loadSettings() {
    apiFetch('/api/settings').then(function(r){return r.json();}).then(function(s) {
        document.getElementById('s-kuma-url').value = s.kuma_url || '';
        document.getElementById('s-kuma-user').value = s.kuma_user || '';
        document.getElementById('s-kuma-pass').value = '';
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
        kuma_url:  document.getElementById('s-kuma-url').value,
        kuma_user: document.getElementById('s-kuma-user').value,
        kuma_pass: document.getElementById('s-kuma-pass').value,
        npm_host:  document.getElementById('s-npm-host').value,
        npm_user:  document.getElementById('s-npm-user').value,
        npm_pass:  document.getElementById('s-npm-pass').value,
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
    var btn = service === 'kuma' ? document.getElementById('btn-test-kuma') : document.getElementById('btn-test-npm');
    var resultEl = document.getElementById('test-' + service + '-result');
    resultEl.innerHTML = '<span class="spinner-sm"></span> Testing...';
    btn.disabled = true;

    var payload = {
        kuma_url:     document.getElementById('s-kuma-url').value,
        kuma_user:    document.getElementById('s-kuma-user').value,
        kuma_pass:    document.getElementById('s-kuma-pass').value || '',
        npm_host:     document.getElementById('s-npm-host').value,
        npm_user:     document.getElementById('s-npm-user').value,
        npm_pass:     document.getElementById('s-npm-pass').value || '',
        compose_path: document.getElementById('s-compose-path').value || ''
    };

    apiFetch('/api/test/' + service, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
        .then(function(r) { return r.json(); })
        .then(function(d) {
            resultEl.innerHTML = d.ok
                ? '<span class="text-success fw-semibold">\u2713 OK</span>'
                : '<span class="text-danger">\u2717 ' + esc(d.message) + '</span>';
            if (d.ok) toast(service === 'kuma' ? 'Uptime Kuma connection OK' : 'NPM connection OK', 'success');
            else toast(d.message, 'error');
        })
        .catch(function(err) { if (err.message === 'not authenticated') return; resultEl.innerHTML = '<span class="text-danger">' + esc('' + err) + '</span>'; toast('Connection test failed', 'error'); })
        .finally(function() { btn.disabled = false; });
}

window.loadSettings = loadSettings;
window.saveSettings = saveSettings;
window.testConnection = testConnection;
