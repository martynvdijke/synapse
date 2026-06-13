import { test, expect } from '@playwright/test';

test.describe('Login page', () => {
  test('login page renders with form fields', async ({ page }) => {
    await page.goto('/login');
    await expect(page.locator('#login-form')).toBeVisible();
    await expect(page.locator('#username')).toBeVisible();
    await expect(page.locator('#password')).toBeVisible();
    await expect(page.locator('#login-form button[type="submit"]')).toBeVisible();
  });

  test('successful login redirects to dashboard', async ({ page }) => {
    // Mock login API
    await page.route('**/api/login', async (route) => {
      const body = JSON.parse(route.request().postData() || '{}');
      if (body.username === 'admin' && body.password === 'correct') {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
      } else {
        await route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: 'Invalid credentials' }) });
      }
    });

    await page.goto('/login');
    await page.fill('#username', 'admin');
    await page.fill('#password', 'correct');
    await page.click('#login-form button[type="submit"]');

    // Should redirect to dashboard (/)
    await page.waitForURL('**/');
  });

  test('failed login shows error message', async ({ page }) => {
    await page.route('**/api/login', async (route) => {
      await route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: 'Invalid credentials' }) });
    });

    await page.goto('/login');
    await page.fill('#username', 'admin');
    await page.fill('#password', 'wrong');
    await page.click('#login-form button[type="submit"]');

    // Error div should appear with message
    await expect(page.locator('#error')).toBeVisible();
    await expect(page.locator('#error')).toContainText('Invalid credentials');
  });

  test('failed login shows error message from server', async ({ page }) => {
    await page.route('**/api/login', async (route) => {
      await route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: 'Account locked' }) });
    });

    await page.goto('/login');
    await page.fill('#username', 'admin');
    await page.fill('#password', 'wrong');
    await page.click('#login-form button[type="submit"]');

    await expect(page.locator('#error')).toBeVisible();
    await expect(page.locator('#error')).toContainText('Account locked');
  });

  test('login page title is correct', async ({ page }) => {
    await page.goto('/login');
    await expect(page).toHaveTitle(/Login/);
  });
});

test.describe('Setup page', () => {
  test('setup page renders with form', async ({ page }) => {
    await page.goto('/setup');
    await expect(page.locator('#setup-form')).toBeVisible();
    await expect(page.locator('#password')).toBeVisible();
    await expect(page.locator('#setup-form button[type="submit"]')).toBeVisible();
  });

  test('setup form submits successfully', async ({ page }) => {
    await page.route('**/api/login', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
    });

    await page.goto('/setup');
    await page.fill('#username', 'admin');
    await page.fill('#password', 'new-password');
    await page.fill('#confirm_password', 'new-password');
    await page.click('#setup-form button[type="submit"]');

    // Should redirect to dashboard
    await page.waitForURL('**/');
    await expect(page.locator('.navbar-brand')).toContainText('Synapse');
  });
});
