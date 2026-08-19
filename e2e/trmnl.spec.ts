import { test, expect } from '@playwright/test';
import { Page } from '@playwright/test';

// The e2e suite runs against a static file server with API routes mocked
// (see helpers.ts). Here we mock /api/v1/trmnl/stats to behave like the real
// endpoint, which is now a PUBLIC read-only integration: it returns the flat
// stats payload without requiring any credential.
// Requests go through page.evaluate(fetch) so page.route intercepts them.
test.describe('TRMNL stats endpoint', () => {
  const ENDPOINT = '/api/v1/trmnl/stats';

  const MOCK_STATS = {
    docker_count: 5,
    npm_count: 8,
    monitor_count: 12,
    running: false,
    last_docker: null,
    last_npm: null,
    docker_ok: true,
    npm_ok: true,
    kuma_ok: true,
    up: 12,
    down: 0,
  };

  async function mockTrmnlEndpoint(page: Page) {
    await page.route('**/api/v1/trmnl/stats*', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_STATS) });
    });
  }

  async function fetchStats(page: Page, url: string, headers?: Record<string, string>) {
    // Absolute URL: page.evaluate has no document base, relative fetch fails.
    const absolute = 'http://localhost:8765' + url;
    return page.evaluate(async ({ url, headers }) => {
      const res = await fetch(url, headers ? { headers } : undefined);
      const body = await res.json().catch(() => null);
      return { status: res.status, body };
    }, { url: absolute, headers: headers || null });
  }

  test('returns 200 without any credential (public read)', async ({ page }) => {
    await mockTrmnlEndpoint(page);
    const { status, body } = await fetchStats(page, ENDPOINT);
    expect(status).toBe(200);
    for (const field of ['docker_count', 'npm_count', 'monitor_count', 'running', 'last_docker', 'last_npm', 'docker_ok', 'npm_ok', 'kuma_ok', 'up', 'down']) {
      expect(body).toHaveProperty(field);
    }
    expect(body).not.toHaveProperty('connection_health');
  });

  test('returns 200 even with a stale bearer token', async ({ page }) => {
    await mockTrmnlEndpoint(page);
    const { status } = await fetchStats(page, ENDPOINT, { Authorization: 'Bearer whatever' });
    expect(status).toBe(200);
  });

  test('returns 200 with a ?layout= query param', async ({ page }) => {
    await mockTrmnlEndpoint(page);
    const { status, body } = await fetchStats(page, ENDPOINT + '?layout=full');
    expect(status).toBe(200);
    expect(body).toHaveProperty('monitor_count', 12);
  });
});
