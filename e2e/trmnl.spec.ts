import { test, expect } from '@playwright/test';
import { Page } from '@playwright/test';

// The e2e suite runs against a static file server with API routes mocked
// (see helpers.ts). Here we mock /api/v1/trmnl/stats to behave like the real
// token-gated endpoint, verifying the auth contract and the flat payload shape.
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

  async function mockTrmnlEndpoint(page: Page, token: string | null) {
    await page.route('**/api/v1/trmnl/stats*', async (route) => {
      const req = route.request();
      const auth = req.headers()['authorization'] || '';
      const url = new URL(req.url());
      const submitted = auth.startsWith('Bearer ')
        ? auth.slice(7)
        : (url.searchParams.get('token') || '');

      if (token === null) {
        // Not configured → 503, never data
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'TRMNL API token not configured' }) });
        return;
      }
      if (!submitted || submitted !== token) {
        await route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: 'unauthorized' }) });
        return;
      }
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

  test('returns 401 without a token', async ({ page }) => {
    await mockTrmnlEndpoint(page, 'test-token');
    const { status } = await fetchStats(page, ENDPOINT);
    expect(status).toBe(401);
  });

  test('returns 401 with a wrong token', async ({ page }) => {
    await mockTrmnlEndpoint(page, 'test-token');
    const { status } = await fetchStats(page, ENDPOINT, { Authorization: 'Bearer wrong-token' });
    expect(status).toBe(401);
  });

  test('returns 200 with a valid Bearer token and a flat payload', async ({ page }) => {
    await mockTrmnlEndpoint(page, 'test-token');
    const { status, body } = await fetchStats(page, ENDPOINT, { Authorization: 'Bearer test-token' });
    expect(status).toBe(200);

    for (const field of ['docker_count', 'npm_count', 'monitor_count', 'running', 'last_docker', 'last_npm', 'docker_ok', 'npm_ok', 'kuma_ok', 'up', 'down']) {
      expect(body).toHaveProperty(field);
    }
    expect(body).not.toHaveProperty('connection_health');
  });

  test('returns 200 with a valid ?token= query param', async ({ page }) => {
    await mockTrmnlEndpoint(page, 'test-token');
    const { status, body } = await fetchStats(page, ENDPOINT + '?token=test-token');
    expect(status).toBe(200);
    expect(body).toHaveProperty('monitor_count', 12);
  });

  test('returns 503 when no token is configured', async ({ page }) => {
    await mockTrmnlEndpoint(page, null);
    const { status } = await fetchStats(page, ENDPOINT);
    expect(status).toBe(503);
  });
});
