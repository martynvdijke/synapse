// Synapse Dashboard — Entry point
import './dashboard.css';
import './api.js';
import { toast, setLoading } from './toast.js';
import { connectSSE, setRefreshAll } from './sse.js';
import { loadStatus, refreshAll } from './stats.js';
import { loadDockerServices, loadKumaMonitors, loadNPMProxies, loadHistory } from './tabs.js';
import { loadSettings, saveSettings, testConnection, loadKumaInstances, showKumaInstanceForm, hideKumaInstanceForm, saveKumaInstance } from './settings.js';
import { loadAutheliaDashboard, loadAutheliaStatus, loadAutheliaAlerts, loadAutheliaTempAccess, resolveAlert, revokeTempAccess, runAutheliaSync } from './authelia.js';
import { setupLogFilters, loadLogs, connectLogSSE, toggleLogMeta, isLogsLoaded } from './logs.js';

// Wire SSE refreshAll reference
setRefreshAll(refreshAll);

// ─── Confirmation Modal ───────────────────────────────────────
function showConfirmModal(message, okText) {
    return new Promise(function(resolve) {
        var modalEl = document.getElementById('confirm-modal');
        var bodyEl = document.getElementById('confirm-modal-body');
        var okBtn = document.getElementById('confirm-modal-ok');
        var cancelBtn = document.getElementById('confirm-modal-cancel');

        bodyEl.textContent = message;
        if (okText) okBtn.textContent = okText;
        else okBtn.textContent = 'Confirm';

        var modal = new bootstrap.Modal(modalEl);
        modal.show();

        function cleanup() {
            modal.hide();
            okBtn.removeEventListener('click', onOk);
            cancelBtn.removeEventListener('click', onCancel);
            modalEl.removeEventListener('hidden.bs.modal', onHide);
        }
        function onOk() { cleanup(); resolve(true); }
        function onCancel() { cleanup(); resolve(false); }
        function onHide() { cleanup(); resolve(false); }

        okBtn.addEventListener('click', onOk);
        cancelBtn.addEventListener('click', onCancel);
        modalEl.addEventListener('hidden.bs.modal', onHide);
    });
}

// Event listeners
document.getElementById('btn-docker').addEventListener('click', function() {
    showConfirmModal('Start Docker Compose sync?', 'Start Sync').then(function(ok) {
        if (ok) startSync('docker');
    });
});
document.getElementById('btn-npm').addEventListener('click', function() {
    showConfirmModal('Start NPM Proxy Hosts sync?', 'Start Sync').then(function(ok) {
        if (ok) startSync('npm');
    });
});
document.getElementById('btn-auth-dryrun').addEventListener('click', function() { runAutheliaSync(true); });
document.getElementById('btn-auth-sync').addEventListener('click', function() {
    showConfirmModal('Auto-add missing CNAMEs to Authelia config? This modifies the Authelia configuration file.', 'Sync').then(function(ok) {
        if (ok) runAutheliaSync(false);
    });
});
document.getElementById('btn-ta-submit').addEventListener('click', function() {
    var ip = document.getElementById('ta-ip').value.trim();
    var reason = document.getElementById('ta-reason').value.trim();
    var duration = document.getElementById('ta-duration').value.trim();
    if (!ip) { toast('IP address is required', 'error'); return; }
    if (!duration) { toast('Duration is required', 'error'); return; }

    apiFetch('/api/authelia/temp-access', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ip: ip, reason: reason, duration: duration }) })
        .then(function(r){return r.json();})
        .then(function(d) { if (d.error) { toast(d.error, 'error'); return; } toast('Temp access rule added', 'success'); document.getElementById('ta-ip').value = ''; document.getElementById('ta-reason').value = ''; document.getElementById('ta-duration').value = ''; loadAutheliaTempAccess(); })
        .catch(function(err) { if (err.message === 'not authenticated') return; toast('Failed to add rule', 'error'); });
});

document.getElementById('btn-logout').addEventListener('click', logout);
document.getElementById('settings-form').addEventListener('submit', saveSettings);
document.getElementById('btn-test-npm').addEventListener('click', function() { testConnection('npm'); });

// Kuma instance management
document.getElementById('btn-kuma-add').addEventListener('click', function() { showKumaInstanceForm(null); });
document.getElementById('btn-kuma-save').addEventListener('click', saveKumaInstance);
document.getElementById('btn-kuma-cancel').addEventListener('click', hideKumaInstanceForm);

// Monitor detail panel close
document.getElementById('monitor-detail-close').addEventListener('click', function() {
    document.getElementById('monitor-detail-panel').classList.add('d-none');
});

// Clickable Docker stat card → switch to Docker tab
function activateDockerTab() {
    var tabBtn = document.getElementById('tab-btn-docker');
    if (tabBtn) {
        var tab = bootstrap.Tab.getInstance(tabBtn) || new bootstrap.Tab(tabBtn);
        tab.show();
    }
}
document.getElementById('stat-docker-card').addEventListener('click', activateDockerTab);
document.getElementById('stat-docker-card').addEventListener('keydown', function(e) {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); activateDockerTab(); }
});

// Tab switching with focus management
function setupTabPanel(target) {
    var pane = document.querySelector(target);
    if (pane) {
        pane.setAttribute('tabindex', '-1');
        var isActive = pane.classList.contains('show');
        if (isActive) pane.focus({ preventScroll: true });
    }
}
document.querySelectorAll('[data-bs-toggle="tab"]').forEach(function(tab) {
    tab.addEventListener('shown.bs.tab', function(e) {
        var target = e.target.getAttribute('data-bs-target');
        setupTabPanel(target);
        if (target === '#tab-docker') loadDockerServices();
        else if (target === '#tab-npm') loadNPMProxies();
        else if (target === '#tab-kuma') loadKumaMonitors();
        else if (target === '#tab-history') loadHistory();
        else if (target === '#tab-settings') { loadSettings(); loadKumaInstances(); }
        else if (target === '#tab-authelia') loadAutheliaDashboard();
        else if (target === '#tab-logs') {
            setupLogFilters();
            if (!isLogsLoaded()) { loadLogs(false); connectLogSSE(); }
        }
    });
});

// WAI-ARIA keyboard navigation for tabs
document.querySelector('.nav-tabs').addEventListener('keydown', function(e) {
    var tabs = Array.from(document.querySelectorAll('[role="tab"]'));
    var currentIdx = tabs.indexOf(document.activeElement);
    if (currentIdx === -1) return;

    var newIdx = currentIdx;
    switch (e.key) {
        case 'ArrowRight':
            newIdx = (currentIdx + 1) % tabs.length;
            e.preventDefault();
            break;
        case 'ArrowLeft':
            newIdx = (currentIdx - 1 + tabs.length) % tabs.length;
            e.preventDefault();
            break;
        case 'Home':
            newIdx = 0;
            e.preventDefault();
            break;
        case 'End':
            newIdx = tabs.length - 1;
            e.preventDefault();
            break;
        default:
            return;
    }

    tabs[newIdx].focus();
    if (tabs[newIdx] !== document.activeElement) tabs[newIdx].click();
});

// Initial load
apiFetch('/api/status').then(function() { refreshAll(); }).catch(function(err) {
    if (err.message === 'not authenticated') return;
});
loadKumaInstances();

// Start SSE
connectSSE();

// Helper: startSync (used by button handlers)
function startSync(source) {
    document.getElementById('btn-docker').disabled = true;
    document.getElementById('btn-npm').disabled = true;
    document.getElementById('stat-status').innerHTML = '<span class="badge bg-primary">Running...</span>';
    toast('Sync started: ' + source, 'info');
    apiFetch('/api/sync/' + source, { method: 'POST' }).then(r => r.json()).then(function(d) {
        console.log(source + ' sync started', d);
    }).catch(function(err) {
        if (err.message === 'not authenticated') return;
        document.getElementById('btn-docker').disabled = false;
        document.getElementById('btn-npm').disabled = false;
        document.getElementById('stat-status').innerHTML = '<span class="badge bg-secondary">Idle</span>';
        toast('Sync failed to start', 'error');
    });
}
window.startSync = startSync;
