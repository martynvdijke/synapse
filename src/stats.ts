// Dashboard stat card loading and connection health indicators
import type { StatusResponse, AutheliaStatusResponse, DashboardResponse } from './types';
import { apiFetch, getDashboard } from './api';
import { loadDockerServices, renderDockerServices } from './docker';
import { loadKumaMonitors, renderKumaMonitors } from './kuma';
import { loadNPMProxies, renderNPMProxies } from './npm';
import { loadHistory, renderHistory } from './history';

export function setHealthDot(id: string, ok: boolean | null): void {
    var dot = document.getElementById(id);
    if (!dot) return;
    dot.className = 'health-dot';
    if (ok === true) {
        dot.classList.add('healthy');
        dot.title = 'Connected';
    } else if (ok === false) {
        dot.classList.add('error');
        dot.title = 'Connection error';
    } else {
        dot.classList.add('unknown');
        dot.title = 'Not configured';
    }
}

export function renderStatus(d: StatusResponse): void {
    document.getElementById('stat-docker')!.textContent = '' + d.docker_count;
    document.getElementById('stat-npm')!.textContent = d.npm_error ? '\u26A0' : '' + d.npm_count;
    document.getElementById('stat-monitors')!.textContent = '' + d.monitor_count;
    if (!d.running) {
        document.getElementById('stat-status')!.innerHTML = '<span class="badge bg-secondary">Idle</span>';
    }
    if (d.connection_health) {
        setHealthDot('health-docker', d.connection_health.docker ? d.connection_health.docker.ok : null);
        if (d.connection_health.npm && d.connection_health.npm.instances && d.connection_health.npm.instances.length > 0) {
            var anyHealthy = d.connection_health.npm.instances.some(function(i) { return i.ok; });
            var allHealthy = d.connection_health.npm.instances.every(function(i) { return i.ok; });
            var healthyCount = d.connection_health.npm.instances.filter(function(i) { return i.ok; }).length;
            var totalCount = d.connection_health.npm.instances.length;
            setHealthDot('health-npm', allHealthy ? true : anyHealthy ? null : false);
            document.getElementById('stat-npm')!.textContent = healthyCount + '/' + totalCount;
        } else {
            setHealthDot('health-npm', d.connection_health.npm ? d.connection_health.npm.ok : null);
        }
        setHealthDot('health-kuma', d.connection_health.kuma ? d.connection_health.kuma.ok : null);
    } else {
        setHealthDot('health-docker', d.docker_error ? false : true);
        setHealthDot('health-npm', d.npm_error ? false : true);
        setHealthDot('health-kuma', d.kuma_error ? false : (d.kuma_error === '' ? true : null));
    }
    (document.getElementById('btn-docker') as HTMLButtonElement).disabled = d.running;
    (document.getElementById('btn-npm') as HTMLButtonElement).disabled = d.running;
    if (d.snapshot?.stale) {
        var el = document.getElementById('stat-status');
        if (el) { el.title = 'snapshot stale (generated at ' + d.snapshot.generated_at + ')'; el.textContent = (el.textContent || '') + ' (stale)'; }
    }
    var statAlerts = document.getElementById('stat-alerts');
    if (statAlerts) {
        var openIncidents = d.open_incidents || 0;
        statAlerts.innerHTML = '<span class="badge ' + (openIncidents > 0 ? 'bg-danger' : 'bg-success') + '">' + openIncidents + ' open</span>';
    }
}

function renderAuthelia(d: AutheliaStatusResponse): void {
    var el = document.getElementById('stat-authelia');
    if (!el) return;
    if (!d.configured) {
        el.innerHTML = '<span class="badge bg-secondary">Not configured</span>';
        return;
    }
    if (d.error) {
        el.innerHTML = '<span class="badge bg-danger">Config error</span>';
        return;
    }
    var total = d.npm_cnames ? d.npm_cnames.length : 0;
    var matched = d.matched ? d.matched.length : 0;
    var coverage = total > 0 ? Math.round(matched / total * 100) : 0;
    el.innerHTML = '<span class="badge ' + (coverage >= 100 ? 'bg-success' : coverage > 0 ? 'bg-warning text-dark' : 'bg-danger') + '">' + matched + '/' + total + ' (' + coverage + '%)</span>';
}

export function loadStatus(): void {
    apiFetch('/api/status').then(function(r){return r.json() as Promise<StatusResponse>;}).then(function(d) {
        renderStatus(d);
    }).catch(function(err: unknown) {
        if (err instanceof Error && err.message === 'not authenticated') return;
    });
    apiFetch('/api/authelia/status').then(function(r){return r.json() as Promise<AutheliaStatusResponse>;}).then(function(d) {
        renderAuthelia(d);
    }).catch(function(err: unknown) {
        if (err instanceof Error && err.message === 'not authenticated') return;
    });
}

export function refreshAll(): void {
    getDashboard().then(function(r){
        if (!r.ok) throw new Error('aggregate failed');
        return r.json() as Promise<DashboardResponse>;
    }).then(function(d){
        var s = d.sections;
        if (s.status) renderStatus(s.status);
        if (s.authelia) renderAuthelia(s.authelia);
        if (s.services) {
            // docker needs links + coverage; if not in aggregate, skip
            // For aggregate, links are s.links, but docker also needs coverage (not in aggregate). Use available.
            // If links present, render with coverage empty; otherwise skip.
            if (s.links) renderDockerServices(s.services, s.links, null);
            else renderDockerServices(s.services, [], null);
        }
        if (s.monitors) renderKumaMonitors(s.monitors);
        if (s.proxies || s.npm_hosts) renderNPMProxies(s.proxies || [], s.npm_hosts || []);
        if (s.history) renderHistory(s.history);
    }).catch(function(){
        loadStatus();
        loadDockerServices();
        loadKumaMonitors();
        loadNPMProxies();
        loadHistory();
    });
}
