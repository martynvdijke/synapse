import { Page } from '@playwright/test';

export const MOCK_STATUS = { docker_count: 5, npm_count: 8, npm_error: false, monitor_count: 12, running: false, connection_health: { docker: { ok: true }, npm: { ok: true, instances: [{ id: 1, name: 'npm-edge', ok: true }, { id: 2, name: 'npm-internal', ok: true }] }, kuma: { ok: true, instances: [] } } };

export const MOCK_SERVICES = [
  { name: 'web-app', container_name: 'synapse-web-1', type: 'http', url: 'http://web.example.com', in_kuma: true },
  { name: 'api', container_name: 'synapse-api-1', type: 'http', url: 'http://api.example.com', in_kuma: false },
  { name: 'db', container_name: 'synapse-db-1', type: 'other', in_kuma: false },
];

export const MOCK_MONITORS = [
  { id: 1, name: 'Web App Monitor', type: 'http', url: 'http://web.example.com' },
  { id: 2, name: 'API Health', type: 'http', url: 'http://api.example.com' },
];

export const MOCK_NPM_INSTANCES = [
  { id: 1, name: 'npm-edge', url: 'https://npm1.test', username: 'admin', enabled: true, created_at: '2024-01-01T00:00:00Z' },
  { id: 2, name: 'npm-internal', url: 'https://npm2.test', username: 'admin', enabled: true, created_at: '2024-01-01T00:00:00Z' },
];

export const MOCK_PROXIES = [
  { cname: 'example.com', container: 'web-app', in_kuma: true, source_instance_name: 'npm-edge' },
  { cname: 'api.example.com', container: 'api', in_kuma: false, source_instance_name: 'npm-edge' },
];

export const MOCK_SETTINGS = {
  compose_path: '/opt/synapse/docker-compose.yml',
  npm_migrated: true,
  authelia_config_path: '/config/configuration.yml', authelia_db_path: '/config/db.sqlite3',
  authelia_sync_enabled: true, authelia_default_policy: 'one_factor', authelia_sync_overrides: '{"admin.example.com":"bypass"}',
};

export const MOCK_SYNC_HISTORY = [
  { id: 1, source: 'docker', status: 'completed', started_at: new Date().toISOString(), added: 3, skipped: 1, failed: 0, error_message: '' },
  { id: 2, source: 'npm', status: 'completed_with_errors', started_at: new Date(Date.now() - 86400000).toISOString(), added: 1, skipped: 0, failed: 2, error_message: 'Connection timeout' },
];

export async function setupBaseMocks(page: Page) {
  await page.route('**/api/status', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_STATUS) });
  });
  await page.route('**/api/services', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_SERVICES) });
  });
  await page.route('**/api/monitors', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_MONITORS) });
  });
  await page.route('**/api/npm-instances', async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ id: 3 }) });
    } else {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_NPM_INSTANCES) });
    }
  });
  await page.route('**/api/proxies', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_PROXIES) });
  });
  await page.route('**/api/sync/history', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_SYNC_HISTORY) });
  });
  await page.route('**/api/settings', async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'saved' }) });
    } else {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_SETTINGS) });
    }
  });
  await page.route('**/api/sync/progress', async (route) => {
    await route.fulfill({ status: 200, headers: { 'content-type': 'text/event-stream' }, body: 'data: {}\n\n' });
  });
  await page.route('**/api/logout', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
  });
  await page.route('**/api/login', async (route) => {
    if (route.request().method() === 'POST') {
      const body = JSON.parse(route.request().postData() || '{}');
      if (body.username === 'admin' && body.password === 'correct') {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
      } else {
        await route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: 'Invalid credentials' }) });
      }
    }
  });
  await page.route('**/api/authelia/status', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ configured: false, message: 'Not configured' }) });
  });
  await page.route('**/api/authelia/alerts', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
  });
  await page.route('**/api/authelia/temp-access', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
  });
  await page.route('**/api/authelia/sync', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ dry_run: true, added: 0, skipped: 0, actions: [] }) });
  });
  await page.route('**/api/monitors/*/stats', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ id: 1, status: 1, uptime_24h: 99.9, uptime_7d: 99.5, uptime_1y: 99.0, avg_ping: 45.2, last_msg: 'OK', cert_info: '' }) });
  });
}

export async function setupSyncMocks(page: Page) {
  await page.route('**/api/sync/docker', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'started' }) });
  });
  await page.route('**/api/sync/npm', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'started' }) });
  });
}

export async function setupTestConnectionMocks(page: Page) {
  await page.route('**/api/test/kuma', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true, message: 'Connection OK' }) });
  });
  await page.route('**/api/test/npm', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: false, message: 'Connection failed' }) });
  });
}
