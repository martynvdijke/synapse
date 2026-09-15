// Uptime Kuma monitor tag helpers (pure, unit-tested).
import type { MonitorTag, TagInput } from './types';
import { esc } from './api';

export function renderTagChips(tags?: MonitorTag[]): string {
    if (!tags || !tags.length) return '<span class="text-muted">—</span>';
    return tags.map(function(t) {
        var label = esc(t.name || String(t.id));
        if (t.value) label += ':' + esc(t.value);
        if (t.color) {
            return '<span class="badge me-1" style="background-color:' + esc(t.color) + ';color:#fff">' + label + '</span>';
        }
        return '<span class="badge bg-dark me-1">' + label + '</span>';
    }).join('');
}

export function parseTagsInput(raw: string): TagInput[] {
    if (!raw.trim()) return [];
    var parts = raw.split(',').map(function(s){ return s.trim(); }).filter(function(s){ return s.length>0; });
    var out: TagInput[] = [];
    for (var i=0;i<parts.length;i++) {
        var p = parts[i];
        var n = parseInt(p, 10);
        if (!isNaN(n) && String(n) === p) { out.push({ id: n }); continue; }
        if (!isNaN(Number(p)) && Number.isInteger(Number(p))) { out.push({ id: Number(p) }); continue; }
        // fallback to name object
        out.push({ name: p });
    }
    return out;
}

export function tagsEqual(a?: MonitorTag[], b?: TagInput[]): boolean {
    var aIds = (a||[]).map(function(t){return String(t.id);}).sort().join(',');
    var bIds = (b||[]).map(function(o: TagInput){return String((o as {id?:number}).id ?? (o as {name?:string}).name ?? '');}).sort().join(',');
    return aIds===bIds;
}
