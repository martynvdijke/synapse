import { test, expect } from '@playwright/test';
import { setupBaseMocks } from './helpers';

test.describe('Logs tab', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('loads log entries when tab is clicked', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-logs"]');

    // Wait for log rows to render
    await page.waitForFunction(() => {
      const tbody = document.getElementById('logs-tbody');
      return tbody && tbody.querySelectorAll('tr').length > 0;
    }, { timeout: 10000 });

    const rows = page.locator('#logs-tbody tr');
    const count = await rows.count();
    expect(count).toBeGreaterThan(0);
  });

  test('displays individual log entries from mock data', async ({ page }) => {
    // Listen for the actual log request and verify it's handled
    await page.click('button[data-bs-target="#tab-logs"]');

    // Wait for real data rows (not skeleton)
    await page.waitForFunction(() => {
      const tbody = document.getElementById('logs-tbody');
      if (!tbody) return false;
      const trs = tbody.querySelectorAll('tr');
      for (let i = 0; i < trs.length; i++) {
        if (!trs[i].classList.contains('skeleton-row') && !trs[i].querySelector('.skeleton')) {
          return true;
        }
      }
      return false;
    }, { timeout: 10000 });

    // Should see known entries from mock data
    await expect(page.locator('#logs-tbody')).toContainText('Synapse started');
  });

  test('clear and refresh reloads log entries', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-logs"]');

    await page.waitForFunction(() => {
      const tbody = document.getElementById('logs-tbody');
      return tbody && tbody.querySelectorAll('tr').length > 0;
    }, { timeout: 10000 });

    // Click clear — resets state
    await page.click('#btn-log-clear');

    // Click refresh — should reload
    await page.click('#btn-log-refresh');
    await page.waitForTimeout(1500);

    // Should have entries again
    const rows = page.locator('#logs-tbody tr');
    const count = await rows.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });
});
