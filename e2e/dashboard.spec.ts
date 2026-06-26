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
    await expect(page.locator('#stat-npm')).toContainText('2/2');
    await expect(page.locator('#stat-monitors')).toHaveText('12');
    await expect(page.locator('#stat-status')).toContainText('Idle');
    await expect(page.locator('#stat-authelia')).toContainText('Not configured');
  });

  test('npm stat shows partial health when instance fails', async ({ page }) => {
    // Override the status mock to simulate a failed NPM instance
    await page.route('**/api/status', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...MOCK_STATUS, connection_health: { ...MOCK_STATUS.connection_health, npm: { ok: false, instances: [{ id: 1, name: 'npm-edge', ok: false, last_error: 'connection refused' }, { id: 2, name: 'npm-internal', ok: true }] } } }) });
    });
    // Reload to pick up new mock
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-npm');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
    await expect(page.locator('#stat-npm')).toContainText('1/2');
  });

  test('stat cards have aria-labels', async ({ page }) => {
    const dockerCard = page.locator('#stat-docker').first();
    await expect(dockerCard).toBeVisible();
    // The card ancestor has aria-label
    const cards = page.locator('[aria-label="Service statistics"] .stat-card');
    await expect(cards.first()).toHaveAttribute('aria-label');
  });
});

test.describe('Dashboard — interactions', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
  });

  test('clicking docker stat card switches to docker tab', async ({ page }) => {
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });

    // Click the docker stat card
    await page.click('#stat-docker-card');

    // Wait for Docker tab to be shown
    await page.waitForTimeout(500);
    await expect(page.locator('#tab-docker')).toHaveClass(/show active/);
  });

  test('npm stat shows health count with all healthy', async ({ page }) => {
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-npm');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
    await expect(page.locator('#stat-npm')).toContainText('2/2');
    // Health dot should be green (class "healthy")
    await expect(page.locator('#health-npm')).toHaveClass(/healthy/);
  });

  test('kuma monitors stat shows count', async ({ page }) => {
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-monitors');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
    await expect(page.locator('#stat-monitors')).toHaveText('12');
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
