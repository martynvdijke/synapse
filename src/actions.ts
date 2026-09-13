import { loadEvents } from './history';
import { openLinkEditorByIndex } from './linkEditor';
import { toggleDockerDetail } from './docker';
import { toggleNPMProxyDetail } from './npm';
import { resumeKumaMonitorAction, pauseKumaMonitorAction, openMonitorEdit, loadMonitorStats } from './kuma';
import { createToken, addNotifyChannel, notifyTest, loadNotifyMissing, copyTrmnlUrl, rotateToken, revokeToken, removeNotifyChannel, showKumaInstanceForm, testKumaInstance, deleteKumaInstance, showNPMInstanceForm, testNPMInstance, deleteNPMInstance, showAutheliaInstanceForm, testAutheliaInstance, deleteAutheliaInstance } from './settings';
import { editAlertRule, deleteAlertRule, ackIncident, resolveIncident } from './alerts';
import { resolveAlert, revokeTempAccess } from './authelia';
import { toggleLogMeta } from './logs';

const ACTIONS: Record<string, (el: HTMLElement) => void> = {
    'load-events': () => loadEvents(),
    'create-token': () => createToken(),
    'add-notify-channel': () => addNotifyChannel(),
    'notify-test': () => notifyTest(),
    'load-notify-missing': () => loadNotifyMissing(),
    'edit-alert-rule': el => editAlertRule(Number(el.dataset.id)),
    'delete-alert-rule': el => deleteAlertRule(Number(el.dataset.id)),
    'ack-incident': el => ackIncident(Number(el.dataset.id)),
    'resolve-incident': el => resolveIncident(Number(el.dataset.id)),
    'resolve-alert': el => resolveAlert(Number(el.dataset.id)),
    'revoke-temp-access': el => revokeTempAccess(Number(el.dataset.id)),
    'toggle-log-meta': el => toggleLogMeta(el),
    'copy-trmnl-url': el => copyTrmnlUrl(el),
    'rotate-token': el => rotateToken(Number(el.dataset.id)),
    'revoke-token': el => revokeToken(Number(el.dataset.id)),
    'remove-notify-channel': el => removeNotifyChannel(Number(el.dataset.idx)),
    'edit-kuma-instance': el => showKumaInstanceForm(Number(el.dataset.id)),
    'test-kuma-instance': el => testKumaInstance(Number(el.dataset.id)),
    'delete-kuma-instance': el => deleteKumaInstance(Number(el.dataset.id), el.dataset.name || ''),
    'edit-npm-instance': el => showNPMInstanceForm(Number(el.dataset.id)),
    'test-npm-instance': el => testNPMInstance(Number(el.dataset.id)),
    'delete-npm-instance': el => deleteNPMInstance(Number(el.dataset.id), el.dataset.name || ''),
    'edit-authelia-instance': el => showAutheliaInstanceForm(Number(el.dataset.id)),
    'test-authelia-instance': el => testAutheliaInstance(Number(el.dataset.id)),
    'delete-authelia-instance': el => deleteAutheliaInstance(Number(el.dataset.id), el.dataset.name || ''),
    'switch-tab': el => { const b = document.getElementById(el.dataset.tab || ''); if (b) b.click(); },
    'open-link-editor': el => openLinkEditorByIndex(Number(el.dataset.idx)),
    'toggle-docker-detail': el => toggleDockerDetail(el),
    'toggle-npm-proxy-detail': el => toggleNPMProxyDetail(el),
    'resume-kuma-monitor': el => resumeKumaMonitorAction(Number(el.dataset.kumaId), Number(el.dataset.instanceId)),
    'pause-kuma-monitor': el => pauseKumaMonitorAction(Number(el.dataset.kumaId), Number(el.dataset.instanceId)),
    'open-monitor-edit': el => openMonitorEdit(Number(el.dataset.id), Number(el.dataset.instanceId)),
    'show-monitor-stats': el => loadMonitorStats(el.dataset.monitorId || '', el.dataset.instanceId || ''),
};

export function installActions(): void {
    document.addEventListener('click', function (e) {
        const el = (e.target as HTMLElement).closest('[data-action]') as HTMLElement | null;
        if (!el) return;
        const fn = ACTIONS[el.dataset.action || ''];
        if (fn) fn(el);
    });
}
