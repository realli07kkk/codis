'use strict';

const { test, expect } = require('@playwright/test');
const {
  api,
  loadContext,
  openFE,
  waitForDrawer,
} = require('./helpers');

test.describe.configure({ mode: 'serial' });

let ctx;

function limitPath() {
  return `/api/topom/proxy/qps-limit/${ctx.xauth}`;
}

async function readLimit(request) {
  const result = await api(request, 'GET', limitPath());
  expect(result.status, JSON.stringify(result.data)).toBe(200);
  return result.data;
}

async function writeLimit(request, limit) {
  const result = await api(request, 'PUT', limitPath(), { limit });
  expect(result.status, JSON.stringify(result.data)).toBe(200);
  return result.data;
}

test.beforeAll(async ({ request }) => {
  ctx = loadContext();
  await writeLimit(request, 0);
});

test.afterAll(async ({ request }) => {
  // Always leave the cluster unlimited, even if an assertion failed mid-flight.
  await writeLimit(request, 0);
});

test('QPS limit: 0 -> 3 enables the limit and 3 -> 0 disables it', async ({ page, request }) => {
  await openFE(page, '/security');
  await page.locator('#security-tab-qps').click();

  const panel = page.locator('#security-panel-qps');
  await expect(panel).toBeVisible();
  await expect(panel).toContainText('Proxy QPS 上限');
  await expect(panel.locator('.metric-card').filter({ hasText: '当前 limit' }))
    .toContainText('0');

  await page.getByRole('button', { name: '编辑限流' }).click();
  const editor = await waitForDrawer(page, '编辑 Proxy 限流');
  await expect(editor.locator('#qps-limit-hint')).toContainText('0 表示关闭限流');
  await editor.locator('#qps-limit-input').fill('3');
  await expect(editor.locator('#qps-limit-error')).toHaveCount(0);
  await editor.getByRole('button', { name: '保存并同步' }).click();
  await expect(page.locator('.resource-drawer')).toHaveCount(0);

  await expect.poll(async () => readLimit(request), {
    timeout: 30 * 1000,
    message: 'dashboard should report limit=3, enabled=true, sync_status=ready',
  }).toMatchObject({ limit: 3, enabled: true, sync_status: 'ready' });

  await expect(panel.locator('.metric-card').filter({ hasText: '当前 limit' }))
    .toContainText('3');
  await expect(panel.locator('.metric-card').filter({ hasText: '限流状态' }))
    .toContainText('已启用');
  await expect(panel.locator('.metric-card').filter({ hasText: '同步状态' }))
    .toContainText('已同步');

  const enabled = await readLimit(request);
  expect(enabled.limit).toBe(3);
  expect(enabled.enabled).toBe(true);
  expect(enabled.sync_status).toBe('ready');

  await page.getByRole('button', { name: '编辑限流' }).click();
  const editorAgain = await waitForDrawer(page, '编辑 Proxy 限流');
  await editorAgain.locator('#qps-limit-input').fill('0');
  await editorAgain.getByRole('button', { name: '保存并同步' }).click();
  await expect(page.locator('.resource-drawer')).toHaveCount(0);

  await expect.poll(async () => readLimit(request), {
    timeout: 30 * 1000,
    message: 'dashboard should report limit=0, enabled=false',
  }).toMatchObject({ limit: 0, enabled: false });

  await expect(panel.locator('.metric-card').filter({ hasText: '当前 limit' }))
    .toContainText('0');
  await expect(panel.locator('.metric-card').filter({ hasText: '限流状态' }))
    .toContainText('未启用');

  const disabled = await readLimit(request);
  expect(disabled.limit).toBe(0);
  expect(disabled.enabled).toBe(false);
});
