import { describe, it, expect } from 'vitest';
import { esc, emptyRow, skeletonCell, skeletonRows } from './api';

describe('esc', () => {
    it('escapes HTML metacharacters', () => {
        expect(esc('<a href="x">&</a>')).toBe('&lt;a href=&quot;x&quot;&gt;&amp;&lt;/a&gt;');
    });
    it('coerces non-strings', () => {
        expect(esc(42 as unknown as string)).toBe('42');
    });
});

describe('skeletonRows', () => {
    it('defaults to 5 rows with cycling widths', () => {
        const html = skeletonRows(3);
        expect((html.match(/skeleton-row/g) || []).length).toBe(5);
        expect((html.match(/<td>/g) || []).length).toBe(15);
        expect(html).toContain('width:40%');
        expect(html).toContain('width:55%');
        expect(html).toContain('width:30%');
    });
    it('honours an explicit count', () => {
        expect((skeletonRows(2, 2).match(/skeleton-row/g) || []).length).toBe(2);
    });
});

describe('emptyRow', () => {
    it('renders colspan and escapes the message', () => {
        const html = emptyRow(6, 'No <x>');
        expect(html).toContain('colspan="6"');
        expect(html).toContain('No &lt;x&gt;');
    });
});

describe('skeletonCell', () => {
    it('defaults width to 60%', () => {
        expect(skeletonCell()).toContain('width:60%');
    });
});
