import { test, expect } from '@playwright/test';
import { setupBaseMocks } from './helpers';

test.describe('Docker tab — services table', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('docker tab shows services', async ({ page }) => {
    await page.click('#tab-btn-docker');
    // Wait for skeleton rows to be replaced with real data rows
    await page.waitForFunction(() => {
      const tbody = document.getElementById('docker-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    const rows = page.locator('#docker-tbody tr.docker-service-row');
    await expect(rows).toHaveCount(3);
  });

  test('docker tab shows In Kuma badge for monitored service', async ({ page }) => {
    await page.click('#tab-btn-docker');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('docker-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    const firstRow = page.locator('#docker-tbody tr').first();
    await expect(firstRow).toContainText('web-app');
    await expect(firstRow.locator('.badge.bg-success')).toContainText('In Kuma');
  });

  test('docker tab shows Missing badge for unmonitored service', async ({ page }) => {
    await page.click('#tab-btn-docker');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('docker-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    const secondRow = page.locator('#docker-tbody tr.docker-service-row').nth(1);
    await expect(secondRow).toContainText('api');
    await expect(secondRow.locator('.badge.bg-secondary')).toContainText('Missing');
  });

  test('docker tab shows type badges', async ({ page }) => {
    await page.click('#tab-btn-docker');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('docker-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    // First badge in first row should be the type badge
    await expect(page.locator('#docker-tbody tr').first().locator('.badge').first()).toContainText('HTTP');
  });
});

test.describe('Kuma tab — monitors table', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('kuma tab shows monitors', async ({ page }) => {
    await page.click('#tab-btn-kuma');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('kuma-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    const rows = page.locator('#kuma-tbody tr');
    await expect(rows).toHaveCount(2);
  });

  test('kuma tab shows monitor names', async ({ page }) => {
    await page.click('#tab-btn-kuma');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('kuma-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    await expect(page.locator('#kuma-tbody tr').first()).toContainText('Web App Monitor');
    await expect(page.locator('#kuma-tbody tr').nth(1)).toContainText('API Health');
  });

  test('clicking monitor row opens detail stats panel', async ({ page }) => {
    // Mock the stats endpoint
    await page.route('**/api/monitors/*/stats', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ id: 1, status: 1, uptime_24h: 99.9, uptime_7d: 99.5, uptime_1y: 99.0, avg_ping: 45.2, last_msg: 'OK', cert_info: '' }) });
    });

    await page.click('#tab-btn-kuma');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('kuma-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });

    // Click the first monitor row
    await page.click('#kuma-tbody tr:first-child');

    // Detail panel should appear
    await expect(page.locator('#monitor-detail-panel')).not.toHaveClass(/d-none/);
    await expect(page.locator('#monitor-detail-title')).toContainText('Monitor #1');
    // Stats should load
    await expect(page.locator('#monitor-detail-body')).toContainText('99.9');
  });

  test('kuma tab shows error state when API fails', async ({ page }) => {
    await page.route('**/api/monitors', async (route) => {
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'Internal server error' }) });
    });

    await page.click('#tab-btn-kuma');
    await page.waitForTimeout(1000);

    // Should show an error message
    const tbody = page.locator('#kuma-tbody');
    await expect(tbody).toContainText('error');
  });

  test('npm tab shows error state when API fails', async ({ page }) => {
    await page.route('**/api/proxies', async (route) => {
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'Upstream error' }) });
    });

    await page.click('#tab-btn-npm');
    await page.waitForTimeout(1000);

    const tbody = page.locator('#npm-tbody');
    await expect(tbody).toContainText('error');
  });

  test('monitor detail panel close button hides it', async ({ page }) => {
    await page.route('**/api/monitors/*/stats', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ id: 1, status: 1, uptime_24h: 99.9, uptime_7d: 99.5, uptime_1y: 99.0, avg_ping: 45.2, last_msg: 'OK', cert_info: '' }) });
    });

    await page.click('#tab-btn-kuma');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('kuma-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });

    // Open panel
    await page.click('#kuma-tbody tr:first-child');
    await expect(page.locator('#monitor-detail-panel')).not.toHaveClass(/d-none/);

    // Click close
    await page.click('#monitor-detail-close');
    await expect(page.locator('#monitor-detail-panel')).toHaveClass(/d-none/);
  });
});

test.describe('NPM tab — proxies table', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('npm tab shows proxies', async ({ page }) => {
    await page.click('#tab-btn-npm');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('npm-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    const rows = page.locator('#npm-tbody tr');
    await expect(rows).toHaveCount(2);
  });

  test('npm tab shows In Kuma status', async ({ page }) => {
    await page.click('#tab-btn-npm');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('npm-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    const firstRow = page.locator('#npm-tbody tr').first();
    await expect(firstRow.locator('.badge.bg-success')).toContainText('In Kuma');
  });

  test('npm tab shows em-dash for proxy hosts without a container', async ({ page }) => {
    await page.click('#tab-btn-npm');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('npm-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelectorAll('tr').length === 2;
    }, { timeout: 10000 });
    // api.example.com (row 2) has no container in the mock — expect the em-dash placeholder.
    const secondRow = page.locator('#npm-tbody tr').nth(1);
    await expect(secondRow).toContainText('api.example.com');
    await expect(secondRow.locator('.text-muted')).toHaveText('—');
  });
});

test.describe('Sync History tab', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('history tab shows sync runs', async ({ page }) => {
    await page.click('#tab-btn-history');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('history-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    const rows = page.locator('#history-tbody tr');
    await expect(rows).toHaveCount(2);
  });

  test('history tab shows status badges', async ({ page }) => {
    await page.click('#tab-btn-history');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('history-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    // First row: second badge is status badge (first is source badge)
    const firstStatusBadge = page.locator('#history-tbody tr').first().locator('.badge').nth(1);
    await expect(firstStatusBadge).toHaveClass(/bg-success/);
    await expect(firstStatusBadge).toContainText('completed');
    // Second row: second badge is status badge
    const secondStatusBadge = page.locator('#history-tbody tr').nth(1).locator('.badge').nth(1);
    await expect(secondStatusBadge).toHaveClass(/bg-warning/);
    await expect(secondStatusBadge).toContainText('completed_with_errors');
  });

  test('history tab shows added/skipped/failed counts', async ({ page }) => {
    await page.click('#tab-btn-history');
    await page.waitForFunction(() => {
      const tbody = document.getElementById('history-tbody');
      return tbody && !tbody.querySelector('.skeleton-row') && tbody.querySelector('td[data-label]');
    }, { timeout: 10000 });
    const firstRow = page.locator('#history-tbody tr').first();
    await expect(firstRow).toContainText('3');
    await expect(firstRow).toContainText('1');
  });
});
