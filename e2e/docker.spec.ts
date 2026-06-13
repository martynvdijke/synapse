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
    const rows = page.locator('#docker-tbody tr');
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
    const secondRow = page.locator('#docker-tbody tr').nth(1);
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
