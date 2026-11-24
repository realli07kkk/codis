'use strict';

const { test, expect } = require('@playwright/test');
const {
  acceptConfirm,
  api,
  dataRow,
  feApi,
  loadContext,
  openFE,
  waitForDrawer,
} = require('./helpers');

test.describe.configure({ mode: 'serial' });

const USER = 'browser-test';
const PASSWORD = 'Browser-Pass-1';
const RULES = '+@read ~*';

let ctx;
let baseline;

function usersOf(data) {
  return Array.isArray(data.users) ? data.users : [];
}

function userPayload(user) {
  const payload = {
    name: user.name,
    enabled: user.enabled === true,
    rules: Array.isArray(user.rules) ? user.rules.slice(0) : [],
  };
  if (user.db !== undefined && user.db !== null) {
    payload.db = user.db;
  }
  return payload;
}

async function readAcl(request) {
  const result = await feApi(request, `/api/topom/acl/${ctx.xauth}`);
  expect(result.status, JSON.stringify(result.data)).toBe(200);
  return result.data;
}

async function writeAcl(request, enabled, users) {
  const result = await api(request, 'PUT', `/api/topom/acl/${ctx.xauth}`, {
    enabled: enabled === true,
    users: users.map(userPayload),
  });
  expect(result.status, JSON.stringify(result.data)).toBe(200);
  return result.data;
}

async function removeTestUser(request) {
  const acl = await readAcl(request);
  const users = usersOf(acl);
  if (!users.some((user) => user.name === USER)) {
    return acl;
  }
  const remaining = users.filter((user) => user.name !== USER);
  return writeAcl(request, acl.enabled === true, remaining);
}

test.beforeAll(async ({ request }) => {
  ctx = loadContext();
  await removeTestUser(request);
  baseline = await readAcl(request);
});

test.afterAll(async ({ request }) => {
  // Best-effort restore: only our own test identity is removed, and the ACL
  // enabled flag is left exactly as it was for every other user.
  await removeTestUser(request);
});

test('ACL: creates browser-test user from the FE and verifies it through the dashboard API', async ({ page, request }) => {
  await openFE(page, '/security');
  await page.locator('#security-tab-acl').click();
  const aclPanel = page.locator('#security-panel-acl');
  await expect(aclPanel).toBeVisible();
  await expect(aclPanel).toContainText('ACL 用户');

  await page.getByRole('button', { name: '新增用户' }).first().click();
  const editor = await waitForDrawer(page, '新增 ACL 用户');
  await editor.locator('#acl-user-name').fill(USER);
  await editor.locator('#acl-user-password').fill(PASSWORD);
  await editor.locator('#acl-user-rules').fill(RULES);

  // Verify the optional DB binding section is reachable, then leave it empty.
  await editor.getByRole('button', { name: /更多配置/ }).click();
  await expect(editor.locator('#acl-user-db')).toBeVisible();
  await expect(editor.locator('#acl-user-db')).toHaveValue('');

  await editor.getByRole('button', { name: '保存用户' }).click();
  await expect(page.locator('.resource-drawer')).toHaveCount(0);
  await expect(dataRow(page, USER)).toBeVisible();
  await expect(dataRow(page, USER)).toContainText('+@read ~*');

  await expect.poll(async () => {
    const acl = await readAcl(request);
    return usersOf(acl).some((user) => user.name === USER);
  }, { timeout: 15 * 1000 }).toBe(true);

  const acl = await readAcl(request);
  const created = usersOf(acl).find((user) => user.name === USER);
  expect(created).toBeTruthy();
  expect(created.enabled).toBe(true);
  expect(created.password_count).toBe(1);
  expect(created.rules).toEqual(['+@read', '~*']);
  expect(acl.enabled).toBe(baseline.enabled);
});

test('ACL: deletes browser-test user and restores the original user list', async ({ page, request }) => {
  await openFE(page, '/security');
  await page.locator('#security-tab-acl').click();
  await expect(page.locator('#security-panel-acl')).toContainText('ACL 用户');

  const row = dataRow(page, USER);
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: '删除' }).click();
  await acceptConfirm(page, '删除 ACL 用户');

  await expect(dataRow(page, USER)).toHaveCount(0);
  await expect.poll(async () => {
    const acl = await readAcl(request);
    return usersOf(acl).some((user) => user.name === USER);
  }, { timeout: 15 * 1000 }).toBe(false);

  const acl = await readAcl(request);
  expect(acl.enabled).toBe(baseline.enabled);
  expect(usersOf(acl).map((user) => user.name).sort())
    .toEqual(usersOf(baseline).map((user) => user.name).sort());

  if (usersOf(baseline).length === 0) {
    await expect(page.locator('#security-panel-acl')).toContainText('还没有 ACL 用户');
  }
});
