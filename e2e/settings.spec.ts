import { test, expect } from '@playwright/test';
import { setupBaseMocks, setupTestConnectionMocks } from './helpers';

test.describe('Settings tab', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await setupTestConnectionMocks(page);
    await page.goto('/');
    // Wait for stats to load
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('settings form loads with correct values', async ({ page }) => {
    await page.click('#tab-btn-settings');

    // Wait for loadSettings() to populate fields
    await page.waitForFunction(() => {
      const el = document.getElementById('s-kuma-url') as HTMLInputElement;
      return el && el.value === 'http://uptime-kuma:3001';
    }, { timeout: 10000 });

    await expect(page.locator('#s-kuma-url')).toHaveValue('http://uptime-kuma:3001');
    await expect(page.locator('#s-kuma-user')).toHaveValue('admin');
    await expect(page.locator('#s-npm-host')).toHaveValue('http://nginx:81');
    await expect(page.locator('#s-npm-user')).toHaveValue('admin');
    await expect(page.locator('#s-compose-path')).toHaveValue('/opt/synapse/docker-compose.yml');
    await expect(page.locator('#s-auth-config-path')).toHaveValue('/config/configuration.yml');
    await expect(page.locator('#s-auth-db-path')).toHaveValue('/config/db.sqlite3');
    await expect(page.locator('#s-auth-sync-enabled')).toBeChecked();
    await expect(page.locator('#s-auth-default-policy')).toHaveValue('one_factor');
    await expect(page.locator('#s-auth-overrides')).toHaveValue('{"admin.example.com":"bypass"}');
  });

  test('settings save makes POST with correct data', async ({ page }) => {
    await page.click('#tab-btn-settings');

    // Wait for form to populate
    await page.waitForFunction(() => {
      const el = document.getElementById('s-kuma-url') as HTMLInputElement;
      return el && el.value === 'http://uptime-kuma:3001';
    }, { timeout: 10000 });

    // Modify a field
    await page.fill('#s-kuma-url', 'http://new-kuma:3001');

    // Listen for the POST
    const savePromise = page.waitForResponse(
      (resp) => resp.url().includes('/api/settings') && resp.request().method() === 'POST'
    );

    // Click Save
    await page.click('#settings-form button[type="submit"]');

    const response = await savePromise;
    const body = JSON.parse(response.request().postData() || '{}');
    expect(body.kuma_url).toBe('http://new-kuma:3001');
    expect(body.authelia_sync_enabled).toBe(true);
  });

  test('test kuma connection button works', async ({ page }) => {
    await page.click('#tab-btn-settings');

    // Wait for form to populate
    await page.waitForFunction(() => {
      const el = document.getElementById('s-kuma-url') as HTMLInputElement;
      return el && el.value === 'http://uptime-kuma:3001';
    }, { timeout: 10000 });

    await page.click('#btn-test-kuma');

    // Should show OK result
    await expect(page.locator('#test-kuma-result')).toContainText('OK');
  });

  test('test npm connection button shows failure', async ({ page }) => {
    await page.click('#tab-btn-settings');

    // Wait for form to populate
    await page.waitForFunction(() => {
      const el = document.getElementById('s-kuma-url') as HTMLInputElement;
      return el && el.value === 'http://uptime-kuma:3001';
    }, { timeout: 10000 });

    await page.click('#btn-test-npm');

    // Should show failure message
    await expect(page.locator('#test-npm-result')).toContainText('Connection failed');
  });

  test('saved indicator appears after save', async ({ page }) => {
    await page.click('#tab-btn-settings');

    // Wait for form to populate
    await page.waitForFunction(() => {
      const el = document.getElementById('s-kuma-url') as HTMLInputElement;
      return el && el.value === 'http://uptime-kuma:3001';
    }, { timeout: 10000 });

    await page.click('#settings-form button[type="submit"]');

    // Check for success toast
    await expect(page.locator('.toast-msg.toast-success')).toContainText('Settings saved');
  });
});
