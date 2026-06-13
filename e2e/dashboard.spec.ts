import { test, expect } from '@playwright/test';
import { setupBaseMocks, MOCK_STATUS } from './helpers';

test.describe('Dashboard — stat cards', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await page.goto('/');
    // Wait for stat content to replace skeleton
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('stat cards render with correct values', async ({ page }) => {
    await expect(page.locator('#stat-docker')).toHaveText('5');
    await expect(page.locator('#stat-npm')).toHaveText('8');
    await expect(page.locator('#stat-monitors')).toHaveText('12');
    await expect(page.locator('#stat-status')).toContainText('Idle');
    await expect(page.locator('#stat-authelia')).toContainText('Not configured');
  });

  test('npm stat shows warning icon on error', async ({ page }) => {
    // Override the status mock to simulate npm_error
    await page.route('**/api/status', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...MOCK_STATUS, npm_error: true }) });
    });
    // Reload to pick up new mock
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-npm');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
    await expect(page.locator('#stat-npm')).toContainText('\u26A0');
  });

  test('stat cards have aria-labels', async ({ page }) => {
    const dockerCard = page.locator('#stat-docker').first();
    await expect(dockerCard).toBeVisible();
    // The card ancestor has aria-label
    const cards = page.locator('[aria-label="Service statistics"] .stat-card');
    await expect(cards.first()).toHaveAttribute('aria-label');
  });
});

test.describe('Dashboard — initial state', () => {
  test('page title is correct', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveTitle(/Synapse/);
  });

  test('nav bar shows Synapse brand', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.navbar-brand')).toContainText('Synapse');
  });

  test('logout button is visible', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#btn-logout')).toBeVisible();
  });

  test('sync controls are visible', async ({ page }) => {
    await setupBaseMocks(page);
    await page.goto('/');
    await expect(page.locator('#btn-docker')).toBeVisible();
    await expect(page.locator('#btn-npm')).toBeVisible();
  });
});
