import { describe, it, expect } from 'vitest';
import { durationStr, levelBadge, renderLogRow } from './logs';
import type { LogEntry } from './types';

describe('durationStr', () => {
    it('formats each magnitude', () => {
        expect(durationStr(0)).toBe('');
        expect(durationStr(-5)).toBe('');
        expect(durationStr(999)).toBe('999ns');
        expect(durationStr(1000)).toBe('1.0\u00b5s');
        expect(durationStr(1500)).toBe('1.5\u00b5s');
        expect(durationStr(1000000)).toBe('1.0ms');
        expect(durationStr(1500000)).toBe('1.5ms');
        expect(durationStr(1000000000)).toBe('1.00s');
        expect(durationStr(2500000000)).toBe('2.50s');
    });
});

describe('levelBadge', () => {
    it('maps known levels', () => {
        expect(levelBadge('ERROR')).toContain('bg-danger');
        expect(levelBadge('WARN')).toContain('bg-warning');
        expect(levelBadge('INFO')).toContain('bg-primary');
        expect(levelBadge('DEBUG')).toContain('bg-secondary');
    });
    it('falls back for unknown levels', () => {
        expect(levelBadge('TRACE')).toBe('<span class="badge bg-secondary">TRACE</span>');
    });
});

const entry: LogEntry = {
    timestamp: '2026-01-01T00:00:00.000Z',
    level: 'INFO',
    source: 'api',
    message: '<hi>',
    duration: 1500,
    error: '',
    error_kind: '',
    metadata: {},
};

describe('renderLogRow', () => {
    it('escapes untrusted fields', () => {
        const html = renderLogRow(entry, false);
        expect(html).toContain('&lt;hi&gt;');
        expect(html).not.toContain('<hi>');
    });
    it('marks SSE rows as new', () => {
        expect(renderLogRow(entry, true)).toContain('log-row-new');
        expect(renderLogRow(entry, false)).not.toContain('log-row-new');
    });
    it('omits the metadata row when empty and includes it when present', () => {
        expect(renderLogRow(entry, false)).not.toContain('log-meta-row');
        const withMeta = renderLogRow({ ...entry, metadata: { a: 1 } }, false);
        expect(withMeta).toContain('log-meta-row');
        expect(withMeta).toContain('&quot;a&quot;');
    });
});
