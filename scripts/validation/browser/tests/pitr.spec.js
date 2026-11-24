'use strict';

const { test, expect } = require('@playwright/test');
const {
  acceptConfirm,
  api,
  confirmDialog,
  dataRow,
  isPitrTerminal,
  loadContext,
  openFE,
  pollJob,
  redisCli,
  sleep,
  waitForDrawer,
} = require('./helpers');

test.describe.configure({ mode: 'serial' });

// A successful job polls the dashboard for up to 180s; leave room for the
// marker-window sleeps and page assertions around that poll.
test.setTimeout(240 * 1000);

const BEFORE_KEY = 'pitr:browser:before';
const MARKER_KEY = 'pitr:browser:marker';
const AFTER_KEY = 'pitr:browser:after';
const MISSING_SERVER = '127.0.0.1:1';

let ctx;
const createdJobIds = new Set();

function jobsPath() {
  return `/api/topom/pitr/jobs/${ctx.xauth}`;
}

function jobPath(id) {
  return `/api/topom/pitr/${ctx.xauth}/${encodeURIComponent(id)}`;
}

async function listJobs(request) {
  const result = await api(request, 'GET', jobsPath());
  expect(result.status, JSON.stringify(result.data)).toBe(200);
  return Array.isArray(result.data) ? result.data : [];
}

async function waitForMatchingJob(request, predicate, timeoutMs) {
  const jobs = await pollJob(
    (method, path) => api(request, method, path),
    jobsPath(),
    (data) => Array.isArray(data) && data.some((job) => predicate(job) && isPitrTerminal(job.state)),
    timeoutMs
  );
  return jobs.find(predicate);
}

async function cleanupJob(request, id) {
  const current = await api(request, 'GET', jobPath(id));
  if (current.status !== 200) {
    return;
  }
  if (!isPitrTerminal(current.data && current.data.state)) {
    await api(request, 'PUT', `/api/topom/pitr/cancel/${ctx.xauth}/${encodeURIComponent(id)}`);
    await pollJob(
      (method, path) => api(request, method, path),
      jobPath(id),
      (job) => isPitrTerminal(job && job.state),
      60 * 1000
    );
  }
  await api(request, 'PUT', `/api/topom/pitr/remove/${ctx.xauth}/${encodeURIComponent(id)}`);
}

function skipUnlessPitrAvailable() {
  test.skip(
    !ctx.pitrMaster,
    'CODIS_PITR_MASTER is unset and cluster.json has no AOF Group Master'
  );
  test.skip(
    ctx.cluster && ctx.cluster.features && ctx.cluster.features.pitr === false,
    'PITR is disabled in cluster.json features.pitr'
  );
}

async function selectMaster(editor, addr) {
  const options = await editor.locator('#pitr-master-options option').all();
  let label = '';
  for (const option of options) {
    const text = await option.innerText();
    if (text.includes(addr)) {
      label = text;
      break;
    }
  }
  expect(label, `master ${addr} should appear in #pitr-master-options`).not.toBe('');
  await editor.locator('#pitr-master-options').selectOption({ label });
  await expect(editor.locator('#pitr-server-addr')).toHaveValue(addr);
}

async function openCreateDrawer(page) {
  await page.getByRole('button', { name: '新建闪回任务' }).first().click();
  return waitForDrawer(page, '新建 PITR 数据闪回任务');
}

async function submitCreate(page, editor, expectedText) {
  await editor.getByRole('button', { name: '创建闪回任务' }).click();
  await expect(confirmDialog(page)).toContainText(expectedText);
  await acceptConfirm(page, expectedText);
  await expect(page.locator('.resource-drawer')).toHaveCount(0);
}

async function deleteJobFromList(page, id) {
  const row = dataRow(page, id);
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: '删除' }).click();
  await expect(confirmDialog(page)).toContainText('确认删除 PITR 任务');
  await acceptConfirm(page, '确认删除 PITR 任务');
  await expect(dataRow(page, id)).toHaveCount(0);
}

test.beforeAll(async () => {
  ctx = loadContext();
});

test.afterAll(async ({ request }) => {
  for (const id of Array.from(createdJobIds)) {
    try {
      await cleanupJob(request, id);
      createdJobIds.delete(id);
    } catch (err) {
      // Best-effort: a leftover job is still visible through the API and will
      // be reported by the next run instead of hiding the original failure.
      console.warn(`failed to clean PITR job ${id}: ${err.message}`);
    }
  }
  if (ctx && ctx.pitrMaster) {
    try {
      redisCli(ctx, ctx.pitrMaster, 'DEL', BEFORE_KEY, MARKER_KEY, AFTER_KEY);
    } catch (err) {
      console.warn(`failed to delete PITR test keys: ${err.message}`);
    }
  }
});

test('PITR: rejects truncate_ts = 0 on the client without creating a job', async ({ page }) => {
  skipUnlessPitrAvailable();
  const createRequests = [];
  page.on('request', (request) => {
    if (request.method() === 'PUT' && request.url().includes('/api/topom/pitr/create/')) {
      createRequests.push(request.url());
    }
  });

  await openFE(page, '/recovery');
  const editor = await openCreateDrawer(page);
  await selectMaster(editor, ctx.pitrMaster);
  await editor.locator('#pitr-truncate-ts').fill('0');
  await editor.getByRole('button', { name: '创建闪回任务' }).click();

  await expect(editor.locator('.form-group:has(#pitr-truncate-ts) .form-error'))
    .toContainText('大于 0');
  await expect(editor).toContainText('请修正表单中的错误后重试');
  await expect(confirmDialog(page)).toHaveCount(0);
  expect(createRequests, 'invalid truncate_ts must not reach the dashboard').toEqual([]);

  await editor.locator('.drawer-close').click();
  await expect(page.locator('.resource-drawer')).toHaveCount(0);
});

test('PITR: creates a job for a marker window, exposes 9 done steps and deletes it', async ({ page, request }) => {
  skipUnlessPitrAvailable();

  const baselineIds = new Set((await listJobs(request)).map((job) => job.id));

  redisCli(ctx, ctx.pitrMaster, 'DEL', BEFORE_KEY, MARKER_KEY, AFTER_KEY);
  redisCli(ctx, ctx.pitrMaster, 'SET', BEFORE_KEY, 'before');
  await sleep(1600);
  redisCli(ctx, ctx.pitrMaster, 'SET', MARKER_KEY, 'marker');
  const truncateTs = Math.floor(Date.now() / 1000);
  await sleep(1600);
  redisCli(ctx, ctx.pitrMaster, 'SET', AFTER_KEY, 'after');
  await sleep(1600);

  await openFE(page, '/recovery');
  const editor = await openCreateDrawer(page);
  await selectMaster(editor, ctx.pitrMaster);
  await editor.locator('#pitr-truncate-ts').fill(String(truncateTs));
  await submitCreate(page, editor, '确认 SHUTDOWN 并创建');

  const job = await waitForMatchingJob(
    request,
    (item) => !baselineIds.has(item.id) &&
            item.server_addr === ctx.pitrMaster &&
            Number(item.truncate_ts) === truncateTs,
    180 * 1000
  );
  expect(job, 'a new PITR job should be visible through the dashboard API').toBeTruthy();
  createdJobIds.add(job.id);

  expect(job.state).toBe('succeeded');
  expect(job.steps).toHaveLength(9);
  expect(job.steps.every((step) => step.status === 'done')).toBe(true);

  await expect.poll(
    () => redisCli(ctx, ctx.pitrMaster, 'GET', BEFORE_KEY),
    { timeout: 20 * 1000 }
  ).toBe('before');
  expect(redisCli(ctx, ctx.pitrMaster, 'GET', AFTER_KEY)).toBe('');

  const row = dataRow(page, job.id);
  await expect(row).toBeVisible({ timeout: 20 * 1000 });
  await expect(row).toContainText('succeeded');

  await row.getByRole('button', { name: '详情' }).click();
  const detail = await waitForDrawer(page, 'PITR 任务详情');
  await expect(detail).toContainText(job.id);
  const stepRows = detail.locator('tbody tr');
  await expect(stepRows).toHaveCount(9);
  const statuses = await stepRows.locator('td:nth-child(2)').allInnerTexts();
  expect(statuses.map((value) => value.trim())).toEqual(Array(9).fill('done'));
  await detail.locator('.drawer-close').click();
  await expect(page.locator('.resource-drawer')).toHaveCount(0);

  await deleteJobFromList(page, job.id);
  await expect.poll(async () => {
    const jobs = await listJobs(request);
    return jobs.some((item) => item.id === job.id);
  }, { timeout: 15 * 1000 }).toBe(false);
  createdJobIds.delete(job.id);

  const remaining = await listJobs(request);
  if (remaining.length === 0) {
    await expect(page.locator('.recovery-page')).toContainText('还没有数据闪回任务');
  }
});

test('PITR: a non-existent server fails with "doesn\'t exist" and can be deleted', async ({ page, request }) => {
  skipUnlessPitrAvailable();

  const baselineIds = new Set((await listJobs(request)).map((job) => job.id));

  await openFE(page, '/recovery');
  const editor = await openCreateDrawer(page);

  const manualInput = editor.locator('#pitr-server-addr');
  if (await manualInput.count() === 0) {
    test.skip(true, 'this FE build does not allow typing server_addr manually');
  }
  await manualInput.fill(MISSING_SERVER);
  await editor.locator('#pitr-truncate-ts').fill(String(Math.floor(Date.now() / 1000)));
  await submitCreate(page, editor, '确认 SHUTDOWN 并创建');

  const job = await waitForMatchingJob(
    request,
    (item) => !baselineIds.has(item.id) && item.server_addr === MISSING_SERVER,
    90 * 1000
  );
  expect(job, 'the dashboard should record the failed PITR attempt').toBeTruthy();
  createdJobIds.add(job.id);
  expect(job.state).toBe('failed');
  expect(job.error).toContain("doesn't exist");

  const row = dataRow(page, job.id);
  await expect(row).toBeVisible({ timeout: 20 * 1000 });
  await expect(row).toContainText("doesn't exist");

  await row.getByRole('button', { name: '详情' }).click();
  const detail = await waitForDrawer(page, 'PITR 任务详情');
  await expect(detail).toContainText("doesn't exist");
  await detail.locator('.drawer-close').click();
  await expect(page.locator('.resource-drawer')).toHaveCount(0);

  await deleteJobFromList(page, job.id);
  await expect.poll(async () => {
    const jobs = await listJobs(request);
    return jobs.some((item) => item.id === job.id);
  }, { timeout: 15 * 1000 }).toBe(false);
  createdJobIds.delete(job.id);
});
