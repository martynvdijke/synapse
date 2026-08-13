// Global function declarations for cross-module functions set on window
// These allow bare function calls (e.g., apiFetch(...)) throughout the codebase.

// ── api.js ──────────────────────────────────────────────────────
declare function esc(s: string): string;
declare function apiFetch(url: string, opts?: RequestInit): Promise<Response>;
declare function logout(): void;
declare function emptyRow(colspan: number, msg: string): string;
declare function loadingRow(colspan: number): string;

// ── toast.js ────────────────────────────────────────────────────
declare function toast(msg: string, type?: string): void;
declare function setLoading(btnId: string, loading: boolean): void;

// ── stats.js ────────────────────────────────────────────────────
declare function loadStatus(): void;
declare function refreshAll(): void;

// ── tabs.js ─────────────────────────────────────────────────────
declare function toggleDockerDetail(row: HTMLElement): void;
declare function toggleLogMeta(row: HTMLElement): void;
declare function loadDockerServices(): void;
declare function loadKumaMonitors(): void;
declare function loadNPMProxies(): void;
declare function loadHistory(): void;

// ── settings.js ─────────────────────────────────────────────────
declare function loadSettings(): void;
declare function saveSettings(e: Event): void;
declare function copyTrmnlUrl(btn: HTMLElement): void;
declare function testConnection(service: string): void;
declare function notifyTest(): void;
declare function loadNotifyMissing(): void;
declare function loadKumaInstances(): void;
declare function showKumaInstanceForm(editId: number | null): void;
declare function hideKumaInstanceForm(): void;
declare function saveKumaInstance(): void;
declare function deleteKumaInstance(id: number, name: string): void;
declare function testKumaInstance(id: number): void;
declare function editKumaInstance(id: number): void;
declare function loadNPMInstances(): void;
declare function showNPMInstanceForm(editId: number | null): void;
declare function hideNPMInstanceForm(): void;
declare function saveNPMInstance(): void;
declare function deleteNPMInstance(id: number, name: string): void;
declare function testNPMInstance(id: number): void;
declare function editNPMInstance(id: number): void;
declare function loadAutheliaInstances(): void;
declare function showAutheliaInstanceForm(editId: number | null): void;
declare function hideAutheliaInstanceForm(): void;
declare function saveAutheliaInstance(): void;
declare function deleteAutheliaInstance(id: number, name: string): void;
declare function testAutheliaInstance(id: number): void;
declare function editAutheliaInstance(id: number): void;

// ── sse.js ──────────────────────────────────────────────────────
declare function connectSSE(): void;

// ── authelia.js ─────────────────────────────────────────────────
declare function loadAutheliaInstanceSelector(): void;
declare function loadAutheliaDashboard(): void;
declare function loadAutheliaStatus(): void;
declare function loadAutheliaAlerts(): void;
declare function resolveAlert(id: number): void;
declare function loadAutheliaTempAccess(): void;
declare function revokeTempAccess(id: number): void;
declare function runAutheliaSync(dryRun: boolean): void;
declare function onInstanceSelectorChange(): void;

// ── logs.js ─────────────────────────────────────────────────────
declare function setupLogFilters(): void;
declare function loadLogs(append: boolean): void;
declare function connectLogSSE(): void;

// ── main.js ─────────────────────────────────────────────────────
declare function startSync(source: string): void;

// ── eink.js ─────────────────────────────────────────────────────
declare function toggleEink(): void;

// Bootstrap (loaded from CDN via HTML)
declare namespace bootstrap {
  class Modal {
    constructor(element: Element, options?: Record<string, unknown>);
    show(): void;
    hide(): void;
  }
  class Tab {
    constructor(element: Element);
    show(): void;
    static getInstance(element: Element): Tab | null;
  }
}
