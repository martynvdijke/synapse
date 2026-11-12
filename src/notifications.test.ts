import { describe, it, expect } from 'vitest';
import {
    urlBase64ToUint8Array,
    normalizeNotificationPreferences,
    formatSSEEvent,
    formatBadgeCount,
} from './notifications';

describe('urlBase64ToUint8Array', () => {
    it('decodes unpadded base64', () => {
        expect(Array.from(urlBase64ToUint8Array('aGVsbG8'))).toEqual([104, 101, 108, 108, 111]);
    });

    it('accepts standard padding', () => {
        expect(Array.from(urlBase64ToUint8Array('aGVsbG8='))).toEqual([104, 101, 108, 108, 111]);
    });

    it('translates url-safe characters', () => {
        expect(Array.from(urlBase64ToUint8Array('-_8'))).toEqual([251, 255]);
    });

    it('pads short input and ignores whitespace', () => {
        expect(Array.from(urlBase64ToUint8Array(' QQ \n'))).toEqual([65]);
    });

    it('returns an empty array for empty input', () => {
        expect(urlBase64ToUint8Array('').length).toBe(0);
    });
});

describe('normalizeNotificationPreferences', () => {
    it('falls back to defaults for missing input', () => {
        expect(normalizeNotificationPreferences(null)).toEqual({
            enabled: true,
            web_push: false,
            fcm: false,
            new_item: true,
        });
        expect(normalizeNotificationPreferences('nope')).toEqual({
            enabled: true,
            web_push: false,
            fcm: false,
            new_item: true,
        });
    });

    it('keeps provided booleans and defaults the rest', () => {
        expect(normalizeNotificationPreferences({ enabled: false, web_push: true })).toEqual({
            enabled: false,
            web_push: true,
            fcm: false,
            new_item: true,
        });
    });

    it('ignores non-boolean values', () => {
        expect(normalizeNotificationPreferences({ enabled: 'yes', fcm: 1 })).toEqual({
            enabled: true,
            web_push: false,
            fcm: false,
            new_item: true,
        });
    });

    it('unwraps a full GET payload', () => {
        expect(normalizeNotificationPreferences({ preferences: { fcm: true, new_item: false } })).toEqual({
            enabled: true,
            web_push: false,
            fcm: true,
            new_item: false,
        });
    });
});

describe('formatSSEEvent', () => {
    it('parses a data frame', () => {
        var raw = 'data: {"id":"evt_12","type":"new_item","title":"Service added","body":"grafana","url":"/","category":"service","at":"2026-11-12T10:00:00Z"}';
        expect(formatSSEEvent(raw)).toEqual({
            id: 'evt_12',
            type: 'new_item',
            title: 'Service added',
            body: 'grafana',
            url: '/',
            category: 'service',
            at: '2026-11-12T10:00:00Z',
        });
    });

    it('parses bare JSON', () => {
        var event = formatSSEEvent('{"id":"3","title":"Alert rule added"}');
        expect(event).not.toBeNull();
        expect(event!.id).toBe('3');
        expect(event!.type).toBe('new_item');
        expect(event!.title).toBe('Alert rule added');
    });

    it('joins multi-line data frames', () => {
        var event = formatSSEEvent('data: {"id":"7",\ndata: "type":"new_item"}');
        expect(event).not.toBeNull();
        expect(event!.id).toBe('7');
    });

    it('fills defaults for missing fields', () => {
        var event = formatSSEEvent('data: {"id":"9"}');
        expect(event).not.toBeNull();
        expect(event!.id).toBe('9');
        expect(event!.title).toBe('New item');
        expect(event!.body).toBe('');
        expect(event!.url).toBe('');
    });

    it('converts numeric ids and tolerates missing ids', () => {
        expect(formatSSEEvent('{"id":42}')!.id).toBe('42');
        expect(formatSSEEvent('{"title":"no id"}')!.id).toBe('');
    });

    it('returns null for heartbeats, empty payloads and bad JSON', () => {
        expect(formatSSEEvent(': keep-alive')).toBeNull();
        expect(formatSSEEvent('data: ')).toBeNull();
        expect(formatSSEEvent('data: {not json}')).toBeNull();
        expect(formatSSEEvent('')).toBeNull();
        expect(formatSSEEvent('data: []')).toBeNull();
    });
});

describe('formatBadgeCount', () => {
    it('hides at zero and below', () => {
        expect(formatBadgeCount(0)).toBe('');
        expect(formatBadgeCount(-3)).toBe('');
        expect(formatBadgeCount(NaN)).toBe('');
    });

    it('prints small counts', () => {
        expect(formatBadgeCount(1)).toBe('1');
        expect(formatBadgeCount(99)).toBe('99');
        expect(formatBadgeCount(4.9)).toBe('4');
    });

    it('caps large counts', () => {
        expect(formatBadgeCount(100)).toBe('99+');
        expect(formatBadgeCount(1234)).toBe('99+');
    });
});
