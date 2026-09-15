// Settings tab logic
import type { KumaInstanceJSON, NPMInstanceJSON, AutheliaInstanceJSON, SettingsResponse, APIToken } from './types';
import { esc, apiFetch, getJSON } from './api';
import { toast } from './toast';

export function loadSettings(): void {
    getJSON<SettingsResponse>('/api/settings').then(function(s) {
        (document.getElementById('s-compose-path') as HTMLInputElement).value = (s.compose_path as string) || '';
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
            + '<button type="button" class="btn btn-outline-secondary" data-action="copy-trmnl-url">Copy</button>'
            + '</div>';
    });
    list.innerHTML = html;
}

// ─── API Token management ───────────────────────────────────────

export function loadTokens(): void {
    var listEl = document.getElementById('token-list');
    if (!listEl) return;
    var el = listEl;
    apiFetch('/api/tokens').then(function(r){return r.json() as Promise<APIToken[]>;}).then(function(tokens) {
        if (!tokens || !tokens.length) {
            el.innerHTML = '<span class="text-muted">No API tokens yet. Create one to use with scripts or the CLI.</span>';
            return;
        }
        var html = '';
        tokens.forEach(function(tok) {
            var revoked = tok.revoked_at ? '<span class="badge bg-danger ms-1">Revoked</span>' : '';
            var expires = tok.expires_at ? new Date(tok.expires_at).toLocaleString() : 'never';
            var actions = tok.revoked_at
                ? ''
                : '<button type="button" class="btn btn-outline-info btn-sm" data-action="rotate-token" data-id="' + tok.id + '">Rotate</button>'
                + '<button type="button" class="btn btn-outline-danger btn-sm" data-action="revoke-token" data-id="' + tok.id + '">Revoke</button>';
            html += '<div class="d-flex flex-row align-items-center justify-content-between mb-1">'
                + '<div class="flex-grow-1">'
                + '<span class="fw-semibold">' + esc(tok.name) + '</span>' + revoked
                + '<div class="small text-muted">Created ' + new Date(tok.created_at).toLocaleString() + ' &middot; Expires ' + expires + '</div>'
                + '</div>'
                + '<div class="d-flex gap-1">' + actions + '</div>'
                + '</div>';
        });
        el.innerHTML = html;
    }).catch(function(err: unknown) {
        if (err instanceof Error && err.message === 'not authenticated') return;
        el.innerHTML = '<span class="text-danger">Failed to load tokens</span>';
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
        .catch(function(err: unknown) { if (err instanceof Error && err.message === 'not authenticated') return; toast('Failed to create token', 'error'); });
}

export function revokeToken(id: number): void {
    if (!confirm('Revoke this API token? It will stop working immediately.')) return;
    apiFetch('/api/tokens/' + id + '/revoke', { method: 'POST' })
        .then(function(r) { if (!r.ok) throw new Error('Revoke failed'); return r.json(); })
        .then(function() { toast('Token revoked', 'success'); loadTokens(); })
        .catch(function(err: unknown) { if (err instanceof Error && err.message === 'not authenticated') return; toast('Failed to revoke token', 'error'); });
}

export function rotateToken(id: number): void {
    if (!confirm('Rotate this API token? The current secret will stop working immediately.')) return;
    apiFetch('/api/tokens/' + id + '/rotate', { method: 'POST' })
        .then(function(r) { if (!r.ok) throw new Error('Rotate failed'); return r.json() as Promise<{token: string}>; })
        .then(function(d) {
            alert('New token — store it now, it will not be shown again:\n\n' + d.token);
            loadTokens();
        })
        .catch(function(err: unknown) { if (err instanceof Error && err.message === 'not authenticated') return; toast('Failed to rotate token', 'error'); });
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
            + '<button type="button" class="btn btn-outline-danger btn-sm ms-auto" data-action="remove-notify-channel" data-idx="' + i + '">Remove</button>'
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

export function channelUrlPlaceholder(type: string): string {
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
        .catch(function(err: unknown) { if (err instanceof Error && err.message === 'not authenticated') return; toast('Failed to save settings', 'error'); })
        .finally(function() { btn.disabled = false; btn.innerHTML = orig; });
}

// ─── Generic instance CRUD (Kuma / NPM / Authelia) ────────────
// All three instance families share one list/form/save/delete/test flow.
// The factory removes ~400 lines of copy-paste; DOM ids are unchanged.

type FieldKind = 'text' | 'pass' | 'check' | 'select';

interface InstanceField {
    id: string;
    prop: string;
    label?: string;      // used in "<label> is required"
    kind?: FieldKind;    // default 'text'
    required?: boolean;
    default?: string | boolean;
}

interface InstanceCrudConfig<T> {
    action: string;            // 'kuma' | 'npm' | 'authelia' -> data-action names
    prefix: string;            // input id prefix, e.g. 'ki'
    endpoint: string;          // '/api/kuma-instances'
    listId: string;
    formId: string;
    titleId: string;
    saveBtnId: string;
    emptyMsg: string;
    fields: InstanceField[];
    subtitle: (inst: T) => string;
    extraBadges?: (inst: T) => string;
    deleteConfirm: (name: string) => string;
}

function instanceValue(inst: Record<string, unknown>, f: InstanceField): unknown {
    var v = inst[f.prop];
    if (f.kind === 'check') return !!v;
    if (v === undefined || v === null || v === '') return f.default !== undefined ? f.default : '';
    return v;
}

function makeInstanceCrud<T extends { id: number; name: string; enabled: boolean }>(cfg: InstanceCrudConfig<T>) {
    var cache: T[] = [];
    var editIdId = cfg.prefix + '-edit-id';

    function fieldEl(f: InstanceField): HTMLInputElement | HTMLSelectElement | null {
        return document.getElementById(f.id) as HTMLInputElement | HTMLSelectElement | null;
    }

    function load(): void {
        var listEl = document.getElementById(cfg.listId);
        if (!listEl) return;
        listEl.innerHTML = '<div class="text-center text-muted py-3"><span class="spinner-sm"></span> Loading...</div>';
        getJSON<T[]>(cfg.endpoint).then(function(instances) {
            cache = instances || [];
            render(cache);
        }).catch(function(err: unknown) {
            if (err instanceof Error && err.message === 'not authenticated') return;
            listEl!.innerHTML = '<div class="text-center text-danger py-3">Failed to load instances</div>';
        });
    }

    function render(instances: T[]): void {
        var listEl = document.getElementById(cfg.listId)!;
        if (!instances.length) {
            listEl.innerHTML = '<div class="text-center text-muted py-3">' + cfg.emptyMsg + '</div>';
            return;
        }
        var html = '';
        instances.forEach(function(inst) {
            var enabledBadge = inst.enabled
                ? '<span class="badge bg-success">Enabled</span>'
                : '<span class="badge bg-secondary">Disabled</span>';
            var extra = cfg.extraBadges ? cfg.extraBadges(inst) : '';
            html += '<div class="card card-body bg-light p-2 mb-2 d-flex flex-row align-items-center justify-content-between">'
                + '<div class="flex-grow-1">'
                + '<div class="fw-semibold">' + esc(inst.name) + ' ' + enabledBadge + ' ' + extra + '</div>'
                + '<div class="small text-muted">' + cfg.subtitle(inst) + '</div>'
                + '</div>'
                + '<div class="d-flex gap-1">'
                + '<button type="button" class="btn btn-outline-secondary btn-sm" data-action="edit-' + cfg.action + '-instance" data-id="' + inst.id + '">Edit</button>'
                + '<button type="button" class="btn btn-outline-info btn-sm" data-action="test-' + cfg.action + '-instance" data-id="' + inst.id + '">Test</button>'
                + '<button type="button" class="btn btn-outline-danger btn-sm" data-action="delete-' + cfg.action + '-instance" data-id="' + inst.id + '" data-name="' + esc(inst.name).replace(/"/g, "&quot;") + '">Delete</button>'
                + '</div>'
                + '</div>';
        });
        listEl.innerHTML = html;
    }

    function show(editId: number | null): void {
        var form = document.getElementById(cfg.formId)!;
        var title = document.getElementById(cfg.titleId)!;
        var editing = editId !== null && editId !== undefined;
        var inst: T | undefined = undefined;

        if (editing) {
            inst = cache.find(function(i) { return i.id === editId; });
            if (!inst) { toast('Instance not found', 'error'); return; }
            title.textContent = 'Edit Instance';
            (document.getElementById(editIdId) as HTMLInputElement).value = '' + editId;
        } else {
            title.textContent = 'Add Instance';
            (document.getElementById(editIdId) as HTMLInputElement).value = '';
        }

        cfg.fields.forEach(function(f) {
            var el = fieldEl(f);
            if (!el) return;
            if (f.kind === 'check') {
                (el as HTMLInputElement).checked = editing
                    ? !!(inst as Record<string, unknown>)[f.prop]
                    : (f.default !== undefined ? !!f.default : true);
            } else if (f.kind === 'pass') {
                (el as HTMLInputElement).value = '';
                (el as HTMLInputElement).placeholder = editing ? 'Leave blank to keep current' : 'Password';
            } else {
                el.value = editing ? '' + String(instanceValue(inst as Record<string, unknown>, f) ?? '') : (f.default !== undefined ? '' + f.default : '');
            }
        });
        form.classList.remove('d-none');
    }

    function hide(): void {
        document.getElementById(cfg.formId)!.classList.add('d-none');
        (document.getElementById(editIdId) as HTMLInputElement).value = '';
    }

    function save(): void {
        var editId = (document.getElementById(editIdId) as HTMLInputElement).value;
        var payload: Record<string, unknown> = {};

        for (var i = 0; i < cfg.fields.length; i++) {
            var f = cfg.fields[i];
            var el = fieldEl(f);
            if (!el) continue;
            var value: unknown;
            if (f.kind === 'check') value = (el as HTMLInputElement).checked;
            else if (f.kind === 'pass') value = (el as HTMLInputElement).value;
            else value = el.value.trim();
            if (f.required && !value) { toast((f.label || f.prop) + ' is required', 'error'); return; }
            if (value === '' && f.default !== undefined) value = f.default;
            payload[f.prop] = value;
        }

        var btn = document.getElementById(cfg.saveBtnId) as HTMLButtonElement;
        btn.disabled = true;
        var orig = btn.innerHTML;
        btn.innerHTML = '<span class="spinner-sm"></span> Saving...';

        var endpoint = editId ? cfg.endpoint + '/' + editId : cfg.endpoint;
        apiFetch(endpoint, { method: editId ? 'PUT' : 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
            .then(function(r) { if (!r.ok) throw new Error('Save failed'); return r.json(); })
            .then(function() {
                toast(editId ? 'Instance updated' : 'Instance added', 'success');
                hide();
                load();
            })
            .catch(function(err: unknown) {
                if (err instanceof Error && err.message === 'not authenticated') return;
                toast('Failed to save instance', 'error');
            })
            .finally(function() { btn.disabled = false; btn.innerHTML = orig; });
    }

    function remove(id: number, name: string): void {
        if (!confirm(cfg.deleteConfirm(name))) return;
        apiFetch(cfg.endpoint + '/' + id, { method: 'DELETE' })
            .then(function(r) { if (!r.ok) throw new Error('Delete failed'); return r.json(); })
            .then(function() { toast('Instance deleted', 'success'); load(); })
            .catch(function(err: unknown) { if (err instanceof Error && err.message === 'not authenticated') return; toast('Failed to delete instance', 'error'); });
    }

    function test(id: number): void {
        toast('Testing connection...', 'info');
        apiFetch(cfg.endpoint + '/' + id + '/test', { method: 'POST' })
            .then(function(r) { return r.json() as Promise<{ok: boolean; message?: string}>; })
            .then(function(d) {
                if (d.ok) toast('Connection OK: ' + (d.message || 'success'), 'success');
                else toast('Connection failed: ' + (d.message || 'unknown error'), 'error');
            })
            .catch(function(err: unknown) { if (err instanceof Error && err.message === 'not authenticated') return; toast('Connection test failed', 'error'); });
    }

    return { load: load, render: render, show: show, hide: hide, save: save, remove: remove, test: test };
}

var kumaCrud = makeInstanceCrud<KumaInstanceJSON>({
    action: 'kuma', prefix: 'ki', endpoint: '/api/kuma-instances',
    listId: 'kuma-instances-list', formId: 'kuma-instance-form', titleId: 'kuma-form-title', saveBtnId: 'btn-kuma-save',
    emptyMsg: 'No Kuma instances configured. Click "Add Instance" to create one.',
    fields: [
        { id: 'ki-name', prop: 'name', label: 'Name', required: true },
        { id: 'ki-url', prop: 'url', label: 'URL', required: true },
        { id: 'ki-user', prop: 'username' },
        { id: 'ki-pass', prop: 'password', kind: 'pass' },
        { id: 'ki-enabled', prop: 'enabled', kind: 'check' },
    ],
    subtitle: function(inst) { return esc(inst.url) + ' &middot; ' + esc(inst.username); },
    deleteConfirm: function(name) { return 'Delete instance "' + name + '"? This will also remove all monitors synced to this instance from the database.'; },
});

var npmCrud = makeInstanceCrud<NPMInstanceJSON>({
    action: 'npm', prefix: 'ni', endpoint: '/api/npm-instances',
    listId: 'npm-instances-list', formId: 'npm-instance-form', titleId: 'npm-form-title', saveBtnId: 'btn-npm-save',
    emptyMsg: 'No NPM instances configured. Click "Add Instance" to create one.',
    fields: [
        { id: 'ni-name', prop: 'name', label: 'Name', required: true },
        { id: 'ni-url', prop: 'url', label: 'URL', required: true },
        { id: 'ni-user', prop: 'username' },
        { id: 'ni-pass', prop: 'password', kind: 'pass' },
        { id: 'ni-enabled', prop: 'enabled', kind: 'check' },
    ],
    subtitle: function(inst) { return esc(inst.url) + ' &middot; ' + esc(inst.username); },
    deleteConfirm: function(name) { return 'Delete NPM instance "' + name + '"? This will remove all proxy hosts synced from this instance.'; },
});

var autheliaCrud = makeInstanceCrud<AutheliaInstanceJSON>({
    action: 'authelia', prefix: 'ai', endpoint: '/api/authelia-instances',
    listId: 'authelia-instances-list', formId: 'authelia-instance-form', titleId: 'authelia-form-title', saveBtnId: 'btn-authelia-save',
    emptyMsg: 'No Authelia instances configured. Click "Add Instance" to create one.',
    fields: [
        { id: 'ai-name', prop: 'name', label: 'Name', required: true },
        { id: 'ai-config-path', prop: 'config_path', label: 'Config Path', required: true },
        { id: 'ai-db-path', prop: 'db_path' },
        { id: 'ai-default-policy', prop: 'default_policy', kind: 'select', default: 'one_factor' },
        { id: 'ai-npm-ids', prop: 'npm_instance_ids', default: '[]' },
        { id: 'ai-overrides', prop: 'overrides' },
        { id: 'ai-auto-sync', prop: 'auto_sync', kind: 'check', default: true },
        { id: 'ai-enabled', prop: 'enabled', kind: 'check', default: true },
    ],
    subtitle: function(inst) { return esc(inst.config_path) + ' &middot; Policy: ' + esc(inst.default_policy); },
    extraBadges: function(inst) { return inst.auto_sync ? '<span class="badge bg-info">Auto-sync</span>' : ''; },
    deleteConfirm: function(name) { return 'Delete Authelia instance "' + name + '"? This will remove all alerts and rules associated with this instance.'; },
});

export function loadKumaInstances(): void { kumaCrud.load(); }
export function showKumaInstanceForm(editId: number | null): void { kumaCrud.show(editId); }
export function hideKumaInstanceForm(): void { kumaCrud.hide(); }
export function saveKumaInstance(): void { kumaCrud.save(); }
export function deleteKumaInstance(id: number, name: string): void { kumaCrud.remove(id, name); }
export function testKumaInstance(id: number): void { kumaCrud.test(id); }

export function loadNPMInstances(): void { npmCrud.load(); }
export function showNPMInstanceForm(editId: number | null): void { npmCrud.show(editId); }
export function hideNPMInstanceForm(): void { npmCrud.hide(); }
export function saveNPMInstance(): void { npmCrud.save(); }
export function deleteNPMInstance(id: number, name: string): void { npmCrud.remove(id, name); }
export function testNPMInstance(id: number): void { npmCrud.test(id); }

export function loadAutheliaInstances(): void { autheliaCrud.load(); }
export function showAutheliaInstanceForm(editId: number | null): void { autheliaCrud.show(editId); }
export function hideAutheliaInstanceForm(): void { autheliaCrud.hide(); }
export function saveAutheliaInstance(): void { autheliaCrud.save(); }
export function deleteAutheliaInstance(id: number, name: string): void { autheliaCrud.remove(id, name); }
export function testAutheliaInstance(id: number): void { autheliaCrud.test(id); }


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
        .catch(function(err: unknown) { if (err instanceof Error && err.message === 'not authenticated') return; toast('Test notification failed', 'error'); })
        .finally(function() { btn.disabled = false; });
}

export function loadNotifyMissing(): void {
    var listEl = document.getElementById('notify-missing-list');
    if (!listEl) return;
    var el2 = listEl;
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
            el2.innerHTML = parts.join('<br>');
        })
        .catch(function(err: unknown) {
            if (err instanceof Error && err.message === 'not authenticated') return;
            el2.innerHTML = '<span class="text-danger">Failed to load missing items</span>';
        });
}

