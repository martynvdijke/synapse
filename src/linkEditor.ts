// Service link editor modal: link a Docker service to NPM/Kuma/Authelia.
import type { ServiceLink, NPMProxyHost, MonitorResponse } from './types';
import { esc, apiFetch, getJSON, createServiceLink, updateServiceLink, deleteServiceLink, refreshServiceLink, createNPMProxyHost, createKumaMonitor } from './api';
import { toast } from './toast';
import { loadDockerServices, linkServices } from './docker';

// ─── Service link editor state ──────────────────────────────────
var linkEditorService = '';
var linkEditorLink: ServiceLink | null = null;
var linkNPMHosts: NPMProxyHost[] = [];
var linkKumaMonitors: MonitorResponse[] = [];
var linkNPMInstances: Array<{ id: number; name: string }> = [];
var linkKumaInstances: Array<{ id: number; name: string }> = [];
var linkAutheliaInstances: Array<{ id: number; name: string }> = [];

function populateSelect(el: HTMLSelectElement, items: Array<{ label: string; value: string }>, selectedValue: string): void {
    var html = '';
    for (var i = 0; i < items.length; i++) {
        html += '<option value="' + items[i].value + '"' + (items[i].value === selectedValue ? ' selected' : '') + '>' + esc(items[i].label) + '</option>';
    }
    el.innerHTML = html;
}

// Cache link targets for a short window. The backend already caches the upstream
// NPM/Kuma responses for 15s per client, but the editor is opened infrequently so
// that cache is usually cold — re-fetching every open pays the full external
// integration cost. A 30s client-side TTL keeps repeated opens instant while
// staying close to the backend's freshness window.
var linkTargetsCache: { npm: NPMProxyHost[]; kuma: MonitorResponse[]; ts: number } | null = null;
var LINK_TARGETS_TTL = 30000;

function populateLinkTargetSelects(): void {
    var npmSel = document.getElementById('link-npm-select') as HTMLSelectElement;
    var opts = '<option value="">— Not linked —</option>';
    for (var i = 0; i < linkNPMHosts.length; i++) {
        var h = linkNPMHosts[i];
        opts += '<option value="' + i + '">' + esc(h.domain_names.join(', ')) + ' (' + esc(h.instance_name || '?') + ')</option>';
    }
    npmSel.innerHTML = opts;
    var kumaSel = document.getElementById('link-kuma-select') as HTMLSelectElement;
    var kopts = '<option value="">— Not linked —</option>';
    for (var j = 0; j < linkKumaMonitors.length; j++) {
        var m = linkKumaMonitors[j];
        kopts += '<option value="' + j + '">' + esc(m.name) + ' (' + esc(m.instance_name || '?') + ')</option>';
    }
    kumaSel.innerHTML = kopts;
}

function loadLinkTargets(force?: boolean): Promise<void> {
    if (!force && linkTargetsCache && Date.now() - linkTargetsCache.ts < LINK_TARGETS_TTL) {
        linkNPMHosts = linkTargetsCache.npm;
        linkKumaMonitors = linkTargetsCache.kuma;
        populateLinkTargetSelects();
        return Promise.resolve();
    }
    var npmReq = apiFetch('/api/npm/proxy-hosts').then(function(r){ return r.ok ? r.json() as Promise<NPMProxyHost[]> : Promise.resolve([] as NPMProxyHost[]); });
    var kumaReq = apiFetch('/api/monitors').then(function(r){ return r.ok ? r.json() as Promise<MonitorResponse[]> : Promise.resolve([] as MonitorResponse[]); });
    return Promise.all([npmReq, kumaReq]).then(function(res: [NPMProxyHost[], MonitorResponse[]]) {
        linkNPMHosts = res[0] || [];
        linkKumaMonitors = res[1] || [];
        linkTargetsCache = { npm: linkNPMHosts, kuma: linkKumaMonitors, ts: Date.now() };
        populateLinkTargetSelects();
    });
}

function selectedNPMHost(): NPMProxyHost | null {
    var sel = document.getElementById('link-npm-select') as HTMLSelectElement;
    var idx = parseInt(sel.value, 10);
    if (isNaN(idx) || !linkNPMHosts[idx]) return null;
    return linkNPMHosts[idx];
}

function selectedKumaMonitor(): MonitorResponse | null {
    var sel = document.getElementById('link-kuma-select') as HTMLSelectElement;
    var idx = parseInt(sel.value, 10);
    if (isNaN(idx) || !linkKumaMonitors[idx]) return null;
    return linkKumaMonitors[idx];
}

export function openLinkEditorByIndex(idx: number): void {
    var svc = linkServices[idx];
    if (!svc) return;
    openLinkEditor(svc.name);
}

export function openLinkEditor(serviceName: string): void {
    linkEditorService = serviceName;
    linkEditorLink = null;
    document.getElementById('link-editor-service')!.textContent = serviceName;

    // Load the independent instance lists in parallel instead of chaining them.
    var pInstances = Promise.all([
        getJSON<{id:number; name:string; enabled:boolean}[]>('/api/npm-instances').then(function(insts) {
            linkNPMInstances = (insts || []).filter(function(i){ return i.enabled; });
            populateSelect(document.getElementById('link-npm-instance') as HTMLSelectElement,
                linkNPMInstances.map(function(i){ return { label: i.name, value: String(i.id) }; }), '');
        }).catch(function(err: unknown) { if (!(err instanceof Error) || err.message !== 'not authenticated') toast('Failed to load NPM instances', 'error'); }),
        getJSON<{id:number; name:string; enabled:boolean}[]>('/api/kuma-instances').then(function(insts) {
            linkKumaInstances = (insts || []).filter(function(i){ return i.enabled; });
            populateSelect(document.getElementById('link-kuma-instance') as HTMLSelectElement,
                linkKumaInstances.map(function(i){ return { label: i.name, value: String(i.id) }; }), '');
        }).catch(function(err: unknown) { if (!(err instanceof Error) || err.message !== 'not authenticated') toast('Failed to load Kuma instances', 'error'); }),
        apiFetch('/api/authelia-instances').then(function(r){ return r.ok ? r.json() as Promise<{id:number; name:string; enabled:boolean}[]> : Promise.resolve([] as {id:number; name:string; enabled:boolean}[]); }).then(function(insts) {
            linkAutheliaInstances = (insts || []).filter(function(i){ return i.enabled; });
            var sel = document.getElementById('link-authelia-instance') as HTMLSelectElement;
            var opts = '<option value="">— Not linked —</option>';
            for (var i = 0; i < linkAutheliaInstances.length; i++) {
                opts += '<option value="' + linkAutheliaInstances[i].id + '">' + esc(linkAutheliaInstances[i].name) + '</option>';
            }
            sel.innerHTML = opts;
        }).catch(function(err: unknown) { if (!(err instanceof Error) || err.message !== 'not authenticated') toast('Failed to load Authelia instances', 'error'); }),
    ]);

    var pLinks = getJSON<ServiceLink[]>('/api/service-links').then(function(links: ServiceLink[]) {
        for (var i = 0; i < links.length; i++) {
            if (links[i].service_name === serviceName) { linkEditorLink = links[i]; break; }
        }
        (document.getElementById('link-unlink-btn') as HTMLButtonElement).disabled = !linkEditorLink;
        (document.getElementById('link-refresh-btn') as HTMLButtonElement).disabled = !linkEditorLink;
    }).catch(function(err: unknown) { if (err instanceof Error && err.message === 'not authenticated') return; toast('Failed to load service links', 'error'); });

    var pTargets = loadLinkTargets();

    Promise.all([pInstances, pLinks, pTargets]).then(function() {
        if (linkEditorLink) {
            var npmSel = document.getElementById('link-npm-select') as HTMLSelectElement;
            for (var i = 0; i < linkNPMHosts.length; i++) {
                if (linkNPMHosts[i].domain_names.indexOf(linkEditorLink.npm_host_name || '') >= 0) {
                    npmSel.value = String(i);
                    break;
                }
            }
            var kumaSel = document.getElementById('link-kuma-select') as HTMLSelectElement;
            for (var j = 0; j < linkKumaMonitors.length; j++) {
                if (linkKumaMonitors[j].id === linkEditorLink.kuma_monitor_id) {
                    kumaSel.value = String(j);
                    break;
                }
            }
        }
        new bootstrap.Modal(document.getElementById('link-editor-modal')!).show();
    }).catch(function(err: unknown) {
        if (err instanceof Error && err.message === 'not authenticated') return;
        toast('Failed to open link editor', 'error');
    });
}

export function saveServiceLink(): void {
    var npmHost = selectedNPMHost();
    var kumaMon = selectedKumaMonitor();
    var input: Record<string, unknown> = { service_name: linkEditorService };
    if (npmHost) {
        input.npm_instance_id = npmHost.instance_id;
        input.npm_host_name = npmHost.domain_names[0];
    } else {
        input.npm_instance_id = 0;
        input.npm_host_name = '';
    }
    if (kumaMon) {
        input.kuma_instance_id = kumaMon.instance_id;
        input.kuma_monitor_id = kumaMon.id;
        input.kuma_monitor_name = kumaMon.name;
    } else {
        input.kuma_instance_id = 0;
        input.kuma_monitor_id = 0;
        input.kuma_monitor_name = '';
    }
    var ensureMissing = (document.getElementById('link-ensure-missing') as HTMLInputElement).checked;
    input.ensure_missing = ensureMissing;
    var autheliaSel = document.getElementById('link-authelia-instance') as HTMLSelectElement;
    var autheliaId = parseInt(autheliaSel.value, 10);
    input.authelia_instance_id = autheliaId > 0 ? autheliaId : null;
    input.authelia_policy = (document.getElementById('link-authelia-policy') as HTMLSelectElement).value;
    var ensureRule = (document.getElementById('link-authelia-ensure') as HTMLInputElement).checked;
    input.dry_run = !ensureRule;
    var req = linkEditorLink
        ? updateServiceLink(linkEditorLink.id, input)
        : createServiceLink(input);
    req.then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body: unknown) { var b = body as {error?: string}; throw new Error((b && b.error) || ('HTTP ' + r.status)); });
        }
        return r.json() as Promise<{authelia_actions?: Array<{action:string;cname:string;policy?:string;message:string}>}>;
    }).then(function(body: {authelia_actions?: Array<{action:string;cname:string;policy?:string;message:string}>}) {
        toast('Service link saved');
        var actions: Array<{ action: string; cname: string; policy?: string; message: string }> = (body && body.authelia_actions) || [];
        var actionsBox = document.getElementById('link-authelia-actions')!;
        var actionsList = document.getElementById('link-authelia-actions-list')!;
        if (actions.length) {
            var html = '';
            for (var i = 0; i < actions.length; i++) {
                var a = actions[i];
                var cls = a.action === 'add' ? 'text-success' : 'text-muted';
                html += '<div class="' + cls + '">\u2022 ' + esc(a.cname) + ' \u2014 ' + esc(a.message) + '</div>';
            }
            actionsList.innerHTML = html;
            actionsBox.classList.remove('d-none');
        } else {
            actionsBox.classList.add('d-none');
            actionsList.innerHTML = '';
        }
        if (actions.length) return;
        var modal = bootstrap.Modal.getInstance(document.getElementById('link-editor-modal')!);
        if (modal) modal.hide();
        loadDockerServices();
    }).catch(function(err: unknown) {
        if (err instanceof Error && err.message === 'not authenticated') return;
        toast('Save failed: ' + (err instanceof Error ? err.message : String(err)), 'error');
    });
}

export function unlinkServiceLink(): void {
    if (!linkEditorLink) return;
    deleteServiceLink(linkEditorLink.id).then(function(r) {
        if (!r.ok) { throw new Error('HTTP ' + r.status); }
        toast('Link removed');
        var modal = bootstrap.Modal.getInstance(document.getElementById('link-editor-modal')!);
        if (modal) modal.hide();
        loadDockerServices();
    }).catch(function(err: unknown) {
        if (err instanceof Error && err.message === 'not authenticated') return;
        toast('Unlink failed', 'error');
    });
}

export function refreshServiceLinkDetails(): void {
    if (!linkEditorLink) return;
    refreshServiceLink(linkEditorLink.id).then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body: unknown) { var b = body as {error?: string}; throw new Error((b && b.error) || ('HTTP ' + r.status)); });
        }
        return r.json() as Promise<unknown>;
    }).then(function() {
        toast('Link details refreshed');
        loadDockerServices();
    }).catch(function(err: unknown) {
        if (err instanceof Error && err.message === 'not authenticated') return;
        toast('Refresh failed: ' + (err instanceof Error ? err.message : String(err)), 'error');
    });
}

export function createNPMHostFromLink(): void {
    var instanceId = parseInt((document.getElementById('link-npm-instance') as HTMLSelectElement).value, 10);
    if (!instanceId) { toast('Select an NPM instance', 'error'); return; }
    var domains = (document.getElementById('link-npm-domains') as HTMLInputElement).value.trim();
    if (!domains) { toast('Enter at least one domain', 'error'); return; }
    var createdDomain = domains.split(',')[0].trim();
    var input: Record<string, unknown> = {
        instance_id: instanceId,
        domain_names: domains.split(',').map(function(d){ return d.trim(); }).filter(function(d){ return d.length > 0; }),
        forward_host: (document.getElementById('link-npm-host') as HTMLInputElement).value.trim(),
        forward_port: parseInt((document.getElementById('link-npm-port') as HTMLInputElement).value, 10) || 80,
        forward_scheme: (document.getElementById('link-npm-scheme') as HTMLSelectElement).value,
        service_name: linkEditorService
    };
    createNPMProxyHost(input).then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body: unknown) { var b = body as {error?: string}; throw new Error((b && b.error) || ('HTTP ' + r.status)); });
        }
        return r.json() as Promise<unknown>;
    }).then(function() {
        toast('NPM proxy host created');
        return loadLinkTargets(true);
    }).then(function() {
        var npmSel = document.getElementById('link-npm-select') as HTMLSelectElement;
        for (var i = 0; i < linkNPMHosts.length; i++) {
            if (linkNPMHosts[i].domain_names[0] === createdDomain) {
                npmSel.value = String(i);
                break;
            }
        }
    }).catch(function(err: unknown) {
        if (err instanceof Error && err.message === 'not authenticated') return;
        toast('Create failed: ' + (err instanceof Error ? err.message : String(err)), 'error');
    });
}

export function createKumaMonitorFromLink(): void {
    var instanceId = parseInt((document.getElementById('link-kuma-instance') as HTMLSelectElement).value, 10);
    if (!instanceId) { toast('Select a Kuma instance', 'error'); return; }
    var name = (document.getElementById('link-kuma-name') as HTMLInputElement).value.trim();
    if (!name) { toast('Enter a monitor name', 'error'); return; }
    var input: Record<string, unknown> = {
        instance_id: instanceId,
        name: name,
        type: (document.getElementById('link-kuma-type') as HTMLSelectElement).value,
        url: (document.getElementById('link-kuma-url') as HTMLInputElement).value.trim(),
        docker_container: (document.getElementById('link-kuma-container') as HTMLInputElement).value.trim(),
        service_name: linkEditorService
    };
    createKumaMonitor(input).then(function(r) {
        if (!r.ok) {
            return r.json().then(function(body: unknown) { var b = body as {error?: string}; throw new Error((b && b.error) || ('HTTP ' + r.status)); });
        }
        return r.json() as Promise<MonitorResponse>;
    }).then(function(res: MonitorResponse) {
        toast('Kuma monitor created');
        return loadLinkTargets(true).then(function() {
            var kumaSel = document.getElementById('link-kuma-select') as HTMLSelectElement;
            for (var i = 0; i < linkKumaMonitors.length; i++) {
                if (linkKumaMonitors[i].id === res.id && linkKumaMonitors[i].instance_id === res.instance_id) {
                    kumaSel.value = String(i);
                    break;
                }
            }
        });
    }).catch(function(err: unknown) {
        if (err instanceof Error && err.message === 'not authenticated') return;
        toast('Create failed: ' + (err instanceof Error ? err.message : String(err)), 'error');
    });
}

export function setupLinkEditorListeners(): void {
    document.getElementById('link-save-btn')!.addEventListener('click', saveServiceLink);
    document.getElementById('link-unlink-btn')!.addEventListener('click', unlinkServiceLink);
    document.getElementById('link-refresh-btn')!.addEventListener('click', refreshServiceLinkDetails);
    document.getElementById('link-npm-create-btn')!.addEventListener('click', createNPMHostFromLink);
    document.getElementById('link-kuma-create-btn')!.addEventListener('click', createKumaMonitorFromLink);
}
