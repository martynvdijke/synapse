// E-ink mode — activation (URL param, cookie, admin setting) + toggle + refresh throttle
import './eink.css';

var EINK_COOKIE = 'eink';
var LAST_AUTO_REFRESH = 0;
var REFRESH_MIN_INTERVAL = 60000; // 60s minimum auto-refresh interval in e-ink mode

function getCookie(name: string): string | null {
    var m = document.cookie.match(new RegExp('(?:^|; )' + name + '=([^;]*)'));
    return m ? decodeURIComponent(m[1]) : null;
}

function setCookie(name: string, value: string, days: number): void {
    var d = new Date();
    d.setTime(d.getTime() + days * 86400000);
    document.cookie = name + '=' + encodeURIComponent(value) + '; path=/; expires=' + d.toUTCString() + '; SameSite=Lax';
}

function deleteCookie(name: string): void {
    document.cookie = name + '=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT';
}

export function isEinkMode(): boolean {
    return document.documentElement.classList.contains('eink-mode');
}

export function isWallboardMode(): boolean {
    return document.documentElement.classList.contains('eink-wallboard');
}

export function applyEink(active: boolean, wallboard: boolean): void {
    var root = document.documentElement;
    root.classList.toggle('eink-mode', active);
    root.classList.toggle('eink-wallboard', active && wallboard);
    var btn = document.getElementById('btn-eink');
    if (btn) btn.textContent = active ? 'E-ink: On' : 'E-ink: Off';
}

export function toggleEink(): void {
    var next = !isEinkMode();
    if (next) setCookie(EINK_COOKIE, '1', 365);
    else deleteCookie(EINK_COOKIE);
    applyEink(next, isWallboardMode());
}

/**
 * Auto-refresh throttle: in e-ink mode, automatic refreshes must be at least
 * 60s apart (spec: "auto-refresh intervals SHALL be disabled or set to
 * minimum 60 seconds"). Returns true when a refresh is allowed.
 */
export function refreshDue(): boolean {
    if (!isEinkMode()) return true;
    var now = Date.now();
    if (now - LAST_AUTO_REFRESH >= REFRESH_MIN_INTERVAL) {
        LAST_AUTO_REFRESH = now;
        return true;
    }
    return false;
}

function init(): void {
    var params = new URLSearchParams(window.location.search);
    var einkParam = params.get('eink');
    var wallboard = params.get('wallboard') === '1' || params.get('wallboard') === 'true';

    var active = false;
    if (einkParam !== null) {
        // ?eink=1 (or ?eink=true) renders e-ink for this session and persists via cookie
        setCookie(EINK_COOKIE, '1', 365);
        active = true;
    } else if (getCookie(EINK_COOKIE) === '1') {
        active = true;
    }

    // Site-wide admin setting — only on authed pages where /api/settings exists
    if (document.body && document.body.dataset.einkCheck !== 'false') {
        fetch('/api/settings', { credentials: 'same-origin' })
            .then(function(r) { return r.ok ? r.json() : null; })
            .then(function(s: { eink_enabled?: boolean } | null) {
                applyEink(!!(s && s.eink_enabled) || active, wallboard);
            })
            .catch(function() { applyEink(active, wallboard); });
    } else {
        applyEink(active, wallboard);
    }

    var btn = document.getElementById('btn-eink');
    if (btn) btn.addEventListener('click', toggleEink);
}

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
} else {
    init();
}

window.toggleEink = toggleEink;
