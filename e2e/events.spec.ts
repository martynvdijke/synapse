import { test, expect } from '@playwright/test';
import { setupBaseMocks } from './helpers';

const MOCK_EVENTS = [
  { time: new Date().toISOString(), kind: 'reconcile', title: 'Reconcile completed', detail: 'added 1, updated 0, skipped 2, failed 0', status: 'completed' },
  { time: new Date(Date.now() - 3600000).toISOString(), kind: 'docker', title: 'web-app died', detail: 'nginx:latest', status: 'die' },
  { time: new Date(Date.now() - 7200000).toISOString(), kind: 'docker', title: 'web-app health_status', detail: 'container', status: 'unhealthy' },
];

const MOCK_RECONCILE_RESULT = {
  changes: [
    { service: 'web', target: 'npm', action: 'created', detail: 'created proxy host for app.example.com' },
    { service: 'web', target: 'kuma', action: 'created', detail: 'created http monitor' },
  ],
  dry_run: true,
  run: { id: 42, source: 'reconcile', status: 'completed', started_at: new Date().toISOString(), added: 2, updated: 0, skipped: 1, failed: 0, dry_run: true, error_message: '' },
};

test.describe('Events tab', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await page.route('**/api/events', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_EVENTS) });
    });
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('events tab renders unified feed with kind badges', async ({ page }) => {
    await page.click('#tab-btn-events');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('events-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    const rows = page.locator('#events-tbody tr');
    await expect(rows).toHaveCount(3);
    // Kind badges: reconcile (bg-info), docker (bg-primary)
    await expect(rows.first().locator('.badge').first()).toHaveClass(/bg-info/);
    await expect(rows.nth(1).locator('.badge').first()).toHaveClass(/bg-primary/);
    // Status badges
    await expect(rows.first().locator('.badge').nth(1)).toContainText('completed');
    await expect(rows.nth(2).locator('.badge').nth(1)).toContainText('unhealthy');
  });

  test('events tab shows title and detail text', async ({ page }) => {
    await page.click('#tab-btn-events');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('events-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    await expect(page.locator('#events-tbody tr').first()).toContainText('Reconcile completed');
    await expect(page.locator('#events-tbody tr').nth(1)).toContainText('web-app died');
  });

  test('events tab shows empty state when no events', async ({ page }) => {
    await page.route('**/api/events', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    });
    await page.click('#tab-btn-events');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('events-tbody');
      return tbody && !tbody.querySelector('.skeleton-row');
    }, { timeout: 10000 });
    await expect(page.locator('#events-tbody')).toContainText('No events recorded yet');
  });
});

test.describe('Reconcile control (Docker tab)', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await page.route('**/api/reconcile', async (route) => {
      const body = JSON.parse(route.request().postData() || '{}');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ...MOCK_RECONCILE_RESULT, dry_run: body.dry_run !== false }),
      });
    });
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('dry run checkbox is checked by default', async ({ page }) => {
    await page.click('#tab-btn-docker');
    const checkbox = page.locator('#reconcile-dry-run');
    await expect(checkbox).toBeChecked();
  });

  test('running reconcile with dry run posts dry_run=true and shows result', async ({ page }) => {
    await page.click('#tab-btn-docker');
    let postedBody: Record<string, unknown> | null = null;
    await page.route('**/api/reconcile', async (route) => {
      postedBody = JSON.parse(route.request().postData() || '{}');
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_RECONCILE_RESULT) });
    });
    await page.click('#btn-reconcile');
    await expect(page.locator('#reconcile-result')).toContainText('2 change(s)', { timeout: 10000 });
    await expect(page.locator('#reconcile-result')).toContainText('dry run');
    expect(postedBody).not.toBeNull();
    expect(postedBody!.dry_run).toBe(true);
  });

  test('unchecking dry run and filtering by service posts dry_run=false and service', async ({ page }) => {
    await page.click('#tab-btn-docker');
    let postedBody: Record<string, unknown> | null = null;
    await page.route('**/api/reconcile', async (route) => {
      postedBody = JSON.parse(route.request().postData() || '{}');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ...MOCK_RECONCILE_RESULT, dry_run: false }),
      });
    });
    await page.uncheck('#reconcile-dry-run');
    await page.fill('#reconcile-service', 'web');
    await page.click('#btn-reconcile');
    await expect(page.locator('#reconcile-result')).toContainText('2 change(s)', { timeout: 10000 });
    expect(postedBody).not.toBeNull();
    expect(postedBody!.dry_run).toBe(false);
    expect(postedBody!.service).toBe('web');
  });
});
