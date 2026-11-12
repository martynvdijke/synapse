// Real-time notifications, Web Push, and per-user notification preferences.
//
// The server owns delivery (SSE for open tabs, Web Push for closed ones, FCM
// for the Android app). This module renders the bell menu, keeps the per-user
// preferences in sync, and manages the browser push subscription.
import type {
    NotificationCapabilities,
    NotificationPreferences,
    NotificationStreamEvent,
    NotificationsResponse,
} from './types';
import { apiFetch } from './api';
import { toast } from './toast';

// ─── Pure helpers (unit tested) ────────────────────────────────

export function urlBase64ToUint8Array(base64: string): Uint8Array {
    var normalized = String(base64 || '').replace(/-/g, '+').replace(/_/g, '/').replace(/\s+/g, '');
    while (normalized.length % 4 !== 0) normalized += '=';
    var raw = atob(normalized);
    var bytes = new Uint8Array(raw.length);
    for (var i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
    return bytes;
}

export function normalizeNotificationPreferences(raw: unknown): NotificationPreferences {
    var out: NotificationPreferences = { enabled: true, web_push: false, fcm: false, new_item: true };
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return out;

    var rec = raw as Record<string, unknown>;
    // Tolerate a full GET-shaped payload as well as a bare preferences object.
    if (rec.preferences && typeof rec.preferences === 'object' && !Array.isArray(rec.preferences) && typeof rec.enabled !== 'boolean') {
        rec = rec.preferences as Record<string, unknown>;
    }
    if (typeof rec.enabled === 'boolean') out.enabled = rec.enabled;
    if (typeof rec.web_push === 'boolean') out.web_push = rec.web_push;
    if (typeof rec.fcm === 'boolean') out.fcm = rec.fcm;
    if (typeof rec.new_item === 'boolean') out.new_item = rec.new_item;
    return out;
}

export function formatSSEEvent(raw: string): NotificationStreamEvent | null {
    if (!raw) return null;

    var dataLines: string[] = [];
    var lines = String(raw).split(/\r?\n/);
    for (var i = 0; i < lines.length; i++) {
        var line = lines[i];
        if (line.charAt(0) === ':') continue;
        if (line.indexOf('data:') === 0) {
            var value = line.slice(5);
            if (value.charAt(0) === ' ') value = value.slice(1);
            dataLines.push(value);
        }
    }

    // Accept both a complete frame ("data: {...}") and already-unwrapped JSON.
    var payload = dataLines.length ? dataLines.join('\n') : String(raw).trim();
    if (!payload) return null;

    var parsed: unknown;
    try {
        parsed = JSON.parse(payload);
    } catch {
        return null;
    }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null;

    var rec = parsed as Record<string, unknown>;
    var id = typeof rec.id === 'string' ? rec.id : (typeof rec.id === 'number' ? String(rec.id) : '');
    return {
        id: id,
        type: typeof rec.type === 'string' && rec.type ? rec.type : 'new_item',
        title: typeof rec.title === 'string' && rec.title ? rec.title : 'New item',
        body: typeof rec.body === 'string' ? rec.body : '',
        url: typeof rec.url === 'string' ? rec.url : '',
        category: typeof rec.category === 'string' ? rec.category : '',
        at: typeof rec.at === 'string' ? rec.at : '',
    };
}

// formatBadgeCount keeps the bell badge readable: hidden at zero, "99+" above.
export function formatBadgeCount(count: number): string {
    if (!isFinite(count) || count <= 0) return '';
    if (count > 99) return '99+';
    return String(Math.floor(count));
}

// ─── State ─────────────────────────────────────────────────────

export interface BrowserPushStatus {
    supported: boolean;
    permission: string;
    subscribed: boolean;
    available: boolean;
    message: string;
}

interface NotificationState {
    preferences: NotificationPreferences;
    capabilities: NotificationCapabilities | null;
    unread: number;
    stream: EventSource | null;
    retryDelay: number;
    initialized: boolean;
    loaded: boolean;
}

var state: NotificationState = {
    preferences: normalizeNotificationPreferences(null),
    capabilities: null,
    unread: 0,
    stream: null,
    retryDelay: 1000,
    initialized: false,
    loaded: false,
};

function normalizeCapabilities(raw: unknown): NotificationCapabilities {
    var rec = (raw && typeof raw === 'object') ? raw as Record<string, unknown> : {};
    return {
        push_enabled: rec.push_enabled === true,
        notify_new_item: rec.notify_new_item !== false,
        web_push_enabled: rec.web_push_enabled === true,
        fcm_enabled: rec.fcm_enabled === true,
        vapid_public_key: typeof rec.vapid_public_key === 'string' ? rec.vapid_public_key : '',
        fcm_project_id: typeof rec.fcm_project_id === 'string' ? rec.fcm_project_id : '',
        fcm_android_channel_id: typeof rec.fcm_android_channel_id === 'string'
            ? rec.fcm_android_channel_id
            : 'synapse_general',
    };
}

function notificationSupported(): boolean {
    return typeof window !== 'undefined' && typeof window.Notification !== 'undefined';
}

function serviceWorkerSupported(): boolean {
    return typeof navigator !== 'undefined'
        && 'serviceWorker' in navigator
        && typeof window !== 'undefined'
        && 'PushManager' in window;
}

// ─── Public entry points ───────────────────────────────────────

export function initNotifications(): void {
    if (state.initialized) return;
    state.initialized = true;

    wireBell();
    wirePreferenceToggles();
    wireBrowserButtons();
    wireAdminButtons();

    window.addEventListener('beforeunload', function() {
        if (state.stream) {
            state.stream.close();
            state.stream = null;
        }
    });

    loadNotifications().catch(function(err: unknown) {
        if (err instanceof Error && err.message === 'not authenticated') return;
        // The API may not be deployed yet; the bell still opens with an error line.
        console.warn('Synapse: notification settings unavailable', err);
        refreshNotificationStatus();
    });

    connectStream();
}

export function loadNotifications(): Promise<void> {
    return apiFetch('/api/notifications')
        .then(function(r) {
            if (!r.ok) throw new Error('notifications load failed');
            return r.json() as Promise<NotificationsResponse>;
        })
        .then(function(d) {
            state.preferences = normalizeNotificationPreferences(d && d.preferences);
            state.capabilities = normalizeCapabilities(d && d.capabilities);
            state.loaded = true;
            renderPreferences();
            refreshNotificationStatus();
        });
}

export function refreshNotificationBadge(): void {
    var badge = document.getElementById('notification-badge');
    var bell = document.getElementById('btn-notifications');
    var label = formatBadgeCount(state.unread);
    if (badge) {
        badge.textContent = label || '0';
        badge.classList.toggle('d-none', label === '');
    }
    if (bell) {
        bell.setAttribute('aria-label', label
            ? 'Notifications, ' + state.unread + ' unread'
            : 'Notifications');
    }
}

export function sendTestNotification(): Promise<void> {
    return apiFetch('/api/notifications/test', { method: 'POST' })
        .then(function(r) {
            if (!r.ok) throw new Error('test notification failed');
            return r.json() as Promise<{ sent?: number; errors?: string[] }>;
        })
        .then(function(d) {
            var sent = typeof d.sent === 'number' ? d.sent : 0;
            var errors = Array.isArray(d.errors) ? d.errors : [];
            if (sent > 0) {
                toast('Test notification sent to ' + sent + (sent === 1 ? ' target' : ' targets'), 'success');
            } else if (!errors.length) {
                toast('No push targets are configured yet', 'info');
            }
            if (errors.length) {
                toast('Push errors: ' + errors.join('; '), 'error');
            }
        })
        .catch(function(err: unknown) {
            if (err instanceof Error && err.message === 'not authenticated') return;
            console.warn('Synapse: test notification failed', err);
            toast('Could not send the test notification', 'error');
        });
}

export function generateVapidKeys(): Promise<void> {
    var btn = document.querySelector('[data-action="generate-vapid"]') as HTMLButtonElement | null;
    if (btn) btn.disabled = true;

    return apiFetch('/api/notifications/vapid', { method: 'POST' })
        .then(function(r) {
            if (!r.ok) throw new Error('vapid generation failed');
            return r.json() as Promise<{ public_key?: string; private_key?: string }>;
        })
        .then(function(d) {
            if (!d.public_key || !d.private_key) throw new Error('vapid keys missing');
            setInputValue('s-push-vapid-public', d.public_key);
            setInputValue('s-push-vapid-private', d.private_key);
            var hint = document.getElementById('s-push-vapid-private-hint');
            if (hint) hint.textContent = 'New keys generated. Save settings to apply them.';
            toast('VAPID keys generated — save settings to apply them', 'success');
        })
        .catch(function(err: unknown) {
            if (err instanceof Error && err.message === 'not authenticated') return;
            console.warn('Synapse: VAPID generation failed', err);
            toast('Could not generate VAPID keys', 'error');
        })
        .finally(function() {
            if (btn) btn.disabled = false;
        });
}

export async function getBrowserPushStatus(): Promise<BrowserPushStatus> {
    var caps = state.capabilities;
    var secure = typeof window === 'undefined' || window.isSecureContext !== false;
    var supported = serviceWorkerSupported() && notificationSupported();
    var permission = notificationSupported() ? window.Notification.permission : 'unsupported';
    var adminEnabled = !!(caps && caps.push_enabled && caps.web_push_enabled && caps.vapid_public_key);
    var subscribed = false;

    if (supported && secure) {
        try {
            var reg = await navigator.serviceWorker.getRegistration();
            if (reg) {
                var sub = await reg.pushManager.getSubscription();
                subscribed = !!sub;
            }
        } catch {
            subscribed = false;
        }
    }

    var message: string;
    if (!caps) {
        message = 'Notification settings could not be loaded. Reload the page to try again.';
    } else if (!adminEnabled) {
        message = 'Browser push is not set up on this server yet. An admin can enable it in Settings → Push notifications.';
    } else if (!secure) {
        message = 'Web Push needs HTTPS. Open this dashboard over HTTPS to enable browser notifications.';
    } else if (!supported) {
        message = 'This browser does not support web push notifications.';
    } else if (permission === 'denied') {
        message = 'Notifications are blocked for this site. Allow them in your browser settings, then reload the page.';
    } else if (subscribed) {
        message = 'This browser is subscribed. Notifications arrive even when this tab is closed.';
    } else if (permission === 'granted') {
        message = 'Permission is granted, but this browser is not subscribed yet.';
    } else {
        message = 'Not subscribed. Enable browser notifications to get alerts when this tab is closed.';
    }

    return {
        supported: supported,
        permission: permission,
        subscribed: subscribed,
        available: adminEnabled && secure && supported && permission !== 'denied' && !subscribed,
        message: message,
    };
}

export async function refreshNotificationStatus(): Promise<void> {
    var statusEl = document.getElementById('np-browser-status');
    var status = await getBrowserPushStatus();

    if (statusEl) statusEl.textContent = status.message;

    setDisabled('btn-push-enable', !status.available);
    setDisabled('btn-push-off', !status.subscribed);
    var caps = state.capabilities;
    setDisabled('btn-push-test', !(caps && caps.push_enabled));

    var webPushToggle = document.getElementById('np-web-push') as HTMLInputElement | null;
    if (webPushToggle) {
        var webPushOff = !(caps && caps.web_push_enabled);
        webPushToggle.disabled = webPushOff;
        webPushToggle.title = webPushOff ? 'Web Push is not enabled by an administrator.' : '';
    }
    var fcmToggle = document.getElementById('np-fcm') as HTMLInputElement | null;
    if (fcmToggle) {
        var fcmOff = !(caps && caps.fcm_enabled);
        fcmToggle.disabled = fcmOff;
        fcmToggle.title = fcmOff ? 'Android push is not enabled by an administrator.' : '';
    }
}

// ─── Rendering ─────────────────────────────────────────────────

function renderPreferences(): void {
    setChecked('np-enabled', state.preferences.enabled);
    setChecked('np-web-push', state.preferences.web_push);
    setChecked('np-fcm', state.preferences.fcm);
    setChecked('np-new-item', state.preferences.new_item);
}

function setChecked(id: string, value: boolean): void {
    var el = document.getElementById(id) as HTMLInputElement | null;
    if (el) el.checked = value;
}

function setDisabled(id: string, value: boolean): void {
    var el = document.getElementById(id) as HTMLButtonElement | null;
    if (el) el.disabled = value;
}

function setInputValue(id: string, value: string): void {
    var el = document.getElementById(id) as HTMLInputElement | HTMLTextAreaElement | null;
    if (el) el.value = value;
}

// ─── Bell + modal wiring ───────────────────────────────────────

function wireBell(): void {
    var bell = document.getElementById('btn-notifications');
    var modalEl = document.getElementById('notifications-modal');
    if (!bell || !modalEl) return;
    var modalTarget: HTMLElement = modalEl;

    bell.addEventListener('click', function() {
        state.unread = 0;
        refreshNotificationBadge();
        renderPreferences();
        refreshNotificationStatus();

        // Pick up changes made in another tab without blocking the open.
        loadNotifications().catch(function() { /* keep the cached view */ });

        if (typeof bootstrap === 'undefined' || !bootstrap.Modal) return;
        var modal = bootstrap.Modal.getInstance(modalTarget) || new bootstrap.Modal(modalTarget);
        modal.show();
    });

    // Bootstrap listens for Escape on the modal element, so a button that gets
    // disabled mid-action can drop focus to <body> and strand the dialog open.
    // Close it here as well; hiding an already-hidden modal is harmless.
    document.addEventListener('keydown', function(e: KeyboardEvent) {
        if (e.key !== 'Escape') return;
        if (!modalTarget.classList.contains('show')) return;
        if (typeof bootstrap === 'undefined' || !bootstrap.Modal) return;
        var modal = bootstrap.Modal.getInstance(modalTarget);
        if (modal) modal.hide();
    });
}

var PREFERENCE_BINDINGS: Array<[string, keyof NotificationPreferences]> = [
    ['np-enabled', 'enabled'],
    ['np-web-push', 'web_push'],
    ['np-fcm', 'fcm'],
    ['np-new-item', 'new_item'],
];

function wirePreferenceToggles(): void {
    PREFERENCE_BINDINGS.forEach(function(binding) {
        var el = document.getElementById(binding[0]) as HTMLInputElement | null;
        if (!el) return;
        var input: HTMLInputElement = el;
        input.addEventListener('change', function() {
            savePreference(binding[1], input.checked);
        });
    });
}

function savePreference(key: keyof NotificationPreferences, value: boolean): void {
    var patch: Partial<NotificationPreferences> = {};
    patch[key] = value;
    savePreferences(patch).catch(function() { /* already reported */ });
}

// savePreferences applies a patch optimistically and reverts it if the PUT fails.
function savePreferences(patch: Partial<NotificationPreferences>): Promise<void> {
    var previous = state.preferences;
    state.preferences = {
        enabled: patch.enabled !== undefined ? patch.enabled : previous.enabled,
        web_push: patch.web_push !== undefined ? patch.web_push : previous.web_push,
        fcm: patch.fcm !== undefined ? patch.fcm : previous.fcm,
        new_item: patch.new_item !== undefined ? patch.new_item : previous.new_item,
    };
    renderPreferences();

    return apiFetch('/api/notifications/preferences', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(state.preferences),
    })
        .then(function(r) {
            if (!r.ok) throw new Error('preference save failed');
            return r.json() as Promise<unknown>;
        })
        .then(function(d) {
            state.preferences = normalizeNotificationPreferences(d);
            renderPreferences();
        })
        .catch(function(err: unknown) {
            state.preferences = previous;
            renderPreferences();
            if (err instanceof Error && err.message === 'not authenticated') throw err;
            console.warn('Synapse: notification preference failed to save', err);
            toast('Could not save the notification preference', 'error');
            throw err;
        });
}

function wireBrowserButtons(): void {
    var enable = document.getElementById('btn-push-enable');
    if (enable) {
        enable.addEventListener('click', function() {
            enableBrowserPush();
        });
    }

    var test = document.getElementById('btn-push-test');
    if (test) {
        test.addEventListener('click', runTestNotification);
    }

    var off = document.getElementById('btn-push-off');
    if (off) {
        off.addEventListener('click', function() {
            disableBrowserPush();
        });
    }
}

var testInFlight = false;

function runTestNotification(): void {
    if (testInFlight) return;
    testInFlight = true;
    sendTestNotification().finally(function() {
        testInFlight = false;
    });
}

function wireAdminButtons(): void {
    document.querySelectorAll('[data-action="generate-vapid"]').forEach(function(el) {
        el.addEventListener('click', function() {
            generateVapidKeys();
        });
    });
    document.querySelectorAll('[data-action="push-test"]').forEach(function(el) {
        el.addEventListener('click', runTestNotification);
    });
}

// ─── Browser push subscribe / unsubscribe ──────────────────────

var pushInFlight = false;

async function enableBrowserPush(): Promise<void> {
    if (pushInFlight) return;
    var caps = state.capabilities;
    if (!caps || !caps.push_enabled || !caps.web_push_enabled || !caps.vapid_public_key) {
        toast('Browser push is not enabled on this server yet', 'error');
        return;
    }
    if (!serviceWorkerSupported() || !notificationSupported()) {
        toast('This browser does not support web push', 'error');
        return;
    }
    if (window.isSecureContext === false) {
        toast('Web Push needs HTTPS', 'error');
        return;
    }

    pushInFlight = true;
    var createdSub: PushSubscription | null = null;
    var registered = false;
    try {
        var permission = window.Notification.permission === 'granted'
            ? 'granted'
            : await window.Notification.requestPermission();
        if (permission !== 'granted') {
            toast('Notification permission was not granted', 'error');
            await refreshNotificationStatus();
            return;
        }

        await navigator.serviceWorker.register('/sw.js');
        var reg = await navigator.serviceWorker.ready;

        var sub = await reg.pushManager.getSubscription();
        if (!sub) {
            sub = await reg.pushManager.subscribe({
                userVisibleOnly: true,
                applicationServerKey: urlBase64ToUint8Array(caps.vapid_public_key) as BufferSource,
            });
            createdSub = sub;
        }

        var json = sub.toJSON();
        var endpoint = json.endpoint || sub.endpoint || '';
        var p256dh = (json.keys && json.keys.p256dh) || '';
        var auth = (json.keys && json.keys.auth) || '';
        if (!endpoint || !p256dh || !auth) throw new Error('subscription is missing endpoint or keys');

        var res = await apiFetch('/api/notifications/subscriptions', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ endpoint: endpoint, keys: { p256dh: p256dh, auth: auth } }),
        });
        if (!res.ok) throw new Error('subscription registration failed');
        registered = true;

        // A subscription is useless while the account switch or the per-user
        // Web Push toggle is off, so turn them on with the explicit action.
        var patch: Partial<NotificationPreferences> = {};
        if (!state.preferences.enabled) patch.enabled = true;
        if (!state.preferences.web_push) patch.web_push = true;
        if (patch.enabled !== undefined || patch.web_push !== undefined) {
            await savePreferences(patch).catch(function() { /* reported by the helper */ });
        }

        toast('Browser notifications enabled', 'success');
    } catch (err) {
        console.warn('Synapse: enabling browser push failed', err);
        toast('Could not enable browser notifications', 'error');
        // Do not leave a browser subscription the server never accepted.
        if (createdSub && !registered) {
            try {
                await createdSub.unsubscribe();
            } catch {
                /* the browser may already have dropped it */
            }
        }
    } finally {
        pushInFlight = false;
        await refreshNotificationStatus();
    }
}

async function disableBrowserPush(): Promise<void> {
    if (pushInFlight) return;
    pushInFlight = true;
    try {
        if (serviceWorkerSupported()) {
            var reg = await navigator.serviceWorker.getRegistration();
            var sub = reg ? await reg.pushManager.getSubscription() : null;
            if (sub) {
                var endpoint = sub.endpoint;
                try {
                    await sub.unsubscribe();
                } catch {
                    // The browser subscription may already be gone; still tell the server.
                }
                if (endpoint) {
                    var res = await apiFetch('/api/notifications/subscriptions', {
                        method: 'DELETE',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ endpoint: endpoint }),
                    });
                    if (!res.ok) throw new Error('subscription removal failed');
                }
            }
        }
        toast('Browser notifications turned off', 'success');
    } catch (err) {
        console.warn('Synapse: disabling browser push failed', err);
        toast('Could not turn off browser notifications', 'error');
    } finally {
        pushInFlight = false;
        await refreshNotificationStatus();
    }
}

// ─── Live stream ───────────────────────────────────────────────

function connectStream(): void {
    if (state.stream || typeof EventSource === 'undefined') return;

    var es = new EventSource('/api/notifications/stream');
    state.stream = es;

    es.onopen = function() {
        state.retryDelay = 1000;
    };

    es.onmessage = function(e: MessageEvent) {
        var event = formatSSEEvent(typeof e.data === 'string' ? e.data : '');
        if (!event) return;
        handleStreamEvent(event);
    };

    es.onerror = function() {
        es.close();
        if (state.stream === es) state.stream = null;
        var delay = state.retryDelay;
        state.retryDelay = Math.min(delay * 2, 30000);
        window.setTimeout(function() {
            if (state.initialized) connectStream();
        }, delay);
    };
}

function handleStreamEvent(event: NotificationStreamEvent): void {
    if (event.type !== 'new_item') return;

    // The stream is server-wide; the per-user switches decide what is shown.
    if (!state.preferences.enabled || !state.preferences.new_item) return;

    var modal = document.getElementById('notifications-modal');
    var modalOpen = !!modal && modal.classList.contains('show');
    if (!modalOpen) {
        state.unread += 1;
        refreshNotificationBadge();
    }

    toast(event.title, 'info');

    if (!notificationSupported() || window.Notification.permission !== 'granted' || !document.hidden) return;
    if (!state.preferences.web_push) return;
    try {
        new window.Notification(event.title, {
            body: event.body,
            // The server tags Web Push messages "new-item" too, so a push that
            // arrives while this one is showing replaces it instead of stacking.
            tag: 'new-item',
            icon: '/favicon.ico',
            data: { url: event.url, category: event.category },
        });
    } catch (err) {
        // Some platforms only allow notifications from a service worker.
        console.warn('Synapse: browser notification failed', err);
    }
}
