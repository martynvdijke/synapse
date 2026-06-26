import { test, expect } from '@playwright/test';
import { setupBaseMocks, MOCK_NPM_INSTANCES, MOCK_MONITORS } from './helpers';

test.describe('Settings tab', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await page.goto('/');
    // Wait for stats to load
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('NPM Instances section is visible with add button', async ({ page }) => {
    await page.click('#tab-btn-settings');

    // Wait for NPM instances section to appear
    await expect(page.locator('text=NPM Instances')).toBeVisible();
    await expect(page.locator('#btn-npm-add')).toBeVisible();
  });

  test('Add Instance button opens form', async ({ page }) => {
    await page.click('#tab-btn-settings');

    // Form should be hidden initially
    await expect(page.locator('#npm-instance-form')).toHaveClass(/d-none/);

    // Click Add
    await page.click('#btn-npm-add');

    // Form should be visible
    await expect(page.locator('#npm-instance-form')).not.toHaveClass(/d-none/);
    await expect(page.locator('#npm-form-title')).toHaveText('Add Instance');
  });

  test('Cancel button hides form', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await page.click('#btn-npm-add');
    await expect(page.locator('#npm-instance-form')).not.toHaveClass(/d-none/);

    await page.click('#btn-npm-cancel');
    await expect(page.locator('#npm-instance-form')).toHaveClass(/d-none/);
  });

  test('saves a new NPM instance and hides the form', async ({ page }) => {
    await page.click('#tab-btn-settings');

    // Open form
    await page.click('#btn-npm-add');
    await expect(page.locator('#npm-instance-form')).not.toHaveClass(/d-none/);

    // Fill form
    await page.fill('#ni-name', 'e2e-test-npm');
    await page.fill('#ni-url', 'https://npm-test:81');
    await page.fill('#ni-user', 'admin');
    await page.fill('#ni-pass', 'secret');
    await page.check('#ni-enabled');

    // Listen for the API call
    const postPromise = page.waitForResponse(
      (resp) => resp.url().includes('/api/npm-instances') && resp.request().method() === 'POST'
    );

    await page.click('#btn-npm-save');

    const response = await postPromise;
    expect(response.status()).toBe(200);

    // Form should hide after save
    await expect(page.locator('#npm-instance-form')).toHaveClass(/d-none/);

    // The list should still show loaded instances (from mock)
    await expect(page.locator('#npm-instances-list')).toContainText('npm-edge');
  });

  test('loads NPM instances list on settings tab open', async ({ page }) => {
    // The MOCK_NPM_INSTANCES (2 instances) should load when settings tab opens
    await page.click('#tab-btn-settings');

    // Wait for instances to render
    await page.waitForFunction(() => {
      const list = document.getElementById('npm-instances-list');
      return list && list.textContent && list.textContent.includes('npm-edge');
    }, { timeout: 10000 });

    await expect(page.locator('#npm-instances-list')).toContainText('npm-edge');
    await expect(page.locator('#npm-instances-list')).toContainText('npm-internal');
    // Should not show "Loading instances..." anymore
    await expect(page.locator('#npm-instances-list')).not.toContainText('Loading instances');
  });

  test('shows edit form when Edit button is clicked', async ({ page }) => {
    await page.click('#tab-btn-settings');

    // Wait for instances to render
    await page.waitForFunction(() => {
      const list = document.getElementById('npm-instances-list');
      return list && list.textContent && list.textContent.includes('npm-edge');
    }, { timeout: 10000 });

    // Click the first Edit button
    const editBtn = page.locator('#npm-instances-list button:has-text("Edit")').first();
    await editBtn.click();

    // Form should show with populated values
    await expect(page.locator('#npm-instance-form')).not.toHaveClass(/d-none/);
    await expect(page.locator('#npm-form-title')).toContainText('Edit');
  });

  test('delete instance removes it from list', async ({ page }) => {
    await page.click('#tab-btn-settings');

    // Wait for instances to render
    await page.waitForFunction(() => {
      const list = document.getElementById('npm-instances-list');
      return list && list.textContent && list.textContent.includes('npm-edge');
    }, { timeout: 10000 });

    // Setup mock for delete response
    await page.route('**/api/npm-instances/*', async (route) => {
      if (route.request().method() === 'DELETE') {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'deleted' }) });
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
      }
    });

    // Click first Delete button
    page.on('dialog', dialog => dialog.accept());
    const deleteBtn = page.locator('#npm-instances-list button:has-text("Delete")').first();
    await deleteBtn.click();
  });

  test('settings form saves compose path correctly', async ({ page }) => {
    await page.click('#tab-btn-settings');

    // Wait for settings to populate
    await page.waitForFunction(() => {
      const el = document.getElementById('s-compose-path') as HTMLInputElement;
      return el && el.value !== '';
    }, { timeout: 10000 });

    // Modify compose path
    await page.fill('#s-compose-path', '/custom/path.yml');

    // Listen for the POST
    const savePromise = page.waitForResponse(
      (resp) => resp.url().includes('/api/settings') && resp.request().method() === 'POST'
    );

    // Click Save
    await page.click('#settings-form button[type="submit"]');

    const response = await savePromise;
    const body = JSON.parse(response.request().postData() || '{}');
    expect(body.compose_path).toBe('/custom/path.yml');
  });

  test('saved indicator appears after settings save', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await page.waitForFunction(() => {
      const el = document.getElementById('s-compose-path') as HTMLInputElement;
      return el && el.value !== '';
    }, { timeout: 10000 });

    await page.click('#settings-form button[type="submit"]');

    // Check for success toast
    await expect(page.locator('.toast-msg.toast-success')).toContainText('Settings saved');
  });
});

test.describe('Settings — Kuma instances', () => {
  test.beforeEach(async ({ page }) => {
    await setupBaseMocks(page);
    await page.goto('/');
    await page.waitForFunction(() => {
      const el = document.getElementById('stat-docker');
      return el && el.textContent !== '' && !el.querySelector('.skeleton');
    }, { timeout: 10000 });
  });

  test('kuma instances section is visible with add button', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await expect(page.locator('text=Uptime Kuma Instances')).toBeVisible();
    await expect(page.locator('#btn-kuma-add')).toBeVisible();
  });

  test('add kuma instance button opens form', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await expect(page.locator('#kuma-instance-form')).toHaveClass(/d-none/);
    await page.click('#btn-kuma-add');
    await expect(page.locator('#kuma-instance-form')).not.toHaveClass(/d-none/);
    await expect(page.locator('#kuma-form-title')).toHaveText('Add Instance');
  });

  test('cancel button hides kuma instance form', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await page.click('#btn-kuma-add');
    await expect(page.locator('#kuma-instance-form')).not.toHaveClass(/d-none/);
    await page.click('#btn-kuma-cancel');
    await expect(page.locator('#kuma-instance-form')).toHaveClass(/d-none/);
  });

  test('saves a new kuma instance and hides the form', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await page.click('#btn-kuma-add');
    await page.fill('#ki-name', 'e2e-kuma');
    await page.fill('#ki-url', 'http://kuma-test:3001');
    await page.fill('#ki-user', 'admin');
    await page.fill('#ki-pass', 'secret');
    await page.check('#ki-enabled');

    const postPromise = page.waitForResponse(
      (resp) => resp.url().includes('/api/kuma-instances') && resp.request().method() === 'POST'
    );

    await page.click('#btn-kuma-save');

    const response = await postPromise;
    expect(response.status()).toBe(200);
    await expect(page.locator('#kuma-instance-form')).toHaveClass(/d-none/);
  });

  test('loads kuma instances list on settings tab open', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await page.waitForFunction(() => {
      const list = document.getElementById('kuma-instances-list');
      return list && list.textContent && list.textContent.includes('prod-kuma');
    }, { timeout: 10000 });

    await expect(page.locator('#kuma-instances-list')).toContainText('prod-kuma');
    await expect(page.locator('#kuma-instances-list')).toContainText('http://kuma:3001');
    await expect(page.locator('#kuma-instances-list')).not.toContainText('Loading instances');
  });

  test('edit button opens kuma form with populated values', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await page.waitForFunction(() => {
      const list = document.getElementById('kuma-instances-list');
      return list && list.textContent && list.textContent.includes('prod-kuma');
    }, { timeout: 10000 });

    await page.click('#kuma-instances-list button:has-text("Edit")');

    await expect(page.locator('#kuma-instance-form')).not.toHaveClass(/d-none/);
    await expect(page.locator('#kuma-form-title')).toContainText('Edit');
  });

  test('delete kuma instance shows confirm and calls API', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await page.waitForFunction(() => {
      const list = document.getElementById('kuma-instances-list');
      return list && list.textContent && list.textContent.includes('prod-kuma');
    }, { timeout: 10000 });

    const deletePromise = page.waitForResponse(
      (resp) => resp.url().includes('/api/kuma-instances/1') && resp.request().method() === 'DELETE'
    );

    page.on('dialog', (dialog) => dialog.accept());
    await page.click('#kuma-instances-list button:has-text("Delete")');

    const response = await deletePromise;
    expect(response.status()).toBe(200);
  });

  test('test kuma instance shows toast result', async ({ page }) => {
    await page.click('#tab-btn-settings');

    await page.waitForFunction(() => {
      const list = document.getElementById('kuma-instances-list');
      return list && list.textContent && list.textContent.includes('prod-kuma');
    }, { timeout: 10000 });

    await page.click('#kuma-instances-list button:has-text("Test")');
    await expect(page.locator('.toast-msg.toast-success')).toContainText('Connection OK');
  });
});
