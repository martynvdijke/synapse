import { test, expect } from '@playwright/test';
import { setupBaseMocks, MOCK_SETTINGS } from './helpers';

test.describe('E-ink mode', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
  });

  test('activates via ?eink=1 URL param and sets a cookie', async ({ page }) => {
    await page.goto('/?eink=1');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });

    // html element carries the eink-mode class
    await expect(page.locator('html')).toHaveClass(/eink-mode/);
    // cookie persists the preference
    const cookies = await page.context().cookies();
    const eink = cookies.find((c) => c.name === 'eink');
    expect(eink).toBeDefined();
    expect(eink!.value).toBe('1');
    // toggle button reflects state
    await expect(page.locator('#btn-eink')).toHaveText('E-ink: On');
  });

  test('does not activate without param or cookie when admin setting is off', async ({ page }) => {
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });

    await expect(page.locator('html')).not.toHaveClass(/eink-mode/);
    await expect(page.locator('#btn-eink')).toHaveText('E-ink: Off');
  });

  test('toggle button flips mode and cookie', async ({ page }) => {
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });

    await page.click('#btn-eink');
    await expect(page.locator('html')).toHaveClass(/eink-mode/);
    let cookies = await page.context().cookies();
    expect(cookies.find((c) => c.name === 'eink')?.value).toBe('1');

    await page.click('#btn-eink');
    await expect(page.locator('html')).not.toHaveClass(/eink-mode/);
    cookies = await page.context().cookies();
    expect(cookies.find((c) => c.name === 'eink')).toBeUndefined();
  });

  test('?eink=1&wallboard=1 activates wallboard mode', async ({ page }) => {
    await page.goto('/?eink=1&wallboard=1');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });

    await expect(page.locator('html')).toHaveClass(/eink-mode/);
    await expect(page.locator('html')).toHaveClass(/eink-wallboard/);
  });

  test('admin eink_enabled setting activates site-wide', async ({ page }) => {
    // Override settings response to enable e-ink site-wide
    await page.route('**/api/settings', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'saved' }) });
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...MOCK_SETTINGS, eink_enabled: true }) });
      }
    });

    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });

    await expect(page.locator('html')).toHaveClass(/eink-mode/);
  });
});
