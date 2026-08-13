import { test, expect } from '@playwright/test';
import { setupBaseMocks } from './helpers';

test.describe('Notifications settings', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await page.goto('/');
    // Wait for stats to load
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('notifications section loads saved settings', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await expect(page.getByRole('heading', { name: 'Notifications' })).toBeVisible();
    await expect(page.locator('#s-notify-enabled')).toBeChecked();
    await expect(page.locator('#s-notify-interval')).toHaveValue('30');
    await expect(page.locator('#s-gotify-url')).toHaveValue('http://gotify:8080');
    await expect(page.locator('#s-gotify-token')).toHaveValue('****');
    await expect(page.locator('#s-gotify-priority')).toHaveValue('5');
  });

  test('saving settings includes notification fields', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await page.waitForFunction(() => {
      const el = document.getElementById('s-gotify-url') as HTMLInputElement;
      return el && el.value !== '';
    }, { timeout: 10000 });

    await page.fill('#s-notify-interval', '15');

    const savePromise = page.waitForResponse(
      (resp) => resp.url().includes('/api/settings') && resp.request().method() === 'POST'
    );
    await page.click('#settings-form button[type="submit"]');

    const response = await savePromise;
    const body = JSON.parse(response.request().postData() || '{}');
    expect(body.notify_enabled).toBe(true);
    expect(body.notify_interval_minutes).toBe(15);
    expect(body.gotify_url).toBe('http://gotify:8080');
  });

  test('send test notification calls the API and shows a success toast', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await expect(page.locator('#btn-notify-test')).toBeVisible();
    const testPromise = page.waitForResponse(
      (resp) => resp.url().includes('/api/notify/test') && resp.request().method() === 'POST'
    );
    await page.click('#btn-notify-test');

    const response = await testPromise;
    expect(response.status()).toBe(200);
    await expect(page.locator('.toast-msg.toast-success')).toContainText('Test notification sent');
  });

  test('preview lists items missing from Uptime Kuma', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await expect(page.locator('#notify-missing-list')).toContainText('synapse-api-1');
    await expect(page.locator('#notify-missing-list')).toContainText('synapse-db-1');
    await expect(page.locator('#notify-missing-list')).toContainText('api.example.com (npm-edge)');
  });
});
