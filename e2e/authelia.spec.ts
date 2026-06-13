import { test, expect } from '@playwright/test';
import { setupBaseMocks, MOCK_SETTINGS } from './helpers';

const MOCK_AUTHELIA_STATUS_CONFIGURED = {
  configured: true,
  domains: ['example.com', 'api.example.com', 'admin.example.com', '*.dev.example.com'],
  npm_cnames: ['example.com', 'api.example.com', 'admin.example.com', 'blog.example.com', 'cdn.example.com', 'dev.example.com', 'staging.example.com', 'docs.example.com'],
  matched: ['example.com', 'api.example.com', 'admin.example.com', 'dev.example.com'],
  missing: ['blog.example.com', 'cdn.example.com', 'staging.example.com', 'docs.example.com'],
  open_alerts: 2,
  sync_enabled: true,
  default_policy: 'one_factor',
  npm_error: false,
  npm_error_msg: '',
};

const MOCK_AUTHELIA_STATUS_NOT_CONFIGURED = {
  configured: false,
  message: 'Authelia config path not set',
};

const MOCK_ALERTS = [
  { id: 1, cname: 'blog.example.com', message: 'Missing access rule in Authelia config', severity: 'warning', status: 'open', created_at: '2025-01-15T10:00:00Z' },
  { id: 2, cname: 'cdn.example.com', message: 'No rule found for this domain', severity: 'warning', status: 'open', created_at: '2025-01-15T10:00:00Z' },
  { id: 3, cname: 'legacy.example.com', message: 'Auto-resolved: rule added', severity: 'info', status: 'resolved', created_at: '2025-01-14T08:00:00Z' },
];

const MOCK_TEMP_ACCESS = [
  { id: 1, ip: '192.168.1.100', reason: 'Developer debugging', expires_at: new Date(Date.now() + 86400000).toISOString(), created_at: new Date().toISOString(), status: 'active' },
  { id: 2, ip: '10.0.0.50', reason: 'Emergency access', expires_at: new Date(Date.now() - 3600000).toISOString(), created_at: new Date(Date.now() - 86400000).toISOString(), status: 'expired' },
  { id: 3, ip: '203.0.113.42', reason: 'Vendor access', expires_at: new Date(Date.now() + 604800000).toISOString(), created_at: new Date().toISOString(), status: 'active' },
];

const MOCK_SYNC_RESULT = {
  dry_run: false,
  added: 2,
  skipped: 1,
  alerted: 1,
  actions: [
    { action: 'add', cname: 'blog.example.com', policy: 'one_factor', message: 'Added access rule for blog.example.com' },
    { action: 'add', cname: 'cdn.example.com', policy: 'one_factor', message: 'Added access rule for cdn.example.com' },
    { action: 'skip', cname: 'staging.example.com', policy: 'one_factor', message: 'Already covered by wildcard rule' },
    { action: 'alert', cname: 'docs.example.com', message: 'Auto-sync disabled; manual action required' },
  ],
};

// Set up API mocking before each test
async function setupAutheliaMocks(page: Page, configured = true) {
  await setupBaseMocks(page);

  // Override authelia-specific endpoints
  await page.route('**/api/authelia/status', async (route) => {
    const data = configured ? MOCK_AUTHELIA_STATUS_CONFIGURED : MOCK_AUTHELIA_STATUS_NOT_CONFIGURED;
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(data) });
  });

  await page.route('**/api/authelia/alerts', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_ALERTS) });
  });

  await page.route('**/api/authelia/alerts/*/resolve', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'resolved' }) });
  });

  await page.route('**/api/authelia/temp-access', async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'created' }) });
    } else {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_TEMP_ACCESS) });
    }
  });

  await page.route('**/api/authelia/temp-access/*/revoke', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'revoked' }) });
  });

  await page.route('**/api/authelia/sync', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_SYNC_RESULT) });
  });
}

test.describe('Authelia UI — configured', () => {
  test.beforeEach(async ({ page }) => {
    await setupAutheliaMocks(page, true);
    await page.goto('/');
    // Wait for initial status load to complete
    await page.waitForSelector('#stat-authelia .badge', { timeout: 10000 });
  });

  test('shows authelia stat card with coverage percentage', async ({ page }) => {
    const statEl = page.locator('#stat-authelia .badge');
    await expect(statEl).toBeVisible();
    // 8 NPM CNAMEs, 4 matched = 50%
    await expect(statEl).toContainText('4/8 (50%)');
    // Should have warning color (not 100%, not 0%)
    await expect(statEl).toHaveClass(/bg-warning/);
  });

  test('authelia tab opens and displays coverage cards', async ({ page }) => {
    // Click the Authelia tab
    await page.click('button[data-bs-target="#tab-authelia"]');
    // Wait for dashboard to load
    await page.waitForSelector('#auth-domain-count:not(:empty)', { timeout: 10000 });

    // Coverage cards should show data
    await expect(page.locator('#auth-domain-count')).toHaveText('4');
    await expect(page.locator('#auth-coverage')).toHaveText('4/8');
    await expect(page.locator('#auth-open-alerts')).toHaveText('2');
  });

  test('CNAME coverage table shows covered and missing badges', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-authelia"]');
    await page.waitForSelector('#auth-coverage-tbody tr td', { timeout: 10000 });

    const rows = page.locator('#auth-coverage-tbody tr');
    const rowCount = await rows.count();
    expect(rowCount).toBe(8);

    // First row: example.com → Covered
    const firstRow = rows.nth(0);
    await expect(firstRow).toContainText('example.com');
    await expect(firstRow.locator('.badge.bg-success')).toContainText('Covered');

    // A missing domain: blog.example.com → Missing
    const missingRow = rows.nth(3);
    await expect(missingRow).toContainText('blog.example.com');
    await expect(missingRow.locator('.badge.bg-danger')).toContainText('Missing');
  });

  test('dry run sync shows results panel', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-authelia"]');
    await page.waitForSelector('#btn-auth-dryrun', { timeout: 10000 });

    // Click Dry Run
    await page.click('#btn-auth-dryrun');

    // Wait for the result panel to appear
    const resultDiv = page.locator('#auth-sync-result');
    await expect(resultDiv).not.toHaveClass(/d-none/);
    // Should show dry run results with action counts
    await expect(resultDiv).toContainText('Dry Run Results');
    await expect(resultDiv).toContainText('Added: 2');
    await expect(resultDiv).toContainText('Skipped: 1');
    await expect(resultDiv).toContainText('Alerted: 1');
  });

  test('sync button triggers modal and shows results', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-authelia"]');
    await page.waitForSelector('#btn-auth-sync', { timeout: 10000 });

    // Click sync button — modal should appear
    await page.click('#btn-auth-sync');
    await page.waitForSelector('#confirm-modal.show', { timeout: 5000 });
    // Confirm the modal
    await page.click('#confirm-modal-ok');
    await page.waitForSelector('#confirm-modal', { state: 'hidden', timeout: 5000 });

    // Wait for result panel
    const resultDiv = page.locator('#auth-sync-result');
    await expect(resultDiv).not.toHaveClass(/d-none/);
    await expect(resultDiv).toContainText('Sync Results');
    await expect(resultDiv).toContainText('Added: 2');
  });

  test('alerts table renders with resolve buttons', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-authelia"]');
    await page.waitForSelector('#auth-alerts-tbody', { timeout: 10000 });

    // Should have open alert rows
    const resolveButtons = page.locator('#auth-alerts-tbody button');
    // 2 open alerts should have resolve buttons
    await expect(resolveButtons).toHaveCount(2);

    // Click resolve on first alert
    await resolveButtons.first().click();

    // Wait for toast notification
    await expect(page.locator('.toast-msg.toast-success')).toContainText('Alert resolved');
  });

  test('temp access table shows active and expired rules', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-authelia"]');
    await page.waitForSelector('#auth-temp-tbody', { timeout: 10000 });

    const rows = page.locator('#auth-temp-tbody tr');
    await expect(rows).toHaveCount(3);

    // First row: active rule with revoke button
    await expect(rows.nth(0)).toContainText('192.168.1.100');
    await expect(rows.nth(0).locator('.badge.bg-success')).toContainText('active');
    await expect(rows.nth(0).locator('button')).toContainText('Revoke');

    // Second row: expired — no revoke button
    await expect(rows.nth(1)).toContainText('10.0.0.50');
    await expect(rows.nth(1).locator('.badge.bg-secondary')).toContainText('expired');
  });

  test('add temp access rule creates and clears form', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-authelia"]');
    await page.waitForSelector('#btn-add-temp-access', { timeout: 10000 });

    // Open the collapse form
    await page.click('#btn-add-temp-access');
    await page.waitForSelector('#temp-access-form.show', { timeout: 5000 });

    // Fill out the form
    await page.fill('#ta-ip', '10.10.10.10');
    await page.fill('#ta-reason', 'Test access');
    await page.fill('#ta-duration', '2h');

    // Submit
    await page.click('#btn-ta-submit');

    // Verify form fields are cleared
    await expect(page.locator('#ta-ip')).toHaveValue('');
    await expect(page.locator('#ta-reason')).toHaveValue('');
    await expect(page.locator('#ta-duration')).toHaveValue('');

    // Toast should appear
    await expect(page.locator('.toast-msg.toast-success')).toContainText('Temp access rule added');
  });

  test('revoke temp access shows toast', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-authelia"]');
    await page.waitForSelector('#auth-temp-tbody', { timeout: 10000 });

    // Click revoke on first active rule
    const revokeBtn = page.locator('#auth-temp-tbody tr').first().locator('button');
    await revokeBtn.click();

    await expect(page.locator('.toast-msg.toast-success')).toContainText('Access rule revoked');
  });
});

test.describe('Authelia UI — not configured', () => {
  test.beforeEach(async ({ page }) => {
    await setupAutheliaMocks(page, false);
    await page.goto('/');
    await page.waitForSelector('#stat-authelia .badge', { timeout: 10000 });
  });

  test('shows not configured badge', async ({ page }) => {
    const statEl = page.locator('#stat-authelia .badge');
    await expect(statEl).toContainText('Not configured');
    await expect(statEl).toHaveClass(/bg-secondary/);
  });

  test('authelia tab shows empty state', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-authelia"]');
    await page.waitForSelector('#auth-domain-count', { timeout: 10000 });

    await expect(page.locator('#auth-domain-count')).toHaveText('—');
    await expect(page.locator('#auth-coverage')).toHaveText('—');
    await expect(page.locator('#auth-open-alerts')).toHaveText('—');
    // Coverage table should show not configured message
    await expect(page.locator('#auth-coverage-tbody')).toContainText('Authelia not configured');
  });
});

test.describe('Settings — Authelia fields', () => {
  test.beforeEach(async ({ page }) => {
    await setupAutheliaMocks(page, true);
    await page.goto('/');
    await page.waitForSelector('#stat-authelia .badge', { timeout: 10000 });
  });

  test('settings tab loads authelia fields with correct values', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-settings"]');
    await page.waitForSelector('#s-auth-config-path', { timeout: 10000 });

    await expect(page.locator('#s-auth-config-path')).toHaveValue('/config/configuration.yml');
    await expect(page.locator('#s-auth-db-path')).toHaveValue('/config/db.sqlite3');
    await expect(page.locator('#s-auth-sync-enabled')).toBeChecked();
    await expect(page.locator('#s-auth-default-policy')).toHaveValue('one_factor');
    await expect(page.locator('#s-auth-overrides')).toHaveValue('{"admin.example.com":"bypass"}');
  });

  test('settings save includes authelia fields', async ({ page }) => {
    await page.click('button[data-bs-target="#tab-settings"]');
    // Wait for loadSettings() async call to populate the field
    await page.waitForFunction(() => {
      const el = document.getElementById('s-auth-config-path') as HTMLInputElement;
      return el && el.value === '/config/configuration.yml';
    }, { timeout: 10000 });

    // Modify an authelia field
    await page.fill('#s-auth-config-path', '/custom/config.yml');

    // Set up listener for the POST request
    const savePromise = page.waitForResponse(
      (resp) => resp.url().includes('/api/settings') && resp.request().method() === 'POST'
    );

    // Click save
    await page.click('#settings-form button[type="submit"]');

    const response = await savePromise;
    const body = await response.request().postDataJSON();
    expect(body.authelia_config_path).toBe('/custom/config.yml');
    expect(body.authelia_sync_enabled).toBe(true);
    expect(body.authelia_default_policy).toBe('one_factor');
  });
});
