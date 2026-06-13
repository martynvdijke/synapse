import { test, expect } from '@playwright/test';
import { setupBaseMocks, setupSyncMocks } from './helpers';

test.describe('Sync controls', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await setupSyncMocks(page);
    await page.goto('/');
    // Wait for stats to load
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('sync buttons are visible and enabled', async ({ page }) => {
    await expect(page.locator('#btn-docker')).toBeVisible();
    await expect(page.locator('#btn-docker')).toBeEnabled();
    await expect(page.locator('#btn-npm')).toBeVisible();
    await expect(page.locator('#btn-npm')).toBeEnabled();
  });

  test('docker sync button click shows confirmation modal', async ({ page }) => {
    await page.click('#btn-docker');

    // Modal should appear
    await expect(page.locator('#confirm-modal')).toHaveClass(/show/);
    await expect(page.locator('#confirm-modal-body')).toContainText('Start Docker Compose sync');
  });

  test('docker sync modal confirm triggers sync API call', async ({ page }) => {
    // Track if the sync API was called
    let syncCalled = false;
    await page.route('**/api/sync/docker', async (route) => {
      syncCalled = true;
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'started' }) });
    });

    await page.click('#btn-docker');
    // Wait for modal to be visible
    await page.waitForSelector('#confirm-modal.show', { timeout: 5000 });
    // Click confirm
    await page.click('#confirm-modal-ok');
    // Wait for modal to hide
    await page.waitForSelector('#confirm-modal', { state: 'hidden', timeout: 5000 });

    expect(syncCalled).toBeTruthy();
  });

  test('docker sync modal cancel does not call API', async ({ page }) => {
    let syncCalled = false;
    await page.route('**/api/sync/docker', async (route) => {
      syncCalled = true;
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'started' }) });
    });

    await page.click('#btn-docker');
    await page.waitForSelector('#confirm-modal.show', { timeout: 5000 });
    // Click cancel
    await page.click('#confirm-modal-cancel');
    await page.waitForSelector('#confirm-modal', { state: 'hidden', timeout: 5000 });

    // Give any pending requests a moment
    await page.waitForTimeout(500);
    expect(syncCalled).toBeFalsy();
  });

  test('sync sets buttons to disabled state', async ({ page }) => {
    await page.click('#btn-docker');
    await page.waitForSelector('#confirm-modal.show', { timeout: 5000 });
    await page.click('#confirm-modal-ok');
    await page.waitForSelector('#confirm-modal', { state: 'hidden', timeout: 5000 });

    // Buttons should be disabled during sync
    await expect(page.locator('#btn-docker')).toBeDisabled();
    await expect(page.locator('#btn-npm')).toBeDisabled();
  });

  test('sync shows Running status', async ({ page }) => {
    await page.click('#btn-docker');
    await page.waitForSelector('#confirm-modal.show', { timeout: 5000 });
    await page.click('#confirm-modal-ok');
    await page.waitForSelector('#confirm-modal', { state: 'hidden', timeout: 5000 });

    // Status should show Running...
    await expect(page.locator('#stat-status')).toContainText('Running');
  });

  test('npm sync flow works with modal', async ({ page }) => {
    let syncCalled = false;
    await page.route('**/api/sync/npm', async (route) => {
      syncCalled = true;
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'started' }) });
    });

    await page.click('#btn-npm');
    await page.waitForSelector('#confirm-modal.show', { timeout: 5000 });
    await expect(page.locator('#confirm-modal-body')).toContainText('Start NPM Proxy Hosts sync');
    await page.click('#confirm-modal-ok');
    await page.waitForSelector('#confirm-modal', { state: 'hidden', timeout: 5000 });

    expect(syncCalled).toBeTruthy();
  });
});
