// Tab visibility — per-browser, persisted in localStorage.
// Lets users hide tabs they don't use (e.g. NPM Hosts when they don't use nginx proxy manager).

export interface TabDef {
    id: string;
    label: string;
    btnId: string;
}

export const ALL_TABS: TabDef[] = [
    { id: 'docker', label: 'Docker Compose', btnId: 'tab-btn-docker' },
    { id: 'kuma', label: 'Uptime Kuma', btnId: 'tab-btn-kuma' },
    { id: 'npm', label: 'NPM Hosts', btnId: 'tab-btn-npm' },
    { id: 'history', label: 'Sync History', btnId: 'tab-btn-history' },
    { id: 'events', label: 'Events', btnId: 'tab-btn-events' },
    { id: 'authelia', label: 'Authelia', btnId: 'tab-btn-authelia' },
    { id: 'logs', label: 'Logs', btnId: 'tab-btn-logs' },
    // Settings is always visible — not listed so it can't be hidden.
];

const STORAGE_KEY = 'synapse_hidden_tabs';

export function getHiddenTabs(): string[] {
    try {
        var raw = localStorage.getItem(STORAGE_KEY);
        if (!raw) return [];
        var parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        // Filter to known ids only
        var known = ALL_TABS.map(function(t) { return t.id; });
        return parsed.filter(function(id: string) { return known.indexOf(id) >= 0; });
    } catch {
        return [];
    }
}

function saveHiddenTabs(ids: string[]): void {
    try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(ids));
    } catch {
        // ignore quota errors
    }
}

export function isTabHidden(id: string): boolean {
    return getHiddenTabs().indexOf(id) >= 0;
}

export function setTabHidden(id: string, hidden: boolean): void {
    var cur = getHiddenTabs();
    var idx = cur.indexOf(id);
    if (hidden && idx === -1) cur.push(id);
    else if (!hidden && idx !== -1) cur.splice(idx, 1);
    saveHiddenTabs(cur);
    applyTabVisibility();
}

export function getVisibleTabButtons(): HTMLElement[] {
    var all = Array.from(document.querySelectorAll('[role="tab"]')) as HTMLElement[];
    var hidden = getHiddenTabs();
    return all.filter(function(btn) {
        var tabId = btn.id.replace('tab-btn-', '');
        return hidden.indexOf(tabId) === -1;
    });
}

export function applyTabVisibility(): void {
    var hidden = getHiddenTabs();
    var hiddenSet: Record<string, boolean> = {};
    hidden.forEach(function(id) { hiddenSet[id] = true; });

    ALL_TABS.forEach(function(tab) {
        var btn = document.getElementById(tab.btnId) as HTMLElement | null;
        if (!btn) return;
        var li = btn.closest('li') as HTMLElement | null;
        var isHidden = !!hiddenSet[tab.id];
        if (li) li.classList.toggle('d-none', isHidden);
        else btn.classList.toggle('d-none', isHidden);
        // aria-hidden for accessibility
        btn.setAttribute('aria-hidden', isHidden ? 'true' : 'false');
    });

    // If the active tab is now hidden, switch to first visible one
    var activeBtn = document.querySelector('.nav-tabs .nav-link.active') as HTMLElement | null;
    if (activeBtn) {
        var activeId = activeBtn.id.replace('tab-btn-', '');
        if (hiddenSet[activeId]) {
            var visible = getVisibleTabButtons();
            if (visible.length) {
                var fallback = visible[0];
                var tab = bootstrap.Tab.getInstance(fallback) || new bootstrap.Tab(fallback);
                tab.show();
            }
        }
    }

    // Re-render the checkboxes to stay in sync (e.g. after reset)
    renderTabVisibilityControls();
}

export function renderTabVisibilityControls(): void {
    var container = document.getElementById('tab-visibility-list');
    if (!container) return;
    var hidden = getHiddenTabs();
    var html = '';
    ALL_TABS.forEach(function(tab) {
        var checked = hidden.indexOf(tab.id) === -1 ? ' checked' : '';
        // NPM tab gets a hint about proxy host linking
        var hint = tab.id === 'npm' ? ' <span class="text-muted small">— nginx proxy host linking</span>' : '';
        html += '<div class="form-check">'
            + '<input class="form-check-input" type="checkbox" id="tab-visible-' + tab.id + '" data-tab-id="' + tab.id + '"' + checked + '>'
            + '<label class="form-check-label" for="tab-visible-' + tab.id + '">' + tab.label + hint + '</label>'
            + '</div>';
    });
    html += '<div class="form-text mt-1">Settings tab is always visible. Hidden tabs are remembered in this browser (<code>localStorage</code>).</div>';
    container.innerHTML = html;

    ALL_TABS.forEach(function(tab) {
        var cb = document.getElementById('tab-visible-' + tab.id) as HTMLInputElement | null;
        if (!cb) return;
        cb.addEventListener('change', function() {
            setTabHidden(tab.id, !cb.checked);
        });
    });

    var resetBtn = document.getElementById('btn-tab-visibility-reset') as HTMLButtonElement | null;
    if (resetBtn) {
        resetBtn.onclick = function() {
            try { localStorage.removeItem(STORAGE_KEY); } catch {}
            applyTabVisibility();
            toast('Tab visibility reset — all tabs shown', 'success');
        };
    }
}

// Apply as soon as DOM is ready — also exposed for manual refresh
function initTabVisibility(): void {
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', applyTabVisibility);
    } else {
        applyTabVisibility();
    }
}

initTabVisibility();

// Expose for e2e / debugging
(window as any).getHiddenTabs = getHiddenTabs;
(window as any).applyTabVisibility = applyTabVisibility;
(window as any).setTabHidden = setTabHidden;
