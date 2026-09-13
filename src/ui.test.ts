import { describe, it, expect, beforeEach } from 'vitest';
import { channelUrlPlaceholder } from './settings';
import { setHealthDot } from './stats';
import { applyEink, isEinkMode, refreshDue, toggleEink } from './eink';

describe('channelUrlPlaceholder', () => {
    it('returns a per-type hint', () => {
        expect(channelUrlPlaceholder('ntfy')).toContain('ntfy');
        expect(channelUrlPlaceholder('discord')).toContain('discord.com');
    });
    it('falls back to URL for unknown types', () => {
        expect(channelUrlPlaceholder('mystery')).toBe('URL');
    });
});

describe('setHealthDot', () => {
    it('sets healthy / error / unknown classes and titles', () => {
        document.body.innerHTML = '<span id="d"></span>';
        const dot = document.getElementById('d')!;

        setHealthDot('d', true);
        expect(dot.className).toBe('health-dot healthy');
        expect(dot.title).toBe('Connected');

        setHealthDot('d', false);
        expect(dot.className).toBe('health-dot error');
        expect(dot.title).toBe('Connection error');

        setHealthDot('d', null);
        expect(dot.className).toBe('health-dot unknown');
        expect(dot.title).toBe('Not configured');
    });
    it('no-ops when the element is missing', () => {
        expect(() => setHealthDot('does-not-exist', true)).not.toThrow();
    });
});

describe('eink', () => {
    beforeEach(() => {
        applyEink(false, false);
        document.cookie = 'eink=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT';
    });

    it('applies e-ink classes and button label', () => {
        document.body.innerHTML = '<button id="btn-eink"></button>';
        applyEink(true, true);
        expect(document.documentElement.classList.contains('eink-mode')).toBe(true);
        expect(document.documentElement.classList.contains('eink-wallboard')).toBe(true);
        expect(document.getElementById('btn-eink')!.textContent).toBe('E-ink: On');
    });

    it('toggleEink flips mode and persists the cookie', () => {
        toggleEink();
        expect(isEinkMode()).toBe(true);
        expect(document.cookie).toContain('eink=1');
        toggleEink();
        expect(isEinkMode()).toBe(false);
        expect(document.cookie).not.toContain('eink=1');
    });

    it('allows refresh when not in e-ink mode', () => {
        expect(refreshDue()).toBe(true);
    });
});
