// Settings tab logic
import type { KumaInstanceJSON, NPMInstanceJSON, AutheliaInstanceJSON, SettingsResponse, APIToken } from './types';

export function loadSettings(): void {
    apiFetch('/api/settings').then(function(r){return r.json() as Promise<SettingsResponse>;}).then(function(s) {
        (document.getElementById('s-compose-path') as HTMLInputElement).value = (s.compose_path as string) || '';
        (document.getElementById('s-auth-config-path') as HTMLInputElement).value = (s.authelia_config_path as string) || '';
        (document.getElementById('s-auth-db-path') as HTMLInputElement).value = (s.authelia_db_path as string) || '';
        (document.getElementById('s-auth-sync-enabled') as HTMLInputElement).checked = !!(s.authelia_sync_enabled);
        (document.getElementById('s-auth-default-policy') as HTMLSelectElement).value = (s.authelia_default_policy as string) || 'one_factor';
        (document.getElementById('s-auth-overrides') as HTMLTextAreaElement).value = (s.authelia_sync_overrides as string) || '';
        var eink = document.getElementById('s-eink-enabled') as HTMLInputElement | null;
        if (eink) eink.checked = !!(s.eink_enabled);
        renderTrmnlUrls();
        var notifyEnabled = document.getElementById('s-notify-enabled') as HTMLInputElement | null;
        if (notifyEnabled) notifyEnabled.checked = !!(s.notify_enabled);
        var notifyInterval = document.getElementById('s-notify-interval') as HTMLInputElement | null;
        if (notifyInterval) notifyInterval.value = String(s.notify_interval_minutes || 60);
        var gotifyUrl = document.getElementById('s-gotify-url') as HTMLInputElement | null;
        if (gotifyUrl) gotifyUrl.value = (s.gotify_url as string) || '';
        var gotifyToken = document.getElementById('s-gotify-token') as HTMLInputElement | null;
        if (gotifyToken) gotifyToken.value = (s.gotify_token as string) || '';
        var gotifyPriority = document.getElementById('s-gotify-priority') as HTMLInputElement | null;
        if (gotifyPriority) gotifyPriority.value = String(s.gotify_priority ?? 5);
        renderNotifyChannels(s.notify_channels || '');
        // Docker events & reconcile
        var dockerSocket = document.getElementById('s-docker-socket') as HTMLInputElement | null;
        if (dockerSocket) dockerSocket.value = (s.docker_socket as string) || '';
        var dockerEvents = document.getElementById('s-docker-events-enabled') as HTMLInputElement | null;
        if (dockerEvents) dockerEvents.checked = !!(s.docker_events_enabled);
        var dockerRetention = document.getElementById('s-docker-retention-days') as HTMLInputElement | null;
        if (dockerRetention) dockerRetention.value = String(s.docker_events_retention_days || 30);
        var reconcileEnabled = document.getElementById('s-reconcile-enabled') as HTMLInputElement | null;
        if (reconcileEnabled) reconcileEnabled.checked = !!(s.reconcile_enabled);
        var reconcileInterval = document.getElementById('s-reconcile-interval') as HTMLInputElement | null;
        if (reconcileInterval) reconcileInterval.value = String(s.reconcile_interval_minutes || 60);
        var reconcileDryRun = document.getElementById('s-reconcile-dry-run') as HTMLInputElement | null;
        if (reconcileDryRun) reconcileDryRun.checked = s.reconcile_dry_run_default !== false;
        var notifyDie = document.getElementById('s-notify-docker-die') as HTMLInputElement | null;
        if (notifyDie) notifyDie.checked = !!(s.notify_docker_die);
        var notifyHealth = document.getElementById('s-notify-docker-health') as HTMLInputElement | null;
        if (notifyHealth) notifyHealth.checked = !!(s.notify_docker_health);
        var notifyImage = document.getElementById('s-notify-docker-image') as HTMLInputElement | null;
        if (notifyImage) notifyImage.checked = !!(s.notify_docker_image);
        var notifyReconcile = document.getElementById('s-notify-reconcile') as HTMLInputElement | null;
        if (notifyReconcile) notifyReconcile.checked = !!(s.notify_reconcile);
        var kumaDefaultTags = document.getElementById('s-kuma-default-tags') as HTMLInputElement | null;
        if (kumaDefaultTags) kumaDefaultTags.value = (s.kuma_default_tags as string) || '';
        var notifyCooldown = document.getElementById('s-notify-cooldown') as HTMLInputElement | null;
        if (notifyCooldown) notifyCooldown.value = String(s.notify_cooldown_minutes || 5);
        var notifyPersistent = document.getElementById('s-notify-persistent') as HTMLInputElement | null;
        if (notifyPersistent) notifyPersistent.checked = !!(s.notify_persistent);
        loadNotifyMissing();
    });
}

function renderTrmnlUrls(): void {
    var section = document.getElementById('trmnl-url-section');
    var list = document.getElementById('trmnl-url-list');
    if (!section || !list) return;
    section.classList.remove('d-none');
    var base = window.location.origin + '/api/v1/trmnl/stats';
    var layouts = ['full', 'half_horizontal', 'half_vertical', 'quadrant'];
    var html = '';
    layouts.forEach(function(layout) {
        var url = base + '?layout=' + layout;
        html += '<div class="input-group input-group-sm mb-1">'
            + '<span class="input-group-text" style="min-width:120px">' + layout + '</span>'
            + '<input class="form-control" type="text" readonly value="' + url + '">'
            + '<button type="button" class="btn btn-outline-secondary" onclick="copyTrmnlUrl(this)">Copy</button>'
            + '</div>';
    });
    list.innerHTML = html;
}

// ─── API Token management ───────────────────────────────────────

export function loadTokens(): void {
    var listEl = document.getElementById('token-list');
    if (!listEl) return;
    apiFetch('/api/tokens').then(function(r){return r.json() as Promise<APIToken[]>;}).then(function(tokens) {
        if (!tokens || !tokens.length) {
            listEl.innerHTML = '<span class="text-muted">No API tokens yet. Create one to use with scripts or the CLI.</span>';
            return;
        }
        var html = '';
        tokens.forEach(function(tok) {
            var revoked = tok.revoked_at ? '<span class="badge bg-danger ms-1">Revoked</span>' : '';
            var expires = tok.expires_at ? new Date(tok.expires_at).toLocaleString() : 'never';
            var actions = tok.revoked_at
                ? ''
                : '<button type="button" class="btn btn-outline-info btn-sm" onclick="rotateToken(' + tok.id + ')">Rotate</button>'
                + '<button type="button" class="btn btn-outline-danger btn-sm" onclick="revokeToken(' + tok.id + ')">Revoke</button>';
            html += '<div class="d-flex flex-row align-items-center justify-content-between mb-1">'
                + '<div class="flex-grow-1">'
                + '<span class="fw-semibold">' + esc(tok.name) + '</span>' + revoked
                + '<div class="small text-muted">Created ' + new Date(tok.created_at).toLocaleString() + ' &middot; Expires ' + expires + '</div>'
                + '</div>'
                + '<div class="d-flex gap-1">' + actions + '</div>'
                + '</div>';
        });
        listEl.innerHTML = html;
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        listEl.innerHTML = '<span class="text-danger">Failed to load tokens</span>';
    });
}

export function createToken(): void {
    var name = ((document.getElementById('s-token-name') as HTMLInputElement)?.value || '').trim();
    if (!name) { toast('Token name is required', 'error'); return; }
    apiFetch('/api/tokens', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: name }) })
        .then(function(r) { if (!r.ok) throw new Error('Create failed'); return r.json() as Promise<{token: string}>; })
        .then(function(d) {
            alert('Store this token now — it will not be shown again:\n\n' + d.token);
            (document.getElementById('s-token-name') as HTMLInputElement).value = '';
            loadTokens();
        })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to create token', 'error'); });
}

export function revokeToken(id: number): void {
    if (!confirm('Revoke this API token? It will stop working immediately.')) return;
    apiFetch('/api/tokens/' + id + '/revoke', { method: 'POST' })
        .then(function(r) { if (!r.ok) throw new Error('Revoke failed'); return r.json(); })
        .then(function() { toast('Token revoked', 'success'); loadTokens(); })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to revoke token', 'error'); });
}

export function rotateToken(id: number): void {
    if (!confirm('Rotate this API token? The current secret will stop working immediately.')) return;
    apiFetch('/api/tokens/' + id + '/rotate', { method: 'POST' })
        .then(function(r) { if (!r.ok) throw new Error('Rotate failed'); return r.json() as Promise<{token: string}>; })
        .then(function(d) {
            alert('New token — store it now, it will not be shown again:\n\n' + d.token);
            loadTokens();
        })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to rotate token', 'error'); });
}

export function copyTrmnlUrl(btn: HTMLElement): void {
    var input = btn.previousElementSibling as HTMLInputElement;
    if (!input) return;
    navigator.clipboard.writeText(input.value).then(function() {
        toast('Polling URL copied', 'success');
    }).catch(function() {
        input.select();
        document.execCommand('copy');
        toast('Polling URL copied', 'success');
    });
}

// ─── Notification Channels (multi-channel fan-out) ─────────────

interface NotifyChannel {
    type: string;
    enabled: boolean;
    url: string;
    token?: string;
    priority?: number;
}

var notifyChannelsCache: NotifyChannel[] = [];

const NOTIFY_CHANNEL_TYPES = ['ntfy', 'telegram', 'discord', 'webhook', 'gotify'];

function renderNotifyChannels(doc: string): void {
    notifyChannelsCache = [];
    try {
        var parsed = doc ? JSON.parse(doc) : [];
        if (Array.isArray(parsed)) notifyChannelsCache = parsed as NotifyChannel[];
    } catch { /* invalid doc — start empty */ }
    drawNotifyChannels();
}

function drawNotifyChannels(): void {
    var list = document.getElementById('notify-channels-list');
    if (!list) return;
    if (!notifyChannelsCache.length) {
        list.innerHTML = '<div class="small text-muted">No extra channels configured. The Gotify settings below act as the single notification channel.</div>';
        return;
    }
    var html = '';
    notifyChannelsCache.forEach(function(ch, i) {
        var typeOpts = NOTIFY_CHANNEL_TYPES.map(function(t) {
            return '<option value="' + t + '"' + (ch.type === t ? ' selected' : '') + '>' + t + '</option>';
        }).join('');
        html += '<div class="border rounded p-2 mb-2 bg-white" data-nc-index="' + i + '">'
            + '<div class="d-flex align-items-center gap-2 mb-1">'
            + '<select class="form-select form-select-sm" style="max-width:140px" id="nc-type-' + i + '">' + typeOpts + '</select>'
            + '<div class="form-check form-check-inline mb-0">'
            + '<input class="form-check-input" type="checkbox" id="nc-enabled-' + i + '"' + (ch.enabled !== false ? ' checked' : '') + '>'
            + '<label class="form-check-label small" for="nc-enabled-' + i + '">Enabled</label>'
            + '</div>'
            + '<button type="button" class="btn btn-outline-danger btn-sm ms-auto" onclick="removeNotifyChannel(' + i + ')">Remove</button>'
            + '</div>'
            + '<input class="form-control form-control-sm mb-1" id="nc-url-' + i + '" placeholder="' + channelUrlPlaceholder(ch.type) + '" value="' + esc(ch.url || '') + '" autocomplete="off">'
            + '<div class="d-flex gap-2">'
            + '<input class="form-control form-control-sm" type="password" style="max-width:240px" id="nc-token-' + i + '" placeholder="Token (blank = keep current)" value="' + esc(ch.token || '') + '" autocomplete="new-password">'
            + '<input class="form-control form-control-sm" type="number" style="max-width:120px" id="nc-priority-' + i + '" placeholder="Priority" min="0" max="10" value="' + (ch.priority ?? '') + '">'
            + '</div>'
            + '</div>';
    });
    list.innerHTML = html;
}

function channelUrlPlaceholder(type: string): string {
    switch (type) {
        case 'ntfy': return 'https://ntfy.example.com/your-topic';
        case 'telegram': return 'https://api.telegram.org/bot<TOKEN>/<CHAT_ID>';
        case 'discord': return 'https://discord.com/api/webhooks/...';
        case 'webhook': return 'https://hooks.example.com/synapse';
        case 'gotify': return 'http://gotify:8080';
        default: return 'URL';
    }
}

export function addNotifyChannel(): void {
    notifyChannelsCache.push({ type: 'ntfy', enabled: true, url: '', token: '', priority: undefined });
    drawNotifyChannels();
}

export function removeNotifyChannel(i: number): void {
    notifyChannelsCache.splice(i, 1);
    drawNotifyChannels();
}

// collectNotifyChannels reads the editor DOM back into the cache and returns
// the JSON document for the notify_channels setting. Masked tokens ("****")
// are sent back verbatim — the backend keeps the stored value.
function collectNotifyChannels(): string {
    notifyChannelsCache.forEach(function(ch, i) {
        var typeEl = document.getElementById('nc-type-' + i) as HTMLSelectElement | null;
        var enabledEl = document.getElementById('nc-enabled-' + i) as HTMLInputElement | null;
        var urlEl = document.getElementById('nc-url-' + i) as HTMLInputElement | null;
        var tokenEl = document.getElementById('nc-token-' + i) as HTMLInputElement | null;
        var prioEl = document.getElementById('nc-priority-' + i) as HTMLInputElement | null;
        if (typeEl) ch.type = typeEl.value;
        if (enabledEl) ch.enabled = enabledEl.checked;
        if (urlEl) ch.url = urlEl.value.trim();
        if (tokenEl) ch.token = tokenEl.value;
        if (prioEl) ch.priority = prioEl.value === '' ? undefined : parseInt(prioEl.value, 10);
    });
    if (!notifyChannelsCache.length) return '';
    return JSON.stringify(notifyChannelsCache);
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
        authelia_sync_overrides: (document.getElementById('s-auth-overrides') as HTMLTextAreaElement)?.value || '',
        eink_enabled: (document.getElementById('s-eink-enabled') as HTMLInputElement)?.checked || false,
        notify_enabled: (document.getElementById('s-notify-enabled') as HTMLInputElement)?.checked || false,
        notify_interval_minutes: parseInt((document.getElementById('s-notify-interval') as HTMLInputElement)?.value || '60', 10) || 60,
        gotify_url: (document.getElementById('s-gotify-url') as HTMLInputElement)?.value || '',
        gotify_priority: parseInt((document.getElementById('s-gotify-priority') as HTMLInputElement)?.value || '5', 10) || 5,
        docker_socket: (document.getElementById('s-docker-socket') as HTMLInputElement)?.value || '',
        docker_events_enabled: (document.getElementById('s-docker-events-enabled') as HTMLInputElement)?.checked || false,
        docker_events_retention_days: parseInt((document.getElementById('s-docker-retention-days') as HTMLInputElement)?.value || '30', 10) || 30,
        reconcile_enabled: (document.getElementById('s-reconcile-enabled') as HTMLInputElement)?.checked || false,
        reconcile_interval_minutes: parseInt((document.getElementById('s-reconcile-interval') as HTMLInputElement)?.value || '60', 10) || 60,
        reconcile_dry_run_default: (document.getElementById('s-reconcile-dry-run') as HTMLInputElement)?.checked || false,
        kuma_default_tags: (document.getElementById('s-kuma-default-tags') as HTMLInputElement)?.value || '',
        notify_docker_die: (document.getElementById('s-notify-docker-die') as HTMLInputElement)?.checked || false,
        notify_docker_health: (document.getElementById('s-notify-docker-health') as HTMLInputElement)?.checked || false,
        notify_docker_image: (document.getElementById('s-notify-docker-image') as HTMLInputElement)?.checked || false,
        notify_reconcile: (document.getElementById('s-notify-reconcile') as HTMLInputElement)?.checked || false,
        notify_cooldown_minutes: parseInt((document.getElementById('s-notify-cooldown') as HTMLInputElement)?.value || '5', 10) || 5,
        notify_persistent: (document.getElementById('s-notify-persistent') as HTMLInputElement)?.checked || false
    };
    // Only send the Gotify token when it has a value ("Leave blank to keep current").
    var gotifyToken = (document.getElementById('s-gotify-token') as HTMLInputElement)?.value || '';
    if (gotifyToken) payload.gotify_token = gotifyToken;
    // Channels document: always sent so removals persist; empty string clears
    // the doc and re-activates the legacy Gotify fallback.
    payload.notify_channels = collectNotifyChannels();
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

// ─── Authelia Instance CRUD ───────────────────────────────────

var autheliaInstancesCache: AutheliaInstanceJSON[] = [];

export function loadAutheliaInstances(): void {
    var listEl = document.getElementById('authelia-instances-list');
    if (!listEl) return;
    listEl.innerHTML = '<div class="text-center text-muted py-3"><span class="spinner-sm"></span> Loading...</div>';
    apiFetch('/api/authelia-instances').then(function(r){return r.json() as Promise<AutheliaInstanceJSON[]>;}).then(function(instances) {
        autheliaInstancesCache = instances || [];
        renderAutheliaInstances(autheliaInstancesCache);
    }).catch(function(err: Error) {
        if (err.message === 'not authenticated') return;
        listEl!.innerHTML = '<div class="text-center text-danger py-3">Failed to load instances</div>';
    });
}

function renderAutheliaInstances(instances: AutheliaInstanceJSON[]): void {
    var listEl = document.getElementById('authelia-instances-list')!;
    if (!instances.length) {
        listEl.innerHTML = '<div class="text-center text-muted py-3">No Authelia instances configured. Click "Add Instance" to create one.</div>';
        return;
    }
    var html = '';
    instances.forEach(function(inst) {
        var enabledBadge = inst.enabled
            ? '<span class="badge bg-success">Enabled</span>'
            : '<span class="badge bg-secondary">Disabled</span>';
        var autoSyncBadge = inst.auto_sync
            ? '<span class="badge bg-info">Auto-sync</span>'
            : '';
        html += '<div class="card card-body bg-light p-2 mb-2 d-flex flex-row align-items-center justify-content-between">'
            + '<div class="flex-grow-1">'
            + '<div class="fw-semibold">' + esc(inst.name) + ' ' + enabledBadge + ' ' + autoSyncBadge + '</div>'
            + '<div class="small text-muted">' + esc(inst.config_path) + ' &middot; Policy: ' + esc(inst.default_policy) + '</div>'
            + '</div>'
            + '<div class="d-flex gap-1">'
            + '<button type="button" class="btn btn-outline-secondary btn-sm" onclick="editAutheliaInstance(' + inst.id + ')">Edit</button>'
            + '<button type="button" class="btn btn-outline-info btn-sm" onclick="testAutheliaInstance(' + inst.id + ')">Test</button>'
            + '<button type="button" class="btn btn-outline-danger btn-sm" onclick="deleteAutheliaInstance(' + inst.id + ',\'' + esc(inst.name).replace(/'/g, "\\'") + '\')">Delete</button>'
            + '</div>'
            + '</div>';
    });
    listEl.innerHTML = html;
}

export function showAutheliaInstanceForm(editId: number | null): void {
    var form = document.getElementById('authelia-instance-form')!;
    var title = document.getElementById('authelia-form-title')!;

    if (editId !== null && editId !== undefined) {
        var inst = autheliaInstancesCache.find(function(i) { return i.id === editId; });
        if (!inst) { toast('Instance not found', 'error'); return; }
        title.textContent = 'Edit Instance';
        (document.getElementById('ai-edit-id') as HTMLInputElement).value = '' + editId;
        (document.getElementById('ai-name') as HTMLInputElement).value = inst.name || '';
        (document.getElementById('ai-config-path') as HTMLInputElement).value = inst.config_path || '';
        (document.getElementById('ai-db-path') as HTMLInputElement).value = inst.db_path || '';
        (document.getElementById('ai-default-policy') as HTMLSelectElement).value = inst.default_policy || 'one_factor';
        (document.getElementById('ai-npm-ids') as HTMLInputElement).value = inst.npm_instance_ids || '[]';
        (document.getElementById('ai-overrides') as HTMLInputElement).value = inst.overrides || '';
        (document.getElementById('ai-auto-sync') as HTMLInputElement).checked = !!inst.auto_sync;
        (document.getElementById('ai-enabled') as HTMLInputElement).checked = !!inst.enabled;
    } else {
        title.textContent = 'Add Instance';
        (document.getElementById('ai-edit-id') as HTMLInputElement).value = '';
        (document.getElementById('ai-name') as HTMLInputElement).value = '';
        (document.getElementById('ai-config-path') as HTMLInputElement).value = '';
        (document.getElementById('ai-db-path') as HTMLInputElement).value = '';
        (document.getElementById('ai-default-policy') as HTMLSelectElement).value = 'one_factor';
        (document.getElementById('ai-npm-ids') as HTMLInputElement).value = '[]';
        (document.getElementById('ai-overrides') as HTMLInputElement).value = '';
        (document.getElementById('ai-auto-sync') as HTMLInputElement).checked = true;
        (document.getElementById('ai-enabled') as HTMLInputElement).checked = true;
    }
    form.classList.remove('d-none');
}

export function hideAutheliaInstanceForm(): void {
    var form = document.getElementById('authelia-instance-form')!;
    form.classList.add('d-none');
    (document.getElementById('ai-edit-id') as HTMLInputElement).value = '';
}

export function saveAutheliaInstance(): void {
    var editId = (document.getElementById('ai-edit-id') as HTMLInputElement).value;
    var name = (document.getElementById('ai-name') as HTMLInputElement).value.trim();
    var configPath = (document.getElementById('ai-config-path') as HTMLInputElement).value.trim();
    var dbPath = (document.getElementById('ai-db-path') as HTMLInputElement).value.trim();
    var defaultPolicy = (document.getElementById('ai-default-policy') as HTMLSelectElement).value;
    var npmIds = (document.getElementById('ai-npm-ids') as HTMLInputElement).value.trim();
    var overrides = (document.getElementById('ai-overrides') as HTMLInputElement).value.trim();
    var autoSync = (document.getElementById('ai-auto-sync') as HTMLInputElement).checked;
    var enabled = (document.getElementById('ai-enabled') as HTMLInputElement).checked;

    if (!name) { toast('Name is required', 'error'); return; }
    if (!configPath) { toast('Config Path is required', 'error'); return; }
    if (!npmIds) { npmIds = '[]'; }

    var payload = {
        name: name, config_path: configPath, db_path: dbPath,
        default_policy: defaultPolicy, npm_instance_ids: npmIds,
        overrides: overrides, auto_sync: autoSync, enabled: enabled
    };
    var btn = document.getElementById('btn-authelia-save') as HTMLButtonElement;
    btn.disabled = true;
    var orig = btn.innerHTML;
    btn.innerHTML = '<span class="spinner-sm"></span> Saving...';

    var method = editId ? 'PUT' : 'POST';
    var endpoint = editId ? '/api/authelia-instances/' + editId : '/api/authelia-instances';

    apiFetch(endpoint, { method: method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
        .then(function(r) { if (!r.ok) throw new Error('Save failed'); return r.json(); })
        .then(function() {
            toast(editId ? 'Instance updated' : 'Instance added', 'success');
            hideAutheliaInstanceForm();
            loadAutheliaInstances();
        })
        .catch(function(err: Error) {
            if (err.message === 'not authenticated') return;
            toast('Failed to save instance', 'error');
        })
        .finally(function() { btn.disabled = false; btn.innerHTML = orig; });
}

export function deleteAutheliaInstance(id: number, name: string): void {
    if (!confirm('Delete Authelia instance "' + name + '"? This will remove all alerts and rules associated with this instance.')) return;
    apiFetch('/api/authelia-instances/' + id, { method: 'DELETE' })
        .then(function(r) { if (!r.ok) throw new Error('Delete failed'); return r.json(); })
        .then(function() { toast('Instance deleted', 'success'); loadAutheliaInstances(); })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Failed to delete instance', 'error'); });
}

export function testAutheliaInstance(id: number): void {
    toast('Testing connection...', 'info');
    apiFetch('/api/authelia-instances/' + id + '/test', { method: 'POST' })
        .then(function(r) { return r.json() as Promise<{ok: boolean; message?: string}>; })
        .then(function(d) {
            if (d.ok) toast('Connection OK: ' + (d.message || 'success'), 'success');
            else toast('Connection failed: ' + (d.message || 'unknown error'), 'error');
        })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Connection test failed', 'error'); });
}

// ─── Notifications (Gotify) ────────────────────────────────────

export function notifyTest(): void {
    var btn = document.getElementById('btn-notify-test') as HTMLButtonElement;
    btn.disabled = true;
    apiFetch('/api/notify/test', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
        .then(function(r) { return r.json() as Promise<{ok: boolean; error?: string; results?: {channel: string; ok: boolean; error?: string}[]}>; })
        .then(function(d) {
            if (d.results && d.results.length) {
                d.results.forEach(function(r) {
                    if (r.ok) toast(r.channel + ': test sent', 'success');
                    else toast(r.channel + ': failed — ' + (r.error || 'unknown error'), 'error');
                });
            } else if (d.ok) {
                toast('Test notification sent', 'success');
            } else {
                toast('Test failed: ' + (d.error || 'unknown error'), 'error');
            }
        })
        .catch(function(err: Error) { if (err.message === 'not authenticated') return; toast('Test notification failed', 'error'); })
        .finally(function() { btn.disabled = false; });
}

export function loadNotifyMissing(): void {
    var listEl = document.getElementById('notify-missing-list');
    if (!listEl) return;
    apiFetch('/api/notify/missing')
        .then(function(r) { return r.json() as Promise<{docker: string[]; npm: string[]; degraded: boolean; reasons?: string[]}>; })
        .then(function(d) {
            var parts: string[] = [];
            if (d.degraded) {
                parts.push('<span class="text-warning">Degraded check — notifications skipped:</span> ' + esc((d.reasons || ['unknown']).join('; ')));
            } else {
                if (!d.docker.length && !d.npm.length) {
                    parts.push('Nothing missing — all services and proxy hosts are covered by Uptime Kuma.');
                }
                if (d.docker.length) parts.push('<span class="fw-semibold">Docker services:</span><ul class="mb-0">' + d.docker.map(function(n) { return '<li>' + esc(n) + '</li>'; }).join('') + '</ul>');
                if (d.npm.length) parts.push('<span class="fw-semibold">NPM proxy hosts:</span><ul class="mb-0">' + d.npm.map(function(n) { return '<li>' + esc(n) + '</li>'; }).join('') + '</ul>');
            }
            listEl.innerHTML = parts.join('<br>');
        })
        .catch(function(err: Error) {
            if (err.message === 'not authenticated') return;
            listEl.innerHTML = '<span class="text-danger">Failed to load missing items</span>';
        });
}

window.loadSettings = loadSettings;
window.saveSettings = saveSettings;
window.notifyTest = notifyTest;
window.addNotifyChannel = addNotifyChannel;
window.removeNotifyChannel = removeNotifyChannel;
window.loadNotifyMissing = loadNotifyMissing;
window.copyTrmnlUrl = copyTrmnlUrl;
window.loadTokens = loadTokens;
window.createToken = createToken;
window.revokeToken = revokeToken;
window.rotateToken = rotateToken;
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
window.loadAutheliaInstances = loadAutheliaInstances;
window.showAutheliaInstanceForm = showAutheliaInstanceForm;
window.hideAutheliaInstanceForm = hideAutheliaInstanceForm;
window.saveAutheliaInstance = saveAutheliaInstance;
window.deleteAutheliaInstance = deleteAutheliaInstance;
window.testAutheliaInstance = testAutheliaInstance;
window.editAutheliaInstance = function(id: number) { showAutheliaInstanceForm(id); };
