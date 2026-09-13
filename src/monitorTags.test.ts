import { describe, it, expect } from 'vitest';
import { parseTagsInput, tagsEqual, renderTagChips } from './monitorTags';
import type { MonitorTag } from './types';

describe('parseTagsInput', () => {
    it('returns empty for blank input', () => {
        expect(parseTagsInput('   ')).toEqual([]);
    });
    it('parses numeric ids', () => {
        expect(parseTagsInput('1, 2, 3')).toEqual([{ id: 1 }, { id: 2 }, { id: 3 }]);
    });
    it('falls back to names for non-numeric tokens', () => {
        expect(parseTagsInput('1, foo, 2a')).toEqual([{ id: 1 }, { name: 'foo' }, { name: '2a' }]);
    });
});

describe('tagsEqual', () => {
    it('compares by id regardless of order', () => {
        const a: MonitorTag[] = [{ id: 2, name: 'b' }, { id: 1, name: 'a' }];
        const b = [{ id: 1 }, { id: 2 }];
        expect(tagsEqual(a, b)).toBe(true);
    });
    it('detects differences', () => {
        expect(tagsEqual([{ id: 1, name: 'a' }], [{ id: 2 }])).toBe(false);
    });
    it('treats missing as empty', () => {
        expect(tagsEqual(undefined, [])).toBe(true);
        expect(tagsEqual([{ id: 1, name: 'a' }], undefined)).toBe(false);
    });
});

describe('renderTagChips', () => {
    it('renders a dash when there are no tags', () => {
        expect(renderTagChips()).toContain('\u2014');
        expect(renderTagChips([])).toContain('\u2014');
    });
    it('escapes colours to prevent attribute injection', () => {
        const html = renderTagChips([{ id: 1, name: 'x', color: 'red" onmouseover="alert(1)' }]);
        expect(html).not.toContain('onmouseover="alert');
        expect(html).toContain('&quot;');
    });
    it('renders value suffix and unnamed ids', () => {
        expect(renderTagChips([{ id: 7, name: '', value: 'v' }])).toContain('7:v');
    });
});
