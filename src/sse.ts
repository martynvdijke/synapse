// SSE connection for sync progress
import type { ProgressEvent } from './types';
import { refreshDue } from './eink';

export function connectSSE(): void {
    var retryDelay = 1000;
    function connect() {
        var es = new EventSource('/api/sync/progress');
        es.onmessage = function(e: MessageEvent) {
            retryDelay = 1000;
            var p: ProgressEvent = JSON.parse(e.data);
            var area = document.getElementById('progress-area')!;
            area.classList.add('active');
            var pct = p.total > 0 ? Math.round((p.current / p.total) * 100) : 0;
            document.getElementById('progress-bar')!.style.width = pct + '%';
            document.getElementById('progress-pct')!.textContent = pct + '%';
            document.getElementById('progress-msg')!.textContent = p.message;
            document.getElementById('count-added')!.textContent = '' + p.added;
            document.getElementById('count-skipped')!.textContent = '' + p.skipped;
            document.getElementById('count-failed')!.textContent = '' + p.failed;
            if (p.status === 'completed' || p.status === 'completed_with_errors' || p.status === 'error') {
                (document.getElementById('btn-docker') as HTMLButtonElement).disabled = false;
                (document.getElementById('btn-npm') as HTMLButtonElement).disabled = false;
                document.getElementById('stat-status')!.innerHTML = '<span class="badge bg-secondary">Idle</span>';
                if (refreshDue()) setTimeout(refreshAll, 500);
            }
        };
        es.onerror = function() {
            es.close();
            setTimeout(connect, retryDelay);
            retryDelay = Math.min(retryDelay * 2, 30000);
        };
    }
    connect();
}

// forward ref (set by main.js)
var refreshAll: () => void;
export function setRefreshAll(fn: () => void) { refreshAll = fn; }

window.connectSSE = connectSSE;
