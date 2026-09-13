import { describe, it, expect } from 'vitest';
import { formatThreshold, typeLabel, statusBadge } from './alerts';

describe('formatThreshold', () => {
    it('renders em dash for falsey input', () => {
        expect(formatThreshold(0)).toBe('\u2014');
    });
    it('picks the largest whole unit', () => {
        expect(formatThreshold(30)).toBe('30s');
        expect(formatThreshold(60)).toBe('1m');
        expect(formatThreshold(3600)).toBe('1h');
        expect(formatThreshold(86400)).toBe('1d');
        expect(formatThreshold(86400 * 2)).toBe('2d');
    });
    it('falls back to seconds when not whole minutes', () => {
        expect(formatThreshold(90)).toBe('90s');
    });
});

describe('typeLabel', () => {
    it('maps known types and passes through unknown', () => {
        expect(typeLabel('monitor_down_for')).toBe('Monitor down for');
        expect(typeLabel('container_down')).toBe('Container down');
        expect(typeLabel('custom_thing')).toBe('custom_thing');
    });
});

describe('statusBadge', () => {
    it('maps known statuses', () => {
        expect(statusBadge('open')).toContain('bg-danger');
        expect(statusBadge('acknowledged')).toContain('bg-warning');
        expect(statusBadge('resolved')).toContain('bg-secondary');
    });
    it('escapes unknown statuses', () => {
        expect(statusBadge('<b>')).toBe('&lt;b&gt;');
    });
});
